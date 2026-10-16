import { useEffect, useState } from 'preact/hooks'
import { revertBlockReason } from '../conflicts.js'
import { renderDiffHtml, summarizeDiff } from '../diff.js'

const PAGE_SIZE = 50

function shortHash(hash) {
  return hash ? hash.slice(0, 12) : ''
}

function sourceLabel(commit) {
  const parts = []
  if (commit.agent) parts.push(commit.agent)
  if (commit.device) parts.push(commit.device)
  if (commit.sessionId) parts.push(commit.sessionId)
  return parts.length > 0 ? parts.join(' · ') : '来源未知'
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
        // Diff2Html is imported on demand inside renderDiffHtml.
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
    return <p className="text-sm text-rose-700 dark:text-rose-300">无法读取 Diff：{error.message}</p>
  }
  if (!diff) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">正在加载 Diff…</p>
  }

  const summary = summarizeDiff(diff.files)

  return (
    <div className="space-y-2">
      <div className="text-xs text-slate-500 dark:text-slate-400">
        文件 {summary.files} · 新增 {summary.additions} · 删除 {summary.deletions}
        {summary.binary > 0 ? ` · 二进制 ${summary.binary}` : ''}
      </div>
      {diff.files.some((file) => file.binary) && (
        <ul className="space-y-1 text-xs text-slate-500 dark:text-slate-400">
          {diff.files
            .filter((file) => file.binary)
            .map((file) => (
              <li key={file.path} className="font-mono break-all">
                {file.path} · 二进制文件，不显示文本 Diff
              </li>
            ))}
        </ul>
      )}
      {html ? (
        <div
          className="diff-preview overflow-x-auto text-xs"
          // Diff2Html escapes the diff content, so memory text is rendered as
          // untrusted text. See web/src/diff.js.
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <p className="text-sm text-slate-500 dark:text-slate-400">该提交没有可显示的文本改动。</p>
      )}
    </div>
  )
}

// RevertPanel is the confirmed revert action of the audit view. It reads the
// current repository revision as the required baseRevision and posts
// POST /api/v1/audit/{hash}/revert. A root or merge commit is disabled up
// front; a conflict is reported as a restored-history state, not as a partial
// revert. See docs/04-ui-and-cli-specification.md section 4.
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
    <div className="mt-3 border-t border-slate-200 pt-3 dark:border-slate-800">
      <div className="flex flex-wrap items-center gap-2">
        {blocked ? (
          <span className="text-xs text-slate-500 dark:text-slate-400">{blocked}</span>
        ) : confirming ? (
          <>
            <button
              type="button"
              disabled={busy || api.readOnly}
              onClick={run}
              className="rounded bg-rose-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
            >
              确认回滚
            </button>
            <button
              type="button"
              onClick={() => setConfirming(false)}
              className="rounded border border-slate-300 px-3 py-1.5 text-sm dark:border-slate-700"
            >
              取消
            </button>
          </>
        ) : (
          <button
            type="button"
            disabled={busy || api.readOnly}
            onClick={() => setConfirming(true)}
            className="rounded border border-rose-300 px-3 py-1.5 text-sm text-rose-700 disabled:opacity-40 dark:border-rose-900 dark:text-rose-300"
          >
            回滚此提交
          </button>
        )}
        <span className="text-xs text-slate-500 dark:text-slate-400">
          回滚创建新提交，保留旧历史，需要当前仓库修订作为基线。
        </span>
      </div>
      {message && <p className={`mt-2 text-sm ${tones[message.tone]}`}>{message.text}</p>}
    </div>
  )
}

// AuditView is the audit and diff preview. It lists commit metadata
// from GET /api/v1/audit and lazily loads one commit's per-file diff from
// GET /api/v1/audit/{hash}/diff. A confirmed revert posts
// POST /api/v1/audit/{hash}/revert. See
// docs/04-ui-and-cli-specification.md section 4.
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
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">审计与 Diff</h1>
        <span className="text-sm text-slate-500 dark:text-slate-400">
          已加载 {commits.length} 个提交{loading ? ' · 读取中…' : ''}
        </span>
      </div>

      {error && <p className="text-sm text-rose-700 dark:text-rose-300">无法读取审计：{error.message}</p>}

      {loaded && commits.length === 0 && !error && (
        <p className="text-sm text-slate-500 dark:text-slate-400">当前仓库还没有提交。</p>
      )}

      <ul className="space-y-2">
        {commits.map((commit) => {
          const open = selected === commit.hash
          return (
            <li
              key={commit.hash}
              className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
            >
              <button
                type="button"
                onClick={() => setSelected(open ? null : commit.hash)}
                className="flex w-full items-baseline justify-between gap-3 text-left"
              >
                <span className="font-medium">{commit.subject || '(无提交说明)'}</span>
                <span className="shrink-0 font-mono text-xs text-slate-500 dark:text-slate-400">
                  {shortHash(commit.hash)}
                </span>
              </button>
              <div className="mt-1 flex flex-wrap items-baseline gap-x-3 text-xs text-slate-500 dark:text-slate-400">
                <span>{commit.timestamp}</span>
                <span>{sourceLabel(commit)}</span>
                <span>文件 {commit.files ? commit.files.length : 0}</span>
              </div>
              {commit.alerts && commit.alerts.length > 0 && (
                <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">
                  可疑内容提示：{commit.alerts.join('、')}
                </p>
              )}
              {open && (
                <div className="mt-3 border-t border-slate-200 pt-3 dark:border-slate-800">
                  <DiffPreview api={api} hash={commit.hash} />
                  <RevertPanel api={api} commit={commit} onChanged={onChanged} />
                </div>
              )}
            </li>
          )
        })}
      </ul>

      {cursor && (
        <button
          type="button"
          onClick={loadMore}
          disabled={loading}
          className="rounded border border-slate-300 px-3 py-1.5 text-sm disabled:opacity-40 dark:border-slate-700"
        >
          加载更早的提交
        </button>
      )}
    </section>
  )
}
