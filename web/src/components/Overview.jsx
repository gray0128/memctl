import { useEffect, useState } from 'preact/hooks'

function StatCard({ label, value, tone = 'default', icon = null, hint = null, onClick = null }) {
  const tones = {
    default: {
      num: 'text-slate-900 dark:text-slate-100',
      border: 'border-slate-200/80 dark:border-slate-800/80 hover:border-slate-300 dark:hover:border-slate-700',
      iconBg: 'bg-indigo-50 text-indigo-600 dark:bg-indigo-950/50 dark:text-indigo-400',
    },
    error: {
      num: 'text-rose-600 dark:text-rose-400',
      border: 'border-rose-200/70 dark:border-rose-900/50 hover:border-rose-300 dark:hover:border-rose-800',
      iconBg: 'bg-rose-50 text-rose-600 dark:bg-rose-950/50 dark:text-rose-400',
    },
    warning: {
      num: 'text-amber-600 dark:text-amber-400',
      border: 'border-amber-200/70 dark:border-amber-900/50 hover:border-amber-300 dark:hover:border-amber-800',
      iconBg: 'bg-amber-50 text-amber-600 dark:bg-amber-950/50 dark:text-amber-400',
    },
    info: {
      num: 'text-sky-600 dark:text-sky-400',
      border: 'border-sky-200/70 dark:border-sky-900/50 hover:border-sky-300 dark:hover:border-sky-800',
      iconBg: 'bg-sky-50 text-sky-600 dark:bg-sky-950/50 dark:text-sky-400',
    },
  }

  const current = tones[tone] || tones.default
  const interactive = typeof onClick === 'function'
  const handleKeyDown = interactive
    ? (event) => {
        if (event.key === 'Enter' || event.key === ' ' || event.key === 'Spacebar') {
          event.preventDefault()
          onClick()
        }
      }
    : undefined

  return (
    <div
      role={interactive ? 'button' : undefined}
      tabIndex={interactive ? 0 : undefined}
      aria-label={interactive ? `${label} ${value}，${hint || ''}；点击查看` : undefined}
      onClick={interactive ? onClick : undefined}
      onKeyDown={handleKeyDown}
      className={`group relative overflow-hidden rounded-xl border bg-white p-5 shadow-2xs transition-all duration-200 hover:shadow-xs dark:bg-slate-900/80 ${current.border} ${
        interactive
          ? 'cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500/40 active:scale-[0.99]'
          : ''
      }`}
    >
      <div className="flex items-center justify-between">
        <span className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
          {label}
        </span>
        {icon && (
          <div className={`flex h-8 w-8 items-center justify-center rounded-lg transition-transform group-hover:scale-105 ${current.iconBg}`}>
            {icon}
          </div>
        )}
      </div>
      <div className="mt-3 flex items-baseline gap-2">
        <div className={`text-3xl font-bold tracking-tight font-mono ${current.num}`}>
          {value}
        </div>
        {hint && <span className="text-xs text-slate-500 dark:text-slate-400">{hint}</span>}
      </div>
    </div>
  )
}

// Overview renders the fields of the read-only snapshot API plus the
// diagnostic counts. It reads the same GET /api/v1/doctor counts the
// diagnostics view shows, so the two views never report different
// error/warning/info numbers for one repository.
export function Overview({ api, refreshKey, snapshot, error, onNavigate }) {
  const [counts, setCounts] = useState(null)

  useEffect(() => {
    let cancelled = false
    api
      .doctor()
      .then((report) => {
        if (!cancelled) setCounts(report.counts)
      })
      .catch(() => {
        if (!cancelled) setCounts(null)
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey])

  if (!snapshot) {
    return (
      <div className="rounded-2xl border border-slate-200/80 bg-white p-12 text-center shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
        <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-slate-100 text-slate-400 dark:bg-slate-800 dark:text-slate-500">
          <svg className="h-6 w-6 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
        </div>
        <h3 className="mt-4 text-sm font-semibold text-slate-900 dark:text-slate-100">
          {error ? '快照读取受阻' : '正在加载仓库快照…'}
        </h3>
        <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
          {error ? error.message : '请稍候，系统正在从本地守护服务获取最新记忆图谱状态'}
        </p>
      </div>
    )
  }

  const entryCount = snapshot.files.reduce((total, file) => total + (file.entries ? file.entries.length : 0), 0)
  const revision = snapshot.revision ? snapshot.revision.replace(/^sha256:/, '').slice(0, 12) : '未知'

  return (
    <section className="space-y-8">
      {/* Overview Hero Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/80 pb-6 dark:border-slate-800/80">
        <div>
          <div className="flex items-center gap-3">
            <h1 className="text-2xl font-bold tracking-tight text-slate-900 dark:text-white">仓库概览</h1>
            {snapshot.stale && (
              <span className="inline-flex items-center gap-1 rounded-full bg-amber-50 border border-amber-200 px-2.5 py-0.5 text-xs font-semibold text-amber-700 dark:bg-amber-950/40 dark:border-amber-800 dark:text-amber-300">
                <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                快照已过期
              </span>
            )}
          </div>
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
            存储库拓扑健康度与记忆指标汇总
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <div className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 text-xs text-slate-700 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900 dark:text-slate-300">
            <span className="text-slate-400">主入口:</span>
            <code className="font-semibold text-indigo-600 dark:text-indigo-400">{snapshot.entryPoint || 'MEMORY.md'}</code>
          </div>
          <div className="inline-flex items-center gap-1.5 rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 text-xs text-slate-700 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900 dark:text-slate-300">
            <span className="text-slate-400">修订版本:</span>
            <code className="font-semibold">{revision}</code>
          </div>
        </div>
      </div>

      {/* Primary Metrics Grid */}
      <div>
        <h2 className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-3 dark:text-slate-400">
          核心资产指标
        </h2>
        <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
          <StatCard
            label="管理文件"
            value={snapshot.files.length}
            hint="文档总数"
            onClick={onNavigate ? () => onNavigate('entries') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
              </svg>
            }
          />
          <StatCard
            label="原子条目"
            value={entryCount}
            hint="事实要点"
            onClick={onNavigate ? () => onNavigate('entries') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4 6h16M4 10h16M4 14h16M4 18h16" />
              </svg>
            }
          />
          <StatCard
            label="图谱节点"
            value={snapshot.graph.nodes.length}
            hint="实体概念"
            onClick={onNavigate ? () => onNavigate('graph') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="3" />
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 2v7m0 6v7M2 12h7m6 0h7" />
              </svg>
            }
          />
          <StatCard
            label="图谱关系"
            value={snapshot.graph.edges.length}
            hint="引用连线"
            onClick={onNavigate ? () => onNavigate('graph') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
              </svg>
            }
          />
        </div>
      </div>

      {/* Doctor Diagnostics Summary */}
      <div>
        <h2 className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-3 dark:text-slate-400">
          诊断审计汇总
        </h2>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
          <StatCard
            label="错误诊断"
            value={counts ? counts.error : '—'}
            tone="error"
            hint="阻断性问题"
            onClick={onNavigate ? () => onNavigate('diagnostics') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10" />
                <path strokeLinecap="round" strokeLinejoin="round" d="M15 9l-6 6m0-6l6 6" />
              </svg>
            }
          />
          <StatCard
            label="告警提示"
            value={counts ? counts.warning : '—'}
            tone="warning"
            hint="建议优化"
            onClick={onNavigate ? () => onNavigate('diagnostics') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
            }
          />
          <StatCard
            label="参考信息"
            value={counts ? counts.info : '—'}
            tone="info"
            hint="结构线索"
            onClick={onNavigate ? () => onNavigate('diagnostics') : null}
            icon={
              <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
                <circle cx="12" cy="12" r="10" />
                <path strokeLinecap="round" strokeLinejoin="round" d="M12 16v-4m0-4h.01" />
              </svg>
            }
          />
        </div>
      </div>
    </section>
  )
}
