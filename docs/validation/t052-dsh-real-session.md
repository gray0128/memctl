# T052：真实 DSH 会话验证（已完成）

环境：macOS arm64，DSH 0.2.1-rc.2；早期记录使用 `memctl-dsh@0.2.0` 与 `desktop` profile，会话 `session-16337cf7-3d27-4493-8c79-09ecea6e5fb0`，记忆仓库 `~/memory`；2026-10-17 22:35–22:49 的复验使用 `memctl-dsh@0.3.0` 与隔离 profile，见下文。

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

`T054` 已把插件卸载路径改为对仍 live 的 state 触发一次 detached `end`，已 disposed 的不重复，并随 `v0.2.1` 发布，见 [T054 记录](./stage-e.md#t054宿主会话结束映射与-dsh-收尾兜底) 与 [T055 发布记录](./stage-e.md#t055发布-v021)。该改动由插件单元测试覆盖；真实退出仍需重新安装 `memctl-dsh@0.2.1` 后复验。

### 插件 0.3.0 复验（2026-10-17 22:35–22:49）

环境：macOS arm64，DSH 0.2.1-rc.2，`memctl-dsh@0.3.0`，隔离 profile `t052`（headless）、`t052web`、`t052ui`，隔离 `AGENT_MEMORY_DIR=/tmp/t052-memory`、`AGENT_MEMORY_STATE=/tmp/t052-state`；`MEMCTL_DSH_BIN` 指向记录 `start`/`sync`/`end` 调用的包装脚本。为区分 `agent/disposed` 与插件卸载兜底，测试使用官方 `0.3.0` 的临时插桩副本，仅在 `created`、`disposed`、`dispose`、`end` 处追加 trace，不改变插件行为；UI 安装使用官方 tarball。测试 profile 在结束后删除，`desktop` 与真实记忆仓库未改动。

1. **Headless 正常退出**：`dsh --profile t052 "只回复 OK，不要调用工具。"` 因 headless profile 缺少凭据以 `MISSING_CREDENTIAL` 退出。调用日志恰好一次 `start`、`sync`、`end`；trace 为：

   ```text
   created source=startup
   dispose live=session-30e7695e-...
   end id=session-30e7695e-...
   ```

   没有 `disposed`，说明本次 `end` 来自 `T054` 的插件卸载兜底，而不是 `agent/disposed`。会话没有成功轮次，记忆仓库 HEAD 未变。

2. **Web 正常退出**：`dsh --profile t052web --port 0 --no-open` 启动后新建会话，调用日志只有 `start`；向 web host 发送 `SIGTERM` 后 trace 为 `dispose live=...` → `end`，调用日志再出现一次 `end`。没有 `disposed`，`end` 恰好一次。这验证了真实 web host 正常退出时的兜底收尾。

3. **Headless 恢复**：执行 `dsh --profile t052 --session-id session-30e7695e-... "继续"`。trace 为 `created source=resume`；调用日志为 `start`、`sync`、`end` 各一次；恢复前后记忆仓库 HEAD 均为 `9d03d0d`，没有重复提交。由于 headless 缺凭据，模型请求未发生，本项只确认恢复会重新执行 `start`；带凭据的恢复注入见第 4 项。

4. **Web 成功轮次与恢复**：在隔离 `t052web` profile 中新建会话，选择 `DeepSeek-V4.1-Flash`，发送「只回复 OK，不要调用工具。」。模型返回 `OK`，会话日志出现 `turn/end`（turn 1，`completed`）和随后的 `sync`。停止 web host 后重启同一 profile，打开已保存会话并再次发送同一提示，模型再次返回 `OK`，日志出现第二个 `turn/end` 和 `sync`。

   - 两次请求的会话日志都包含 `runtime-context` 的 `snapshot`，其中 `memctl:memory` section 的文本以 `[agent-memory] repo=/private/tmp/t052-memory sync=local ...` 开头；它是动态上下文快照，不是伪造的 inbox 用户消息。
   - 调用日志：首次 `start` 22:52:13、`sync` 22:52:28、`end` 22:52:53；重启后 `start` 22:53:29、`sync` 22:53:37、`end` 22:53:48。
   - 临时仓库 HEAD 保持 `d0dcf3b`；两轮都没有记忆写入，`sync` 未产生额外提交，恢复后也没有重复提交。

5. **memctl 缺失**：`MEMCTL_DSH_BIN` 指向不存在的路径。trace 为 `created` → `dispose live=...` → `end`，没有 `start`；DSH 仍以 `MISSING_CREDENTIAL` 结束，没有因插件错误崩溃。因为二进制不存在，`end` 只是一次失败的触发，不构成收尾同步。

6. **仓库未初始化**：`AGENT_MEMORY_DIR=/tmp/t052-no-repo`。调用日志为 `start`、`sync`、`end` 各一次；DSH 只报告 `MISSING_CREDENTIAL`，没有因 `NO_REPO` 中断会话。插件按设计把 `start` 失败写入自己的日志而不写宿主 stderr，因此本项确认的是「失败不阻断会话」，没有确认注入内容。

7. **UI 安装绝对 tarball**：在隔离 `t052ui` web profile 中打开「插件 → 添加插件」，输入 `/Users/libo/.agent-memory/dsh/memctl-dsh-0.3.0.tgz`。界面显示「已安装」、`版本 0.3.0`；点击「立即启用」后，`package.json` 记录该 tarball，`dsh --profile t052ui --dump-config` 出现 `memctl-dsh`。这补齐了 `I06` 的绝对 tarball 界面路径；npm 包名与绝对目录未在界面复验。

8. **锁忙**：人工持有 `/tmp/t052-memory/.git/memctl.lock` 时，headless 从 `start` 到退出约 245 秒，未在插件声明的 20 秒 `start` 超时内结束；测试被中止。该结果与「Hook 不阻塞会话」的目标不一致，但测试持锁脚本和插件超时的交互尚未隔离，不能判定为产品缺陷，也不计入通过。后续用可控短时持锁和单独日志复现。

### 0.4.0 桌面插件更新复验（2026-10-17 23:20–23:22）

`desktop` profile 的 `memctl-dsh` 已从 `0.3.0` 更新到 `0.4.0`。`memctl install --status` 返回：

```text
dsh: configured host=dsh@0.2.1-rc.2 plugin@0.4.0
  note: memctl-dsh is installed; dsh hooks.json is not needed
```

DSH 桌面进程在 23:20:16 重启；当前会话 `session-d0cf71d5-7fea-4b71-a82b-f39bb2392021` 的 `runtime-context` 快照于 23:22:21 记录 `memctl:memory`，文本以 `[agent-memory] repo=/Users/libo/memory sync=ok device=MacBookPro.lan` 开头。同步缓存的 `lastAttemptAt` 与 `lastSuccessAt` 均为 23:20:24，`remoteState=synced`。这表明 `0.4.0` 已在真实 desktop 会话加载并完成注入与同步。

该轮没有产生新的记忆提交；后续 `0.4.0` 受管写入、锁忙与断网复验见下节。

### 0.4.0 受管写入、锁忙与断网复验（2026-10-17 23:24–23:26）

环境：隔离 `t052web` profile、临时仓库 `/tmp/t052-memory`、临时状态 `/tmp/t052-state`，官方 `memctl-dsh@0.4.0` 的临时插桩副本（仅 trace `created`/`disposed`/`dispose`/`end`），`MEMCTL_DSH_BIN` 指向记录调用的包装脚本；模型为 `DeepSeek-V4.1-Flash`。

1. **轮次内受管写入**：提示模型用终端执行 `memctl read MEMORY.md --json` 和 `memctl write`。模型实际执行了终端命令并回复 `OK`；仓库产生提交 `61fd68e memctl write`，`MEMORY.md` 包含新增的 `t052 managed write test`。会话日志出现 `turn/end`（turn 1，`completed`），调用日志随后出现 `sync`；同步缓存 head 为 `61fd68e`，`lastAttemptAt` 为 23:24:55，工作树干净。这补齐了 H07 的轮次内受管写入提交。
2. **锁忙**：人工持有 `/tmp/t052-memory/.git/memctl.lock` 时，直接运行 `memctl sync --wait` 在 3.1 秒内返回 `BUSY: lock ... is held by another process`。在持锁期间再发送一轮提示，模型仍返回 `OK`，插件在 `turn/end` 后调用 `sync`，会话未被阻断；同步缓存没有写入新的成功时间，HEAD 仍为 `61fd68e`。这验证了锁忙失败不会回滚本地提交或阻塞会话。
3. **断网**：临时仓库配置一个已确认的本地远端并完成首推，然后把远端目录移走；下一轮模型仍返回 `OK`，插件调用 `sync` 后缓存记录 `remoteState=pending`、`reasonCode=NETWORK_UNAVAILABLE`、`head=61fd68e`、`localState=clean`。会话继续，本地提交保留。这验证了断网降级。
4. **`agent/disposed` 可达性**：会话头部「更多操作」菜单只有「下载 Session 日志」和「反馈」，没有关闭或删除会话入口；DSH 0.2.1-rc.2 未提供单会话销毁入口。正常 web/headless 退出仍只观察到插件 `dispose` 兜底，没有 `disposed` trace。因此 `agent/disposed` 事件本身无法在当前宿主复现，只能由插件单元测试覆盖；真实退出由 `T054` 的卸载兜底保证恰好一次 `end`。

## 未验证与已知限制

- `agent/disposed` 事件本身无法在当前 DSH 0.2.1-rc.2 复现：会话菜单没有关闭或删除入口，正常退出只触发插件 `dispose` 兜底。该事件由插件单元测试覆盖，真实退出由兜底保证一次 `end`；这是宿主限制，不按产品缺陷处理。
- 通过「插件 → 添加插件」安装 npm 包名与绝对目录路径。绝对 tarball 路径已通过界面完成；npm 包尚未发布，绝对目录未单独走界面。
- 插件卸载清理子进程、旧 hooks 共存与 DSH 范围外版本的界面拒绝。`T054`/`T048` 的假宿主测试覆盖部分行为，真实宿主未复验。
- macOS amd64、Linux 与 Windows 的运行时行为未验证；仅交叉编译。

## 结论

`H07`–`H10` 的真实宿主路径已覆盖：注入、成功轮次后的 `turn/end` 同步、轮次内受管写入提交、恢复注入、子代理跳过、正常退出兜底、锁忙与断网降级。`agent/disposed` 事件因 DSH 当前没有可触发的会话销毁入口而保留为单元测试覆盖，真实退出以插件卸载兜底为准。`I03`–`I06` 的 tarball 界面安装、版本探测与兼容拒绝已有证据；npm 包名和绝对目录界面仍未覆盖。

`T052` 按上述边界完成，不再把部分证据计入 `H07`–`H10` 全部通过；剩余项登记为已知限制。
