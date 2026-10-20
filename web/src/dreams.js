// Dreaming proposal helpers for the 整理 view.
//
// docs/04-ui-and-cli-specification.md section 4 requires the view to generate a
// proposal, preview the fixed diff, query its status and only apply an
// already-reviewed proposal whose patchHash is quoted. A stale proposal can
// never be applied and generation stays disabled until the repository is ready
// for the GitHub Actions backend.
// This module keeps that decision logic free of request-shape details so it can
// be unit tested without a DOM.

export const PROPOSAL_STATUS_LABELS = {
  generating: '生成中',
  ready: '待确认',
  applying: '应用中',
  applied: '已应用',
  stale: '已过期',
  failed: '失败',
}

export const CHANGE_ACTION_LABELS = {
  create: '新增',
  update: '更新',
  delete: '删除',
}

// The four built-in model providers of the GitHub Actions worker. The label is
// the provider name; the id is the `--runner` value the API accepts.
export const RUNNERS = [
  { id: 'deepseek', label: 'DeepSeek' },
  { id: 'minimax', label: 'MiniMax' },
  { id: 'openai', label: 'OpenAI' },
  { id: 'anthropic', label: 'Anthropic' },
]

export function proposalStatusLabel(status) {
  return PROPOSAL_STATUS_LABELS[status] || status || '未知'
}

export function changeActionLabel(action) {
  return CHANGE_ACTION_LABELS[action] || action || '未知'
}

// isTerminalProposal reports whether polling can stop.
export function isTerminalProposal(status) {
  return status === 'ready' || status === 'applied' || status === 'stale' || status === 'failed'
}

// readyHint explains why generation is disabled and names the one action the
// operator can take. The server reports the machine-readable cause.
export function readyHint(reason) {
  if (!reason) return '未配置 Actions 远端或工作流，生成能力已禁用'
  return `生成已禁用：${reason}`
}

// canGenerate disables generation in read-only mode and when the repository is
// not ready for the GitHub Actions backend (missing confirmed remote or missing
// workflow file), both of which the server would reject.
export function canGenerate(readOnly, backendReady) {
  return Boolean(!readOnly && backendReady)
}

// previewFailureDisablesBackend reports whether a preview rejection should latch
// generation off for the session. Only a missing backend prerequisite means the
// capability is unavailable; a provider failure or another UNSUPPORTED cause
// must not make the panel claim the repository is not ready.
export function previewFailureDisablesBackend(errorCode, backendReady) {
  return (errorCode === 'UNSUPPORTED' || errorCode === 'REMOTE_NOT_APPROVED') && !backendReady
}

// canApply reports whether a proposal is ready to confirm. A stale, failed or
// already applied proposal is never applyable again.
export function canApply(proposal, readOnly) {
  if (readOnly) return false
  return Boolean(proposal && proposal.status === 'ready' && proposal.patchHash && proposal.proposalId)
}

// applyPayload quotes exactly the reviewed proposal and its patch hash.
export function applyPayload(proposal) {
  return { proposalId: proposal.proposalId, patchHash: proposal.patchHash }
}

// changeSummary renders the ordered change list in one readable line.
export function changeSummary(proposal) {
  const changes = (proposal && proposal.changes) || []
  if (changes.length === 0) return '无变化'
  return changes.map((change) => `${changeActionLabel(change.action)} ${change.path}`).join('，')
}

// summarizeProposal renders one status line, keeping the review status and the
// applied commit separate.
export function summarizeProposal(proposal) {
  if (!proposal) return ''
  const parts = [proposalStatusLabel(proposal.status)]
  if (proposal.status === 'applied' && proposal.appliedCommit) {
    parts.push(`提交 ${proposal.appliedCommit.slice(0, 12)}`)
  }
  const changes = (proposal.changes || []).length
  if (changes > 0) parts.push(`${changes} 项变化`)
  if (proposal.warnings && proposal.warnings.length > 0) {
    parts.push(`${proposal.warnings.length} 条警告`)
  }
  return parts.join(' · ')
}

// RUNNER_LABELS names the backend that produced a proposal. The four remote
// providers are the `--runner` values of the GitHub Actions path;
// `local-agent` is the proposal produced by `memctl dream --propose` inside the
// current agent session. See docs/12-session-corpus-dreaming.md section 2.3.
export const RUNNER_LABELS = {
  'local-agent': '本地 Agent',
  deepseek: 'DeepSeek',
  minimax: 'MiniMax',
  openai: 'OpenAI',
  anthropic: 'Anthropic',
}

export function runnerLabel(runner) {
  return RUNNER_LABELS[runner] || runner || '未知'
}

// isLocalProposal reports whether the proposal came from the local current-agent
// path. Only that path records how many session digests were scanned and how
// many new memories survived the provenance guard, so the view shows those
// counters for local proposals only.
export function isLocalProposal(proposal) {
  return Boolean(proposal && proposal.runner === 'local-agent')
}

// proposalDetail renders the extra line of one listed proposal: the backend,
// the local corpus counters and, when the review ended without a commit, the
// failure reason. It never invents a count the server did not send.
export function proposalDetail(proposal) {
  if (!proposal) return ''
  const parts = [runnerLabel(proposal.runner)]
  if (isLocalProposal(proposal)) {
    parts.push(`扫描 ${proposal.sessionsScanned || 0} 个会话`)
    parts.push(`提炼 ${proposal.newMemoriesExtracted || 0} 条`)
  }
  if (proposal.error) parts.push(proposal.error)
  return parts.join(' · ')
}
