import { useEffect, useState } from 'preact/hooks'
import {
  actionLabel,
  adapterByAgent,
  canInstall,
  capabilityLabel,
  installBaseline,
  planArtifacts,
  statusLabel,
  summarizeInstall,
} from '../adapters.js'
import { DreamPanel } from './DreamPanel.jsx'

const STATUS_CONFIG = {
  configured: {
    badge: 'bg-emerald-50 text-emerald-700 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
    label: '已配置',
  },
  verified: {
    badge: 'bg-emerald-50 text-emerald-700 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
    label: '已验证',
  },
  not_installed: {
    badge: 'bg-slate-100 text-slate-600 border-slate-200 dark:bg-slate-800 dark:text-slate-300 dark:border-slate-700',
    label: '未安装',
  },
  needs_attention: {
    badge: 'bg-amber-50 text-amber-700 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
    label: '需关注',
  },
  unsupported: {
    badge: 'bg-rose-50 text-rose-700 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/60',
    label: '不支持',
  },
}

const ACTION_CONFIG = {
  create: 'bg-emerald-50 text-emerald-700 border-emerald-200 dark:bg-emerald-950/40 dark:text-emerald-300',
  update: 'bg-indigo-50 text-indigo-700 border-indigo-200 dark:bg-indigo-950/40 dark:text-indigo-300',
  skip: 'bg-slate-100 text-slate-600 border-slate-200 dark:bg-slate-800 dark:text-slate-300',
  manual: 'bg-amber-50 text-amber-700 border-amber-200 dark:bg-amber-950/40 dark:text-amber-300',
}

const TONES = {
  ok: 'bg-emerald-50 text-emerald-800 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60',
  info: 'bg-slate-50 text-slate-700 border-slate-200/80 dark:bg-slate-900 dark:text-slate-300 dark:border-slate-800',
  warn: 'bg-amber-50 text-amber-800 border-amber-200/80 dark:bg-amber-950/40 dark:text-amber-300 dark:border-amber-800/60',
  error: 'bg-rose-50 text-rose-800 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/60',
}

function changedLines(patch) {
  return String(patch || '')
    .split('\n')
    .filter((line) => line !== '' && line[0] !== ' ')
}

export function AdapterView({ api, refreshKey }) {
  const [adapters, setAdapters] = useState(null)
  const [selected, setSelected] = useState(null)
  const [plan, setPlan] = useState(null)
  const [pendingRecovery, setPendingRecovery] = useState(false)
  const [busy, setBusy] = useState(false)
  const [message, setMessage] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    api
      .adapters()
      .then((result) => {
        if (cancelled) return
        setAdapters(result.adapters || [])
        setError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey])

  async function reload() {
    const result = await api.adapters()
    setAdapters(result.adapters || [])
  }

  async function preview(agent) {
    setSelected(agent)
    setPlan(null)
    setPendingRecovery(false)
    setMessage(null)
    setBusy(true)
    try {
      const result = await api.installAdapter(agent, { dryRun: true })
      setPlan(result.plan)
      setPendingRecovery(Boolean(result.pendingRecovery))
      if (result.pendingRecovery) {
        setMessage({
          tone: 'warn',
          text: '检测到中断的未竟安装；本次安装前将先恢复现场基线。',
        })
      }
    } catch (requestError) {
      setMessage({ tone: 'error', text: requestError.message })
    } finally {
      setBusy(false)
    }
  }

  async function install() {
    if (!plan) return
    setBusy(true)
    setMessage(null)
    try {
      const result = await api.installAdapter(plan.agent, {
        dryRun: false,
        expectedConfigRevisions: installBaseline(plan),
      })
      setMessage({ tone: 'ok', text: summarizeInstall(result) })
      await reload()
      const fresh = await api.installAdapter(plan.agent, { dryRun: true })
      setPlan(fresh.plan)
      setPendingRecovery(Boolean(fresh.pendingRecovery))
    } catch (requestError) {
      if (requestError.code === 'REVISION_MISMATCH' || requestError.code === 'PRECONDITION_REQUIRED') {
        setMessage({ tone: 'warn', text: '配置基线已过期，已重新探测；请确认最新预览后再安装。' })
        try {
          const fresh = await api.installAdapter(plan.agent, { dryRun: true })
          setPlan(fresh.plan)
          setPendingRecovery(Boolean(fresh.pendingRecovery))
        } catch {}
      } else {
        setMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setBusy(false)
    }
  }

  if (error) {
    return (
      <div className="rounded-xl border border-rose-200 bg-rose-50 p-4 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
        无法读取接入状态：{error.message}
      </div>
    )
  }
  if (!adapters) {
    return (
      <div className="rounded-2xl border border-slate-200/80 bg-white p-12 text-center shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
        <p className="text-xs text-slate-500 dark:text-slate-400">正在探测本地 Agent 宿主环境与接入配置文件…</p>
      </div>
    )
  }

  const current = adapterByAgent(adapters, selected)

  return (
    <section className="space-y-6">
      {/* Top Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/80 pb-4 dark:border-slate-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">整理与客户端接入</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            AI 整理建议审查与宿主 IDE / 终端适配器配置注入
          </p>
        </div>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          {api.readOnly ? '只读模式：安装入口已禁用' : '安装操作只更新接入配置，绝不触碰记忆仓库'}
        </span>
      </div>

      {/* AI Dreaming Proposal Panel */}
      <DreamPanel api={api} refreshKey={refreshKey} />

      {/* Adapters Section Title */}
      <div>
        <h2 className="text-xs font-bold uppercase tracking-wider text-slate-500 mb-3 dark:text-slate-400">
          宿主客户端适配矩阵
        </h2>

        {/* Adapters Grid */}
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {adapters.map((adapter) => {
            const cfg = STATUS_CONFIG[adapter.status] || STATUS_CONFIG.not_installed
            const isSelected = selected === adapter.agent

            return (
              <div
                key={adapter.agent}
                className={`relative flex flex-col justify-between rounded-xl border bg-white p-4 shadow-2xs transition-all duration-200 dark:bg-slate-900/80 ${
                  isSelected
                    ? 'border-indigo-500 ring-2 ring-indigo-500/20'
                    : 'border-slate-200/80 hover:border-slate-300 dark:border-slate-800/80 dark:hover:border-slate-700'
                }`}
              >
                <div>
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-mono text-sm font-bold text-slate-900 dark:text-white">
                      {adapter.agent}
                    </span>
                    <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] font-semibold ${cfg.badge}`}>
                      {statusLabel(adapter.status)}
                    </span>
                  </div>

                  <div className="mt-2 text-[11px] text-slate-500 dark:text-slate-400">
                    <div>宿主: <strong className="text-slate-700 dark:text-slate-300">{adapter.hostVersion || '未探测到可执行文件'}</strong></div>
                    <div>插件: <span className="text-slate-600 dark:text-slate-400">{adapter.pluginVersion || '未使用'}</span></div>
                  </div>

                  {adapter.reason && (
                    <p className="mt-2 rounded bg-slate-50 px-2 py-1 text-[11px] text-slate-600 dark:bg-slate-800/50 dark:text-slate-400">
                      {adapter.reason}
                    </p>
                  )}

                  <div className="mt-2 text-[11px] text-slate-400">
                    <div>支持能力: {(adapter.capabilities || []).map(capabilityLabel).join('、') || '无'}</div>
                    <div className="truncate" title={(adapter.configPaths || []).join(', ')}>
                      配置目标: {(adapter.configPaths || []).join('、')}
                    </div>
                  </div>
                </div>

                <div className="mt-4 pt-3 border-t border-slate-100 dark:border-slate-800">
                  <button
                    type="button"
                    onClick={() => preview(adapter.agent)}
                    disabled={busy || api.readOnly}
                    className="w-full rounded-lg border border-slate-300/80 bg-white py-1.5 text-xs font-semibold text-slate-700 shadow-2xs hover:bg-slate-50 active:scale-95 disabled:opacity-40 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200 dark:hover:bg-slate-700"
                  >
                    预览安装计划
                  </button>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      {/* Selected Adapter Plan Drawer */}
      {selected && (
        <div className="rounded-2xl border border-indigo-200/80 bg-white p-5 shadow-xs dark:border-indigo-900/60 dark:bg-slate-900">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-100 pb-3 dark:border-slate-800">
            <div>
              <div className="flex items-center gap-2">
                <span className="text-xs font-bold uppercase tracking-wider text-indigo-600 dark:text-indigo-400">
                  安装计划预览
                </span>
                <span className="font-mono text-sm font-bold text-slate-900 dark:text-slate-100">
                  {current ? current.agent : selected}
                </span>
              </div>
              {plan && plan.reason && (
                <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">{plan.reason}</p>
              )}
            </div>

            <button
              type="button"
              onClick={install}
              disabled={busy || !canInstall(plan, api.readOnly, pendingRecovery)}
              className="rounded-lg bg-indigo-600 px-4 py-2 text-xs font-semibold text-white shadow-2xs hover:bg-indigo-700 active:scale-95 disabled:cursor-not-allowed disabled:opacity-40"
            >
              {busy ? '正在安装…' : '应用并注入接入配置'}
            </button>
          </div>

          {!plan && !busy && (
            <p className="py-4 text-xs text-slate-500 dark:text-slate-400">正在生成配置补丁计划…</p>
          )}

          {plan && (
            <ul className="mt-4 space-y-4">
              {planArtifacts(plan).map((artifact) => (
                <li
                  key={artifact.path}
                  className="rounded-xl border border-slate-200/80 bg-slate-50/60 p-3.5 dark:border-slate-800 dark:bg-slate-950/40"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className={`inline-flex rounded border px-1.5 py-0.2 text-[10px] font-semibold ${ACTION_CONFIG[artifact.action] || ''}`}>
                        {actionLabel(artifact.action)}
                      </span>
                      <span className="font-mono text-xs font-semibold text-slate-800 dark:text-slate-200 break-all">
                        {artifact.path}
                      </span>
                    </div>
                    <span className="rounded bg-slate-200/70 px-1.5 py-0.2 font-mono text-[10px] text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                      格式: {artifact.format}
                    </span>
                  </div>

                  {artifact.note && (
                    <p className="mt-2 rounded bg-amber-50 p-2 text-xs text-amber-800 dark:bg-amber-950/40 dark:text-amber-300">
                      💡 {artifact.note}
                    </p>
                  )}

                  {artifact.patch && (
                    <div className="mt-2.5">
                      <pre className="max-h-56 overflow-auto rounded-lg border border-slate-200/80 bg-slate-900 p-2.5 font-mono text-[11px] leading-relaxed text-slate-100 shadow-inner dark:border-slate-800">
                        {changedLines(artifact.patch)
                          .map((line) => `${line}\n`)
                          .join('')}
                      </pre>
                    </div>
                  )}
                </li>
              ))}
            </ul>
          )}

          {message && (
            <div className={`mt-4 rounded-xl border px-4 py-2.5 text-xs font-medium ${TONES[message.tone] || TONES.info}`}>
              {message.text}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
