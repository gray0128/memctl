import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { test } from 'node:test'

import { createBridge } from '../index.js'

const silentLogger = { warn() {} }
const BIN = '/fake/bin/memctl'

function scriptedSpawn(behaviors, calls = []) {
  const spawn = (bin, args, opts) => {
    const call = { bin, args, opts, child: null }
    calls.push(call)
    const child = new EventEmitter()
    child.stdout = new EventEmitter()
    child.stderr = new EventEmitter()
    child.killed = []
    child.unrefCalled = false
    child.kill = (signal = 'SIGTERM') => {
      child.killed.push(signal)
      if (child.autoCloseOnKill !== false) queueMicrotask(() => child.emit('close', null, signal))
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

function fakeCtx(id, { cwd = '/work', idle = true, pending = false, sessionFile } = {}) {
  return {
    cwd,
    sessionManager: {
      getSessionId: () => id,
      getCwd: () => cwd,
      ...(sessionFile === undefined ? {} : { getSessionFile: () => sessionFile }),
    },
    isIdle: () => idle,
    hasPendingMessages: () => pending,
  }
}

function bridgeWith(behaviors, options = {}) {
  const { spawn, calls } = scriptedSpawn(behaviors)
  const logged = []
  const bridge = createBridge({
    spawn,
    env: { MEMCTL_PI_BIN: BIN },
    home: '/nonexistent-home',
    logger: { warn: (...args) => logged.push(args.join(' ')) },
    startWaitMs: 100,
    endJoinMs: 20,
    ...options,
  })
  return { bridge, calls, logged }
}

async function settle() {
  await new Promise((resolve) => setImmediate(resolve))
  await new Promise((resolve) => setImmediate(resolve))
}

test('session_start starts memctl and the first prompt injects a stable section', async () => {
  const { bridge, calls } = bridgeWith([{ stdout: startPayload('[agent-memory] rules') }])
  const ctx = fakeCtx('sid-1', { cwd: '/project' })

  bridge.onSessionStart({ type: 'session_start', reason: 'startup' }, ctx)
  const event = { type: 'before_agent_start', systemPrompt: 'base', systemPromptOptions: { sections: {} } }
  await bridge.onBeforeAgentStart(event, ctx)

  assert.equal(calls[0].bin, BIN)
  assert.deepEqual(calls[0].args, ['start', '--agent', 'pi', '--sid', 'sid-1', '--json'])
  assert.equal(calls[0].opts.cwd, '/project')
  assert.equal(calls[0].opts.shell, false)
  assert.equal(event.systemPromptOptions.sections.memctl, '[agent-memory] rules')

  const before = event.systemPromptOptions.sections.memctl
  await bridge.onBeforeAgentStart(event, ctx)
  assert.equal(event.systemPromptOptions.sections.memctl, before)
  assert.equal(calls.length, 1)
})

test('an empty context deletes the section instead of keeping stale text', async () => {
  const { bridge } = bridgeWith([{ stdout: startPayload('[agent-memory] rules') }, { stdout: startPayload('') }])
  const ctx = fakeCtx('sid-empty')
  const event = { systemPrompt: 'base', systemPromptOptions: { sections: { memctl: 'old' } } }

  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await bridge.onBeforeAgentStart(event, ctx)
  assert.equal(event.systemPromptOptions.sections.memctl, '[agent-memory] rules')

  bridge.onSessionStart({ reason: 'resume' }, ctx)
  await bridge.onBeforeAgentStart(event, ctx)
  assert.equal('memctl' in event.systemPromptOptions.sections, false)
})

test('a reload keeps the previous snapshot until the new start finishes', async () => {
  const { bridge } = bridgeWith([{ stdout: startPayload('[agent-memory] old') }, { hang: true }], {
    startWaitMs: 30,
  })
  const ctx = fakeCtx('sid-reload')
  const event = { systemPrompt: 'base', systemPromptOptions: { sections: {} } }

  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await bridge.onBeforeAgentStart(event, ctx)
  assert.equal(event.systemPromptOptions.sections.memctl, '[agent-memory] old')

  bridge.onSessionStart({ reason: 'reload' }, ctx)
  await bridge.onBeforeAgentStart(event, ctx)
  assert.equal(event.systemPromptOptions.sections.memctl, '[agent-memory] old')
})

test('the first prompt waits no longer than startWaitMs', async () => {
  const { bridge } = bridgeWith([{ hang: true }], { startWaitMs: 20, startTimeoutMs: 5_000 })
  const ctx = fakeCtx('sid-wait')
  const event = { systemPrompt: 'base', systemPromptOptions: { sections: {} } }

  bridge.onSessionStart({ reason: 'startup' }, ctx)
  const started = Date.now()
  await bridge.onBeforeAgentStart(event, ctx)
  assert.ok(Date.now() - started >= 15)
  assert.equal('memctl' in event.systemPromptOptions.sections, false)
})

test('missing sections fall back to replacing the system prompt once', async () => {
  const { bridge, logged } = bridgeWith([{ stdout: startPayload('[agent-memory] body') }])
  const ctx = fakeCtx('sid-fallback')
  const event = { systemPrompt: 'base prompt' }

  bridge.onSessionStart({ reason: 'startup' }, ctx)
  const result = await bridge.onBeforeAgentStart(event, ctx)
  assert.deepEqual(result, { systemPrompt: 'base prompt\n\n[agent-memory] body' })

  await bridge.onBeforeAgentStart(event, ctx)
  assert.equal(logged.filter((line) => line.includes('falling back')).length, 1)
})

test('a forced system prompt uses the string fallback', async () => {
  const { bridge } = bridgeWith([{ stdout: startPayload('[agent-memory] body') }])
  const ctx = fakeCtx('sid-forced')
  const event = { systemPrompt: 'forced prompt', systemPromptOptions: { sections: {}, forceSystemPrompt: 'forced' } }

  bridge.onSessionStart({ reason: 'startup' }, ctx)
  const result = await bridge.onBeforeAgentStart(event, ctx)
  assert.deepEqual(result, { systemPrompt: 'forced prompt\n\n[agent-memory] body' })
  assert.equal('memctl' in event.systemPromptOptions.sections, false)
})

test('agent_settled syncs once and merges concurrent settlements', async () => {
  const { bridge, calls } = bridgeWith([
    { stdout: startPayload('body') },
    { hang: true },
    { stdout: '' },
  ])
  const ctx = fakeCtx('sid-sync')
  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await settle()

  bridge.onAgentSettled({ type: 'agent_settled' }, ctx)
  bridge.onAgentSettled({ type: 'agent_settled' }, ctx)
  await settle()

  const syncs = calls.filter((call) => call.args[0] === 'sync')
  assert.equal(syncs.length, 1)
  assert.deepEqual(syncs[0].args, ['sync', '--agent', 'pi', '--sid', 'sid-sync'])

  calls.at(-1).child.emit('close', 0, null)
  await settle()
  assert.equal(calls.filter((call) => call.args[0] === 'sync').length, 2)
})

test('agent_end only syncs when idle and is not repeated by agent_settled', async () => {
  const { bridge, calls } = bridgeWith([
    { stdout: startPayload('body') },
    { stdout: '' },
    { stdout: '' },
  ])
  const busy = fakeCtx('sid-busy', { idle: false })
  bridge.onSessionStart({ reason: 'startup' }, busy)
  await settle()

  bridge.onAgentEnd({ type: 'agent_end' }, busy)
  await settle()
  assert.equal(calls.filter((call) => call.args[0] === 'sync').length, 0)
  bridge.onAgentSettled({ type: 'agent_settled' }, busy)
  await settle()
  assert.equal(calls.filter((call) => call.args[0] === 'sync').length, 1)

  const quiet = fakeCtx('sid-quiet')
  bridge.onSessionStart({ reason: 'startup' }, quiet)
  await settle()
  bridge.onAgentEnd({ type: 'agent_end' }, quiet)
  bridge.onAgentSettled({ type: 'agent_settled' }, quiet)
  await settle()
  assert.equal(calls.filter((call) => call.args[0] === 'sync').length, 2)
})

test('session_shutdown ends quit/new/resume/fork but not reload', async () => {
  for (const reason of ['quit', 'new', 'resume', 'fork', 'reload']) {
    const { bridge, calls } = bridgeWith([{ stdout: startPayload('body') }])
    const ctx = fakeCtx(`sid-${reason}`)
    bridge.onSessionStart({ reason: 'startup' }, ctx)
    await settle()
    await bridge.onSessionShutdown({ reason }, ctx)

    const ends = calls.filter((call) => call.args[0] === 'end')
    if (reason === 'reload') {
      assert.equal(ends.length, 0, 'reload must not end the session')
    } else {
      assert.equal(ends.length, 1, `${reason} must end the session once`)
      assert.deepEqual(ends[0].args, ['end', '--agent', 'pi', '--sid', `sid-${reason}`])
      assert.equal(ends[0].opts.detached, true)
      assert.equal(ends[0].opts.stdio, 'ignore')
      assert.equal(ends[0].child.unrefCalled, true)
    }
    assert.equal(bridge.states.size, 0)
  }
})

test('shutdown cancels the in-flight start and sync child', async () => {
  const { bridge, calls } = bridgeWith([
    { stdout: startPayload('body') },
    { hang: true },
    { stdout: '' },
  ])
  const ctx = fakeCtx('sid-cancel')
  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await settle()
  bridge.onAgentSettled({ type: 'agent_settled' }, ctx)
  await settle()

  const syncChild = calls.filter((call) => call.args[0] === 'sync')[0].child
  await bridge.onSessionShutdown({ reason: 'quit' }, ctx)
  assert.equal(syncChild.killed[0], 'SIGTERM')
})

test('a missing binary logs once and never breaks the host', async () => {
  const { spawn, calls } = scriptedSpawn([])
  const logged = []
  const bridge = createBridge({
    spawn,
    env: {},
    home: '/nonexistent-home',
    logger: { warn: (...args) => logged.push(args.join(' ')) },
    startWaitMs: 10,
  })
  const ctx = fakeCtx('sid-missing')

  bridge.onSessionStart({ reason: 'startup' }, ctx)
  const event = { systemPrompt: 'base', systemPromptOptions: { sections: {} } }
  await bridge.onBeforeAgentStart(event, ctx)
  bridge.onAgentSettled({ type: 'agent_settled' }, ctx)
  await settle()

  assert.equal(calls.length, 0)
  assert.equal(logged.filter((line) => line.includes('not found')).length, 1)
  assert.deepEqual(event.systemPromptOptions.sections, {})
})

test('start failures are bounded and never throw', async () => {
  const cases = [
    [{ error: new Error('EACCES') }],
    [{ stdout: '{not json', code: 0 }],
    [{ stdout: startPayload('body'), code: 3, stderr: 'sync failed' }],
    [{ stdout: 'x'.repeat(64), code: 0 }],
    [{ hang: true }],
  ]
  for (const behaviors of cases) {
    const { bridge, logged } = bridgeWith(behaviors, { maxStartBytes: 8, startTimeoutMs: 5 })
    const ctx = fakeCtx('sid-fail')
    const event = { systemPrompt: 'base', systemPromptOptions: { sections: {} } }
    bridge.onSessionStart({ reason: 'startup' }, ctx)
    await bridge.onBeforeAgentStart(event, ctx)
    await settle()
    assert.ok(Array.isArray(logged))
  }
})

test('a non-zero start with a valid envelope keeps the context and logs the error', async () => {
  const { bridge, logged } = bridgeWith([{ stdout: startPayload('[agent-memory] partial'), code: 1, stderr: 'busy' }])
  const ctx = fakeCtx('sid-partial')
  const event = { systemPromptOptions: { sections: {} } }
  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await bridge.onBeforeAgentStart(event, ctx)

  assert.equal(event.systemPromptOptions.sections.memctl, '[agent-memory] partial')
  assert.ok(logged.some((line) => line.includes('exited with 1')))
})

test('dispose ends every live session and clears state', async () => {
  const { bridge, calls } = bridgeWith([
    { stdout: startPayload('a') },
    { stdout: startPayload('b') },
  ])
  const first = fakeCtx('sid-a')
  const second = fakeCtx('sid-b')
  bridge.onSessionStart({ reason: 'startup' }, first)
  bridge.onSessionStart({ reason: 'startup' }, second)
  await settle()

  await bridge.dispose()
  const ends = calls.filter((call) => call.args[0] === 'end')
  assert.equal(ends.length, 2)
  assert.equal(bridge.states.size, 0)
})

test('MEMCTL_PI_BIN and the home binary are resolved before PATH', () => {
  const home = mkdtempSync(join(tmpdir(), 'memctl-pi-home-'))
  mkdirSync(join(home, '.agent-memory', 'bin'), { recursive: true })
  writeFileSync(join(home, '.agent-memory', 'bin', 'memctl'), '#!/bin/sh\n')

  const fromHome = createBridge({ env: {}, home, logger: silentLogger })
  assert.equal(fromHome.resolveBinary(), join(home, '.agent-memory', 'bin', 'memctl'))

  const fromEnv = createBridge({ env: { MEMCTL_PI_BIN: '/custom/memctl' }, home, logger: silentLogger })
  assert.equal(fromEnv.resolveBinary(), '/custom/memctl')
})

test('the pi session file is passed to sync and end when the host exposes one', async () => {
  const sessionFile = '/work/.pi/sessions/2026-10-20T01-00-00_abc.jsonl'
  const { bridge, calls } = bridgeWith(
    [{ stdout: startPayload('memory') }, { code: 0 }, { code: 0 }],
  )
  const ctx = fakeCtx('sid-transcript', { sessionFile })
  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await new Promise((resolve) => setImmediate(resolve))
  bridge.onAgentSettled({}, ctx)
  await new Promise((resolve) => setImmediate(resolve))
  bridge.onSessionShutdown({ reason: 'quit' }, ctx)
  await new Promise((resolve) => setImmediate(resolve))

  const sync = calls.find((call) => call.args[0] === 'sync')
  assert.ok(sync, 'sync was not called')
  assert.deepEqual(sync.args, ['sync', '--agent', 'pi', '--sid', 'sid-transcript', '--transcript', sessionFile])

  const end = calls.find((call) => call.args[0] === 'end')
  assert.ok(end, 'end was not called')
  assert.deepEqual(end.args, ['end', '--agent', 'pi', '--sid', 'sid-transcript', '--transcript', sessionFile])
})

test('a pi session without getSessionFile calls sync without --transcript', async () => {
  const { bridge, calls } = bridgeWith([{ stdout: startPayload('memory') }, { code: 0 }])
  const ctx = fakeCtx('sid-plain')
  bridge.onSessionStart({ reason: 'startup' }, ctx)
  await new Promise((resolve) => setImmediate(resolve))
  bridge.onAgentSettled({}, ctx)
  await new Promise((resolve) => setImmediate(resolve))

  const sync = calls.find((call) => call.args[0] === 'sync')
  assert.deepEqual(sync.args, ['sync', '--agent', 'pi', '--sid', 'sid-plain'])
})
