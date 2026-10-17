import { useEffect, useState } from 'preact/hooks'
import { autofixableCodes, summarizeDiagnostics } from '../diagnostics.js'

const SEVERITY_STYLES = {
  error: {
    label: '错误',
    badge: 'bg-rose-100 text-rose-900 dark:bg-rose-900/50 dark:text-rose-100',
    border: 'border-rose-200 dark:border-rose-900',
  },
  warning: {
    label: '警告',
    badge: 'bg-amber-100 text-amber-900 dark:bg-amber-900/50 dark:text-amber-100',
    border: 'border-amber-200 dark:border-amber-900',
  },
  info: {
    label: '信息',
    badge: 'bg-sky-100 text-sky-900 dark:bg-sky-900/50 dark:text-sky-100',
    border: 'border-sky-200 dark:border-sky-900',
  },
}

// DiagnosticsView renders the DoctorReport: issue counts by error/warning/info
// and the issue list, plus the deterministic repair action of
// POST /api/v1/doctor:fix. It intentionally shows no health score, per
// docs/04-ui-and-cli-specification.md section 4.
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
      // The response carries the post-fix report; showing it keeps the list and
      // the next baseline current instead of repeating a stale request.
      if (result.report) setReport(result.report)
      if (result.result) {
        const commit = result.result.commit
        setFixMessage({
          tone: 'ok',
          text: `已提交确定性修复 ${commit ? commit.slice(0, 12) : result.result.transactionId}`,
        })
      } else {
        setFixMessage({ tone: 'info', text: '当前没有可执行的确定性修复。' })
      }
    } catch (requestError) {
      if (requestError.code === 'UNSUPPORTED') {
        setFixMessage({ tone: 'warn', text: '存在没有确定性修复的诊断，需人工处理。' })
      } else if (requestError.code === 'REVISION_MISMATCH') {
        setFixMessage({ tone: 'warn', text: '诊断基线已过期，请刷新后重试。' })
      } else {
        setFixMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setFixing(false)
    }
  }

  if (error) {
    return <p className="text-sm text-rose-700 dark:text-rose-300">无法读取诊断：{error.message}</p>
  }
  if (!report) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">正在运行只读诊断…</p>
  }

  const { counts, total, groups } = summarizeDiagnostics(report)
  const fixable = autofixableCodes(report)
  const OPEN_TONES = {
    ok: 'text-emerald-700 dark:text-emerald-300',
    info: 'text-slate-500 dark:text-slate-400',
    warn: 'text-amber-700 dark:text-amber-300',
    error: 'text-rose-700 dark:text-rose-300',
  }

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">诊断</h1>
        <span className="text-sm text-slate-500 dark:text-slate-400">
          修订 {report.revision ? report.revision.replace(/^sha256:/, '').slice(0, 12) : '未知'} · 检查于{' '}
          {report.checkedAt}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={fix}
          disabled={fixing || fixable.length === 0 || api.readOnly}
          className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
        >
          执行确定性修复
        </button>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          {fixable.length === 0
            ? '没有标记为可自动修复的问题'
            : `可自动修复：${fixable.join('、')}`}
        </span>
      </div>
      {fixMessage && <p className={`text-sm ${OPEN_TONES[fixMessage.tone]}`}>{fixMessage.text}</p>}

      <div className="grid grid-cols-3 gap-4">
        {groups.map((group) => (
          <div
            key={group.severity}
            className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900"
          >
            <div
              className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${SEVERITY_STYLES[group.severity].badge}`}
            >
              {SEVERITY_STYLES[group.severity].label}
            </div>
            <div className="mt-1 text-2xl font-semibold">{group.count}</div>
          </div>
        ))}
      </div>

      {total === 0 && <p className="text-sm text-slate-500 dark:text-slate-400">没有诊断问题。</p>}

      {groups.map((group) =>
        group.count === 0 ? null : (
          <div key={group.severity} className="space-y-2">
            <h2 className="text-sm font-semibold text-slate-600 dark:text-slate-300">
              {SEVERITY_STYLES[group.severity].label}（{group.count}）
            </h2>
            <ul className="space-y-2">
              {group.issues.map((issue, index) => (
                <li
                  key={`${issue.code}-${issue.filePath}-${issue.lineNumber}-${index}`}
                  className={`rounded-lg border bg-white p-3 dark:bg-slate-900 ${SEVERITY_STYLES[group.severity].border}`}
                >
                  <div className="flex flex-wrap items-baseline gap-x-2 text-xs text-slate-500 dark:text-slate-400">
                    <span className="font-mono">{issue.code}</span>
                    {issue.filePath && (
                      <span className="font-mono break-all">
                        {issue.filePath}
                        {issue.lineNumber ? `:${issue.lineNumber}` : ''}
                      </span>
                    )}
                    {issue.autoFixable && <span>可自动修复</span>}
                  </div>
                  <p className="mt-1 text-sm">{issue.message}</p>
                  {issue.suggestion && (
                    <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{issue.suggestion}</p>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ),
      )}
    </section>
  )
}
