// Dreaming proposal helpers for the 整理接入 view.
//
// docs/04-ui-and-cli-specification.md section 4 requires the view to generate a
// proposal, preview the fixed diff, query its status and only apply an
// already-reviewed proposal whose patchHash is quoted. A stale proposal can
// never be applied and generation stays disabled without a verified sandbox.
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

export const RUNNERS = [
  { id: 'claude', label: 'Claude Code' },
  { id: 'codex', label: 'Codex' },
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

// canGenerate disables generation in read-only mode and without a verified
// isolation backend, both of which the server would reject.
export function canGenerate(readOnly, sandboxAvailable) {
  return Boolean(!readOnly && sandboxAvailable)
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
