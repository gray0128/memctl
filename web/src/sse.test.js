import { describe, expect, it, vi } from 'vitest'
import { createEventStream, createSSEParser } from './sse.js'

// This file verifies the browser half of A05 from
// docs/05-validation-and-delivery.md: the SSE frames split across network
// chunks and multi-line data are parsed correctly, and every reconnect
// refreshes the full snapshot instead of trusting missed events.

function streamFrom(chunks, close = true) {
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(new TextEncoder().encode(chunk))
      if (close) controller.close()
    },
  })
}

describe('createSSEParser', () => {
  it('reassembles a frame split across one-byte chunks', () => {
    const parser = createSSEParser()
    const stream =
      'event: vault.updated\n' +
      'data: {"revision":"sha256:abc",\n' +
      'data: "changedFiles":["MEMORY.md"]}\n' +
      '\n' +
      ': heartbeat\n' +
      '\n' +
      'event: resync.required\n' +
      'data: {}\n' +
      '\n'
    const events = []
    for (const char of stream) events.push(...parser.push(char))

    expect(events).toEqual([
      {
        name: 'vault.updated',
        data: '{"revision":"sha256:abc",\n"changedFiles":["MEMORY.md"]}',
      },
      { name: 'resync.required', data: '{}' },
    ])
  })

  it('handles CRLF line endings split between chunks', () => {
    const parser = createSSEParser()
    const events = []
    events.push(...parser.push('event: vault.updated\r'))
    events.push(...parser.push('\ndata: {}\r\n'))
    events.push(...parser.push('\r\n'))
    expect(events).toEqual([{ name: 'vault.updated', data: '{}' }])
  })

  it('ignores comments and dispatches several events from one chunk', () => {
    const parser = createSSEParser()
    const events = parser.push(': heartbeat\n\nevent: vault.updated\ndata: a\n\nevent: vault.updated\ndata: b\n\n')
    expect(events).toEqual([
      { name: 'vault.updated', data: 'a' },
      { name: 'vault.updated', data: 'b' },
    ])
  })

  it('keeps a partial frame buffered until its blank line arrives', () => {
    const parser = createSSEParser()
    expect(parser.push('event: vault.updated\ndata: {')).toEqual([])
    expect(parser.push('}\n')).toEqual([])
    expect(parser.push('\n')).toEqual([{ name: 'vault.updated', data: '{}' }])
  })
})

describe('createEventStream', () => {
  it('refreshes the snapshot after every connect and every invalidation hint', async () => {
    let calls = 0
    const invalidations = []
    const states = []
    const fetchImpl = vi.fn(async () => {
      calls += 1
      if (calls === 1) {
        return { ok: true, status: 200, body: streamFrom(['event: vault.updated\ndata: {}\n\n']) }
      }
      return { ok: true, status: 200, body: streamFrom([], false) }
    })

    const stream = createEventStream({
      token: 't',
      fetchImpl,
      onInvalidate: (reason) => invalidations.push(reason),
      onState: (state) => states.push(state),
      reconnectBaseMs: 1,
      reconnectMaxMs: 4,
    })
    stream.start()

    await vi.waitFor(() => expect(invalidations).toEqual(['connect', 'vault.updated', 'connect']))
    expect(states).toContain('live')

    stream.stop()
    expect(states[states.length - 1]).toBe('stopped')
  })

  it('retries with a reconnect state after a failed connection', async () => {
    let calls = 0
    const invalidations = []
    const states = []
    const fetchImpl = vi.fn(async () => {
      calls += 1
      if (calls === 1) throw new Error('network down')
      return { ok: true, status: 200, body: streamFrom([], false) }
    })

    const stream = createEventStream({
      token: 't',
      fetchImpl,
      onInvalidate: (reason) => invalidations.push(reason),
      onState: (state) => states.push(state),
      reconnectBaseMs: 1,
      reconnectMaxMs: 2,
    })
    stream.start()

    await vi.waitFor(() => expect(invalidations).toEqual(['connect']))
    expect(states).toContain('reconnecting')
    stream.stop()
  })

  it('does not schedule another attempt after stop', async () => {
    const states = []
    const fetchImpl = vi.fn(async () => {
      throw new Error('down')
    })
    const stream = createEventStream({
      token: 't',
      fetchImpl,
      onState: (state) => states.push(state),
      reconnectBaseMs: 1,
      reconnectMaxMs: 2,
    })
    stream.start()

    await vi.waitFor(() => expect(fetchImpl).toHaveBeenCalledTimes(1))
    stream.stop()
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(states[states.length - 1]).toBe('stopped')
  })
})
