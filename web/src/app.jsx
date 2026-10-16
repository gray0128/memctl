import { useEffect, useMemo, useState } from 'preact/hooks'
import { createApi } from './api.js'
import { createEventStream } from './sse.js'
import { TopBar } from './components/TopBar.jsx'
import { Overview } from './components/Overview.jsx'

// App is the read-only shell of the local Web UI. It loads the snapshot once,
// keeps it fresh through the SSE invalidation stream and renders the shared
// status bar described in docs/04-ui-and-cli-specification.md section 4. The
// knowledge graph, entry browser, audit and diagnosis views are added in the
// next B-3 subtask.
export function App() {
  const api = useMemo(() => createApi(), [])
  const [snapshot, setSnapshot] = useState(null)
  const [error, setError] = useState(null)
  const [connection, setConnection] = useState('connecting')

  useEffect(() => {
    let cancelled = false

    async function refresh() {
      try {
        const next = await api.snapshot()
        if (cancelled) return
        setSnapshot(next)
        setError(null)
      } catch (err) {
        if (!cancelled) setError(err)
      }
    }

    const stream = createEventStream({
      token: api.token,
      onState: setConnection,
      onInvalidate: refresh,
    })
    stream.start()
    refresh()

    return () => {
      cancelled = true
      stream.stop()
    }
  }, [api])

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <TopBar
        connection={connection}
        snapshot={snapshot}
        readOnly={api.readOnly}
        error={error}
      />
      <main className="mx-auto max-w-5xl px-6 py-8">
        <Overview snapshot={snapshot} error={error} />
      </main>
    </div>
  )
}
