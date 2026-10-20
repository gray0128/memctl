import { DreamPanel } from './DreamPanel.jsx'

// The 整理 view carries only the Dreaming proposal workflow. Client adapter and
// MCP registration live in the separate 客户端接入 view, so an operator
// reviewing a proposal is not asked to reason about host configuration at the
// same time. See docs/04-ui-and-cli-specification.md section 4.
export function DreamView({ api, refreshKey }) {
  return (
    <section className="space-y-5">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/80 pb-4 dark:border-slate-800/80">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">记忆整理</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            两个后端的提案在此汇总：GitHub Actions 远端整理，或当前 Agent 会话内的本地交互式整理；人工审查后确认应用
          </p>
        </div>
        <span className="text-xs text-slate-500 dark:text-slate-400">
          {api.readOnly ? '只读模式：生成与确认已禁用' : '确认前绝不触碰主库'}
        </span>
      </div>

      <DreamPanel api={api} refreshKey={refreshKey} />
    </section>
  )
}
