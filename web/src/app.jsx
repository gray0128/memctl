import { useEffect, useMemo, useState } from 'preact/hooks'
import { createApi } from './api.js'
import { createEventStream } from './sse.js'
import { TopBar } from './components/TopBar.jsx'
import { Overview } from './components/Overview.jsx'
import { KnowledgeGraph } from './components/KnowledgeGraph.jsx'
import { EntryBrowser } from './components/EntryBrowser.jsx'
import { AuditView } from './components/AuditView.jsx'
import { DiagnosticsView } from './components/DiagnosticsView.jsx'

// The five views of docs/04-ui-and-cli-specification.md section 4. The
// conflict-resolution and adapter views arrive with stages C-2 and C-3.
const VIEWS = [
  { id: 'overview', label: '概览' },
  { id: 'graph', label: '知识图谱' },
  { id: 'entries', label: '条目' },
  { id: 'audit', label: '审计' },
  { id: 'diagnostics', label: '诊断' },
]

// App is the shell of the local Web UI. It loads the snapshot once, keeps it
// fresh through the SSE invalidation stream and renders the shared status bar
// plus the selected view. Every invalidation also bumps refreshKey so views
// that fetch their own endpoint re-read it. A manual sync refreshes the
// snapshot directly because a sync without a local commit produces no
// vault.updated event.
export function App() {
  const api = useMemo(() => createApi(), [])
  const [snapshot, setSnapshot] = useState(null)
  const [error, setError] = useState(null)
  const [connection, setConnection] = useState('connecting')
  const [view, setView] = useState('overview')
  const [refreshKey, setRefreshKey] = useState(0)
  const [syncBusy, setSyncBusy] = useState(false)
  const [syncMessage, setSyncMessage] = useState(null)

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
      if (!cancelled) setRefreshKey((current) => current + 1)
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

  async function runSync() {
    setSyncBusy(true)
    setSyncMessage(null)
    try {
      const summary = await api.sync()
      const parts = [`远端状态 ${summary.remoteState}`]
      if (summary.reasonCode) parts.push(summary.reasonCode)
      parts.push(`领先 ${summary.aheadCount ?? '未知'} · 落后 ${summary.behindCount ?? '未知'}`)
      const failed = summary.remoteState === 'failed' || summary.remoteState === 'conflict'
      setSyncMessage({ tone: failed ? 'warn' : 'ok', text: parts.join(' · ') })
      try {
        const next = await api.snapshot()
        setSnapshot(next)
      } catch {
        // The status message above still reports the sync result.
      }
    } catch (syncError) {
      setSyncMessage({ tone: 'error', text: syncError.message })
    } finally {
      setSyncBusy(false)
    }
  }

  let content
  switch (view) {
    case 'graph':
      content = <KnowledgeGraph api={api} snapshot={snapshot} refreshKey={refreshKey} />
      break
    case 'entries':
      content = <EntryBrowser api={api} refreshKey={refreshKey} snapshot={snapshot} />
      break
    case 'audit':
      content = <AuditView api={api} refreshKey={refreshKey} />
      break
    case 'diagnostics':
      content = <DiagnosticsView api={api} refreshKey={refreshKey} />
      break
    default:
      content = <Overview api={api} refreshKey={refreshKey} snapshot={snapshot} error={error} />
  }

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <TopBar
        connection={connection}
        snapshot={snapshot}
        readOnly={api.readOnly}
        error={error}
        onSync={runSync}
        syncBusy={syncBusy}
        syncMessage={syncMessage}
      />
      <nav className="border-b border-slate-200 bg-white px-6 dark:border-slate-800 dark:bg-slate-900">
        <div className="mx-auto flex max-w-5xl gap-1 overflow-x-auto">
          {VIEWS.map((item) => (
            <button
              key={item.id}
              type="button"
              aria-current={view === item.id ? 'page' : undefined}
              onClick={() => setView(item.id)}
              className={`whitespace-nowrap border-b-2 px-3 py-2 text-sm ${
                view === item.id
                  ? 'border-indigo-600 font-medium text-indigo-700 dark:text-indigo-300'
                  : 'border-transparent text-slate-600 hover:text-slate-900 dark:text-slate-300 dark:hover:text-slate-100'
              }`}
            >
              {item.label}
            </button>
          ))}
        </div>
      </nav>
      <main className="mx-auto max-w-5xl px-6 py-8">{content}</main>
    </div>
  )
}
