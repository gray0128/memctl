import { useEffect, useRef } from 'preact/hooks'
import { formatTimestamp } from '../time.js'

const CONNECTION_CONFIG = {
  live: {
    dot: 'bg-emerald-500 ring-emerald-500/30',
    badge: 'bg-emerald-50 text-emerald-700 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
    label: '已连接',
  },
  connecting: {
    dot: 'bg-amber-500 ring-amber-500/30',
    badge: 'bg-amber-50 text-amber-700 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
    label: '连接中',
  },
  reconnecting: {
    dot: 'bg-amber-500 ring-amber-500/30',
    badge: 'bg-amber-50 text-amber-700 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
    label: '重连中',
  },
  stopped: {
    dot: 'bg-slate-400 ring-slate-400/20',
    badge: 'bg-slate-100 text-slate-600 border-slate-200 dark:bg-slate-800/60 dark:text-slate-300 dark:border-slate-700/60',
    label: '已断开',
  },
}

function StatusIndicator({ state }) {
  const conf = CONNECTION_CONFIG[state] || CONNECTION_CONFIG.stopped
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium tracking-tight shadow-xs transition-colors ${conf.badge}`}>
      <span className="relative flex h-2 w-2">
        {state === 'live' && (
          <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75 duration-1000" />
        )}
        <span className={`relative inline-flex h-2 w-2 rounded-full ${conf.dot}`} />
      </span>
      {conf.label}
    </span>
  )
}

function StatItem({ label, value, monospace = false, icon = null }) {
  return (
    <div className="flex flex-col min-w-0">
      <span className="text-[10px] font-semibold tracking-wider uppercase text-slate-500 dark:text-slate-400">
        {label}
      </span>
      <span
        className={`truncate text-xs font-medium text-slate-800 dark:text-slate-200 ${
          monospace ? 'font-mono' : ''
        }`}
        title={value}
      >
        {value}
      </span>
    </div>
  )
}

function aheadBehind(sync) {
  if (!sync || sync.aheadCount === null || sync.aheadCount === undefined) return '未知'
  if (sync.behindCount === null || sync.behindCount === undefined) return '未知'
  return `+${sync.aheadCount} / -${sync.behindCount}`
}

export function TopBar({ connection, snapshot, readOnly, error, onSync, syncBusy, syncMessage }) {
  const headerRef = useRef(null)
  const sync = snapshot ? snapshot.syncState : null
  const localState = snapshot ? sync.localState : error ? '不可用' : '读取中'
  const remoteState = snapshot ? sync.remoteState : error ? '不可用' : '读取中'
  const lastSync = snapshot && sync.lastSuccessAt ? formatTimestamp(sync.lastSuccessAt) : '未知'

  // The header height changes when the sync banner or a snapshot error is
  // shown. The sticky nav reads the measured height so it never slides under
  // the header; see docs/04-ui-and-cli-specification.md section 4.
  useEffect(() => {
    const element = headerRef.current
    if (!element) return undefined
    const apply = () => {
      const height = Math.ceil(element.getBoundingClientRect().height)
      document.documentElement.style.setProperty('--memctl-header-height', `${height}px`)
    }
    apply()
    const observer =
      typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(apply)
    if (observer) observer.observe(element)
    return () => {
      if (observer) observer.disconnect()
      document.documentElement.style.removeProperty('--memctl-header-height')
    }
  }, [])

  const SYNC_TONES = {
    ok: 'bg-emerald-50 text-emerald-800 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
    warn: 'bg-amber-50 text-amber-800 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
    error: 'bg-rose-50 text-rose-800 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/60',
  }

  return (
    <header ref={headerRef} className="sticky top-0 z-30 border-b border-slate-200/80 bg-white/90 backdrop-blur-md dark:border-slate-800/80 dark:bg-slate-900/90 shadow-2xs">
      <div className="mx-auto flex max-w-7xl flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-6">
        {/* Brand & Connection State */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2.5">
            <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-tr from-indigo-600 to-indigo-500 text-white shadow-xs">
              <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
              </svg>
            </div>
            <span className="text-base font-bold tracking-tight text-slate-900 dark:text-white">memctl</span>
          </div>

          <div className="flex items-center gap-1.5">
            {readOnly && (
              <span className="inline-flex items-center gap-1 rounded-full border border-indigo-200/80 bg-indigo-50/80 px-2 py-0.5 text-xs font-semibold tracking-tight text-indigo-700 dark:border-indigo-800/60 dark:bg-indigo-950/40 dark:text-indigo-300">
                <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
                </svg>
                只读
              </span>
            )}
            <StatusIndicator state={connection} />
          </div>
        </div>

        {/* Global Repository & Sync Stats Strip */}
        <div className="grid grid-cols-2 gap-x-4 gap-y-2 rounded-xl border border-slate-200/60 bg-slate-50/50 p-2 sm:flex sm:items-center sm:gap-x-6 sm:bg-transparent sm:border-0 sm:p-0 dark:border-slate-800/60 dark:bg-slate-950/30 sm:dark:bg-transparent">
          <StatItem label="仓库路径" value={snapshot ? snapshot.repoPath : '未知'} monospace />
          <StatItem label="本地状态" value={localState} />
          <StatItem label="远端状态" value={remoteState} />
          <StatItem label="同步差额" value={aheadBehind(sync)} monospace />
          <StatItem label="最近同步" value={lastSync} />
        </div>

        {/* Quick Sync Action */}
        <div className="flex items-center">
          <button
            type="button"
            onClick={onSync}
            disabled={readOnly || syncBusy}
            className="inline-flex w-full items-center justify-center gap-1.5 rounded-lg border border-slate-300/80 bg-white px-3.5 py-1.5 text-xs font-semibold text-slate-700 shadow-2xs transition-all hover:bg-slate-50 hover:text-slate-900 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-40 sm:w-auto dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700/80 dark:hover:text-white"
          >
            <svg
              className={`h-3.5 w-3.5 text-slate-500 dark:text-slate-400 ${syncBusy ? 'animate-spin' : ''}`}
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M21.5 2v6h-6M21.34 15.57a10 10 0 1 1-.57-8.38l5.67-5.67" />
            </svg>
            <span>{syncBusy ? '正在同步…' : '立即同步'}</span>
          </button>
        </div>
      </div>

      {/* Sync / Error Feedback Banner */}
      {syncMessage && (
        <div className="border-t border-slate-200/60 bg-slate-50/50 px-4 py-2 sm:px-6 dark:border-slate-800/60 dark:bg-slate-900/50">
          <div className={`mx-auto flex max-w-7xl items-center gap-2 rounded-lg border px-3 py-1.5 text-xs font-medium ${SYNC_TONES[syncMessage.tone] || SYNC_TONES.warn}`}>
            <svg className="h-4 w-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>{syncMessage.text}</span>
          </div>
        </div>
      )}

      {error && (
        <div className="border-t border-rose-200/60 bg-rose-50/50 px-4 py-2 sm:px-6 dark:border-rose-900/60 dark:bg-rose-950/30">
          <div className="mx-auto flex max-w-7xl items-center gap-2 rounded-lg border border-rose-200/80 bg-white/80 px-3 py-1.5 text-xs font-medium text-rose-700 shadow-2xs dark:border-rose-800/60 dark:bg-slate-900/80 dark:text-rose-300">
            <svg className="h-4 w-4 shrink-0 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <span>无法读取快照：{error.message}</span>
          </div>
        </div>
      )}
    </header>
  )
}
