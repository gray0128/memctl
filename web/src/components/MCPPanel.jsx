import { useEffect, useState } from 'preact/hooks'
import { mcpAction, mcpAvailable, mcpReason, mcpTools } from '../adapters.js'

// MCPPanel shows the memctl MCP channel: whether the running binary serves the
// memory tools and which command registers it in each host. It only displays
// the probe result from GET /api/v1/mcp; memctl never runs a host's `mcp add`
// and never edits host MCP configuration, so the panel has no apply action.
// See docs/09-mcp-memory-tools.md section 5.3.
export function MCPPanel({ api, refreshKey, adapters }) {
  const [status, setStatus] = useState(null)
  const [error, setError] = useState(null)
  const [copied, setCopied] = useState(null)

  useEffect(() => {
    let cancelled = false
    api
      .mcpStatus()
      .then((result) => {
        if (cancelled) return
        setStatus(result)
        setError(null)
      })
      .catch((requestError) => {
        if (!cancelled) setError(requestError)
      })
    return () => {
      cancelled = true
    }
  }, [api, refreshKey])

  function copy(value) {
    if (typeof navigator === 'undefined' || !navigator.clipboard) return
    navigator.clipboard
      .writeText(value)
      .then(() => {
        setCopied(value)
        setTimeout(() => setCopied(null), 1500)
      })
      .catch(() => {})
  }

  // Detection status of the registered host, when the adapter probe knows it.
  // The MCP action is independent of hook configuration, so an unregistered
  // host still gets a command.
  function hostStatus(agent) {
    const adapter = (adapters || []).find((item) => item.agent === agent)
    return adapter ? adapter.status : null
  }

  const available = mcpAvailable(status)
  const registrations = (status && status.registrations) || []

  return (
    <section className="space-y-4 rounded-2xl border border-slate-200/80 bg-white p-5 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-100 pb-3 dark:border-slate-800">
        <div className="flex items-center gap-2.5">
          <div className="flex h-7 w-7 items-center justify-center rounded-lg bg-gradient-to-tr from-cyan-600 to-blue-600 text-white shadow-xs">
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M13 10V3L4 14h7v7l9-11h-7z" />
            </svg>
          </div>
          <div>
            <h2 className="text-sm font-bold tracking-tight text-slate-900 dark:text-white">MCP 记忆工具通道</h2>
            <span className="text-[11px] text-slate-500 dark:text-slate-400">
              宿主以子进程启动 <code className="font-mono">memctl mcp</code>，工具使用宿主权限写入记忆仓库，不受 Agent 的 bash 沙箱限制
            </span>
          </div>
        </div>
        {status && (
          <span
            className={`inline-flex items-center rounded-full border px-2.5 py-0.5 text-[10px] font-semibold ${
              available
                ? 'bg-emerald-50 text-emerald-700 border-emerald-200/80 dark:bg-emerald-950/40 dark:text-emerald-300 dark:border-emerald-800/60'
                : 'bg-rose-50 text-rose-700 border-rose-200/80 dark:bg-rose-950/40 dark:text-rose-300 dark:border-rose-800/60'
            }`}
          >
            {available ? '可用' : '不可用'}
          </span>
        )}
      </div>

      {error && (
        <div className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs font-semibold text-rose-700 dark:border-rose-900/60 dark:bg-rose-950/30 dark:text-rose-300">
          无法读取 MCP 状态：{error.message}
        </div>
      )}

      {!status && !error && (
        <p className="py-2 text-xs text-slate-500 dark:text-slate-400">正在探测当前二进制的 MCP 通道…</p>
      )}

      {status && (
        <>
          <dl className="grid grid-cols-1 gap-2 text-[11px] sm:grid-cols-3">
            <div>
              <dt className="text-slate-400">二进制</dt>
              <dd className="truncate font-mono text-slate-700 dark:text-slate-300" title={status.executable}>
                {status.executable || '未知'}
              </dd>
            </div>
            <div>
              <dt className="text-slate-400">版本</dt>
              <dd className="font-mono text-slate-700 dark:text-slate-300">{status.version || '未知'}</dd>
            </div>
            <div>
              <dt className="text-slate-400">工具</dt>
              <dd className="text-slate-700 dark:text-slate-300">{mcpTools(status)}</dd>
            </div>
          </dl>

          {!available && (
            <div className="rounded-xl border border-amber-200 bg-amber-50 p-3 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-950/30 dark:text-amber-300">
              二进制未响应 <code className="font-mono">mcp</code>：{mcpReason(status)}。升级或重新安装二进制后再注册，避免注册一个每次调用都失败的 server。
            </div>
          )}

          <div>
            <h3 className="mb-2 text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              各宿主注册动作
            </h3>
            <ul className="space-y-2">
              {registrations.map((registration) => {
                const action = mcpAction(registration)
                const detected = hostStatus(registration.agent)
                return (
                  <li
                    key={registration.agent}
                    className="rounded-xl border border-slate-200/80 bg-slate-50/60 p-3 dark:border-slate-800 dark:bg-slate-950/40"
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className="font-mono text-xs font-bold text-slate-800 dark:text-slate-200">
                          {registration.agent}
                        </span>
                        {detected && (
                          <span className="rounded bg-slate-200/70 px-1.5 py-0.2 text-[10px] text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                            宿主状态: {detected}
                          </span>
                        )}
                      </div>
                      {action && (
                        <button
                          type="button"
                          onClick={() => copy(action)}
                          className="rounded-lg border border-slate-300/80 bg-white px-2 py-1 text-[10px] font-medium text-slate-700 shadow-2xs hover:bg-slate-50 active:scale-95 dark:border-slate-700 dark:bg-slate-800 dark:text-slate-200"
                        >
                          {copied === action ? '已复制' : '复制'}
                        </button>
                      )}
                    </div>
                    {action && (
                      <code className="mt-2 block break-all rounded-lg border border-slate-200/80 bg-slate-50 p-2.5 font-mono text-[11px] leading-relaxed text-slate-800 dark:border-slate-800 dark:bg-slate-950 dark:text-slate-100">
                        {action}
                      </code>
                    )}
                    {registration.agent === 'codex' && (
                      <p className="mt-1.5 text-[11px] text-slate-500 dark:text-slate-400">
                        需要 <code className="font-mono">default_tools_approval_mode = "approve"</code>；<code className="font-mono">approval_policy = "never"</code> 会拒绝调用。
                      </p>
                    )}
                    {registration.agent === 'pi' && (
                      <p className="mt-1.5 text-[11px] text-slate-500 dark:text-slate-400">
                        默认 <code className="font-mono">codemode</code> 暴露不会向模型声明工具，注册时必须指定 <code className="font-mono">--exposure direct</code>。
                      </p>
                    )}
                    {registration.agent === 'grok' && (
                      <p className="mt-1.5 text-[11px] text-slate-500 dark:text-slate-400">
                        <code className="font-mono">workspace</code> profile 会阻断子进程，需要把记忆仓库加入 <code className="font-mono">read_write</code>。
                      </p>
                    )}
                  </li>
                )
              })}
            </ul>
            <p className="mt-2 text-[11px] text-slate-400">
              注册由宿主自己的命令完成；memctl 只探测并展示动作，不运行 <code className="font-mono">mcp add</code>，也不修改宿主配置。
            </p>
          </div>
        </>
      )}
    </section>
  )
}