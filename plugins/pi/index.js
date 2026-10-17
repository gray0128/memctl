/**
 * memctl pi extension.
 *
 * The extension only bridges pi lifecycle events to the memctl binary: it loads
 * the session context at session start, injects the cached text as a system
 * prompt section before each run, triggers a bounded background sync after a
 * settled run and asks memctl to finish when the session runtime is torn down.
 * Memory storage, locking, synchronization and conflict handling stay in
 * memctl. See docs/08-pi-plugin.md.
 */
import { spawn } from 'node:child_process'
import { statSync } from 'node:fs'
import { homedir } from 'node:os'
import { delimiter, join } from 'node:path'
import process from 'node:process'

const SECTION_NAME = 'memctl'
const START_TIMEOUT_MS = 20_000
const START_WAIT_MS = 8_000
const SYNC_TIMEOUT_MS = 15_000
const END_JOIN_MS = 1_500
const MAX_START_BYTES = 1024 * 1024
const MAX_STDERR_BYTES = 8 * 1024
const KILL_GRACE_MS = 1_000
const FORCE_FINISH_MS = 1_500

const HOME_BINARY = ['.agent-memory', 'bin', 'memctl']
const FALLBACK_BINARIES = ['/opt/homebrew/bin/memctl', '/usr/local/bin/memctl']

/**
 * Build the lifecycle bridge. Exported for unit tests; the plugin entry point
 * below wires it to a pi ExtensionAPI.
 */
export function createBridge(options = {}) {
  const {
    spawn: spawnImpl = spawn,
    env = process.env,
    home = homedir(),
    logger = console,
    startTimeoutMs = START_TIMEOUT_MS,
    startWaitMs = START_WAIT_MS,
    syncTimeoutMs = SYNC_TIMEOUT_MS,
    endJoinMs = END_JOIN_MS,
    maxStartBytes = MAX_START_BYTES,
  } = options

  const states = new Map()
  let missingLogged = false
  let fallbackLogged = false
  let disposed = false

  function warn(message, error) {
    const detail = error === undefined ? '' : `: ${formatError(error)}`
    try {
      logger.warn(`memctl-pi: ${message}${detail}`)
    } catch {
      // Logging must never break the host.
    }
  }

  function logMissingBinary() {
    if (missingLogged) return
    missingLogged = true
    warn('memctl binary not found; memory context and synchronization are disabled until it is installed')
  }

  function logFallback() {
    if (fallbackLogged) return
    fallbackLogged = true
    warn('system prompt sections are unavailable; falling back to replacing the system prompt')
  }

  function resolveBinary() {
    const override = env.MEMCTL_PI_BIN
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

  function sessionId(ctx) {
    try {
      const id = ctx?.sessionManager?.getSessionId?.()
      if (typeof id === 'string' && id !== '') return id
      warn('sessionManager.getSessionId() returned no stable session id; memory is not injected')
      return null
    } catch (error) {
      warn('sessionManager.getSessionId() failed', error)
      return null
    }
  }

  function sessionCwd(ctx) {
    try {
      const cwd = ctx?.sessionManager?.getCwd?.()
      if (typeof cwd === 'string' && cwd !== '') return cwd
    } catch {
      // Fall back to the pi process directory.
    }
    try {
      return ctx?.cwd
    } catch {
      return undefined
    }
  }

  function onSessionStart(event, ctx) {
    try {
      if (disposed) return
      const id = sessionId(ctx)
      if (id === null) return

      const previous = states.get(id)
      // Only an extension reload keeps the already-injected snapshot while the
      // new start runs. A new/resume/fork session must load its own context.
      const context = event?.reason === 'reload' ? previous?.context ?? null : null
      if (previous !== undefined) cleanupState(previous)

      const bin = resolveBinary()
      const state = {
        id,
        cwd: sessionCwd(ctx),
        bin,
        // null: not loaded yet; '': loaded and empty; string: context body.
        context,
        startChild: null,
        startPromise: null,
        startSettled: false,
        controller: new AbortController(),
        syncChild: null,
        syncRunning: false,
        syncPending: false,
        runCovered: false,
        disposed: false,
      }
      states.set(id, state)

      if (bin === null) {
        logMissingBinary()
        state.startSettled = true
        return
      }
      beginStart(state)
    } catch (error) {
      warn('session_start listener failed', error)
    }
  }

  function beginStart(state) {
    state.startPromise = execStart(state)
      .then((context) => {
        if (state.disposed || disposed) return
        state.context = context
        state.startSettled = true
      })
      .catch((error) => {
        if (state.disposed || disposed) return
        // Keep a previously injected reload snapshot; a failed first start
        // leaves the context unknown, so this run is skipped.
        state.startSettled = true
        warn(`session ${state.id} start failed`, error)
      })
  }

  function waitForStart(state) {
    // A reload keeps the previous snapshot usable while the new start runs;
    // waiting here would delay the first prompt for no new content.
    if (state.context !== null || state.startSettled || state.startPromise === null) return Promise.resolve()
    return new Promise((resolve) => {
      const timer = setTimeout(finish, startWaitMs)
      timer.unref?.()
      function finish() {
        clearTimeout(timer)
        resolve()
      }
      state.startPromise.then(finish, finish)
    })
  }

  async function onBeforeAgentStart(event, ctx) {
    try {
      const id = sessionId(ctx)
      if (id === null) return undefined
      const state = states.get(id)
      if (state === undefined || state.disposed || disposed) return undefined

      await waitForStart(state)
      if (state.disposed || disposed) return undefined
      const context = state.context
      if (context === null) return undefined

      const options = event?.systemPromptOptions
      const sections = options?.sections
      if (sections !== undefined && sections !== null && typeof sections === 'object') {
        if (options.forceSystemPrompt) return fallbackPrompt(event, context)
        if (context === '') delete sections[SECTION_NAME]
        else sections[SECTION_NAME] = context
        return undefined
      }
      return fallbackPrompt(event, context)
    } catch (error) {
      warn('before_agent_start listener failed', error)
      return undefined
    }
  }

  function fallbackPrompt(event, context) {
    logFallback()
    if (context === '') return undefined
    const base = typeof event?.systemPrompt === 'string' ? event.systemPrompt : ''
    return { systemPrompt: `${base}\n\n${context}` }
  }

  function stateFor(ctx) {
    const id = sessionId(ctx)
    if (id === null) return undefined
    const state = states.get(id)
    if (state === undefined || state.disposed || disposed) return undefined
    return state
  }

  function onAgentEnd(_event, ctx) {
    try {
      const state = stateFor(ctx)
      if (state === undefined) return
      // Older pi versions have no agent_settled. Only sync here when the run
      // is truly idle; auto retries, compaction and queued work continue and
      // settle later.
      if (ctx?.isIdle?.() !== true) return
      if (ctx?.hasPendingMessages?.() === true) return
      state.runCovered = true
      scheduleSync(state)
    } catch (error) {
      warn('agent_end listener failed', error)
    }
  }

  function onAgentSettled(_event, ctx) {
    try {
      const state = stateFor(ctx)
      if (state === undefined) return
      if (state.runCovered) {
        state.runCovered = false
        return
      }
      scheduleSync(state)
    } catch (error) {
      warn('agent_settled listener failed', error)
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
    runCommand(bin, ['sync', '--agent', 'pi', '--sid', state.id], {
      timeoutMs: syncTimeoutMs,
      cwd: state.cwd,
      capture: false,
      signal: state.controller.signal,
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

  async function onSessionShutdown(event, ctx) {
    try {
      const id = sessionId(ctx)
      if (id === null) return
      const state = states.get(id)
      if (state === undefined) return
      states.delete(id)
      cleanupState(state)

      // A reload keeps the same session alive. Ending it would add a redundant
      // final sync and release a session that is about to continue.
      if (event?.reason === 'reload') return
      if (state.bin === null) return
      await spawnEndProcess(state.bin, state.id, state.cwd, endJoinMs)
    } catch (error) {
      warn('session_shutdown listener failed', error)
    }
  }

  function cleanupState(state) {
    state.disposed = true
    try {
      state.controller.abort()
    } catch {
      // The signal may already be aborted.
    }
    killChild(state.startChild)
    state.startChild = null
    killChild(state.syncChild)
    state.syncChild = null
  }

  async function dispose() {
    if (disposed) return
    disposed = true
    const pending = []
    for (const state of states.values()) {
      cleanupState(state)
      if (state.bin !== null) pending.push(spawnEndProcess(state.bin, state.id, state.cwd, endJoinMs))
    }
    states.clear()
    await Promise.all(pending)
  }

  async function execStart(state) {
    const result = await runCommand(state.bin, ['start', '--agent', 'pi', '--sid', state.id, '--json'], {
      timeoutMs: startTimeoutMs,
      cwd: state.cwd,
      maxBytes: maxStartBytes,
      signal: state.controller.signal,
      onSpawn: (child) => {
        state.startChild = child
      },
    })
    state.startChild = null

    if (state.disposed) return ''
    if (result.error !== undefined) throw result.error
    if (result.timedOut) throw new Error(`memctl start timed out after ${startTimeoutMs} ms`)
    if (result.overflow) throw new Error(`memctl start output exceeded ${maxStartBytes} bytes`)

    const output = result.stdout.trim()
    if (output === '') {
      if (result.code !== 0) throw new Error(`memctl start exited with ${result.code ?? result.signal}: ${tail(result.stderr)}`)
      return ''
    }

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
    // A failed start may still carry a usable envelope; keep the context but
    // report the synchronization error.
    if (result.code !== 0) {
      warn(`session ${state.id} start exited with ${result.code ?? result.signal}: ${tail(result.stderr)}`)
    }
    return context
  }

  function runCommand(bin, args, { timeoutMs, cwd, capture = true, maxBytes = MAX_START_BYTES, signal, onSpawn } = {}) {
    return new Promise((resolve) => {
      if (signal?.aborted === true) {
        resolve({ code: null, signal: null, stdout: '', stderr: '', timedOut: false, overflow: false })
        return
      }
      let child
      const spawnOptions = {
        shell: false,
        windowsHide: true,
        stdio: ['ignore', capture ? 'pipe' : 'ignore', capture ? 'pipe' : 'ignore'],
      }
      if (typeof cwd === 'string' && cwd !== '') spawnOptions.cwd = cwd
      try {
        child = spawnImpl(bin, args, spawnOptions)
      } catch (error) {
        resolve({ error })
        return
      }
      if (typeof onSpawn === 'function') onSpawn(child)
      const abort = () => killChild(child)
      signal?.addEventListener?.('abort', abort, { once: true })

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
        signal?.removeEventListener?.('abort', abort)
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
      timeoutTimer.unref?.()

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
      child.on?.('close', (code, childSignal) => finish({ code, signal: childSignal }))
    })
  }

  function spawnEndProcess(bin, id, cwd, joinMs) {
    return new Promise((resolve) => {
      let child
      const spawnOptions = {
        shell: false,
        windowsHide: true,
        detached: true,
        stdio: 'ignore',
      }
      if (typeof cwd === 'string' && cwd !== '') spawnOptions.cwd = cwd
      try {
        child = spawnImpl(bin, ['end', '--agent', 'pi', '--sid', id], spawnOptions)
      } catch (error) {
        warn(`session ${id} end failed`, error)
        resolve()
        return
      }
      let settled = false
      let timer
      const finish = () => {
        if (settled) return
        settled = true
        clearTimeout(timer)
        resolve()
      }
      timer = setTimeout(() => {
        // The detached process keeps its own life; only stop waiting for it.
        child?.unref?.()
        finish()
      }, joinMs)
      timer.unref?.()
      child?.on?.('error', (error) => {
        warn(`session ${id} end failed`, error)
        finish()
      })
      child?.on?.('exit', finish)
      child?.unref?.()
    })
  }

  return {
    onSessionStart,
    onBeforeAgentStart,
    onAgentEnd,
    onAgentSettled,
    onSessionShutdown,
    dispose,
    resolveBinary,
    warn,
    states,
  }
}

/**
 * pi extension entry point. It only registers listeners: processes and timers
 * start from session_start, never from the factory.
 */
export default function memctlPi(pi) {
  const bridge = createBridge()
  const listeners = [
    ['session_start', bridge.onSessionStart],
    ['before_agent_start', bridge.onBeforeAgentStart],
    ['agent_settled', bridge.onAgentSettled],
    ['agent_end', bridge.onAgentEnd],
    ['session_shutdown', bridge.onSessionShutdown],
  ]
  for (const [event, handler] of listeners) {
    try {
      pi.on(event, handler)
    } catch (error) {
      bridge.warn(`pi does not support the ${event} event; that lifecycle step is disabled`, error)
    }
  }
  return bridge
}

function isFile(path) {
  try {
    return statSync(path).isFile()
  } catch {
    return false
  }
}

function binaryNames() {
  return process.platform === 'win32' ? ['memctl.exe', 'memctl.cmd', 'memctl'] : ['memctl']
}

function killChild(child, signal = 'SIGTERM') {
  if (child === undefined || child === null) return
  try {
    child.kill?.(signal)
  } catch {
    // The process may already have exited.
  }
}

function tail(text) {
  if (typeof text !== 'string') return ''
  return text.trim().slice(-MAX_STDERR_BYTES)
}

function formatError(error) {
  if (error instanceof Error) return error.message
  return String(error)
}
