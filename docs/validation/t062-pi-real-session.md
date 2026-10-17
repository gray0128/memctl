# T062：真实 pi 会话验证（进行中）

环境：macOS arm64，pi `1.3.0`，`memctl-pi@0.5.0`，memctl `v0.5.0`；真实记忆仓库 `~/memory`，模型 `deepseek/deepseek-flash`。本机 pi 已从旧 `pi-yaml-hooks` 路径迁移到原生插件。

## 迁移记录

- 旧 `~/.pi/agent/hook/hooks.yaml` 备份到 `~/.agent-memory/backups/pi-hooks-20261018-004927.yaml`。
- `memctl pi plugin-dir` 输出 `/Users/libo/.agent-memory/pi`，并由 `pi install` 写入 `~/.pi/agent/settings.json`。
- 旧 `agent-memory-context`、`agent-memory-sync`、`agent-memory-end` 条目已移除；`hooks.yaml` 保留为 `hooks: []`，避免旧插件对空 `hooks:` 报解析错误。
- `pi-yaml-hooks` 包仍保留，当前不承载本工具条目。
- `memctl install --status` 返回：

```text
pi: configured host=pi@1.3.0 plugin@0.5.0
  note: memctl-pi is installed; legacy host configuration is not needed
```

## 已验证

### 注入（H13 部分）

执行 `pi --model deepseek/deepseek-flash --no-tools -p "Reply with exactly: OK"`，退出码 `0`，模型回复 `OK`。会话文件 `2026-10-17T16-49-40-669Z_01a14ac4-e73c-725a-a706-6663c084c199.jsonl` 的 system sections 中存在 `<memctl>`，正文包含：

```text
[agent-memory] repo=/Users/libo/memory sync=ok device=MacBookPro.lan
[agent-memory] source tag: agent://MacBookPro.lan/pi/01a14ac4-e73c-725a-a706-6663c084c199
```

随后是 `INSTRUCTIONS` 与 `MEMORY.md` 内容，未伪造 user message。

### 收尾与同步（H12 部分）

进程退出触发 `end`；同步缓存随后更新：

```json
{
  "remoteState": "synced",
  "localState": "clean",
  "head": "1d80a734e54da375c79e63e2f62c21381a0c6bd2",
  "lastAttemptAt": "2026-10-17T16:49:47Z",
  "lastSuccessAt": "2026-10-17T16:49:47Z"
}
```

`agent_settled` 触发的 `sync` 在 `-p` 立即退出时被 shutdown 取消并记录 `sync exited with SIGTERM`；最终同步由 `end` 完成。该行为符合 [§6.9](../08-pi-plugin.md#69-进程超时与输出) 的设计，交互式会话的 `sync` 完成路径仍待验证。

### 交互式写入与同步（H12 部分）

用户在真实 pi TUI 会话 `01a14ac7-3efe-77d2-acd2-e93421199c6c` 中通过 `memctl write` 写入两次，产生提交 `ff2d05d`（00:56:55）与 `4a0f82a`（00:57:04）。同步缓存随后更新：

```json
{
  "remoteState": "synced",
  "localState": "clean",
  "head": "4a0f82aa1543bc35ab053b1a00876255675a949e",
  "lastSuccessAt": "2026-10-17T16:57:07Z"
}
```

会话文件的 system sections 同样包含 `<memctl>` 与 `[agent-memory]` 正文。同步发生在提交后 3 秒内；当前日志不能区分该次同步由 `agent_settled` 还是退出时的 `end` 触发，但可确认本地提交没有丢失、远端已同步。

## 未验证

- 交互式会话中 `agent_settled` 的 `sync` 完成与提交、推送。
- `resume`、`/new`、`/fork`、子代理的 SID、注入与去重。
- 二进制缺失、锁忙、断网、`start` 超时时的失败降级。
- `sections` 缺失的低版本 pi 回退。
