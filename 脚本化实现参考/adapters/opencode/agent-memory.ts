// 放到 ~/.config/opencode/plugins/agent-memory.ts（重启 opencode 后自动加载）
// 插件无法把 stdout 注入上下文，所以"读记忆"靠 ~/.config/opencode/AGENTS.md 里的指引；插件只负责 pull / 同步。
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
      if (event?.type === "session.created") await run("pull", p.info?.id) // 属性结构按 SDK 类型写，未实测
      if (event?.type === "session.idle") await run("sync", p.sessionID)
    },
  }
}
