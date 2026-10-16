// Adapter detection and install helpers for the 整理接入 view.
//
// The REST contract of docs/02-domain-models-and-contracts.md section 4.2
// returns an AdapterInstallPlan whose artifacts carry the configuration
// revision read during the preview. This module turns that preview into the
// expectedConfigRevisions baseline of a real install, so the view components
// stay free of request-shape details and can be unit tested without a DOM.

export const STATUS_LABELS = {
  unsupported: '不支持',
  not_installed: '未安装',
  configured: '已配置',
  verified: '已验证',
  needs_attention: '需要处理',
}

export const ACTION_LABELS = {
  create: '新建',
  update: '更新',
  skip: '已是最新',
  manual: '人工补丁',
}

export const CAPABILITY_LABELS = {
  hooks: '命令 Hook',
  config: '功能开关',
  plugin: '插件文件',
}

export function statusLabel(status) {
  return STATUS_LABELS[status] || status || '未知'
}

export function actionLabel(action) {
  return ACTION_LABELS[action] || action || '未知'
}

export function capabilityLabel(capability) {
  return CAPABILITY_LABELS[capability] || capability
}

export function adapterByAgent(adapters, agent) {
  return (adapters || []).find((item) => item.agent === agent) || null
}

// installBaseline builds expectedConfigRevisions from the previewed plan. Only
// a target that already exists carries a revision; a file that does not exist
// yet needs no baseline.
export function installBaseline(plan) {
  const revisions = {}
  for (const artifact of (plan && plan.artifacts) || []) {
    if (artifact.currentRevision) revisions[artifact.path] = artifact.currentRevision
  }
  return revisions
}

// canInstall reports whether the previewed plan has something to write and the
// session is not read-only. A pending interrupted install keeps the action
// enabled even when the previewed plan only skips, because the install request
// is what recovers it.
export function canInstall(plan, readOnly, pendingRecovery = false) {
  if (readOnly) return false
  return Boolean(plan && (plan.applicable || pendingRecovery))
}

// planArtifacts returns the previewed artifacts sorted so writes precede
// manual patches, keeping the view stable when the server reorders them.
export function planArtifacts(plan) {
  const order = { create: 0, update: 1, manual: 2, skip: 3 }
  return [...((plan && plan.artifacts) || [])].sort(
    (left, right) => (order[left.action] ?? 9) - (order[right.action] ?? 9),
  )
}

// summarizeInstall renders one line for the install result of
// POST /api/v1/adapters/{agent}/install.
export function summarizeInstall(result) {
  if (!result) return ''
  const parts = []
  const applied = result.applied || []
  if (applied.length > 0) {
    parts.push(`已写入 ${applied.length} 个文件`)
  }
  const manual = result.manual || []
  if (manual.length > 0) {
    parts.push(`需人工处理 ${manual.length} 个文件`)
  }
  const skipped = result.skipped || []
  if (skipped.length > 0 && applied.length === 0 && manual.length === 0) {
    parts.push('配置已是最新')
  }
  if (parts.length === 0) parts.push('没有可安装的变更')
  return `${statusLabel(result.status && result.status.status)}：${parts.join('，')}`
}
