// memctl 管理的 opencode 插件。放到 ~/.config/opencode/plugins/agent-memory.ts，
// 重启 opencode 后自动加载。插件无法把 stdout 注入上下文，读记忆依靠全局 AGENTS.md 指引。
// 该文件由 memctl install opencode 写入；重复安装内容一致时不重写。
import type { Plugin } from "@opencode-ai/plugin"

const MEMCTL = `${process.env.HOME}/.agent-memory/bin/memctl`

export const AgentMemory: Plugin = async ({ $ }) => {
  const run = async (ev: string, sid?: string) => {
    try {
      await $`${MEMCTL} ${ev} --agent opencode --sid ${sid ?? ""}`.quiet()
    } catch {
      /* 同步失败不应影响会话 */
    }
  }
  return {
    event: async ({ event }: { event: any }) => {
      const p = event?.properties ?? {}
      if (event?.type === "session.created") await run("pull", p.info?.id)
      if (event?.type === "session.idle") await run("sync", p.sessionID)
    },
  }
}
