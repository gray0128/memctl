import { useEffect, useState } from 'preact/hooks'
import {
  RUNNERS,
  applyPayload,
  canApply,
  canGenerate,
  changeSummary,
  isTerminalProposal,
  previewFailureDisablesSandbox,
  proposalStatusLabel,
  summarizeProposal,
} from '../dreams.js'

const TONES = {
  ok: 'text-emerald-700 dark:text-emerald-300',
  info: 'text-slate-500 dark:text-slate-400',
  warn: 'text-amber-700 dark:text-amber-300',
  error: 'text-rose-700 dark:text-rose-300',
}

const STATUS_STYLES = {
  ready: 'bg-indigo-100 text-indigo-900 dark:bg-indigo-900/50 dark:text-indigo-100',
  generating: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200',
  applying: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200',
  applied: 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/50 dark:text-emerald-100',
  stale: 'bg-amber-100 text-amber-900 dark:bg-amber-900/50 dark:text-amber-100',
  failed: 'bg-rose-100 text-rose-900 dark:bg-rose-900/50 dark:text-rose-100',
}

// DreamPanel renders the 整理接入 Dreaming panel of
// docs/04-ui-and-cli-specification.md section 4: proposal generation (disabled
// without a verified sandbox), the fixed diff preview, a status query and the
// confirmed apply of an already-reviewed proposal. A stale proposal is never
// applyable from the UI.
export function DreamPanel({ api, refreshKey }) {
  const [runner, setRunner] = useState(RUNNERS[0].id)
  const [proposalId, setProposalId] = useState('')
  const [lookup, setLookup] = useState('')
  const [proposal, setProposal] = useState(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState(null)
  const [error, setError] = useState(null)
  const [sandboxUnavailable, setSandboxUnavailable] = useState(false)
  const [reload, setReload] = useState(0)

  useEffect(() => {
    if (!proposalId) return undefined
    let cancelled = false
    let timer = null
    async function load() {
      try {
        const next = await api.dreamProposal(proposalId)
        if (cancelled) return
        setProposal(next)
        setError(null)
        if (!isTerminalProposal(next.status)) {
          timer = setTimeout(load, 1000)
        }
      } catch (requestError) {
        if (!cancelled) setError(requestError)
      }
    }
    load()
    return () => {
      cancelled = true
      if (timer) clearTimeout(timer)
    }
  }, [api, proposalId, refreshKey, reload])

  async function generate() {
    setBusy(true)
    setMessage(null)
    setError(null)
    setProposal(null)
    try {
      const started = await api.dreamPreview(runner, '')
      setProposalId(started.proposalId)
    } catch (requestError) {
      if (previewFailureDisablesSandbox(requestError.code, api.dreamSandbox)) {
        setSandboxUnavailable(true)
        setMessage({ tone: 'warn', text: `生成已禁用：${requestError.message}` })
      } else if (requestError.code === 'UNSUPPORTED') {
        setMessage({ tone: 'warn', text: requestError.message })
      } else {
        setMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setBusy(false)
    }
  }

  async function apply() {
    if (!proposal) return
    setBusy(true)
    setMessage(null)
    try {
      const result = await api.dreamApply(applyPayload(proposal))
      setMessage({ tone: 'ok', text: `已应用提案，提交 ${result.commit}` })
      setReload((current) => current + 1)
    } catch (requestError) {
      if (requestError.code === 'PROPOSAL_STALE') {
        setMessage({ tone: 'warn', text: '提案已过期，主库在生成后被修改；请重新生成提案。' })
        setReload((current) => current + 1)
      } else {
        setMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setBusy(false)
    }
  }

  function query() {
    const value = lookup.trim()
    if (!value) return
    setProposalId(value)
    setProposal(null)
    setError(null)
    setMessage(null)
    // Bump the reload key even when the id did not change, so querying the
    // proposal already on screen re-runs the effect instead of leaving the
    // panel in a permanent loading state.
    setReload((current) => current + 1)
  }

  const sandboxAvailable = api.dreamSandbox && !sandboxUnavailable
  const generationEnabled = canGenerate(api.readOnly, sandboxAvailable)

  return (
    <section className="space-y-3 rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-sm font-semibold">整理提案（Dreaming）</h2>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          {api.readOnly
            ? '只读模式：生成与确认已禁用'
            : sandboxAvailable
              ? '生成在隔离沙箱中运行，确认前不修改主库'
              : '本主机没有已验证的隔离沙箱，生成已禁用'}
        </span>
      </div>
      <p className="text-xs text-slate-500 dark:text-slate-400">
        选择 runner 即确认将导出的记忆文本发送给该 runner 的模型端点；确认应用前不会修改主库。
      </p>

      <div className="flex flex-wrap items-center gap-2">
        <select
          aria-label="runner"
          value={runner}
          onChange={(event) => setRunner(event.target.value)}
          disabled={busy || api.readOnly}
          className="rounded border border-slate-300 bg-white px-2 py-1 text-sm dark:border-slate-700 dark:bg-slate-950"
        >
          {RUNNERS.map((item) => (
            <option key={item.id} value={item.id}>
              {item.label}
            </option>
          ))}
        </select>
        <button
          type="button"
          onClick={generate}
          disabled={busy || !generationEnabled}
          className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
        >
          生成提案
        </button>
        <span className="text-xs text-slate-400">或</span>
        <input
          type="text"
          value={lookup}
          onInput={(event) => setLookup(event.target.value)}
          placeholder="查询已有提案 ID"
          className="rounded border border-slate-300 px-2 py-1 text-sm dark:border-slate-700 dark:bg-slate-950"
        />
        <button
          type="button"
          onClick={query}
          disabled={!lookup.trim()}
          className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
        >
          查询状态
        </button>
      </div>

      {error && <p className="text-sm text-rose-700 dark:text-rose-300">读取提案失败：{error.message}</p>}

      {proposalId && !proposal && !error && (
        <p className="text-sm text-slate-500 dark:text-slate-400">正在生成或读取提案 {proposalId}…</p>
      )}

      {proposal && (
        <div className="space-y-2 border-t border-slate-100 pt-3 dark:border-slate-800">
          <div className="flex flex-wrap items-center gap-2">
            <span
              className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
                STATUS_STYLES[proposal.status] || STATUS_STYLES.failed
              }`}
            >
              {proposalStatusLabel(proposal.status)}
            </span>
            <span className="font-mono text-xs">{proposal.proposalId}</span>
            <span className="text-xs text-slate-500 dark:text-slate-400">{summarizeProposal(proposal)}</span>
          </div>
          <p className="text-xs text-slate-500 dark:text-slate-400">变化：{changeSummary(proposal)}</p>
          {proposal.error && (
            <p className="text-xs text-rose-700 dark:text-rose-300">错误：{proposal.error}</p>
          )}
          {proposal.warnings && proposal.warnings.length > 0 && (
            <ul className="space-y-0.5 text-xs text-amber-700 dark:text-amber-300">
              {proposal.warnings.map((warning) => (
                <li key={warning}>{warning}</li>
              ))}
            </ul>
          )}
          {proposal.diff && (
            <div>
              <h3 className="text-xs font-medium text-slate-500 dark:text-slate-400">固定 Diff 预览</h3>
              <pre className="mt-1 max-h-72 overflow-auto rounded bg-slate-50 p-2 text-xs dark:bg-slate-950">
                {proposal.diff}
              </pre>
            </div>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              onClick={apply}
              disabled={busy || !canApply(proposal, api.readOnly)}
              className="rounded bg-emerald-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
            >
              确认应用
            </button>
            {proposal.status === 'stale' && (
              <span className="text-xs text-amber-700 dark:text-amber-300">
                提案已过期，需重新生成后才能应用。
              </span>
            )}
            {proposal.status === 'applied' && proposal.appliedCommit && (
              <span className="text-xs text-slate-500 dark:text-slate-400">
                已提交 {proposal.appliedCommit}
              </span>
            )}
          </div>
        </div>
      )}

      {message && <p className={`text-sm ${TONES[message.tone]}`}>{message.text}</p>}
    </section>
  )
}
