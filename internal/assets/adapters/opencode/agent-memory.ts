// memctl 管理的 opencode 插件。放到 ~/.config/opencode/plugins/agent-memory.ts，
// 重启 opencode 后自动加载。插件无法把 stdout 注入上下文，读记忆依靠全局 AGENTS.md 指引。
// 该文件由 memctl install opencode 写入；重复安装内容一致时不重写。
import { spawn } from "node:child_process"
import type { Plugin } from "@opencode-ai/plugin"

const MEMCTL = `${process.env.HOME}/.agent-memory/bin/memctl`

const run = (event: string, sid?: string) =>
  new Promise<void>((resolve) => {
    const child = spawn(MEMCTL, [event, "--agent", "opencode", "--sid", sid ?? ""], {
      stdio: "ignore",
    })
    child.on("error", () => resolve())
    child.on("exit", () => resolve())
  })

// OpenCode 1 loads the named callable plugin and emits session.created/session.idle.
export const AgentMemory: Plugin = async ({ $ }) => {
  const runV1 = async (event: string, sid?: string) => {
    try {
      await $`${MEMCTL} ${event} --agent opencode --sid ${sid ?? ""}`.quiet()
    } catch {
      /* 同步失败不应影响会话 */
    }
  }
  return {
    event: async ({ event }: any) => {
      const p = event?.properties ?? {}
      if (event?.type === "session.created") await runV1("pull", p.info?.id)
      if (event?.type === "session.idle") await runV1("sync", p.sessionID)
    },
  }
}

// OpenCode 2 requires a default object with an id and setup function. Its
// event stream uses session.execution.started/succeeded instead of the v1
// session.created/session.idle pair.
const setup = async (ctx: any) => {
  if (typeof ctx?.event?.subscribe !== "function") return async () => {}

  const controller = new AbortController()
  const consume = (async () => {
    for await (const input of ctx.event.subscribe({ signal: controller.signal })) {
      if (controller.signal.aborted) break
      const type = input?.type ?? input?.event?.type
      const data = input?.data ?? input?.event?.properties ?? {}
      if (type === "session.execution.started" || type === "session.created") {
        await run("pull", data.sessionID ?? data.info?.id ?? data.id)
      } else if (
        type === "session.execution.succeeded" ||
        type === "session.execution.failed" ||
        type === "session.idle"
      ) {
        await run("sync", data.sessionID ?? data.sessionId ?? data.info?.id)
      }
    }
  })().catch(() => {})

  return async () => {
    controller.abort()
    await consume
  }
}

export default {
  id: "agent-memory",
  setup,
}
