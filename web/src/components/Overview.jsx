import { useEffect, useState } from 'preact/hooks'

function Stat({ label, value, tone = 'default' }) {
  const tones = {
    default: 'text-slate-900 dark:text-slate-100',
    error: 'text-rose-700 dark:text-rose-300',
    warning: 'text-amber-700 dark:text-amber-300',
    info: 'text-sky-700 dark:text-sky-300',
  }
  return (
    <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">{label}</div>
      <div className={`mt-1 text-2xl font-semibold ${tones[tone] || tones.default}`}>{value}</div>
    </div>
  )
}

// Overview renders the fields of the read-only snapshot API plus the
// diagnostic counts. It reads the same GET /api/v1/doctor counts the
// diagnostics view shows, so the two views never report different
// error/warning/info numbers for one repository.
export function Overview({ api, refreshKey, snapshot, error }) {
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
      <p className="text-sm text-slate-500 dark:text-slate-400">
        {error ? '仓库快照不可用。' : '正在加载仓库快照…'}
      </p>
    )
  }

  const entryCount = snapshot.files.reduce((total, file) => total + (file.entries ? file.entries.length : 0), 0)
  const revision = snapshot.revision ? snapshot.revision.replace(/^sha256:/, '').slice(0, 12) : '未知'

  return (
    <section className="space-y-6">
      <div>
        <h1 className="text-xl font-semibold">仓库概览</h1>
        <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">
          入口 {snapshot.entryPoint || 'MEMORY.md'} · 修订 {revision}
          {snapshot.stale ? ' · 快照已过期' : ''}
        </p>
      </div>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Stat label="文件" value={snapshot.files.length} />
        <Stat label="条目" value={entryCount} />
        <Stat label="图谱节点" value={snapshot.graph.nodes.length} />
        <Stat label="图谱关系" value={snapshot.graph.edges.length} />
      </div>
      <div className="grid grid-cols-3 gap-4">
        <Stat label="错误" value={counts ? counts.error : '—'} tone="error" />
        <Stat label="警告" value={counts ? counts.warning : '—'} tone="warning" />
        <Stat label="信息" value={counts ? counts.info : '—'} tone="info" />
      </div>
    </section>
  )
}
