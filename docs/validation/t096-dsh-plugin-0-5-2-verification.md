# T096：`memctl-dsh` 0.5.2 真实会话复验与 0.5.3 修复

环境：macOS arm64（`MacBookPro.local`），DSH 0.2.1-rc.2（应用进程于 2026-10-20 10:52 重启），`memctl v0.9.0`（`~/.agent-memory/bin/memctl`）。真实会话使用本机 `desktop` profile 与 `memctl-dsh@0.5.2`（npm），记忆仓库 `~/memory`；隔离复验使用临时 `DSH_HOME`、临时 `AGENT_MEMORY_DIR`/`AGENT_MEMORY_STATE`（均在 `/tmp`）与记录 argv 的 `MEMCTL_DSH_BIN` 包装脚本，结束后全部删除，真实 profile 与真实记忆仓库未改动。

## 1. `0.5.2` 真实会话复验

安装状态：`~/.dsh/profiles/desktop/package.json` 的依赖为 `memctl-dsh: 0.5.2`，`node_modules/memctl-dsh/package.json` 的 `version` 为 `0.5.2`，`memctl install --status` 返回 `dsh: configured host=dsh@0.2.1-rc.2 plugin@0.5.2`。

会话 `session-36d9fbd5-6157-402c-ae1b-085ac3475e11` 的 `runtime-context` 快照：

| 轮次 | 快照序 | 时间 | section |
| --- | --- | --- | --- |
| 1 | `seq=10` | 10:27:23 | 无 `memctl:memory` |
| 2 | `seq=253` | 10:52:19 | 含 `memctl:memory` |

第 2 轮快照的 `memctl:memory` 文本以 `[agent-memory] repo=/Users/libo/memory sync=ok device=MacBookPro.local` 与 `[agent-memory] source tag: agent://MacBookPro.local/dsh/session-36d9fbd5-…` 开头，并包含 `INSTRUCTIONS` 与 `MEMORY.md` 正文；它是 `systemPrompt.context` 快照，不是 inbox 用户消息。

第 1 轮缺失注入的原因可由缓存与本次缺陷复现对上：会话开始的 `start` 在 02:26:25Z（本地 10:26:25）写入 `injected=true`，同期 `sync.json` 记录 `remoteState=pending`、`reasonCode=NETWORK_UNAVAILABLE`（02:26:27Z）。`start` 在远端同步失败时仍输出完整 envelope，但显式调用以退出码 `1` 结束；`0.5.2` 在解析 envelope 之前就因非零退出抛错，整段上下文被丢弃。宿主在 10:52 重启并恢复该会话后重新执行 `start`，这次同步成功（`lastSuccessAt=02:52:16Z`），上下文才被注册进第 2 轮快照。

隔离 profile 中同一路径的结果与上述一致：

- 远端已挂载但未确认私有（`start` 退出 `1`，stderr `REMOTE_NOT_APPROVED`，stdout 仍为合法 envelope）：`0.5.2` 的 `runtime-context` 只有 `working-directory:current`、`sandbox:policy`、`approval:policy`；`0.5.3` 多出 `memctl:memory`。
- 已确认私有的本地裸仓库（`start` 退出 `0`）：两个版本都注入 `memctl:memory`，并记录 `start` → `sync` → `end`，同步缓存 `remoteState=synced`。

`turn/end` 后的同步与收尾：隔离 headless 会话（模型请求因凭据无效失败，轮次仍以 `turn/end` 结束）的插桩 trace 显示 `turn/end` 后 1 ms 派生 `sync`，17 ms 后插件卸载终止在途 `sync` 并派生 detached `end` 完成最终同步（缓存 `lastSuccessAt=04:17:23Z`）。真实 `desktop` 会话只有一次成功同步的缓存证据（`02:52:16Z`，与 10:52 的重启恢复同期）；第 1 轮 `turn/end`（10:39:50）之后是否有独立同步无法从被覆盖的缓存判断。真实会话的收尾需要在应用退出时观察，本轮未重复 T052 的退出探针，仍以插件卸载兜底与单元测试为准。

## 2. `0.5.3` 修复

缺陷：`execStart` 在检查 `result.code` 之前就解析 stdout，导致「非零退出但 envelope 合法」的 `start` 被整体当作失败；这与 `memctl start` 的契约冲突——显式调用（不带 `--stdin`）在远端同步失败时以 `1` 退出，但上下文 body 已经写出。

修复（`plugins/dsh/index.js`）：

- 先读取 stdout。stdout 为空且退出码非零时仍按失败处理；stdout 非空时先解析 envelope，合法则保留上下文，只记录一条 `start exited with N; keeping the returned context` 警告。
- 与 `plugins/pi/index.js` 的既有处理一致；两处对同一信封的语义不再分叉。

测试：`plugins/dsh/test/index.test.js` 从 14 项增加到 15 项。新增「非零退出 + 合法 envelope 保留上下文」；原「start failure」改为「非零退出且无 envelope 仍按失败处理」，断言不变。

版本：`plugins/dsh/package.json` 与 `plugins/dsh/README.md` 升到 `0.5.3`；`memctl dsh plugin-path` 输出 `memctl-dsh-0.5.3.tgz`，解包后 `index.js` 与仓库源码逐字节一致。`0.5.3` 尚未发布到 npm，也没有随 Release 发布；本机 `desktop` profile 仍为 `0.5.2`。

## 3. 验证命令与结果

- `node --test plugins/dsh/test/*.test.js`：15 项全部通过。
- `node --test plugins/pi/test/*.test.js`：15 项全部通过（回归，未改动 pi）。
- `go test ./plugins/dsh/...`：通过；`memctl-dsh-0.5.3.tgz` 的清单、版本与运行时文件与源码一致。
- `go test ./...`（`GOCACHE` 指向临时目录）：全部包通过，`tests/integration` 216 秒。
- 隔离 profile：`dsh <profile> --from-default-profile headless` 建 profile，`dsh plugin --profile <profile> add <tgz>` 安装，`dsh --profile <profile> "hi"` 跑一轮；A/B 对照见第 1 节。
- `0.5.3` 的每轮同步与收尾：包装脚本的 argv 只出现 `start` 与 `end`，同步缓存仍为 `synced`。用只增加 trace 的临时插件副本复现了调用顺序：

  ```text
  04:17:23.147 turn/end id=session-a0a5833a-…
  04:17:23.148 sync spawn id=session-a0a5833a-…
  04:17:23.164 kill sync child id=session-a0a5833a-…
  04:17:23.164 end spawn id=session-a0a5833a-…
  ```

  headless 一次性会话在 `turn/end` 后 17 ms 就进入插件卸载：`turn/end` 确实触发了 `sync`，但 `dispose` 按 T054 的约定终止在途 `sync`，再由 detached `end` 完成最终同步（缓存 `lastSuccessAt=04:17:23Z`）。长驻的 `desktop` 会话不会在同一时刻卸载，`sync` 会自行完成；`end` 是幂等的后台同步，两条路径写同一缓存，任一条成功即可。

## 4. 未验证与遗留

- 首轮 `turn/end` 可能不触发 `sync`：包装脚本让 `start` 延迟 3 秒时，`0.5.2` 的记录为 `start` 派生在 `…601.844`、`turn/end` 在 `…605.396`、`end` 派生在 `…605.440`，中间没有 `sync`；收尾的 `end` 仍完成同步，数据不会丢失。把 `session/event` 监听器提前到 `await start` 之前后，同样的慢启动对照仍只记录 `start` 与 `end`，未出现预期的 `sync`，原因未定位，因此没有保留该改动；它作为已知限制留在插件文档。
- `agent/disposed` 事件本身仍无法在 DSH 0.2.1-rc.2 复现，由插件单元测试覆盖。
- `0.5.3` 未发布：npm `latest` 与 Release 附件仍是 `0.5.2`。发布需要新的 tag 与 npm 发布审批。
- 真实 `desktop` 会话只在 `0.5.2` 上复验；`0.5.3` 只有隔离 headless 证据。headless 会话在 `turn/end` 后立即卸载，`sync` 会被在途终止并由 `end` 接管，因此 `0.5.3` 的「长驻会话 `sync` 自行完成」未单独观测。
- macOS amd64、Linux 与 Windows 未验证；仅 `0.5.2` 的 tarball 与 Node 单元测试与平台无关。

## 结论

`0.5.2` 的注入、`turn/end` 同步与收尾在真实 `desktop` 会话与隔离 profile 上均有证据；同时暴露并定位了一个真实缺陷：远端同步失败导致的 `start` 非零退出会让插件丢弃整段记忆。`0.5.3` 修复该缺陷并用 A/B 对照与单元测试锁定；发布仍待决策。
