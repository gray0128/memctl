# T052：真实 DSH 会话验证（进行中）

环境：macOS arm64，DSH 0.2.1-rc.2，`memctl-dsh@0.2.0`，`desktop` profile，会话 `session-16337cf7-3d27-4493-8c79-09ecea6e5fb0`，记忆仓库 `~/memory`。

## 已验证

### H08：上下文注入

本轮会话的模型上下文在首个请求前出现：

```text
[agent-memory] repo=/Users/libo/memory sync=ok device=MacBookPro.lan
[agent-memory] source tag: agent://MacBookPro.lan/dsh/session-16337cf7-3d27-4493-8c79-09ecea6e5fb0
```

同时包含 `INSTRUCTIONS` 与 `MEMORY.md` 内容。它是 `systemPrompt.context` 快照，不是伪造的 inbox 用户消息。`memctl install --status` 返回：

```text
dsh: configured host=dsh@0.2.1-rc.2 plugin@0.2.0
  note: memctl-dsh is installed; dsh hooks.json is not needed
```

### 每轮同步（H07/H09 部分）

会话日志中的第 5 轮 `turn/end` 时间为 `2026-10-17 19:59:37`（`seq=1929`，`reason.kind=completed`）；对应记忆仓库的同步缓存随后更新：

```json
{
  "remoteState": "synced",
  "localState": "clean",
  "head": "1d80a734e54da375c79e63e2f62c21381a0c6bd2",
  "lastAttemptAt": "2026-10-17T11:59:40Z",
  "lastSuccessAt": "2026-10-17T11:59:40Z"
}
```

`turn/end` 与同步缓存更新间隔约 3 秒，符合插件在提交后触发 `memctl sync` 的行为。未观察到 `memctl-dsh:` 错误日志。

### 子代理跳过（H10 部分）

通过 `subagent` 创建子代理 `f802fbbd-7672-49ef-9826-3092c11bcb0d`，其会话头包含：

```json
{"parentSession":"session-16337cf7-3d27-4493-8c79-09ecea6e5fb0","origin":"subagent","delegationDepth":1}
```

子代理会话中没有 `[agent-memory]` 内容；测试期间临时在 `memctl` 前放置记录调用的包装脚本，日志中没有该子代理的 `start`、`sync` 或 `end` 调用。测试后已恢复原 `memctl` 二进制，`memctl version` 仍为 `v0.2.0`。

## 未验证

- 父会话 `agent/disposed` 的 `end` 调用；需要关闭当前会话后从外部检查。
- 恢复旧会话时重新注册上下文且不重复创建提交。
- 断网、锁忙、`memctl` 缺失时的失败降级。
- 通过「插件 → 添加插件」界面完成的安装；本次安装使用绝对 tarball 路径。

## 下一步

继续在可关闭的隔离 DSH 会话中验证 `agent/disposed` 与恢复流程；失败路径使用隔离 `HOME`、`AGENT_MEMORY_STATE` 和可控远端构造。验证完成前 `T052` 保持「进行中」，不把部分证据计入 `H07`–`H10` 全部通过。
