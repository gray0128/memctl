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


### 隔离 headless 会话与失败路径

使用 `dsh t052 --from-default-profile headless` 创建隔离 `t052` profile，安装 `memctl-dsh@0.2.0`，以 `MEMCTL_DSH_BIN` 指向记录调用的包装脚本后运行：

```text
dsh --profile t052 "只回复 OK，不要调用工具。"
```

模型因未配置 `DEEPSEEK_API_KEY` 以 `MISSING_CREDENTIAL` 失败，但插件仍完成：

```text
1792238542	start --agent dsh --sid session-82589075-b5d5-4f86-9c29-9ec307f8b6c4 --json
1792238545	sync --agent dsh --sid session-82589075-b5d5-4f86-9c29-9ec307f8b6c4
```

会话日志中该轮 `turn/end` 的 `reason.kind` 为 `error`，同步仍被触发，说明失败轮次不会跳过同步。该进程在错误后直接退出，未观察到 `agent/disposed` 或 `end` 调用；因此父会话正常关闭时的收尾仍未验证，不能把本次结果当作 `agent/disposed` 通过。隔离 profile 与包装脚本在测试后已删除，`memctl` 二进制已恢复。

### 退出应用探针（2026-10-17 20:05）

为观察 `agent/disposed`，在 `~/.agent-memory/bin/memctl` 前放置记录调用的包装脚本，正常退出 DSH 后重新打开。探针日志只有：

```text
20:04:49  version
20:04:54  sync  --agent dsh --sid session-16337cf7-3d27-4493-8c79-09ecea6e5fb0
20:05:38  start --agent dsh --sid session-16337cf7-3d27-4493-8c79-09ecea6e5fb0 --json
20:05:53  start --agent dsh --sid session-cc826fba-a67d-49e1-9c4f-f9d9575f8b51 --json
```

`SingletonLock` 在 20:05:35 指向新 PID 4685，旧会话在 20:05:38 被 resume（`start` 与 `session/end-seed`），说明这次操作是退出并重启 DSH，不是关闭单个会话。旧 agent 随旧 host 退出，日志中没有 `end`。这只能说明当前插件在 host 退出路径上没有产生可观察的收尾调用，不能判定 `agent/disposed` 失败，也不能记为通过。

`T054` 已把插件卸载路径改为对仍 live 的 state 触发一次 detached `end`，已 disposed 的不重复，见 [T054 记录](../STATUS.md#t054宿主会话结束映射与-dsh-收尾兜底)。该改动由插件单元测试覆盖；真实退出仍需重新安装插件后复验。

## 未验证

- 父会话 `agent/disposed` 的 `end` 调用；隔离 headless 会话在模型错误后直接退出，退出应用探针也没有观察到 `end`。`T054` 的卸载兜底尚未在真实退出中复验。
- 恢复旧会话时重新注册上下文且不重复创建提交。
- 断网、锁忙、`memctl` 缺失时的失败降级。
- 通过「插件 → 添加插件」界面完成的安装；本次安装使用绝对 tarball 路径。

## 下一步

继续在可关闭的隔离 DSH 会话中验证 `agent/disposed` 与恢复流程；`T054` 已完成，重新安装 `memctl-dsh`，正常退出 DSH，确认探针日志恰好出现一次 `end`。失败路径使用隔离 `HOME`、`AGENT_MEMORY_STATE` 和可控远端构造。验证完成前 `T052` 保持「进行中」，不把部分证据计入 `H07`–`H10` 全部通过。
