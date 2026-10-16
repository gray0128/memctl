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

const STATUS_STYLES = {
  configured: 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/50 dark:text-emerald-100',
  verified: 'bg-emerald-100 text-emerald-900 dark:bg-emerald-900/50 dark:text-emerald-100',
  not_installed: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200',
  needs_attention: 'bg-amber-100 text-amber-900 dark:bg-amber-900/50 dark:text-amber-100',
  unsupported: 'bg-rose-100 text-rose-900 dark:bg-rose-900/50 dark:text-rose-100',
}

const ACTION_STYLES = {
  create: 'text-emerald-700 dark:text-emerald-300',
  update: 'text-indigo-700 dark:text-indigo-300',
  skip: 'text-slate-500 dark:text-slate-400',
  manual: 'text-amber-700 dark:text-amber-300',
}

const TONES = {
  ok: 'text-emerald-700 dark:text-emerald-300',
  info: 'text-slate-500 dark:text-slate-400',
  warn: 'text-amber-700 dark:text-amber-300',
  error: 'text-rose-700 dark:text-rose-300',
}

// changedLines keeps the added and removed lines of a plan patch, dropping the
// unchanged context so the preview stays readable.
function changedLines(patch) {
  return String(patch || '')
    .split('\n')
    .filter((line) => line !== '' && line[0] !== ' ')
}

// AdapterView renders the 整理接入 adapter panel: detection status, the
// previewed install plan and the confirmed install of
// docs/04-ui-and-cli-specification.md section 4. Configuration success and
// host verification stay separate: installing only ever reports configured.
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
          text: '检测到一次中断的安装；安装会先恢复现场再继续。',
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
        setMessage({ tone: 'warn', text: '配置基线已过期，已重新探测；确认最新预览后再安装。' })
        try {
          const fresh = await api.installAdapter(plan.agent, { dryRun: true })
          setPlan(fresh.plan)
          setPendingRecovery(Boolean(fresh.pendingRecovery))
        } catch {
          // The warning above still explains that the preview must be reloaded.
        }
      } else {
        setMessage({ tone: 'error', text: requestError.message })
      }
    } finally {
      setBusy(false)
    }
  }

  if (error) {
    return <p className="text-sm text-rose-700 dark:text-rose-300">无法读取接入状态：{error.message}</p>
  }
  if (!adapters) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">正在探测宿主与插件…</p>
  }

  const current = adapterByAgent(adapters, selected)

  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">整理接入</h1>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          {api.readOnly ? '只读模式：安装入口已禁用' : '安装只写入接入配置，不修改记忆仓库'}
        </span>
      </div>

      <ul className="space-y-2">
        {adapters.map((adapter) => (
          <li
            key={adapter.agent}
            className="rounded-lg border border-slate-200 bg-white p-3 dark:border-slate-800 dark:bg-slate-900"
          >
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <span className="font-mono text-sm">{adapter.agent}</span>
                <span
                  className={`inline-flex rounded-full px-2 py-0.5 text-xs font-medium ${
                    STATUS_STYLES[adapter.status] || STATUS_STYLES.not_installed
                  }`}
                >
                  {statusLabel(adapter.status)}
                </span>
                <span className="text-xs text-slate-500 dark:text-slate-400">
                  宿主 {adapter.hostVersion || '未探测'} · 插件 {adapter.pluginVersion || '未使用'}
                </span>
              </div>
              <button
                type="button"
                onClick={() => preview(adapter.agent)}
                disabled={busy}
                className="rounded border border-slate-300 px-2 py-1 text-xs disabled:opacity-40 dark:border-slate-700"
              >
                预览计划
              </button>
            </div>
            {adapter.reason && (
              <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{adapter.reason}</p>
            )}
            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">
              能力：{(adapter.capabilities || []).map(capabilityLabel).join('、') || '无'} · 配置：
              {(adapter.configPaths || []).join('、')}
            </p>
          </li>
        ))}
      </ul>

      {selected && (
        <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h2 className="text-sm font-semibold">{current ? current.agent : selected} 安装计划</h2>
            <button
              type="button"
              onClick={install}
              disabled={busy || !canInstall(plan, api.readOnly, pendingRecovery)}
              className="rounded bg-indigo-600 px-3 py-1.5 text-sm font-medium text-white disabled:opacity-40"
            >
              安装接入配置
            </button>
          </div>
          {plan && plan.reason && (
            <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{plan.reason}</p>
          )}
          {!plan && !busy && <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">正在生成计划…</p>}
          {plan && (
            <ul className="mt-3 space-y-3">
              {planArtifacts(plan).map((artifact) => (
                <li key={artifact.path} className="border-t border-slate-100 pt-2 first:border-t-0 first:pt-0 dark:border-slate-800">
                  <div className="flex flex-wrap items-baseline gap-2 text-xs">
                    <span className={`font-medium ${ACTION_STYLES[artifact.action] || ''}`}>
                      {actionLabel(artifact.action)}
                    </span>
                    <span className="font-mono break-all">{artifact.path}</span>
                    <span className="text-slate-400">{artifact.format}</span>
                  </div>
                  {artifact.note && (
                    <p className="mt-1 text-xs text-amber-700 dark:text-amber-300">{artifact.note}</p>
                  )}
                  {artifact.patch && (
                    <pre className="mt-1 max-h-56 overflow-auto rounded bg-slate-50 p-2 text-xs dark:bg-slate-950">
                      {changedLines(artifact.patch)
                        .map((line) => `${line}\n`)
                        .join('')}
                    </pre>
                  )}
                </li>
              ))}
            </ul>
          )}
          {message && <p className={`mt-3 text-sm ${TONES[message.tone]}`}>{message.text}</p>}
        </div>
      )}
    </section>
  )
}
