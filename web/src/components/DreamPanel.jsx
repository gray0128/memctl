import { useEffect, useState } from 'preact/hooks'
import {
  RUNNERS,
  applyPayload,
  canApply,
  canGenerate,
  changeSummary,
  isLocalProposal,
  isTerminalProposal,
  previewFailureDisablesBackend,
  proposalDetail,
  proposalStatusLabel,
  readyHint,
  runnerLabel,
  summarizeProposal,
} from '../dreams.js'
import { formatTimestamp } from '../time.js'

const TONES = {
  ok: 'bg-emerald-50 text-emerald-800 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
  info: 'bg-slate-50 text-slate-700 border-slate-200/80 dark:bg-slate-900 dark:text-slate-300 dark:border-slate-800',
  warn: 'bg-amber-50 text-amber-800 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
  error: 'bg-rose-50 text-rose-800 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/60',
}

const STATUS_CONFIG = {
  ready: 'bg-indigo-50 text-indigo-700 border-indigo-200/80 dark:bg-indigo-950/50 dark:text-indigo-300 dark:border-indigo-900/60',
  generating: 'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800/60 dark:text-slate-200 dark:border-slate-700',
  applying: 'bg-slate-100 text-slate-700 border-slate-200 dark:bg-slate-800/60 dark:text-slate-200 dark:border-slate-700',
  applied: 'bg-emerald-50 text-emerald-700 border-emerald-200/80 dark:bg-emerald-950/50 dark:text-emerald-300 dark:border-emerald-900/60',
  stale: 'bg-amber-50 text-amber-700 border-amber-200/80 dark:bg-amber-950/50 dark:text-amber-300 dark:border-amber-900/60',
  failed: 'bg-rose-50 text-rose-700 border-rose-200/80 dark:bg-rose-950/50 dark:text-rose-300 dark:border-rose-900/60',
}

export function DreamPanel({ api, refreshKey }) {
  const [runner, setRunner] = useState(RUNNERS[0].id)
  const [proposalId, setProposalId] = useState('')
  const [lookup, setLookup] = useState('')
  const [proposal, setProposal] = useState(null)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState(null)
  const [error, setError] = useState(null)
  const [backendUnavailable, setBackendUnavailable] = useState(false)
  const [reload, setReload] = useState(0)
  const [listed, setListed] = useState([])
  const [listError, setListError] = useState(null)

  // The proposal table lists every persisted proposal, including the
  // `local-agent` ones written by `memctl dream --propose` in the current agent
  // session. Those are reviewable without a remote or the Actions workflow, so
  // the list must not depend on the generation readiness flag. See
  // docs/04-ui-and-cli-specification.md section 4.
  useEffect(() => {
    let cancelled = false
    api
      .dreamList(20)
      .then((result) => {
        if (cancelled) return
        setListed((result && result.proposals) || [])
        setListError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setListError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey, reload])

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
      if (previewFailureDisablesBackend(requestError.code, api.dreamSandbox)) {
        setBackendUnavailable(true)
        setMessage({ tone: 'warn', text: `生成已禁用：${requestError.message}` })
      } else if (requestError.code === 'UNSUPPORTED' || requestError.code === 'REMOTE_NOT_APPROVED') {
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
      setMessage({ tone: 'ok', text: `已成功应用提案，提交哈希 ${result.commit}` })
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
    select(value)
  }

  function select(value) {
    setProposalId(value)
    setProposal(null)
    setError(null)
    setMessage(null)
    setReload((current) => current + 1)
  }

  const backendReady = api.dreamSandbox && !backendUnavailable
  const generationEnabled = canGenerate(api.readOnly, backendReady)

  return (
    <section className="space-y-4 rounded-2xl border border-indigo-200/80 bg-white p-5 shadow-2xs dark:border-indigo-900/60 dark:bg-slate-900">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-100 pb-3 dark:border-slate-800">
        <div className="flex items-center gap-2.5">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-tr from-purple-600 to-indigo-600 text-white shadow-xs">
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M19.428 15.428a2 2 0 00-1.022-.547l-2.387-.477a6 6 0 00-3.86.517l-.318.158a6 6 0 01-3.86.517L6.05 15.21a2 2 0 00-1.806.547M8 4h8l-1 1v5.172a2 2 0 00.586 1.414l5 5c1.26 1.26.367 3.414-1.415 3.414H4.828c-1.782 0-2.674-2.154-1.414-3.414l5-5A2 2 0 009 10.172V5L8 4z" />
            </svg>
          </div>
          <div>
            <h2 className="text-sm font-bold tracking-tight text-slate-900 dark:text-white">AI 记忆整理提案（Dreaming）</h2>
            <span className="text-[11px] text-slate-500 dark:text-slate-400">
              {api.readOnly
                ? '只读模式：生成与确认已禁用'
                : backendReady
                  ? 'GitHub Actions 远端整理 · 确认前绝不触碰主库'
                  : readyHint(api.dreamReason)}
            </span>
          </div>
        </div>
      </div>

      <p className="text-xs text-slate-500 dark:text-slate-400">
        选择模型提供商并发起整理：受管记忆文本将在仓库的 GitHub Actions 中发送给该提供商的 API，整理结果回传到本地并重新校验；必须经操作者人工审查后方可应用。
      </p>

      {/* Persisted proposals, including the local-agent ones */}
      <div className="space-y-2 rounded-xl border border-slate-200/80 bg-slate-50/60 p-3 dark:border-slate-800 dark:bg-slate-950/40">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-xs font-bold tracking-tight text-slate-700 dark:text-slate-200">
            已保存提案
            <span className="ml-1.5 font-normal text-slate-400 dark:text-slate-500">
              {listed.length > 0 ? `${listed.length} 条` : ''}
            </span>
          </h3>
          <button
            type="button"
            onClick={() => setReload((current) => current + 1)}
            className="rounded-md border border-slate-300/80 bg-white px-2 py-0.5 text-[11px] font-medium text-slate-600 shadow-2xs hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-300"
          >
            刷新列表
          </button>
        </div>

        {listError && (
          <p className="text-[11px] font-semibold text-rose-700 dark:text-rose-300">
            读取提案列表失败：{listError.message}
          </p>
        )}

        {!listError && listed.length === 0 && (
          <p className="text-[11px] text-slate-500 dark:text-slate-400">
            尚无提案。可在当前 Agent 会话运行 `memctl dream --prepare` 与 `memctl dream --propose` 生成本地提案，或用上方提供商发起远端整理。
          </p>
        )}

        {listed.length > 0 && (
          <ul className="divide-y divide-slate-200/70 dark:divide-slate-800">
            {listed.map((item) => (
              <li key={item.proposalId}>
                <button
                  type="button"
                  onClick={() => select(item.proposalId)}
                  className={`flex w-full flex-col gap-0.5 rounded-lg px-2 py-1.5 text-left hover:bg-white dark:hover:bg-slate-900 ${
                    item.proposalId === proposalId ? 'bg-white dark:bg-slate-900' : ''
                  }`}
                >
                  <span className="flex flex-wrap items-center gap-1.5">
                    <span
                      className={`inline-flex rounded-full border px-1.5 py-0.5 text-[10px] font-semibold ${
                        STATUS_CONFIG[item.status] || STATUS_CONFIG.failed
                      }`}
                    >
                      {proposalStatusLabel(item.status)}
                    </span>
                    {isLocalProposal(item) && (
                      <span className="inline-flex rounded-full border border-violet-200/80 bg-violet-50 px-1.5 py-0.5 text-[10px] font-semibold text-violet-700 dark:border-violet-900/60 dark:bg-violet-950/50 dark:text-violet-300">
                        {runnerLabel(item.runner)}
                      </span>
                    )}
                    <span className="font-mono text-[11px] font-bold text-slate-800 dark:text-slate-200">
                      {item.proposalId}
                    </span>
                    <span className="ml-auto text-[10px] text-slate-400 dark:text-slate-500">
                      {formatTimestamp(item.createdAt)}
                    </span>
                  </span>
                  <span className="text-[11px] text-slate-500 dark:text-slate-400">
                    {proposalDetail(item)}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Control Strip */}
      <div className="flex flex-wrap items-center gap-2">
        <div className="flex items-center gap-1.5">
          <select
            aria-label="provider"
            value={runner}
            onChange={(event) => setRunner(event.target.value)}
            disabled={busy || api.readOnly}
            className="rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 text-xs font-semibold text-slate-800 shadow-2xs outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500/20 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-200"
          >
            {RUNNERS.map((item) => (
              <option key={item.id} value={item.id}>
                提供商: {item.label}
              </option>
            ))}
          </select>
          <button
            type="button"
            onClick={generate}
            disabled={busy || !generationEnabled}
            className="rounded-lg bg-indigo-600 px-3.5 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40"
          >
            {busy ? '等待 Actions 结果…' : '发起整理提案'}
          </button>
        </div>

        <span className="text-xs text-slate-400 px-1">或</span>

        <div className="flex items-center gap-1.5">
          <input
            type="text"
            value={lookup}
            onInput={(event) => setLookup(event.target.value)}
            placeholder="输入已有提案 Proposal ID"
            className="rounded-lg border border-slate-200/80 bg-white px-3 py-1.5 font-mono text-xs text-slate-900 shadow-2xs outline-none placeholder:text-slate-400 focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500/20 dark:border-slate-700 dark:bg-slate-950 dark:text-slate-100"
          />
          <button
            type="button"
            onClick={query}
            disabled={!lookup.trim()}
            className="rounded-lg border border-slate-300/80 bg-white px-3 py-1.5 text-xs font-medium text-slate-700 shadow-2xs hover:bg-slate-50 active:scale-95 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
          >
            查询状态
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
          读取提案失败：{error.message}
        </div>
      )}

      {proposalId && !proposal && !error && (
        <div className="flex items-center gap-2 py-2 text-xs text-slate-500 dark:text-slate-400">
          <svg className="h-4 w-4 animate-spin text-indigo-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>正在生成或读取提案 {proposalId}…</span>
        </div>
      )}

      {/* Proposal Card */}
      {proposal && (
        <div className="space-y-3 rounded-xl border border-slate-200/80 bg-slate-50/60 p-4 shadow-2xs dark:border-slate-800 dark:bg-slate-950/40">
          <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-200/70 pb-2.5 dark:border-slate-800">
            <div className="flex items-center gap-2">
              <span className={`inline-flex rounded-full border px-2.5 py-0.5 text-xs font-semibold ${STATUS_CONFIG[proposal.status] || STATUS_CONFIG.failed}`}>
                {proposalStatusLabel(proposal.status)}
              </span>
              <span className="font-mono text-xs font-bold text-slate-800 dark:text-slate-200">
                {proposal.proposalId}
              </span>
            </div>
            <span className="text-xs text-slate-500 dark:text-slate-400">{summarizeProposal(proposal)}</span>
          </div>

          <div className="text-xs text-slate-600 dark:text-slate-300">
            <strong>预期变动:</strong> {changeSummary(proposal)}
          </div>

          {proposal.error && (
            <p className="rounded-lg bg-rose-50 p-2.5 text-xs font-semibold text-rose-700 dark:bg-rose-950/40 dark:text-rose-300">
              错误: {proposal.error}
            </p>
          )}

          {proposal.warnings && proposal.warnings.length > 0 && (
            <div className="rounded-lg bg-amber-50 p-2.5 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
              <span className="font-semibold">提案警告:</span>
              <ul className="mt-1 list-inside list-disc space-y-0.5">
                {proposal.warnings.map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            </div>
          )}

          {proposal.diff && (
            <div className="space-y-1.5">
              <span className="text-xs font-semibold text-slate-500 dark:text-slate-400">固定 Diff 预览</span>
              <pre className="max-h-72 overflow-auto rounded-lg border border-slate-200/80 bg-slate-900 p-3 font-mono text-[11px] leading-relaxed text-slate-100 shadow-inner dark:border-slate-800">
                {proposal.diff}
              </pre>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-2 pt-1">
            <button
              type="button"
              onClick={apply}
              disabled={busy || !canApply(proposal, api.readOnly)}
              className="rounded-lg bg-emerald-600 px-4 py-1.5 text-xs font-semibold text-white shadow-2xs hover:bg-emerald-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {busy ? '正在写入…' : '审查通过，确认应用此提案'}
            </button>
            {proposal.status === 'stale' && (
              <span className="text-xs text-amber-600 dark:text-amber-400">
                ⚠️ 提案已过期，主库已发生新变动，需重新生成后方可应用
              </span>
            )}
            {proposal.status === 'applied' && proposal.appliedCommit && (
              <span className="rounded-md bg-emerald-50 px-2 py-0.5 text-xs text-emerald-700 dark:bg-emerald-950/40 dark:text-emerald-300">
                已提交至 Git: <code className="font-mono">{proposal.appliedCommit}</code>
              </span>
            )}
          </div>
        </div>
      )}

      {message && (
        <div className={`rounded-xl border px-4 py-2.5 text-xs font-medium ${TONES[message.tone] || TONES.info}`}>
          {message.text}
        </div>
      )}
    </section>
  )
}
