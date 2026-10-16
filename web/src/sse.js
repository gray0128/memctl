// Browser client for GET /api/v1/events.
//
// The server only streams invalidation hints, never authoritative state. This
// module implements the client half of A05 in docs/05-validation-and-delivery.md
// and docs/02-domain-models-and-contracts.md section 5:
//
// - it parses the SSE wire format across arbitrary network chunk boundaries,
//   including multi-line `data` fields terminated by a blank line;
// - after every connect and reconnect it refreshes the whole snapshot, so a
//   dropped event or a missed connection cannot leave a stale view;
// - a `resync.required` hint (queue overflow or lost watcher event) triggers
//   the same full refresh.
//
// The native EventSource cannot send the X-Memctl-Token header, so the stream
// is read with fetch and a ReadableStream reader.

// createSSEParser turns decoded network chunks into complete events. It keeps
// an unterminated line buffered, so a frame split across many chunks is
// reassembled before it is dispatched.
export function createSSEParser() {
  let pending = ''
  let name = ''
  let data = []
  let seen = false

  function dispatch(events) {
    if (!seen) return
    events.push({ name: name || 'message', data: data.join('\n') })
    name = ''
    data = []
    seen = false
  }

  function handleLine(text, events) {
    if (text === '') {
      dispatch(events)
      return
    }
    if (text.startsWith(':')) return
    const colon = text.indexOf(':')
    let field = text
    let value = ''
    if (colon !== -1) {
      field = text.slice(0, colon)
      value = text.slice(colon + 1)
      if (value.startsWith(' ')) value = value.slice(1)
    }
    seen = true
    if (field === 'event') name = value
    else if (field === 'data') data.push(value)
    // `id` and `retry` are not used by this stream.
  }

  return {
    push(chunk) {
      const events = []
      const text = pending + chunk
      pending = ''
      let start = 0
      for (let i = 0; i < text.length; i += 1) {
        const ch = text[i]
        if (ch === '\n') {
          handleLine(text.slice(start, i), events)
          start = i + 1
          continue
        }
        if (ch !== '\r') continue
        if (i === text.length - 1) break // a lone CR may be the first half of a CRLF
        handleLine(text.slice(start, i), events)
        if (text[i + 1] === '\n') i += 1
        start = i + 1
      }
      if (start < text.length) pending = text.slice(start)
      return events
    },
  }
}

// createEventStream keeps one SSE connection open and reconnects with bounded
// exponential backoff. `onInvalidate(reason)` is called after every (re)connect
// with reason "connect" and for every vault.updated or resync.required hint;
// the caller re-reads the snapshot. `onState` reports connecting/live/
// reconnecting/stopped for the status bar.
export function createEventStream(options) {
  const {
    token,
    fetchImpl = globalThis.fetch,
    onInvalidate = () => {},
    onState = () => {},
    reconnectBaseMs = 500,
    reconnectMaxMs = 10000,
  } = options

  let stopped = false
  let attempt = 0
  let timer = null
  let controller = null

  function schedule() {
    if (stopped) return
    onState('reconnecting')
    const delay = Math.min(reconnectMaxMs, reconnectBaseMs * 2 ** attempt)
    attempt += 1
    timer = setTimeout(() => {
      timer = null
      run()
    }, delay)
  }

  async function read(body) {
    const reader = body.getReader()
    const decoder = new TextDecoder()
    const parser = createSSEParser()
    for (;;) {
      const { value, done } = await reader.read()
      if (done) return
      const events = parser.push(decoder.decode(value, { stream: true }))
      for (const event of events) {
        if (event.name === 'vault.updated' || event.name === 'resync.required') {
          await onInvalidate(event.name)
        }
      }
    }
  }

  async function run() {
    if (stopped) return
    controller = new AbortController()
    onState(attempt === 0 ? 'connecting' : 'reconnecting')
    try {
      const response = await fetchImpl('/api/v1/events', {
        headers: { 'X-Memctl-Token': token },
        signal: controller.signal,
      })
      if (!response.ok || !response.body) {
        throw new Error(`event stream request failed with status ${response.status}`)
      }
      attempt = 0
      onState('live')
      // Events are hints, not the truth: refresh after every connect.
      await onInvalidate('connect')
      await read(response.body)
    } catch {
      if (stopped) return
    }
    schedule()
  }

  return {
    start() {
      if (!stopped) run()
    },
    stop() {
      stopped = true
      if (timer !== null) {
        clearTimeout(timer)
        timer = null
      }
      if (controller) controller.abort()
      onState('stopped')
    },
  }
}
