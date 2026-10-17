/**
 * Host half of the memctl DSH bundle.
 *
 * The plugin only bridges lifecycle events to the memctl binary: it reads the
 * session context at agent creation, registers it as a scoped dynamic prompt
 * context, triggers a bounded background sync after each committed turn and
 * asks memctl to finish when the agent is disposed. Memory storage, locking,
 * synchronization and conflict handling stay in memctl. See
 * docs/07-dsh-plugin.md.
 */
import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { homedir } from 'node:os'
import { delimiter, join } from 'node:path'
import process from 'node:process'

export const name = 'memctl-dsh'

// The loader mounts the plugin only when the prompt registry is available.
// Every listener still contains its own errors so a failed memctl call cannot
// fail agent creation.
export const inject = ['systemPrompt']

const CONTEXT_NAME = 'memctl:memory'
const CONTEXT_ORDER = 9000
const START_TIMEOUT_MS = 20_000
const SYNC_TIMEOUT_MS = 15_000
const END_TIMEOUT_MS = 15_000
const MAX_START_BYTES = 1024 * 1024
const MAX_STDERR_BYTES = 8 * 1024
const KILL_GRACE_MS = 1_000
const FORCE_FINISH_MS = 1_500

const HOME_BINARY = ['.agent-memory', 'bin', 'memctl']
const FALLBACK_BINARIES = ['/opt/homebrew/bin/memctl', '/usr/local/bin/memctl']

/**
 * Build the lifecycle bridge. Exported for unit tests; the plugin entry point
 * below wires it to a Cordis context.
 */
export function createBridge(options = {}) {
  const {
    spawn: spawnImpl = spawn,
    env = process.env,
    home = homedir(),
    logger = console,
    startTimeoutMs = START_TIMEOUT_MS,
    syncTimeoutMs = SYNC_TIMEOUT_MS,
    maxStartBytes = MAX_START_BYTES,
  } = options

  const states = new Map()
  let disposed = false
  let missingLogged = false

  function warn(message, error) {
    const detail = error === undefined ? '' : `: ${formatError(error)}`
    try {
      logger.warn(`memctl-dsh: ${message}${detail}`)
    } catch {
      // Logging must never break the host.
    }
  }

  function logMissingBinary() {
    if (missingLogged) return
    missingLogged = true
    warn('memctl binary not found; memory context and synchronization are disabled until it is installed')
  }

  function resolveBinary() {
    const override = env.MEMCTL_DSH_BIN
    if (typeof override === 'string' && override.trim() !== '') return override.trim()

    const candidates = [join(home, ...HOME_BINARY), ...FALLBACK_BINARIES]
    for (const candidate of candidates) {
      if (isFile(candidate)) return candidate
    }

    const searchPath = env.PATH ?? env.Path ?? ''
    for (const directory of searchPath.split(delimiter)) {
      if (directory === '') continue
      for (const binary of binaryNames()) {
        const candidate = join(directory, binary)
        if (isFile(candidate)) return candidate
      }
    }
    return null
  }

  async function onCreated(agent, source, signal) {
    try {
      if (disposed || agent === undefined || agent === null) return
      const id = agent.id
      if (typeof id !== 'string' || id === '') return
      if (isChildAgent(agent)) return

      const previous = states.get(id)
      if (previous !== undefined) cleanupState(previous)

      const bin = resolveBinary()
      const state = {
        id,
        agent,
        bin,
        context: '',
        contextDisposer: null,
        listenerDisposer: null,
        startChild: null,
        syncChild: null,
        syncRunning: false,
        syncPending: false,
        disposed: false,
      }
      states.set(id, state)

      if (bin === null) {
        logMissingBinary()
        return
      }

      // A compacted agent keeps its already-registered context snapshot. Do not
      // call start again; only the per-turn synchronization listener is new.
      if (source !== 'compact') {
        try {
          const context = await execStart(state, signal)
          if (state.disposed || disposed) return
          if (context !== '') {
            state.context = context
            registerContext(agent, state)
          }
        } catch (error) {
          warn(`session ${id} start failed`, error)
        }
      }
      if (state.disposed || disposed) return
      registerSyncListener(agent, state)
    } catch (error) {
      warn('agent/created listener failed', error)
    }
  }

  function registerContext(agent, state) {
    const service = agent?.ctx?.systemPrompt
    if (service === undefined || typeof service.context !== 'function') {
      warn('agent context does not expose systemPrompt.context; memory was not injected')
      return
    }
    try {
      state.contextDisposer = service.context({
        name: CONTEXT_NAME,
        order: CONTEXT_ORDER,
        text: () => state.context,
        interpolate: false,
        required: true,
      })
    } catch (error) {
      warn('failed to register the memory context', error)
    }
  }

  function registerSyncListener(agent, state) {
    const scoped = agent?.ctx
    if (scoped === undefined || typeof scoped.on !== 'function') {
      warn('agent context does not expose event listeners; turn synchronization is disabled')
      return
    }
    try {
      state.listenerDisposer = scoped.on('session/event', (session, event) => {
        if (session?.id !== state.id) return
        if (event?.type !== 'turn/end') return
        scheduleSync(state)
      })
    } catch (error) {
      warn('failed to register the turn synchronization listener', error)
    }
  }

  function scheduleSync(state) {
    if (state.disposed || disposed) return
    if (state.syncRunning) {
      state.syncPending = true
      return
    }
    const bin = state.bin ?? resolveBinary()
    if (bin === null) {
      logMissingBinary()
      return
    }
    state.bin = bin
    state.syncRunning = true
    runCommand(bin, ['sync', '--agent', 'dsh', '--sid', state.id], {
      timeoutMs: syncTimeoutMs,
      capture: false,
      onSpawn: (child) => {
        state.syncChild = child
      },
    })
      .then((result) => {
        if (result.error !== undefined) {
          warn(`session ${state.id} sync failed`, result.error)
        } else if (result.timedOut) {
          warn(`session ${state.id} sync timed out after ${syncTimeoutMs} ms`)
        } else if (result.code !== 0) {
          warn(`session ${state.id} sync exited with ${result.code ?? result.signal}: ${tail(result.stderr)}`)
        }
      })
      .catch((error) => warn(`session ${state.id} sync failed`, error))
      .finally(() => {
        state.syncChild = null
        state.syncRunning = false
        if (state.syncPending && !state.disposed && !disposed) {
          state.syncPending = false
          scheduleSync(state)
        }
      })
  }

  function onSessionEvent(session, event) {
    const state = states.get(session?.id)
    if (state === undefined) return
    if (event?.type !== 'turn/end') return
    scheduleSync(state)
  }

  function onDisposed(agent) {
    try {
      const id = agent?.id
      if (typeof id !== 'string') return
      const state = states.get(id)
      if (state === undefined || state.agent !== agent) return
      states.delete(id)
      cleanupState(state, { spawnEnd: true })
    } catch (error) {
      warn('agent/disposed listener failed', error)
    }
  }

  function cleanupState(state, { spawnEnd = false } = {}) {
    state.disposed = true
    if (state.contextDisposer !== null) {
      try {
        state.contextDisposer()
      } catch {
        // The agent scope may already be unwinding.
      }
      state.contextDisposer = null
    }
    if (state.listenerDisposer !== null) {
      try {
        state.listenerDisposer()
      } catch {
        // The agent scope may already be unwinding.
      }
      state.listenerDisposer = null
    }
    killChild(state.startChild)
    state.startChild = null
    killChild(state.syncChild)
    state.syncChild = null
    if (spawnEnd && state.bin !== null) spawnEndProcess(state.bin, state.id)
  }

  function dispose() {
    if (disposed) return
    disposed = true
    // Host teardown may unload the plugin before agent/disposed reaches this
    // bridge. End the sessions that are still live so a normal app exit keeps
    // the same final-sync guarantee as an explicit agent disposal. end is an
    // idempotent background sync, so a plugin reload adds no session state.
    for (const state of states.values()) cleanupState(state, { spawnEnd: true })
    states.clear()
  }

  async function execStart(state, signal) {
    const result = await runCommand(state.bin, ['start', '--agent', 'dsh', '--sid', state.id, '--json'], {
      timeoutMs: startTimeoutMs,
      maxBytes: maxStartBytes,
      onSpawn: (child) => {
        state.startChild = child
        if (signal !== undefined && typeof signal.addEventListener === 'function') {
          signal.addEventListener('abort', () => killChild(child), { once: true })
        }
      },
    })
    state.startChild = null

    if (signal?.aborted === true) return ''
    if (result.error !== undefined) throw result.error
    if (result.timedOut) throw new Error(`memctl start timed out after ${startTimeoutMs} ms`)
    if (result.overflow) throw new Error(`memctl start output exceeded ${maxStartBytes} bytes`)
    if (result.code !== 0) {
      throw new Error(`memctl start exited with ${result.code ?? result.signal}: ${tail(result.stderr)}`)
    }

    const output = result.stdout.trim()
    if (output === '') return ''
    let payload
    try {
      payload = JSON.parse(output)
    } catch (error) {
      throw new Error(`memctl start returned invalid JSON: ${formatError(error)}`)
    }
    const context = payload?.hookSpecificOutput?.additionalContext
    if (typeof context !== 'string') {
      throw new Error('memctl start returned no hookSpecificOutput.additionalContext')
    }
    return context
  }

  function runCommand(bin, args, { timeoutMs, capture = true, maxBytes = MAX_START_BYTES, onSpawn } = {}) {
    return new Promise((resolve) => {
      let child
      try {
        child = spawnImpl(bin, args, {
          shell: false,
          windowsHide: true,
          stdio: ['ignore', capture ? 'pipe' : 'ignore', capture ? 'pipe' : 'ignore'],
        })
      } catch (error) {
        resolve({ error })
        return
      }
      if (typeof onSpawn === 'function') onSpawn(child)

      let stdout = ''
      let stderr = ''
      let stdoutBytes = 0
      let overflow = false
      let timedOut = false
      let settled = false
      let timeoutTimer
      let killTimer
      let forceTimer

      const clearTimers = () => {
        clearTimeout(timeoutTimer)
        clearTimeout(killTimer)
        clearTimeout(forceTimer)
      }
      const finish = (result) => {
        if (settled) return
        settled = true
        clearTimers()
        resolve({ stdout, stderr, timedOut, overflow, ...result })
      }
      const terminate = () => {
        killChild(child)
        killTimer = setTimeout(() => killChild(child, 'SIGKILL'), KILL_GRACE_MS)
        forceTimer = setTimeout(
          () => finish({ code: null, signal: null }),
          KILL_GRACE_MS + FORCE_FINISH_MS,
        )
      }

      timeoutTimer = setTimeout(() => {
        timedOut = true
        terminate()
      }, timeoutMs)

      if (capture && child.stdout !== undefined && child.stdout !== null) {
        child.stdout.on('data', (chunk) => {
          if (overflow) return
          stdoutBytes += chunk.length
          if (stdoutBytes > maxBytes) {
            overflow = true
            stdout = ''
            terminate()
            return
          }
          stdout += chunk.toString('utf8')
        })
      }
      if (capture && child.stderr !== undefined && child.stderr !== null) {
        child.stderr.on('data', (chunk) => {
          stderr = (stderr + chunk.toString('utf8')).slice(-MAX_STDERR_BYTES)
        })
      }
      child.on?.('error', (error) => finish({ error, code: null, signal: null }))
      child.on?.('close', (code, signal) => finish({ code, signal }))
    })
  }

  function spawnEndProcess(bin, id) {
    try {
      const child = spawnImpl(bin, ['end', '--agent', 'dsh', '--sid', id], {
        shell: false,
        windowsHide: true,
        detached: true,
        stdio: 'ignore',
      })
      const timer = setTimeout(() => killChild(child, 'SIGKILL'), END_TIMEOUT_MS)
      timer.unref?.()
      child.on?.('error', (error) => warn(`session ${id} end failed`, error))
      child.on?.('exit', () => clearTimeout(timer))
      child.unref?.()
    } catch (error) {
      warn(`session ${id} end failed`, error)
    }
  }

  return { onCreated, onSessionEvent, onDisposed, dispose, resolveBinary }
}

class Bridge {
  constructor(bridge) {
    this.bridge = bridge
  }

  onCreated(agent, source, signal) {
    return this.bridge.onCreated(agent, source, signal)
  }

  onSessionEvent(session, event) {
    return this.bridge.onSessionEvent(session, event)
  }

  onDisposed(agent) {
    return this.bridge.onDisposed(agent)
  }

  dispose() {
    return this.bridge.dispose()
  }

  resolveBinary() {
    return this.bridge.resolveBinary()
  }
}

/** Host plugin entry point. */
export function apply(ctx) {
  const bridge = createBridge({ logger: ctx.logger ?? console })
  ctx.effect(() => () => bridge.dispose())
  ctx.on('agent/created', ({ agent, source, signal }) => bridge.onCreated(agent, source, signal))
  ctx.on('agent/disposed', ({ agent }) => bridge.onDisposed(agent))
}

function isChildAgent(agent) {
  const parent = agent?.session?.header?.parentSession
  return typeof parent === 'string' && parent !== ''
}

function binaryNames() {
  return process.platform === 'win32' ? ['memctl.exe', 'memctl.cmd'] : ['memctl']
}

function isFile(path) {
  try {
    return existsSync(path)
  } catch {
    return false
  }
}

function killChild(child, signal = 'SIGTERM') {
  try {
    child?.kill?.(signal)
  } catch {
    // A child that already exited needs no cleanup.
  }
}

function tail(text) {
  const value = typeof text === 'string' ? text.trim() : ''
  return value.length > 512 ? value.slice(-512) : value
}

function formatError(error) {
  if (error instanceof Error) return error.message
  return String(error)
}
