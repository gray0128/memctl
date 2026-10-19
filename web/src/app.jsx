import { useEffect, useMemo, useState } from 'preact/hooks'
import { createApi } from './api.js'
import { loadFreshSnapshot } from './refresh.js'
import { createEventStream } from './sse.js'
import { TopBar } from './components/TopBar.jsx'
import { Overview } from './components/Overview.jsx'
import { KnowledgeGraph } from './components/KnowledgeGraph.jsx'
import { EntryBrowser } from './components/EntryBrowser.jsx'
import { ConflictView } from './components/ConflictView.jsx'
import { AuditView } from './components/AuditView.jsx'
import { DiagnosticsView } from './components/DiagnosticsView.jsx'
import { AdapterView } from './components/AdapterView.jsx'
import { DreamView } from './components/DreamView.jsx'

// The views of docs/04-ui-and-cli-specification.md section 4. 整理 carries the
// Dreaming proposal workflow; 客户端接入 carries the MCP channel status and
// the adapter install panel. They are separate views so reviewing a proposal
// never asks the operator to reason about host configuration at the same time.
const VIEWS = [
  {
    id: 'overview',
    label: '概览',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
      </svg>
    ),
  },
  {
    id: 'graph',
    label: '知识图谱',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <circle cx="6" cy="6" r="3" />
        <circle cx="18" cy="18" r="3" />
        <circle cx="18" cy="6" r="3" />
        <path strokeLinecap="round" strokeLinejoin="round" d="M8.5 8.5l7 7M15.5 6H8.5" />
      </svg>
    ),
  },
  {
    id: 'entries',
    label: '条目',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
      </svg>
    ),
  },
  {
    id: 'conflict',
    label: '冲突解决',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4" />
      </svg>
    ),
  },
  {
    id: 'audit',
    label: '审计',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    ),
  },
  {
    id: 'diagnostics',
    label: '诊断',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
      </svg>
    ),
  },
  {
    id: 'dream',
    label: '整理',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M19.428 15.428a2 2 0 00-1.022-.547l-2.387-.477a6 6 0 00-3.86.517l-.318.158a6 6 0 01-3.86.517L6.05 15.21a2 2 0 00-1.806.547M8 4h8l-1 1v5.172a2 2 0 00.586 1.414l5 5c1.26 1.26.367 3.414-1.415 3.414H4.828c-1.782 0-2.674-2.154-1.414-3.414l5-5A2 2 0 009 10.172V5L8 4z" />
      </svg>
    ),
  },
  {
    id: 'adapters',
    label: '客户端接入',
    icon: (
      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
        <path strokeLinecap="round" strokeLinejoin="round" d="M11 4a2 2 0 114 0v1a1 1 0 001 1h3a1 1 0 011 1v3a1 1 0 01-1 1h-1a2 2 0 100 4h1a1 1 0 011 1v3a1 1 0 01-1 1h-3a1 1 0 01-1-1v-1a2 2 0 10-4 0v1a1 1 0 01-1 1H7a1 1 0 01-1-1v-3a1 1 0 00-1-1H4a2 2 0 110-4h1a1 1 0 001-1V7a1 1 0 011-1h3a1 1 0 001-1V4z" />
      </svg>
    ),
  },
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
        const next = await loadFreshSnapshot(api)
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

  // refreshSnapshot re-reads the snapshot and bumps refreshKey so views that
  // fetch their own endpoint re-read it. Conflict resolve, abort and audit
  // revert use it because they change history without always emitting a
  // vault.updated event.
  async function refreshSnapshot() {
    // A resolved conflict or an applied revert changes the sync summary, so
    // the banner from the previous sync attempt is stale by definition.
    setSyncMessage(null)
    try {
      const next = await loadFreshSnapshot(api)
      setSnapshot(next)
      setError(null)
    } catch (err) {
      setError(err)
    }
    setRefreshKey((current) => current + 1)
  }

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
    case 'conflict':
      content = <ConflictView api={api} refreshKey={refreshKey} onChanged={refreshSnapshot} />
      break
    case 'audit':
      content = <AuditView api={api} refreshKey={refreshKey} onChanged={refreshSnapshot} />
      break
    case 'diagnostics':
      content = <DiagnosticsView api={api} refreshKey={refreshKey} />
      break
    case 'dream':
      content = <DreamView api={api} refreshKey={refreshKey} />
      break
    case 'adapters':
      content = <AdapterView api={api} refreshKey={refreshKey} />
      break
    default:
      content = (
        <Overview
          api={api}
          refreshKey={refreshKey}
          snapshot={snapshot}
          error={error}
          onNavigate={setView}
        />
      )
  }

  return (
    <div className="min-h-screen bg-slate-50/70 text-slate-900 transition-colors dark:bg-[#0b0f17] dark:text-slate-100 antialiased selection:bg-indigo-500/20 selection:text-indigo-600">
      <TopBar
        connection={connection}
        snapshot={snapshot}
        readOnly={api.readOnly}
        error={error}
        onSync={runSync}
        syncBusy={syncBusy}
        syncMessage={syncMessage}
      />

      {/* Floating Segmented Navigation Bar */}
      <nav className="border-b border-slate-200/70 bg-white/70 backdrop-blur-sm dark:border-slate-800/70 dark:bg-slate-900/60 sticky top-[var(--memctl-header-height,57px)] z-20">
        <div className="mx-auto flex max-w-7xl items-center gap-1.5 overflow-x-auto px-4 py-2 sm:px-6 scrollbar-none">
          {VIEWS.map((item) => {
            const active = view === item.id
            return (
              <button
                key={item.id}
                type="button"
                aria-current={active ? 'page' : undefined}
                onClick={() => setView(item.id)}
                className={`group flex items-center gap-2 whitespace-nowrap rounded-lg px-3.5 py-1.5 text-xs font-medium transition-all duration-150 active:scale-95 ${
                  active
                    ? 'bg-indigo-50 text-indigo-700 shadow-xs dark:bg-indigo-950/60 dark:text-indigo-300 font-semibold ring-1 ring-indigo-500/20'
                    : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900 dark:text-slate-400 dark:hover:bg-slate-800/60 dark:hover:text-slate-200'
                }`}
              >
                <span className={`transition-colors ${active ? 'text-indigo-600 dark:text-indigo-400' : 'text-slate-400 group-hover:text-slate-600 dark:text-slate-500 dark:group-hover:text-slate-300'}`}>
                  {item.icon}
                </span>
                <span>{item.label}</span>
              </button>
            )
          })}
        </div>
      </nav>

      {/* Main Workspace */}
      <main className="mx-auto max-w-7xl px-4 py-8 sm:px-6">{content}</main>
    </div>
  )
}
