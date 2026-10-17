# T062：真实 pi 会话验证（已完成）

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

### 交互式会话的每次 run 同步（H12、H14）

用 `pi --mode rpc` 保持进程存活，通过包装命令记录 memctl 调用；真实模型 `deepseek/deepseek-flash` 完成三轮：

| 阶段 | SID | `start` | `agent_settled` 后 `sync` | 退出/切换时 `end` |
| --- | --- | --- | --- | --- |
| 首个会话 | `01a14ace-3ee3-71bd-a3fc-6527013f099c` | 一次，退出码 0 | 一次，退出码 0 | 一次 |
| `new_session` | `01a14ace-5ac7-71bd-a3fc-6529aa65a314` | 一次，退出码 0 | 一次，退出码 0 | 一次 |
| `clone`（分叉） | `01a14ace-7286-71bd-a3fc-652b49b0fb76` | 一次，退出码 0 | 一次，退出码 0 | 一次 |

`sync` 在 settle 后约 0.7 秒完成并返回 0，没有被 shutdown 取消。`new_session` 和 `clone` 各自获得新 SID，旧会话切换时只 `end` 一次，没有重复 `start` 或重复同步。

### 恢复会话（H15）

用 `pi --mode rpc --session <旧会话文件>` 恢复 `01a14ace-3ee3-71bd-a3fc-6527013f099c`。`get_state` 返回同一 SID；`start`、settle 后 `sync`、退出 `end` 各一次，退出码均为 0。

### 子代理（H15）

- 前台子代理：`pi-subagents` 在当前进程内运行子会话，会话记录中 `source=foreground`、`childIndex=0`，但未触发 `session_start`，其 system sections 不含 `memctl`；插件没有按 `parentSession` 跳过，也没有可注入的生命周期事件。
- 后台子代理：以 `PI_SUBAGENT_CHILD=1` 的子进程运行，触发独立 `session_start`，获得 SID `01a14ad1-46f7-77cb-98e3-4fe9bd9b55dd`，执行一次 `start` 与退出时一次 `end`；父会话 `01a14ad1-1cf9-70ab-99b5-2a1de35d78b3` 在 settle 后同步一次。
- 结论：后台子代理获得独立生命周期；前台子代理不注入记忆是宿主不触发会话事件导致的限制，不是 `parentSession` 误判。

### 失败降级（H14）

| 场景 | 结果 |
| --- | --- |
| `MEMCTL_PI_BIN=/nonexistent/memctl` | 会话正常完成；只记录 start/sync/end 的 `spawn ... ENOENT`，不注入伪造上下文 |
| `start` 输出非法 JSON | 会话正常完成，记录 `invalid JSON`，本轮不注入 |
| `start` 挂起 30 秒 | 首个请求在约 9.2 秒后继续，不注入，会话正常完成 |
| 记忆仓库锁被占用 | 会话正常完成，settle 后 `sync` 返回 0，不阻塞下一轮 |
| 远端不可达（错误代理） | `start` 返回退出码 1 但 envelope 仍含正文，注入成功并记录 `GIT_FAILED`；会话正常完成，本地提交保留 |

## 未验证

- `sections` 缺失的低版本 pi 回退只由单元测试覆盖；本机只有 pi `1.3.0`。
- 前台 in-process 子代理没有生命周期事件，无法由本插件注入记忆；是否支持取决于宿主 API。
