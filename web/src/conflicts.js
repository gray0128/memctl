// Conflict-resolution and audit-revert helpers for stage C-2 subtask 2.
//
// The conflict view calls the same multi-stage state machine as the CLI
// `resolve` command through POST /api/v1/conflict:resolve and
// POST /api/v1/conflict:abort. The audit view calls
// POST /api/v1/audit/{hash}/revert. This module keeps the request shaping and
// the revert gating out of the components so both can be unit tested without a
// DOM. See docs/04-ui-and-cli-specification.md section 4 and
// docs/02-domain-models-and-contracts.md section 4.2.

// SUPPORTED_KINDS are the conflict kinds planResolved accepts. The server
// refuses binary and unsupported entries with UNSUPPORTED, so the view offers
// no text merge for them and points at manual resolution instead.
const SUPPORTED_KINDS = new Set(['text', 'delete_modify'])

// isUnsupportedConflict reports a path the UI must leave to manual handling.
export function isUnsupportedConflict(file) {
  return !file || !SUPPORTED_KINDS.has(file.kind)
}

// supportedConflictFiles returns the paths a stage can resolve in the UI.
export function supportedConflictFiles(conflict) {
  return ((conflict && conflict.files) || []).filter((file) => !isUnsupportedConflict(file))
}

// unsupportedConflictFiles returns the paths that block the resolve action.
export function unsupportedConflictFiles(conflict) {
  return ((conflict && conflict.files) || []).filter(isUnsupportedConflict)
}

// sideText returns the readable text of one captured side, or null when the
// side is absent or binary. An existing empty file returns an empty string, so
// an empty file is never confused with a deletion.
export function sideText(side) {
  if (!side || !side.exists || side.binary) return null
  return side.content === undefined || side.content === null ? '' : side.content
}

// defaultChoice is the initial merge decision for one supported file. It keeps
// the replayed local intent: the replayed content when the local commit has the
// file, and a deletion when the local commit deleted it. The operator can pick
// the upstream baseline or edit the text before confirming.
export function defaultChoice(file) {
  const replayed = file ? sideText(file.replayed) : null
  if (replayed !== null) return { action: 'put', rawContent: replayed }
  return { action: 'delete', rawContent: '' }
}

// initialChoices builds the per-path draft for every supported file.
export function initialChoices(conflict) {
  const choices = {}
  for (const file of supportedConflictFiles(conflict)) {
    choices[file.path] = defaultChoice(file)
  }
  return choices
}

// buildResolvedFiles turns the draft into the documented resolvedFiles array.
// It covers every supported file of the stage; the caller blocks the submit
// while an unsupported path exists.
export function buildResolvedFiles(conflict, choices) {
  return supportedConflictFiles(conflict).map((file) => {
    const choice = (choices && choices[file.path]) || defaultChoice(file)
    if (choice.action === 'delete') return { path: file.path, action: 'delete' }
    return { path: file.path, action: 'put', rawContent: choice.rawContent }
  })
}

// canResolve reports whether a stage can be submitted: no unsupported path and
// a decision for every supported path.
export function canResolve(conflict, choices) {
  if (!conflict || unsupportedConflictFiles(conflict).length > 0) return false
  const files = supportedConflictFiles(conflict)
  return files.length > 0 && files.every((file) => Boolean(choices && choices[file.path]))
}

// conflictPhaseLabel describes the captured or resolving progress of one stage.
export function conflictPhaseLabel(phase) {
  if (phase === 'resolving') return '重放进行中，等待处理本阶段冲突。'
  return '冲突已捕获，确认后从固定上游重放。'
}

// revertBlockReason reports why a commit cannot be reverted, or null when it
// can. The revert route refuses a root commit (0 parents) and a merge commit
// (2 or more parents) with UNSUPPORTED; the view disables the action for the
// same cases instead of relying on the error response.
export function revertBlockReason(commit) {
  const parents = (commit && commit.parents) || []
  if (parents.length === 0) return '根提交不支持回滚。'
  if (parents.length > 1) return '合并提交不支持回滚。'
  return null
}
