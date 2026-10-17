import { useEffect, useState } from 'preact/hooks'
import { revertBlockReason } from '../conflicts.js'
import { renderDiffHtml, summarizeDiff } from '../diff.js'

const PAGE_SIZE = 50

function shortHash(hash) {
  return hash ? hash.slice(0, 10) : ''
}

function sourceLabel(commit) {
  const parts = []
  if (commit.agent) parts.push(commit.agent)
  if (commit.device) parts.push(commit.device)
  if (commit.sessionId) parts.push(commit.sessionId)
  return parts.length > 0 ? parts.join(' · ') : '未知来源'
}

function DiffPreview({ api, hash }) {
  const [diff, setDiff] = useState(null)
  const [html, setHtml] = useState('')
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    setDiff(null)
    setHtml('')
    setError(null)
    api
      .auditDiff(hash)
      .then((result) => {
        if (cancelled) return
        setDiff(result)
        return renderDiffHtml(result.files).then((markup) => {
          if (!cancelled) setHtml(markup)
        })
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, hash])

  if (error) {
    return <p className="text-xs text-rose-600 dark:text-rose-400">无法读取 Diff：{error.message}</p>
  }
  if (!diff) {
    return (
      <div className="flex items-center gap-2 py-3 text-xs text-slate-500 dark:text-slate-400">
        <svg className="h-4 w-4 animate-spin text-indigo-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
        <span>正在渲染差异视图…</span>
      </div>
    )
  }

  const summary = summarizeDiff(diff.files)

  return (
    <div className="space-y-3">
      {/* Diff Stat Summary Ribbon */}
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="rounded-md border border-slate-200/80 bg-slate-50/80 px-2.5 py-1 text-slate-700 shadow-2xs dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300">
          改动文件 <strong className="font-mono text-slate-900 dark:text-white">{summary.files}</strong>
        </span>
        <span className="rounded-md border border-emerald-200/80 bg-emerald-50/70 px-2.5 py-1 font-mono font-semibold text-emerald-700 dark:border-emerald-900/60 dark:bg-emerald-950/40 dark:text-emerald-300">
          +{summary.additions}
        </span>
        <span className="rounded-md border border-rose-200/80 bg-rose-50/70 px-2.5 py-1 font-mono font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300">
          -{summary.deletions}
        </span>
        {summary.binary > 0 && (
          <span className="rounded-md border border-slate-200/80 bg-slate-50 px-2.5 py-1 text-slate-500 dark:border-slate-800 dark:bg-slate-900 dark:text-slate-400">
            二进制 {summary.binary}
          </span>
        )}
      </div>

      {diff.files.some((file) => file.binary) && (
        <ul className="space-y-1 rounded-lg border border-slate-200/80 bg-slate-50/40 p-2 text-xs text-slate-500 dark:border-slate-800 dark:bg-slate-900/40 dark:text-slate-400">
          {diff.files
            .filter((file) => file.binary)
            .map((file) => (
              <li key={file.path} className="font-mono break-all">
                {file.path} · 二进制文件，不显示行文本 Diff
              </li>
            ))}
        </ul>
      )}

      {html ? (
        <div
          className="diff-preview overflow-x-auto text-xs"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <p className="text-xs text-slate-500 dark:text-slate-400">该提交无文本差异可供显示。</p>
      )}
    </div>
  )
}

function RevertPanel({ api, commit, onChanged }) {
  const [busy, setBusy] = useState(false)
  const [confirming, setConfirming] = useState(false)
  const [message, setMessage] = useState(null)
  const blocked = revertBlockReason(commit)

  async function run() {
    setBusy(true)
    setMessage(null)
    try {
      const snapshot = await api.snapshot()
      const result = await api.revertCommit(commit.hash, snapshot.revision)
      const count = (result.changedFiles || []).length
      setMessage({
        tone: 'ok',
        text: `已生成回滚提交 ${shortHash(result.commit)}，涉及 ${count} 个文件，远端状态 ${result.syncState}。`,
      })
      setConfirming(false)
      onChanged?.()
    } catch (requestError) {
      setConfirming(false)
      if (requestError.code === 'CONTENT_CONFLICT') {
        setMessage({ tone: 'warn', text: '回滚遇到冲突，已中止并恢复原始历史；冲突内容需在 memctl 外人工处理。' })
      } else if (requestError.code === 'UNSUPPORTED') {
        setMessage({ tone: 'warn', text: `该提交不支持自动回滚：${requestError.message}` })
      } else if (requestError.code === 'REVISION_MISMATCH') {
        setMessage({ tone: 'warn', text: '仓库基线已过期，请刷新后重试。' })
      } else if (requestError.code === 'RECOVERY_REQUIRED') {
        setMessage({ tone: 'error', text: `回滚恢复不完整，需人工检查仓库：${requestError.message}` })
      } else {
        setMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setBusy(false)
    }
  }

  const tones = {
    ok: 'text-emerald-700 dark:text-emerald-300',
    warn: 'text-amber-700 dark:text-amber-300',
    error: 'text-rose-700 dark:text-rose-300',
  }

  return (
    <div className="mt-4 rounded-xl border border-slate-200/70 bg-slate-50/60 p-3.5 dark:border-slate-800/80 dark:bg-slate-950/40">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <span className="text-xs font-semibold text-slate-800 dark:text-slate-200">
            受管历史回滚 (Revert)
          </span>
          <p className="text-[11px] text-slate-500 dark:text-slate-400">
            以非破坏方式创建逆向提交，严格保留历史审计链条
          </p>
        </div>

        <div className="flex items-center gap-2">
          {blocked ? (
            <span className="rounded-md bg-slate-100 px-2.5 py-1 text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400">
              {blocked}
            </span>
          ) : confirming ? (
            <div className="flex items-center gap-2">
              <button
                type="button"
                disabled={busy || api.readOnly}
                onClick={run}
                className="rounded-lg bg-rose-600 px-3 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-rose-700 active:scale-95 disabled:opacity-40"
              >
                {busy ? '正在回滚…' : '确认生成回滚提交'}
              </button>
              <button
                type="button"
                onClick={() => setConfirming(false)}
                className="rounded-lg border border-slate-300 px-3 py-1.5 text-xs font-medium text-slate-700 hover:bg-slate-100 dark:border-slate-700 dark:text-slate-300 dark:hover:bg-slate-800"
              >
                取消
              </button>
            </div>
          ) : (
            <button
              type="button"
              disabled={busy || api.readOnly}
              onClick={() => setConfirming(true)}
              className="rounded-lg border border-rose-200/80 bg-rose-50/60 px-3 py-1.5 text-xs font-semibold text-rose-700 shadow-2xs hover:bg-rose-100 active:scale-95 disabled:opacity-40 dark:border-rose-900/60 dark:bg-rose-950/40 dark:text-rose-300"
            >
              回滚此提交
            </button>
          )}
        </div>
      </div>

      {message && <p className={`mt-2 text-xs font-medium ${tones[message.tone]}`}>{message.text}</p>}
    </div>
  )
}

export function AuditView({ api, refreshKey, onChanged }) {
  const [commits, setCommits] = useState([])
  const [cursor, setCursor] = useState('')
  const [selected, setSelected] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    api
      .audit({ limit: PAGE_SIZE })
      .then((page) => {
        if (cancelled) return
        setCommits(page.commits || [])
        setCursor(page.nextCursor || '')
        setError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false)
          setLoaded(true)
        }
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey])

  async function loadMore() {
    if (!cursor || loading) return
    setLoading(true)
    try {
      const page = await api.audit({ limit: PAGE_SIZE, before: cursor })
      setCommits((current) => [...current, ...(page.commits || [])])
      setCursor(page.nextCursor || '')
    } catch (requestError) {
      setError(requestError)
    } finally {
      setLoading(false)
    }
  }

  return (
    <section className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/80 pb-4 dark:border-slate-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">审计与 Diff 追溯</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            Git 提交历史脉络、变更比对与原子历史回滚
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 text-xs text-slate-600 shadow-2xs dark:border-slate-800 dark:bg-slate-900 dark:text-slate-300">
            已载入提交: <strong className="font-mono text-slate-900 dark:text-white">{commits.length}</strong>
          </span>
          {loading && (
            <span className="inline-flex items-center gap-1 text-xs text-indigo-600 dark:text-indigo-400">
              <svg className="h-3 w-3 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              读取中…
            </span>
          )}
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
          无法读取审计日志：{error.message}
        </div>
      )}

      {loaded && commits.length === 0 && !error && (
        <div className="rounded-2xl border border-dashed border-slate-300 p-12 text-center dark:border-slate-800">
          <p className="text-sm font-medium text-slate-600 dark:text-slate-300">当前仓库还没有提交记录</p>
        </div>
      )}

      {/* Commit Timeline View */}
      <div className="relative pl-6 before:absolute before:top-3 before:bottom-3 before:left-2.5 before:w-0.5 before:bg-slate-200 dark:before:bg-slate-800">
        <ul className="space-y-4">
          {commits.map((commit) => {
            const open = selected === commit.hash
            return (
              <li key={commit.hash} className="relative group">
                {/* Timeline Node Dot */}
                <span
                  className={`absolute -left-6 top-4 flex h-3.5 w-3.5 items-center justify-center rounded-full border-2 bg-white transition-all dark:bg-slate-900 ${
                    open
                      ? 'border-indigo-600 ring-4 ring-indigo-500/20'
                      : 'border-slate-300 group-hover:border-slate-400 dark:border-slate-700'
                  }`}
                />

                {/* Commit Card Container */}
                <div
                  className={`rounded-xl border bg-white p-4 shadow-2xs transition-all duration-200 dark:bg-slate-900/80 ${
                    open
                      ? 'border-indigo-300/80 ring-1 ring-indigo-500/20 dark:border-indigo-800/80'
                      : 'border-slate-200/80 hover:border-slate-300 dark:border-slate-800/80 dark:hover:border-slate-700'
                  }`}
                >
                  <button
                    type="button"
                    onClick={() => setSelected(open ? null : commit.hash)}
                    className="flex w-full items-start justify-between gap-3 text-left"
                  >
                    <div>
                      <span className="text-sm font-semibold text-slate-900 dark:text-slate-100">
                        {commit.subject || '(无提交说明)'}
                      </span>
                      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-slate-500 dark:text-slate-400">
                        <span className="font-mono text-[11px]">{commit.timestamp}</span>
                        <span className="inline-flex items-center gap-1 rounded bg-slate-100/70 px-1.5 py-0.2 font-mono text-[11px] text-slate-600 dark:bg-slate-800/70 dark:text-slate-300">
                          {sourceLabel(commit)}
                        </span>
                        <span>涉及文件: <strong className="font-mono text-slate-700 dark:text-slate-300">{commit.files ? commit.files.length : 0}</strong></span>
                      </div>
                    </div>

                    <div className="flex items-center gap-2 shrink-0">
                      <span className="rounded-md border border-slate-200/80 bg-slate-50 px-2 py-0.5 font-mono text-xs font-semibold text-slate-700 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-300">
                        {shortHash(commit.hash)}
                      </span>
                      <svg
                        className={`h-4 w-4 text-slate-400 transition-transform ${open ? 'rotate-180' : ''}`}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                        strokeWidth="2"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
                      </svg>
                    </div>
                  </button>

                  {/* Suspicious Pattern Alert */}
                  {commit.alerts && commit.alerts.length > 0 && (
                    <div className="mt-3 flex items-center gap-2 rounded-lg bg-amber-50/80 px-3 py-1.5 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
                      <svg className="h-4 w-4 shrink-0 text-amber-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M12 9v2m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                      </svg>
                      <span>可疑文本线索识别: {commit.alerts.join('、')}</span>
                    </div>
                  )}

                  {/* Expanded Diff Preview & Revert */}
                  {open && (
                    <div className="mt-4 border-t border-slate-100 pt-4 dark:border-slate-800">
                      <DiffPreview api={api} hash={commit.hash} />
                      <RevertPanel api={api} commit={commit} onChanged={onChanged} />
                    </div>
                  )}
                </div>
              </li>
            )
          })}
        </ul>
      </div>

      {cursor && (
        <div className="pt-2 text-center">
          <button
            type="button"
            onClick={loadMore}
            disabled={loading}
            className="rounded-lg border border-slate-300/80 bg-white px-4 py-2 text-xs font-semibold text-slate-700 shadow-2xs hover:bg-slate-50 active:scale-95 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
          >
            {loading ? '载入中…' : '加载更早的提交记录'}
          </button>
        </div>
      )}
    </section>
  )
}
