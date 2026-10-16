# 阶段 B 实现审查报告

本报告记录阶段 B（`B-1` 至 `B-3`，对应任务 `T023`–`T026`）源码、测试与前端构建的全面审查结果。审查以 [方案索引](../README.md) 下的 01–06 规范为唯一权威契约，阶段 A 历史验收见 [阶段 A 已完成任务归档](./stage-a.md)，阶段 A 的实现审查与收口见 [阶段 A 实现审查报告](./stage-a-review.md)，当前进度见 [开发状态](../STATUS.md)。

## 审查结论与范围

- 审查范围：`internal/server`、`internal/watcher`、`internal/cli/ui.go` 与平台打开文件、`internal/app/readsvc.go`、`internal/git/log.go` 与 `diff.go`、`tests/integration/b1_server_test.go`–`b3_ui_test.go`、`web/` 全量前端源码与构建配置。
- 审查边界：阶段 A 已由 `T022` 评审并收口，本轮不做重复评审；对阶段 B 依赖的阶段 A 不变量（锁描述符继承、环境白名单、只读门禁、`wrap` 信号转发、锁释放语义）做回归抽查，结论见第 4 节。
- 验证环境：2026-10-16，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1。
- 验证命令：`make ui`、`go vet ./...`、`go test ./... -count=1`、`cd web && npm test` 全部通过；`tests/integration` 全量 170.8 秒通过，前端 6 个文件 19 项通过。为核实第 1 节问题构造了一次性 HTTP 探针用例，确认后已删除，工作树保持干净。
- 总体结论：阶段 B 已完成本地只读 HTTP 安全框架、SSE 状态流与内嵌前端只读视图的主链路；鉴权、`Host`/`Origin` 校验、安全响应头、只读拦截、路径边界、SSE 有界队列与断线校正、内嵌资产无外部 CDN、不可信文本渲染整体符合设计，且对阶段 A 契约的回归保持成立。
- 发现问题：共识别 **1 项高优先级问题（P1，破坏契约或安全边界）**、**2 项中优先级问题（P2，行为偏差或恢复语义缺失）** 与 **6 项低优先级问题（P3，边界条件、一致性与测试稳健性）**。进入阶段 C（`C-1`）前，建议先完成 P1 与 P2 收口。

| 编号 | 级别 | 涉及模块 | 问题摘要 | 关联契约与验收项 |
| --- | --- | --- | --- | --- |
| [P1-1](#p1-1audit-的-before-游标未校验即进入-git-argv构成选项注入) | P1 | `internal/server`、`internal/app`、`internal/git` | `GET /api/v1/audit` 的 `before` 查询参数未校验即追加到 `git log` 参数列表，可被用作 Git 选项注入（实测 `--output=<路径>` 可写任意文件） | [REST 契约](../02-domain-models-and-contracts.md#42-路由)、[错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)、`A01`、`A02` |
| [P2-1](#p2-1snapshot-在保护态只返回-busy且-stale-恒为-false) | P2 | `internal/app`、`internal/server` | `snapshot` 在 Git 操作、冲突或待恢复期间只返回 `503 BUSY`，`stale` 恒为 `false`，未实现「返回最近完整快照并标记 `stale: true`」分支，整个 UI 在该期间不可读 | [修订标识](../02-domain-models-and-contracts.md#11-修订标识)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、`P01`–`P03` 的 HTTP 读取场景 |
| [P2-2](#p2-2conflict-读路径使用阻塞取锁并放行任意-git-中间态) | P2 | `internal/app`、`internal/server` | `GET /api/v1/conflict` 走 `beginResolve`（阻塞取锁、放行任意 Git 中间态、不检查待恢复事务），与其余只读路由的非阻塞 `BUSY` 语义不一致 | [写入不变量](../01-architecture-and-modules.md#23-写入不变量)、[中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复)、`P01`–`P03` |
| [P3-1](#p3-1audit-的分页游标与非法-before-处理) | P3 | `internal/app` | `NextCursor` 在恰好取满时也返回，`agent` 过滤在分页后执行会产生空页；非法 `before` 返回 `GIT_FAILED`/500 而非参数错误 | [REST 契约](../02-domain-models-and-contracts.md#42-路由) |
| [P3-2](#p3-2审计来源缺少设备维度) | P3 | `internal/app`、`web/` | 提交消息只记录 `Agent`/`Session`，`AuditCommit.device` 与前端 `sourceLabel` 的设备维度永远为空 | [审计对象](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui) |
| [P3-3](#p3-3快照-health-计数与-doctor-计数不一致) | P3 | `internal/app`、`web/` | `snapshot.health` 只统计链接、孤立文件与入口膨胀，`info` 恒为 `0`，与 `doctor` 的 error/warning/info 计数不一致，概览与诊断两个视图给出不同数字 | [记忆快照](../02-domain-models-and-contracts.md#21-记忆快照)、[审计与诊断对象](../02-domain-models-and-contracts.md#23-审计诊断整理与接入) |
| [P3-4](#p3-4diffcommit-的逐文件调用与路径过滤语义) | P3 | `internal/git` | `DiffCommit` 逐文件启动 `git diff-tree`，大提交产生大量子进程；路径过滤未使用字面 pathspec，含 glob 元字符的文件名可能匹配到其他文件 | [技术依赖](../01-architecture-and-modules.md#技术依赖)、[REST 契约](../02-domain-models-and-contracts.md#42-路由) |
| [P3-5](#p3-5前端未展示-aheadbehind) | P3 | `web/` | TopBar 未展示 `aheadCount`/`behindCount`，未落实「未知时显示『未知』，不显示零」 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui) |
| [P3-6](#p3-6知识图谱在每次失效时重建-cytoscape-实例) | P3 | `web/` | `KnowledgeGraph` 依赖 `snapshot.graph` 的对象标识，每次 SSE 失效都销毁并重建 Cytoscape 实例与布局，重置平移缩放 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存) |

---

## 1. 高优先级问题（P1）

### P1-1：`audit` 的 `before` 游标未校验即进入 Git argv，构成选项注入

- **位置**：[`internal/server/handlers.go`](../../internal/server/handlers.go)（`handleAudit` 第 83–101 行）、[`internal/app/readsvc.go`](../../internal/app/readsvc.go)（`Audit` 第 284–320 行）、[`internal/git/log.go`](../../internal/git/log.go)（`Log` 第 27–63 行）。
- **关联契约**：[REST 契约](../02-domain-models-and-contracts.md#42-路由) 与 [应用与基础设施接口](../02-domain-models-and-contracts.md#3-应用与基础设施接口) 规定 HTTP 调用方「不能由客户端指定任意 Git 命令、refspec 或绝对路径」；`GET /api/v1/audit` 的 `before` 只应作为分页游标。
- **触发与机制**：
  1. `handleAudit` 直接把 `r.URL.Query().Get("before")` 放入 `AuditQuery.Before`，未做格式校验。
  2. `App.Audit` 原样传给 `git.Log`；`Log` 以 `args = append(args, before)` 把它追加到 `git log` 参数列表，位置在 `--name-only` 之后、结束符 `--` 之前。
  3. 由于该位置仍是选项解析区，`before` 可被解释为 Git 选项。实测 `GET /api/v1/audit?before=--output=/tmp/xxx` 返回 `200 {"commits":[]}`，同时由服务端启动的 `git log` 把完整提交日志写入 `/tmp/xxx`（探针证据：文件创建成功并含提交记录）。
- **影响后果**：持有有效 Session Token 的本地调用方（包括通过首页 meta 取得 Token 的同机进程）可借由该参数触发任意路径的 Git 输出写入，越过「客户端不能指定任意 Git 命令」的边界；同时非法游标（不存在或非十六进制）会以 `GIT_FAILED`/500 上报，掩盖真正的参数错误。
- **修复建议**：
  1. 在 HTTP 或应用层用与 [`internal/git/diff.go`](../../internal/git/diff.go) `commitArg` 相同的白名单（`^[0-9a-fA-F]{4,64}$`）校验 `before`，非法值返回 `INVALID_ARGUMENT`/400。
  2. 防御性加固 `git.Log`：在修订参数前加入 `--end-of-options`，避免同类调用再次把调用方数据放进选项区。
  3. 补充回归：新增 HTTP 用例断言 `?before=--output=...` 返回 400 且目标文件未被创建；检查 `git.CommittedPaths` 等其他接收哈希的函数在阶段 C 接入前复用同一校验。

---

## 2. 中优先级问题（P2）

### P2-1：`snapshot` 在保护态只返回 `BUSY`，且 `stale` 恒为 `false`

- **位置**：[`internal/app/readsvc.go`](../../internal/app/readsvc.go)（`Snapshot` 第 214–234 行）、[`internal/server/handlers.go`](../../internal/server/handlers.go)（`handleSnapshot` 第 16–30 行）、[`internal/app/mutation.go`](../../internal/app/mutation.go)（`beginProtected` 第 528–558 行）。
- **关联契约**：[修订标识](../02-domain-models-and-contracts.md#11-修订标识) 规定「快照从同一次受控读取生成。Git 操作或恢复期间返回最近完整快照并标记 `stale: true`；没有完整快照时返回 `BUSY`」；[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 的五个视图在过期时也应保留可读状态。`03` §2.1 进一步规定事务期间「UI 使用带 `stale` 标记的最近完整快照，CLI 读取返回忙碌」。
- **触发与机制**：`Snapshot` 先调用 `beginRead`；`beginRead` 在锁忙、存在未决冲突、存在待恢复事务或 Git 中间态时直接返回错误，`handleSnapshot` 把 `Busy`/`OperationActive`/`RecoveryRequired` 一律映射为 `503 BUSY`。`Snapshot` 的 `Stale` 字段被硬编码为 `false`，全项目没有缓存「最近完整快照」的实现。
- **影响后果**：同步、rebase、冲突解决或 `recovery_required` 期间，`/api/v1/snapshot` 不可用，依赖快照的概览与知识图谱视图同时失效；契约要求的 `stale: true` 分支从未出现，UI 无法区分「保护态下的旧快照」与「仓库不可读」。
- **修复建议**：在服务层缓存最近一次成功快照与修订（进程内即可，随写事务与监听失效）；保护态下优先返回该快照并置 `stale: true`，仅在从未获得完整快照时返回 `BUSY`。补充覆盖 rebase 中途与 `recovery_required` 的 HTTP 用例。

### P2-2：`conflict` 读路径使用阻塞取锁并放行任意 Git 中间态

- **位置**：[`internal/app/readsvc.go`](../../internal/app/readsvc.go)（`ConflictState` 第 350–368 行）、[`internal/app/resolve.go`](../../internal/app/resolve.go)（`beginResolve` 第 514–527 行）、[`internal/lock/lock.go`](../../internal/lock/lock.go)（`Acquire` 第 33–57 行）。
- **关联契约**：[写入不变量](../01-architecture-and-modules.md#23-写入不变量) 与 [中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复) 要求读取在保护态给出可预期的忙碌结果；[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全) 要求限制空闲连接，读取请求不应在锁上长时间等待。
- **触发与机制**：`GET /api/v1/conflict` 走 `beginResolve`，最终调用 `beginLocked(requireEntry=true, allowGitState=true, background=false)`：锁由 `lock.Acquire` 以默认 3 秒预算阻塞获取（其余只读路由使用 `TryAcquire` 立即返回 `BUSY`）；同时 `allowGitState=true` 跳过 `IndexLocked`、`Unmerged` 与进行中操作检查，且不检查 `store.Pending()`。
- **影响后果**：当另一个 `memctl` 进程正在长同步持锁时，该请求最多阻塞 3 秒才返回 `BUSY`，与快照、条目等路由的即时语义不一致；放行 `IndexLocked`/`Unmerged` 后，它可能在其他只读路由返回 `BUSY` 的同时照常给出冲突快照，缺少统一的忙碌判定。
- **修复建议**：明确该路由「读取冲突需要放行本工具拥有的 rebase 中间态」这一唯一豁免，改用非阻塞取锁；锁忙时返回 `BUSY`，并与其他只读查询共用同一份保护态判定。补充「长同步持锁期间 `GET /api/v1/conflict` 在预算内返回 `BUSY`」的用例。

---

## 3. 低优先级问题（P3）

### P3-1：`audit` 的分页游标与非法 `before` 处理

- **位置**：[`internal/app/readsvc.go`](../../internal/app/readsvc.go)（`Audit` 第 284–320 行）、[`internal/git/log.go`](../../internal/git/log.go)（`Log` 第 27–63 行）。
- **触发与机制**：
  1. `NextCursor` 只要 `len(commits) == query.Limit` 就写入，剩余条目恰好等于 `limit` 时仍会给出游标，客户端多发一次空页请求。
  2. `agent` 过滤在取满分页之后执行，过滤后条目可远少于 `limit`，但游标基于未过滤结果，UI 会出现「已加载 0 个提交」但仍显示「加载更早」的中间页。
  3. 非法或未知 `before` 会触发 `git log` 失败，最终以 `GIT_FAILED`/500 上报而非 `INVALID_ARGUMENT`/400（与 P1-1 同源）。
- **修复建议**：多取一条以判定是否还有下一页；把 `agent` 过滤下推到读取或明确分页语义；非法 `before` 统一返回参数错误。

### P3-2：审计来源缺少设备维度

- **位置**：[`internal/app/mutation.go`](../../internal/app/mutation.go)（`commitMessage` 第 683–696 行）、[`internal/app/readsvc.go`](../../internal/app/readsvc.go)（`toAuditCommit` 第 662–700 行）、[`web/src/components/AuditView.jsx`](../../web/src/components/AuditView.jsx)（`sourceLabel`）。
- **触发与机制**：受管写入的提交消息只写入 `Memctl-Transaction`、`Agent` 与 `Session`；`MutationRequest` 也没有设备字段。`toAuditCommit` 与前端 `sourceLabel` 都读取 `device`，因此该维度始终为空，审计的「可解析来源」只剩 Agent 与 SID。
- **修复建议**：在写入事务的提交消息中记录规范化设备标识（与 `agent://<device>/<agent>/<sid>` 同源），或在契约中明确首版不提供设备维度并移除 UI 的空字段渲染。

### P3-3：快照 `health` 计数与 `doctor` 计数不一致

- **位置**：[`internal/app/readsvc.go`](../../internal/app/readsvc.go)（`HealthDTO` 第 123–131 行、`scanVault` 第 429–520 行）、[`web/src/components/Overview.jsx`](../../web/src/components/Overview.jsx)、[`web/src/components/DiagnosticsView.jsx`](../../web/src/components/DiagnosticsView.jsx)。
- **触发与机制**：`scanVault` 只累计链接问题、孤立文件与入口膨胀，`Info` 字段没有任何自增路径，恒为 `0`。`doctor` 还包含 `MISSING_RECOMMENDED_METADATA`（info）与 `SUSPICIOUS_INSTRUCTION`（warning）等检查。结果是同一仓库在概览与诊断两个视图显示不同的错误/警告/信息数字。
- **修复建议**：明确快照 `health` 的定义边界（子集还是全量），或在概览中复用 `doctor` 计数，避免两个视图对同一事实给出不同结论。

### P3-4：`DiffCommit` 的逐文件调用与路径过滤语义

- **位置**：[`internal/git/diff.go`](../../internal/git/diff.go)（`DiffCommit` 第 45–90 行）。
- **触发与机制**：
  1. 每个非二进制变更文件都单独启动一次 `git diff-tree -p`；一个含数百文件的提交会产生数百个子进程，且每个子进程的上限是 4 MiB 总预算的剩余量。
  2. 路径过滤写作 `... -p <resolved> -- <path>`，`--` 之后仍是 Git 通配 pathspec 语义。文件名若含 `*`、`?`、`[` 等元字符（[`internal/vault/path.go`](../../internal/vault/path.go) 只拒绝 `.git`、盘符、控制字符等，不禁止元字符），可能匹配到同提交的其他文件，使 `path` 与 `patch` 内容不一致。
- **修复建议**：路径过滤使用字面语义（复用 `GIT_LITERAL_PATHSPECS=1` 或 `:(literal)` 前缀）；单次读取可改为一次 `diff-tree` 后按文件切分，或在契约中明确逐文件子进程与体积开销。

### P3-5：前端未展示 ahead/behind

- **位置**：[`web/src/components/TopBar.jsx`](../../web/src/components/TopBar.jsx)（第 36–52 行）。
- **触发与机制**：`SyncStateDTO` 已提供可空的 `aheadCount`/`behindCount`，TopBar 只渲染仓库路径、本地状态、远端状态与最后同步时间，未使用这两个字段，契约要求的「未知时显示『未知』，不显示零」没有落点。
- **修复建议**：在顶栏补充 ahead/behind，`null` 时显示「未知」。

### P3-6：知识图谱在每次失效时重建 Cytoscape 实例

- **位置**：[`web/src/components/KnowledgeGraph.jsx`](../../web/src/components/KnowledgeGraph.jsx)（第 60–92 行）。
- **触发与机制**：`built` 以 `snapshot.graph` 的对象标识为依赖，而每次 SSE 失效都会用新对象替换 `snapshot`，导致 `useMemo` 重算、`useEffect` 重新执行 `cytoscape(...)` 并跑一次 `cose` 布局；用户当前的平移、缩放与选中节点在每次刷新后重置，编辑期间会反复重建画布。
- **修复建议**：以图谱内容的稳定键（如节点/边集合摘要）而不是对象标识作为依赖，或复用实例、仅增量更新 elements。

---

## 4. 阶段 A 契约回归抽查

- **锁描述符继承（`P1-2`）**：[`internal/app/mutation.go`](../../internal/app/mutation.go) `beginLocked` 用 `git.WithInheritedFile(ctx, held.File())` 构造 `session.ctx`，`Apply`、`Capture`、`ResolveApply`、`ResolveAbort`、`recover` 与 `sync` 均在入口处 `ctx = session.ctx`，抽查未发现新的裸 `ctx` Git 调用。
- **环境白名单（`P1-3`）**：[`internal/git/exec.go`](../../internal/git/exec.go) `childEnv` 统一调用 [`internal/git/env.go`](../../internal/git/env.go) 的 `SyncEnv`，前后台共用同一份白名单，重定向变量不在名单内，符合「全项目唯一定义」。
- **读取门禁（`P2-1`）**：`App.ReadFile` 走 `beginRead`（非阻塞锁 + 冲突/待恢复/Git 中间态拦截），不再直接读工作区。
- **`wrap` 信号转发（`P2-6`）**：[`internal/cli/wrap_exit_unix.go`](../../internal/cli/wrap_exit_unix.go) 通过 `forwardSignals` 转发 `SIGINT`/`SIGTERM`/`SIGHUP`，非 Unix 平台保留退出码语义。
- **锁释放语义与状态字段（`P3-2`）**：[`internal/lock/lock.go`](../../internal/lock/lock.go) `Release` 只关闭描述符、不显式 `LOCK_UN`；[`internal/state/cache.go`](../../internal/state/cache.go) `SyncSummary` 已含 `repoId` 与 `localState`。
- **结论**：抽查项均保持修复后的行为，未发现阶段 B 引入的回归。

---

## 5. 已验证达标项

- **HTTP 安全框架（`A01`–`A03`、`P01`–`P03`）**：[`internal/server/server.go`](../../internal/server/server.go) 仅绑定环回地址，按实际监听端口精确校验 `Host`，拒绝跨源与 `null` `Origin`，以常量时间比较 32 字节 Session Token，返回 `Cache-Control: no-store`、`nosniff`、`referrer-policy` 与 `frame-ancestors 'none'`，并在路由前拒绝只读模式下的全部非 GET/HEAD 请求。
- **路径边界与限额**：`validateRequestPath` 在进入 `ServeMux` 前拒绝空段、`.`、`..`、NUL 与反斜杠；请求体 8 MiB、分页 200、单文件 2 MiB、Diff 4 MiB 均超限即拒绝，不静默截断。
- **SSE 状态流（`A05`）**：[`internal/server/events.go`](../../internal/server/events.go) 与服务端解码用例覆盖跨块拆分、多行 `data`、评论心跳与 `resync.required` 溢出降级；客户端每次连接与重连都以快照全量校正，不依赖事件恰好一次送达。
- **文件监听（`B-2`）**：[`internal/watcher/watcher.go`](../../internal/watcher/watcher.go) 递归登记新建目录、处理 rename/remove/原子替换、排除 `.git`（大小写等价）并以 150ms 防抖合并；`Bus` 为每个订阅者维护独立有界队列，溢出合并为一次 `resync.required`，测试在 `-race` 下通过。
- **内嵌前端与只读视图（`R01`、`B-3`）**：`web/embed.go` 内嵌 `dist`，首页注入 Session Token 与只读标识，`/assets/` 只服务普通文件且不列目录；页面与脚本不含外部 CDN 引用、不泄露 Token；Cytoscape.js 与 Diff2Html 按需分块并随二进制内嵌。
- **不可信文本渲染**：[`web/src/diff.js`](../../web/src/diff.js) 通过 Diff2Html 渲染并转义，前端以文本节点渲染条目、来源、文件名与提交说明，未发现把记忆内容当 HTML 执行或注入脚本的路径。
- **前端构建与测试链路**：`make ui` 先于 Go 构建；`make test` 串联 `vitest` 与 `go test`；`web/dist` 不提交且在 [`README.md`](../../README.md) 与 Makefile 中明确了构建顺序。

---

## 6. 收口建议

1. 在 [开发状态](../STATUS.md) 登记阶段 B 收口修复任务（建议 `T027`），将 P1-1、P2-1、P2-2 与 P3-1–P3-6 纳入受控修复；阶段 B 已到达停止条件，本轮审查不改变「待用户确认后再启动阶段 C」的结论。
2. 优先修复 P1-1 并补充回归：断言 `GET /api/v1/audit?before=<非十六进制或选项>` 返回 `INVALID_ARGUMENT`/400，且不会触发任何 Git 写文件行为；同步为 `git.Log` 增加 `--end-of-options` 防护。
3. P2-1 需要新增「最近完整快照」缓存与 `stale` 分支，并覆盖 rebase 中途、冲突与 `recovery_required` 场景；P2-2 需要统一只读路由的锁语义并补一条长同步期间的 HTTP 用例。
4. 修复全部通过 `make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 与 `cd web && npm test` 后，再按阶段 C 的依赖顺序推进 `C-1`。
