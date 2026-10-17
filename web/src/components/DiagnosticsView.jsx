import { useEffect, useState } from 'preact/hooks'
import { autofixableCodes, summarizeDiagnostics } from '../diagnostics.js'

const SEVERITY_CONFIG = {
  error: {
    label: '阻断错误',
    badge: 'bg-rose-50 text-rose-700 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-900/60',
    card: 'border-rose-200/80 bg-white hover:border-rose-300 dark:border-rose-900/50 dark:bg-slate-900',
    dot: 'bg-rose-500',
  },
  warning: {
    label: '告警隐患',
    badge: 'bg-amber-50 text-amber-700 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-900/60',
    card: 'border-amber-200/80 bg-white hover:border-amber-300 dark:border-amber-900/50 dark:bg-slate-900',
    dot: 'bg-amber-500',
  },
  info: {
    label: '参考建议',
    badge: 'bg-sky-50 text-sky-700 border-sky-200/80 dark:bg-sky-950/40 dark:text-sky-300 dark:border-sky-900/60',
    card: 'border-sky-200/80 bg-white hover:border-sky-300 dark:border-sky-900/50 dark:bg-slate-900',
    dot: 'bg-sky-500',
  },
}

export function DiagnosticsView({ api, refreshKey }) {
  const [report, setReport] = useState(null)
  const [error, setError] = useState(null)
  const [fixing, setFixing] = useState(false)
  const [fixMessage, setFixMessage] = useState(null)

  useEffect(() => {
    let cancelled = false
    api
      .doctor()
      .then((result) => {
        if (cancelled) return
        setReport(result)
        setError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey])

  async function fix() {
    if (!report) return
    setFixing(true)
    setFixMessage(null)
    try {
      const result = await api.doctorFix({
        issueCodes: autofixableCodes(report),
        baseRevision: report.revision,
      })
      if (result.report) setReport(result.report)
      if (result.result) {
        const commit = result.result.commit
        setFixMessage({
          tone: 'ok',
          text: `已完成确定性修复，提交哈希 ${commit ? commit.slice(0, 12) : result.result.transactionId}`,
        })
      } else {
        setFixMessage({ tone: 'info', text: '当前无适用的确定性修复操作。' })
      }
    } catch (requestError) {
      if (requestError.code === 'UNSUPPORTED') {
        setFixMessage({ tone: 'warn', text: '存在无确定性修复规则的诊断，需人工干预处理。' })
      } else if (requestError.code === 'REVISION_MISMATCH') {
        setFixMessage({ tone: 'warn', text: '诊断基线已过期，请刷新后再试。' })
      } else {
        setFixMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setFixing(false)
    }
  }

  if (error) {
    return (
      <div className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
        无法读取诊断：{error.message}
      </div>
    )
  }
  if (!report) {
    return (
      <div className="rounded-2xl border border-slate-200/80 bg-white p-12 text-center shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
        <p className="text-xs text-slate-500 dark:text-slate-400">正在执行只读静态诊断扫描…</p>
      </div>
    )
  }

  const { counts, total, groups } = summarizeDiagnostics(report)
  const fixable = autofixableCodes(report)

  const TONES = {
    ok: 'bg-emerald-50 text-emerald-800 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
    info: 'bg-slate-50 text-slate-700 border-slate-200/80 dark:bg-slate-900 dark:text-slate-300 dark:border-slate-800',
    warn: 'bg-amber-50 text-amber-800 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
    error: 'bg-rose-50 text-rose-800 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/60',
  }

  return (
    <section className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/80 pb-4 dark:border-slate-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">健康诊断与自愈</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            只读拓扑一致性校验、孤立文件检测与确定性修复
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded-lg border border-slate-200/80 bg-white px-2.5 py-1 text-slate-600 shadow-2xs dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300">
            基线修订: <code className="font-mono font-semibold">{report.revision ? report.revision.replace(/^sha256:/, '').slice(0, 10) : '未知'}</code>
          </span>
          <span className="rounded-lg border border-slate-200/80 bg-white px-2.5 py-1 text-slate-600 shadow-2xs dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300">
            检查时间: {report.checkedAt}
          </span>
        </div>
      </div>

      {/* Auto Fix Action Banner */}
      <div className="flex flex-col gap-3 rounded-xl border border-indigo-200/80 bg-indigo-50/50 p-4 sm:flex-row sm:items-center sm:justify-between shadow-2xs dark:border-indigo-900/60 dark:bg-indigo-950/20">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-indigo-600 text-white shadow-xs">
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path strokeLinecap="round" strokeLinejoin="round" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
          </div>
          <div>
            <span className="text-xs font-bold text-slate-900 dark:text-slate-100">确定性原子修复引擎</span>
            <p className="text-[11px] text-slate-500 dark:text-slate-400">
              {fixable.length === 0
                ? '当前诊断无需要自动修复的问题'
                : `支持自动修复的规则集: ${fixable.join('、')}`}
            </p>
          </div>
        </div>

        <button
          type="button"
          onClick={fix}
          disabled={fixing || fixable.length === 0 || api.readOnly}
          className="rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40"
        >
          {fixing ? '正在修复…' : '执行确定性修复'}
        </button>
      </div>

      {fixMessage && (
        <div className={`rounded-xl border px-4 py-2.5 text-xs font-medium ${TONES[fixMessage.tone] || TONES.info}`}>
          {fixMessage.text}
        </div>
      )}

      {/* Severity Counters Grid */}
      <div className="grid grid-cols-3 gap-3">
        {groups.map((group) => {
          const cfg = SEVERITY_CONFIG[group.severity] || SEVERITY_CONFIG.info
          return (
            <div
              key={group.severity}
              className={`rounded-xl border p-4 shadow-2xs transition-all ${cfg.card}`}
            >
              <div className="flex items-center justify-between">
                <span className={`inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-semibold ${cfg.badge}`}>
                  <span className={`h-1.5 w-1.5 rounded-full ${cfg.dot}`} />
                  {cfg.label}
                </span>
              </div>
              <div className="mt-2 font-mono text-2xl font-bold tracking-tight text-slate-900 dark:text-white">
                {group.count}
              </div>
            </div>
          )
        })}
      </div>

      {/* All Healthy Empty State */}
      {total === 0 && (
        <div className="rounded-2xl border border-slate-200/80 bg-white p-12 text-center shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
          <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600 dark:bg-emerald-950/50 dark:text-emerald-400">
            <svg className="h-6 w-6" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z" />
            </svg>
          </div>
          <h3 className="mt-4 text-sm font-semibold text-slate-900 dark:text-slate-100">
            记忆库结构处于完美状态
          </h3>
          <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
            未发现死链、孤立文件、入口膨胀或 Git 状态不一致隐患
          </p>
        </div>
      )}

      {/* Issues Groups */}
      {groups.map((group) => {
        if (group.count === 0) return null
        const cfg = SEVERITY_CONFIG[group.severity] || SEVERITY_CONFIG.info
        return (
          <div key={group.severity} className="space-y-3">
            <div className="flex items-center gap-2">
              <span className={`h-2 w-2 rounded-full ${cfg.dot}`} />
              <h2 className="text-xs font-bold uppercase tracking-wider text-slate-700 dark:text-slate-300">
                {cfg.label} ({group.count})
              </h2>
            </div>
            <ul className="space-y-2.5">
              {group.issues.map((issue, index) => (
                <li
                  key={`${issue.code}-${issue.filePath}-${issue.lineNumber}-${index}`}
                  className="rounded-xl border border-slate-200/80 bg-white p-3.5 shadow-2xs transition-all hover:border-slate-300 dark:border-slate-800/80 dark:bg-slate-900/80 dark:hover:border-slate-700"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="rounded-md border border-slate-200/80 bg-slate-100/70 px-2 py-0.5 font-mono text-[11px] font-semibold text-slate-700 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300">
                        {issue.code}
                      </span>
                      {issue.filePath && (
                        <span className="font-mono text-xs text-slate-600 dark:text-slate-400 break-all">
                          {issue.filePath}{issue.lineNumber ? `:${issue.lineNumber}` : ''}
                        </span>
                      )}
                    </div>
                    {issue.autoFixable && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-50 px-2 py-0.5 text-[10px] font-semibold text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300">
                        <svg className="h-3 w-3" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.5">
                          <path strokeLinecap="round" strokeLinejoin="round" d="M5 13l4 4L19 7" />
                        </svg>
                        可自动修复
                      </span>
                    )}
                  </div>
                  <p className="mt-2 text-xs font-medium text-slate-900 dark:text-slate-100 leading-relaxed">
                    {issue.message}
                  </p>
                  {issue.suggestion && (
                    <div className="mt-2 rounded-lg bg-slate-50 px-2.5 py-1.5 text-[11px] text-slate-500 dark:bg-slate-800/50 dark:text-slate-400">
                      💡 修复建议: {issue.suggestion}
                    </div>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )
      })}
    </section>
  )
}
