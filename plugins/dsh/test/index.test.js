import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

import { createBridge } from '../index.js'

const silentLogger = { warn() {} }

function scriptedSpawn(behaviors) {
  const calls = []
  const spawn = (bin, args, opts) => {
    const call = { bin, args, opts, child: null }
    calls.push(call)
    const child = new EventEmitter()
    child.stdout = new EventEmitter()
    child.stderr = new EventEmitter()
    child.killed = []
    child.kill = (signal = 'SIGTERM') => {
      child.killed.push(signal)
      if (child.autoCloseOnKill === true) queueMicrotask(() => child.emit('close', null, signal))
    }
    child.unref = () => {
      child.unrefCalled = true
    }
    call.child = child
    const behavior = behaviors.shift() ?? {}
    queueMicrotask(() => {
      if (behavior.error !== undefined) {
        child.emit('error', behavior.error)
        return
      }
      if (behavior.stdout !== undefined) child.stdout.emit('data', Buffer.from(behavior.stdout))
      if (behavior.stderr !== undefined) child.stderr.emit('data', Buffer.from(behavior.stderr))
      if (behavior.hang === true) return
      child.emit('close', behavior.code ?? 0, null)
    })
    return child
  }
  return { spawn, calls }
}

function startPayload(text) {
  return JSON.stringify({ hookSpecificOutput: { additionalContext: text } })
}

function fakeAgent(id, { parentSession } = {}) {
  const agent = {
    id,
    session: { header: { cwd: '/tmp', parentSession } },
    contexts: [],
    listeners: {},
    disposedListeners: 0,
    ctx: {
      systemPrompt: {
        context(entry) {
          agent.contexts.push(entry)
          return () => {}
        },
      },
      on(event, listener) {
        agent.listeners[event] = listener
        return () => {
          agent.disposedListeners += 1
        }
      },
    },
  }
  return agent
}

function bridgeWith(spawn, overrides = {}) {
  return createBridge({
    spawn,
    env: { MEMCTL_DSH_BIN: '/fake/memctl' },
    home: '/nonexistent-home',
    logger: silentLogger,
    ...overrides,
  })
}

const tick = () => new Promise((resolve) => setImmediate(resolve))

test('start uses the fixed argument array and registers a scoped context', async () => {
  const { spawn, calls } = scriptedSpawn([{ stdout: startPayload('[agent-memory]\nhello') }])
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('session-1')

  await bridge.onCreated(agent, 'startup')

  assert.equal(calls.length, 1)
  assert.equal(calls[0].bin, '/fake/memctl')
  assert.deepEqual(calls[0].args, ['start', '--agent', 'dsh', '--sid', 'session-1', '--json'])
  assert.equal(calls[0].opts.shell, false)
  assert.equal(agent.contexts.length, 1)
  assert.deepEqual(
    {
      name: agent.contexts[0].name,
      order: agent.contexts[0].order,
      interpolate: agent.contexts[0].interpolate,
      required: agent.contexts[0].required,
    },
    { name: 'memctl:memory', order: 9000, interpolate: false, required: true },
  )
  assert.equal(agent.contexts[0].text(), '[agent-memory]\nhello')
  assert.equal(typeof agent.listeners['session/event'], 'function')
})

test('subagents are skipped entirely', async () => {
  const { spawn, calls } = scriptedSpawn([])
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('child-1', { parentSession: 'parent-1' })

  await bridge.onCreated(agent, 'startup')

  assert.equal(calls.length, 0)
  assert.equal(agent.contexts.length, 0)
})

test('start failure logs and leaves the session usable', async () => {
  const warnings = []
  const { spawn, calls } = scriptedSpawn([{ stdout: '', stderr: 'NO_REPO', code: 1 }])
  const bridge = bridgeWith(spawn, { logger: { warn: (message) => warnings.push(message) } })
  const agent = fakeAgent('session-2')

  await bridge.onCreated(agent, 'startup')

  assert.equal(calls.length, 1)
  assert.equal(agent.contexts.length, 0)
  assert.equal(typeof agent.listeners['session/event'], 'function')
  assert.ok(warnings.some((message) => message.includes('start failed')))
})

test('empty start output means no memory context', async () => {
  const { spawn } = scriptedSpawn([{ stdout: '' }])
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('session-3')

  await bridge.onCreated(agent, 'startup')

  assert.equal(agent.contexts.length, 0)
  assert.equal(typeof agent.listeners['session/event'], 'function')
})

test('compact does not call start again', async () => {
  const { spawn, calls } = scriptedSpawn([])
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('session-4')

  await bridge.onCreated(agent, 'compact')

  assert.equal(calls.length, 0)
  assert.equal(agent.contexts.length, 0)
  assert.equal(typeof agent.listeners['session/event'], 'function')
})

test('oversized start output is rejected without parsing a truncated payload', async () => {
  const warnings = []
  const base = scriptedSpawn([{ stdout: 'x'.repeat(64), hang: true }])
  const spawn = (bin, args, opts) => {
    const child = base.spawn(bin, args, opts)
    child.autoCloseOnKill = true
    return child
  }
  const bridge = bridgeWith(spawn, {
    maxStartBytes: 16,
    startTimeoutMs: 1_000,
    logger: { warn: (message) => warnings.push(message) },
  })
  const agent = fakeAgent('session-5')

  await bridge.onCreated(agent, 'startup')

  assert.equal(agent.contexts.length, 0)
  assert.ok(warnings.some((message) => message.includes('exceeded')))
})

test('start timeout is contained and does not throw', async () => {
  const warnings = []
  const base = scriptedSpawn([{ hang: true }])
  const spawn = (bin, args, opts) => {
    const child = base.spawn(bin, args, opts)
    child.autoCloseOnKill = true
    return child
  }
  const bridge = bridgeWith(spawn, {
    startTimeoutMs: 5,
    logger: { warn: (message) => warnings.push(message) },
  })
  const agent = fakeAgent('session-timeout')

  await bridge.onCreated(agent, 'startup')

  assert.equal(agent.contexts.length, 0)
  assert.ok(warnings.some((message) => message.includes('timed out')))
})

test('invalid start JSON is contained', async () => {
  const warnings = []
  const { spawn } = scriptedSpawn([{ stdout: 'not json' }])
  const bridge = bridgeWith(spawn, { logger: { warn: (message) => warnings.push(message) } })
  const agent = fakeAgent('session-invalid')

  await bridge.onCreated(agent, 'startup')

  assert.equal(agent.contexts.length, 0)
  assert.ok(warnings.some((message) => message.includes('invalid JSON')))
})

test('turn/end keeps at most one running sync and replays one pending turn', async () => {
  const { spawn, calls } = scriptedSpawn([
    { stdout: startPayload('ctx') },
    { hang: true },
    { code: 0 },
  ])
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('session-6')
  await bridge.onCreated(agent, 'startup')

  bridge.onSessionEvent({ id: 'session-6' }, { type: 'turn/end' })
  bridge.onSessionEvent({ id: 'session-6' }, { type: 'turn/end' })
  bridge.onSessionEvent({ id: 'session-6' }, { type: 'turn/start' })
  await tick()

  const syncCalls = calls.filter((call) => call.args[0] === 'sync')
  assert.equal(syncCalls.length, 1)
  assert.deepEqual(syncCalls[0].args, ['sync', '--agent', 'dsh', '--sid', 'session-6'])

  calls.find((call) => call.args[0] === 'sync').child.emit('close', 0, null)
  await tick()
  await tick()

  assert.equal(calls.filter((call) => call.args[0] === 'sync').length, 2)
  assert.equal(agent.contexts.length, 1)
})

test('disposed triggers one detached end and clears state', async () => {
  const { spawn, calls } = scriptedSpawn([{ stdout: startPayload('ctx') }, {}])
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('session-7')
  await bridge.onCreated(agent, 'startup')

  bridge.onDisposed(agent)
  bridge.onDisposed(agent)

  const endCalls = calls.filter((call) => call.args[0] === 'end')
  assert.equal(endCalls.length, 1)
  assert.deepEqual(endCalls[0].args, ['end', '--agent', 'dsh', '--sid', 'session-7'])
  assert.equal(endCalls[0].opts.detached, true)
  assert.equal(endCalls[0].child.unrefCalled, true)
  assert.equal(agent.disposedListeners, 1)
})

test('dispose kills pending children and unregisters scoped handlers', async () => {
  const base = scriptedSpawn([{ stdout: startPayload('ctx') }, { hang: true }])
  const spawn = (bin, args, opts) => {
    const child = base.spawn(bin, args, opts)
    child.autoCloseOnKill = true
    return child
  }
  const bridge = bridgeWith(spawn)
  const agent = fakeAgent('session-8')
  await bridge.onCreated(agent, 'startup')

  bridge.onSessionEvent({ id: 'session-8' }, { type: 'turn/end' })
  await tick()
  const syncChild = base.calls.find((call) => call.args[0] === 'sync').child

  bridge.dispose()

  assert.ok(syncChild.killed.includes('SIGTERM'))
  assert.equal(agent.disposedListeners, 1)
})

test('binary resolution prefers MEMCTL_DSH_BIN, then the home install and PATH', () => {
  const home = mkdtempSync(join(tmpdir(), 'memctl-dsh-home-'))
  const homeBin = join(home, '.agent-memory', 'bin')
  mkdirSync(homeBin, { recursive: true })
  writeFileSync(join(homeBin, 'memctl'), '')

  assert.equal(createBridge({ env: { MEMCTL_DSH_BIN: '/custom/memctl' }, home }).resolveBinary(), '/custom/memctl')
  assert.equal(createBridge({ env: {}, home }).resolveBinary(), join(homeBin, 'memctl'))

  const pathDir = mkdtempSync(join(tmpdir(), 'memctl-dsh-path-'))
  writeFileSync(join(pathDir, 'memctl'), '')
  assert.equal(createBridge({ env: { PATH: pathDir }, home: '/nonexistent' }).resolveBinary(), join(pathDir, 'memctl'))
  assert.equal(createBridge({ env: { PATH: '' }, home: '/nonexistent' }).resolveBinary(), null)
})

test('missing binary is reported once and does not register listeners', async () => {
  const warnings = []
  const bridge = createBridge({
    env: { PATH: '' },
    home: '/nonexistent',
    logger: { warn: (message) => warnings.push(message) },
  })
  const first = fakeAgent('session-9')
  const second = fakeAgent('session-10')

  await bridge.onCreated(first, 'startup')
  await bridge.onCreated(second, 'startup')

  assert.equal(first.listeners['session/event'], undefined)
  assert.equal(second.listeners['session/event'], undefined)
  assert.equal(warnings.filter((message) => message.includes('binary not found')).length, 1)
})
