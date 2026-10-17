# 阶段 A 实现审查报告

本报告记录阶段 A（`A-1` 至 `A-6`，对应任务 `T001`–`T021`）源码、测试与构建脚本的全面审查结果。审查以 [方案索引](../README.md) 下的 01–06 规范为唯一权威契约，历史验收归档见 [阶段 A 已完成任务归档](./stage-a.md)，当前进度见 [开发状态](../STATUS.md)。

## 审查结论与范围

- 审查范围：`cmd/memctl`、`internal/` 下全部 11 个包、`tests/integration`、`Makefile` 与 `scripts/next-round.sh`。
- 验证环境：2026-10-16，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0。
- 总体结论：阶段 A 已完成核心 CLI、本地事务、远端同步、冲突采集、后台派生、Hook 协议与只读诊断的主链路实现；模块边界、[`os.Root`](../../internal/vault/path.go) 路径防逃逸、无损 Markdown 解析与回写、Git 重定向环境清洗、凭据脱敏和无宿主集成测试整体符合设计要求。
- 发现问题：共识别 **3 项高优先级问题（P1，破坏契约或状态安全）**、**7 项中优先级问题（P2，行为偏差或恢复语义缺失）** 与 **3 项低优先级问题（P3，边界条件与测试稳健性）**。进入阶段 B（`B-1`）前，建议优先完成 P1 与核心 P2 问题收口。

| 编号 | 级别 | 涉及模块 | 问题摘要 | 关联契约与验收项 |
| --- | --- | --- | --- | --- |
| [P1-1](#p1-1resolveapply-在-captured-阶段重启变基后若校验失败会遗留未合并状态) | P1 | `internal/app` | `ResolveApply` 在 `captured` 阶段先重启 rebase 再校验冲突类型与输入，失败后遗留未合并 rebase 并导致后续报 `CONFLICT_STALE` | [冲突采集与解决](../03-workflows-and-dataflows.md#52-ui-与-cli-共用的解决过程)、`C01`–`C04` |
| [P1-2](#p1-2持锁会话未将锁文件描述符传递给多数-git-子进程) | P1 | `internal/app`、`internal/git` | `Apply`、`Capture`、`Recover`、`Resolve` 等路径未向 `git.*` 传入带锁描述符的 `session.ctx` | [平台与进程生命周期](../01-architecture-and-modules.md#6-平台与进程生命周期)、`L01` |
| [P1-3](#p1-3前台-git-子进程未经过统一环境白名单过滤) | P1 | `internal/git`、`internal/syncer` | 前台 Git 调用仅剔除 9 个重定向变量，未复用 `syncer.SyncEnv` 单一环境白名单 | [凭据与信任边界](../01-architecture-and-modules.md#34-凭据与信任边界)、[凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理) |
| [P2-1](#p2-1readfile-未检查仓库锁待恢复事务与未合并-git-状态) | P2 | `internal/app` | `App.ReadFile` 直接读取工作区文件，未做锁探测、待恢复事务拦截与 Git 中间态门禁 | [写入不变量](../01-architecture-and-modules.md#23-写入不变量)、[中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复) |
| [P2-2](#p2-2syncer-错误分类覆盖-no_upstream同步后残留旧计数且-hasupstream-分支恒假) | P2 | `internal/syncer`、`internal/app` | `Classify` 将 `*memerr.Error`（如 `NO_UPSTREAM`）覆盖为 `GIT_FAILED`，同步成功后未清零 `Ahead`/`Behind`，且 `!res.HasUpstream` 恒为 `false` | [凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理)、[同步与恢复对象](../02-domain-models-and-contracts.md#22-同步与恢复) |
| [P2-3](#p2-3本地提交固定返回-local_only-且-sync---capture-漏解析身份与后台同步) | P2 | `internal/app`、`internal/cli` | `Apply`/`Capture` 固定返回 `local_only` 且不将 `sync.json` 置为 `pending`；`sync --capture` 未解析 Agent/SID 且未触发后台同步 | [受管写入事务](../03-workflows-and-dataflows.md#2-受管写入事务)、[Git 同步步骤](../03-workflows-and-dataflows.md#42-git-同步步骤) |
| [P2-4](#p2-4doctor-存在-4-处状态与内容诊断缺口) | P2 | `internal/doctor` | `checkGitState` 用 `switch` 掩盖多重异常、未检查孤立 `operation.json`、推荐元数据判断写为 `&&`、可疑短语扫描忽略非列表行 | [审计、诊断、整理与接入](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[同步可观测性](../03-workflows-and-dataflows.md#8-同步可观测性) |
| [P2-5](#p2-5resolve---apply-缺少基线必填校验且回写丢失文件模式与文本边界检查) | P2 | `internal/app`、`internal/cli` | `ResolveApply` 允许空 `baseRevision` 与空 `--conflict`，回写固定使用 `0644` 并跳过大小与 UTF-8 校验 | [格式保真](../02-domain-models-and-contracts.md#12-格式保真)、[REST 与解决契约](../02-domain-models-and-contracts.md#42-路由) |
| [P2-6](#p2-6wrap-未向被包装子进程转发宿主信号) | P2 | `internal/cli` | `runWrap` 未注册 `signal.Notify` 将包装器收到的 `SIGINT`/`SIGTERM`/`SIGHUP` 转发给子进程 | [CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、`H05` |
| [P2-7](#p2-7init---remote-在空目录非默认分支与多-url-远端场景存在边界缺陷) | P2 | `internal/app` | `initWithNewRemote` 误删调用方空目录、未预检远端默认分支；`connectExistingRemote` 未校验全部远端 URL 且过早落盘批准记录 | [初始化与远端接入](../03-workflows-and-dataflows.md#1-初始化与远端接入)、`G01`–`G03` |
| [P3-1](#p3-1writeatomic-权限设置listmanaged-临时文件过滤与-stat-长度哈希细节) | P3 | `internal/vault` | `WriteAtomic` 未显式 `Chmod`、`ListManaged` 未过滤 `.*.memctl-tmp`、`Stat` 在 8 MiB 截断处未将完整文件长度纳入哈希 | [修订标识](../02-domain-models-and-contracts.md#11-修订标识)、[中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复) |
| [P3-2](#p3-2lockrelease-显式解锁语义偏差与-syncsummary-字段缺失) | P3 | `internal/lock`、`internal/state` | `Lock.Release` 在 `Close` 前调用 `LOCK_UN` 提前释放共享文件描述锁；`state.SyncSummary` 缺少 `repoId` 与 `localState` | [技术依赖](../01-architecture-and-modules.md#技术依赖)、[同步与恢复对象](../02-domain-models-and-contracts.md#22-同步与恢复) |
| [P3-3](#p3-3并发测试负载下凭据超时用例的-1-秒预算过紧) | P3 | `tests/integration` | `TestA4CredentialTimeoutCleansUpCoversG08_5AndL01` 设置 `sync_timeout_sec = 1`，全量并发测试下偶发在进入 `fetch` 前超时 | [G08 凭据子场景](../05-validation-and-delivery.md#21-g08-凭据子场景) |

---

## 1. 高优先级问题（P1）

### P1-1：`ResolveApply` 在 `captured` 阶段重启变基后若校验失败会遗留未合并状态

- **位置**：[`internal/app/resolve.go`](../../internal/app/resolve.go)（`ResolveApply` 第 114–140 行、`restartRebase` 第 212–246 行、`applyResolved` 第 250–297 行）
- **关联契约**：[UI 与 CLI 共用的解决过程](../03-workflows-and-dataflows.md#52-ui-与-cli-共用的解决过程) 规定 `captured` 阶段已中止首次变基并保持工作区干净；不支持自动重放的冲突（二进制、重命名、模式冲突等）只允许 `abort` 或手动 Git 处理。
- **触发与机制**：
  1. 当 `conflict.Phase == syncer.PhaseCaptured` 时，`ResolveApply` 先调用 `restartRebase(ctx, session, conflict)`，将 `operation.json` 的 `Phase` 改写为 `"running"` 并执行 `git rebase`，使工作树和索引停在未合并冲突态（`HEAD` 切换至 `ontoRevision`）。
  2. 随后才在 `applyResolved` 中检查冲突文件类型（`KindBinary`、`KindUnsupported`）及调用方传入的 `resolvedFiles`（路径缺失、未知路径、非法 `action`）。
  3. 当 `applyResolved` 返回错误时，`ResolveApply` 直接返回，既未调用 `git.RebaseAbort`，也未将 `operation.json` 恢复为 `"captured"`。
- **影响后果**：任何一次非法的 `resolve --apply` 输入或对二进制/重命名冲突的误调用，都会把原本干净的 `captured` 仓库破坏为挂起的未合并 rebase 状态。且由于 `HEAD` 在 `restartRebase` 中已变为 `ontoRevision`，调用方修正参数后再次执行 `resolve --apply` 时，`conflict.BaseRevision != revision` 成立，触发 `staleConflict` 删除 `conflict.json` 与 `operation.json` 并返回 `CONFLICT_STALE`。（在 [`tests/integration/a4_conflict_test.go`](../../tests/integration/a4_conflict_test.go) 第 387–391 行中，二进制冲突测试在 `apply` 失败后未检查 `git status`，掩盖了该残留状态。）
- **修复建议**：
  1. 在调用 `restartRebase` 之前，先完成对 `conflict.Files` 可解决性（拒绝 `KindBinary`、`KindUnsupported`）及 `resolved` 切片（路径覆盖率、`action` 枚举、内容大小与 UTF-8 合法性）的全部静态校验。
  2. 若 `restartRebase` 之后的步骤在完成前失败，执行 `git.RebaseAbort` 并将 `operation.json` 恢复为 `"captured"`。

### P1-2：持锁会话未将锁文件描述符传递给多数 Git 子进程

- **位置**：[`internal/app/mutation.go`](../../internal/app/mutation.go)（`beginLocked` 第 564–570 行、`Apply` 第 107–201 行、`Capture` 第 241–301 行）、[`internal/app/recover.go`](../../internal/app/recover.go)（第 151–160 行）、[`internal/app/resolve.go`](../../internal/app/resolve.go)（第 123–372 行）、[`internal/app/sync.go`](../../internal/app/sync.go)（第 175 行）
- **关联契约**：[技术依赖](../01-architecture-and-modules.md#技术依赖) 与 [平台与进程生命周期](../01-architecture-and-modules.md#6-平台与进程生命周期)（`L-04`）要求锁文件描述符可被子进程继承，父进程被强杀后锁不早于 Git 子进程释放。
- **触发与机制**：`beginLocked` 已通过 `git.WithInheritedFile(ctx, held.File())` 构造了携带锁文件描述符的 `session.ctx`（由 [`internal/git/exec.go`](../../internal/git/exec.go) 第 74–76 行挂载到 `cmd.ExtraFiles`），但全项目仅有 `SyncWithOptions` 使用了 `session.ctx`；其余 `Apply`、`Capture`、`RecoverRollback`、`ResolveApply`、`ResolveAbort`、`finishResolve`（含 `git push`）以及 `beginLocked`/`beginSync` 内部的 `git.Inspect` 均直接传入外层 `ctx`。
- **影响后果**：若 `memctl` 父进程在执行 `git add`、`git commit`、`git reset`、`git rebase` 或 `resolve` 推送期间被 `SIGKILL` 强杀，仓库锁会随父进程退出立即释放，而仍在运行的 Git 子进程会在无锁保护下继续修改仓库。
- **修复建议**：将持锁会话（`lockSession`）生命周期内的所有 `git.*` 调用统一改为传入 `session.ctx`。

### P1-3：前台 Git 子进程未经过统一环境白名单过滤

- **位置**：[`internal/git/exec.go`](../../internal/git/exec.go)（`childEnv` 第 109–115 行）、[`internal/git/git.go`](../../internal/git/git.go)（`scrubEnv` 第 270–280 行）对比 [`internal/git/env.go`](../../internal/git/env.go)（`SyncEnv` 第 55 行；审查时位于 `internal/syncer/env.go`，`T022` 收口后下沉至 `internal/git`）
- **关联契约**：[凭据与信任边界](../01-architecture-and-modules.md#34-凭据与信任边界) 与 [凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理) 规定：「凭据与网络环境另按固定白名单传递……前台使用调用方环境中的白名单项；后台派生只获得父进程传入的白名单快照……该白名单是全项目唯一定义」。
- **触发与机制**：`syncer.SyncEnv` 仅在后台派生的 [`internal/syncer/spawn.go`](../../internal/syncer/spawn.go)（第 36 行）中被调用；前台 Git 命令通过 `childEnv` 启动，仅调用 `scrubEnv` 剔除了 9 个仓库重定向变量（`GIT_DIR` 等），将调用方环境中其余全部变量（包括宿主私有 API Key、`GIT_EXEC_PATH`、`GIT_EXTERNAL_DIFF`、`GIT_TEMPLATE_DIR` 等）原样透传给 Git 及其派生的钩子与凭据辅助进程。
- **修复建议**：将单一环境白名单下沉或共享至 `internal/git`，使前台与后台 Git 子进程统一按同一份白名单构造环境变量。

---

## 2. 中优先级问题（P2）

### P2-1：`ReadFile` 未检查仓库锁、待恢复事务与未合并 Git 状态

- **位置**：[`internal/app/app.go`](../../internal/app/app.go)（`ReadFile` 第 122–134 行）
- **关联契约**：[写入不变量](../01-architecture-and-modules.md#23-写入不变量) 与 [中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复)（「读取事务中间内容不是一致性读取；事务期间 UI 使用带 `stale` 标记的最近完整快照，CLI 读取返回忙碌」）。
- **触发与机制**：`App.ReadFile` 直接调用 `vault.OpenRoot(repoPath)` 读取工作区文件，未探测仓库锁，也未检查 `store.Pending()`、`store.LoadConflict()` 或 `git.Inspect`。
- **影响后果**：在多文件写入落盘中途、崩溃待恢复（`recovery_required`）或 `resolving` 阶段工作区文件含冲突标记时，`memctl read` 会静默返回不一致的中间内容或 `<<<<<<<` 冲突标记，并生成对应的 `fileRevision`。
- **修复建议**：在 `ReadFile` 中执行非阻塞或有界锁检查（锁忙返回 `BUSY`），并在存在待恢复事务、未决冲突或活跃 Git 中间态时拒绝读取。

### P2-2：`syncer` 错误分类覆盖 `NO_UPSTREAM`、同步后残留旧计数且 `HasUpstream` 分支恒假

- **位置**：[`internal/syncer/syncer.go`](../../internal/syncer/syncer.go)（`Run` 第 94–157 行、`establishUpstream` 第 163–191 行、`Classify` 第 319–323 行）、[`internal/app/remote.go`](../../internal/app/remote.go)（`remoteFailure` 第 211–214 行）
- **关联契约**：[凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理) 与 [同步与恢复对象](../02-domain-models-and-contracts.md#22-同步与恢复)。
- **触发与机制**：
  1. `establishUpstream` 与 `git.RemoteDefaultBranch` 返回 `*memerr.Error`（错误码 `NO_UPSTREAM`），但 `Classify(err)` 仅匹配 `*git.ExecError`，对其余错误一律返回 `memerr.GitFailed`，导致 `sync.json` 的 `reasonCode` 与 `init` 远端错误被覆盖为 `GIT_FAILED`。
  2. `syncer.Run` 在第 118 行读取 `res.Ahead, res.Behind`，但在快进（第 127 行）或变基并推送成功（第 147–157 行）后未将其重置为 `0, 0`，导致同步成功后的 `sync.json` 与 `memctl sync --wait` 输出仍显示非零的旧 `ahead`/`behind`。
  3. `syncer.Run` 在第 101 行无条件设置 `res.HasUpstream = true`，使第 147 行 `git.Push(..., !res.HasUpstream)` 的 `setUpstream` 参数恒为 `false`；且当 `establishUpstream` 已向空远端完成首次 `push -u`（第 169 行）后，`Run` 仍会重复执行一次 `Fetch` 和 `Push`。
- **修复建议**：在 `Classify` 中优先保留已有 `*memerr.Error` 的错误码；在 `establishUpstream` 完成首次推送时直接返回同步成功结果；在快进或推送成功后将 `Ahead` 与 `Behind` 置零。

### P2-3：本地提交固定返回 `local_only` 且 `sync --capture` 漏解析身份与后台同步

- **位置**：[`internal/app/mutation.go`](../../internal/app/mutation.go)（`Apply` 第 217–228 行、`Capture` 第 314–321 行）、[`internal/cli/cli.go`](../../internal/cli/cli.go)（`runSync` 第 376–405 行）
- **关联契约**：[受管写入事务](../03-workflows-and-dataflows.md#2-受管写入事务)、[Git 同步步骤](../03-workflows-and-dataflows.md#42-git-同步步骤)（「`Synced --> Pending: 新本地提交`」）与 [错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)。
- **触发与机制**：
  1. `Apply` 与 `Capture` 在本地提交成功后硬编码 `SyncState: "local_only"`，即使仓库已配置远端也不返回 `"pending"`，且未将 `sync.json` 的 `remoteState` 更新为 `"pending"`。
  2. `runSync` 在处理 `memctl sync --capture`（不带 `--wait`）时，未调用 `hookio.ResolveAgent` 与 `hookio.ResolveSID`（丢失环境变量与 `--stdin` 中的会话身份），也未在捕获产生新提交且 `cfg.BackgroundSync` 开启时触发后台同步。
- **修复建议**：在 `Apply` 与 `Capture` 中检查是否存在远端，存在时更新 `sync.json` 为 `"pending"` 并返回 `SyncState: "pending"`；在 `runSync` 的 `capture` 路径补齐 `hookio` 身份解析与后台同步触发。

### P2-4：`doctor` 存在 4 处状态与内容诊断缺口

- **位置**：[`internal/doctor/doctor.go`](../../internal/doctor/doctor.go)（`checkEntries` 第 305–334 行、`checkGitState` 第 360–390 行、`checkState` 第 394–428 行）
- **关联契约**：[审计、诊断、整理与接入](../02-domain-models-and-contracts.md#23-审计诊断整理与接入) 与 [同步可观测性](../03-workflows-and-dataflows.md#8-同步可观测性)（「`doctor` 检查运行状态与 Git 状态是否一致，发现孤立操作记录、遗留索引锁或异常退出时给出具体恢复入口」）。
- **触发与机制**：
  1. `checkGitState` 使用 `switch` 分支检查 `repoState.Unmerged`、`repoState.Operation != "" || repoState.Sequencer` 与 `repoState.IndexLocked`，多项同时发生时只报告首项；且当 `repoState.Operation == ""` 而 `repoState.Sequencer == true` 时，`fmt.Sprintf("a %s operation is in progress", repoState.Operation)` 输出 `"a  operation is in progress"`。
  2. `checkState` 只检查了 `store.Pending()` 和 `store.LoadConflict()`，未调用 `store.LoadOperation()` 检测孤立 `operation.json` 或状态文件与 Git 实态的不一致。
  3. `checkEntries` 第 305 行条件写为 `if !hasMetadata(entry, "source") && !hasMetadata(entry, "added")`，导致仅缺少其中一个推荐元数据时不报告 `MISSING_RECOMMENDED_METADATA`。
  4. `checkEntries` 第 323–334 行仅扫描顶层 `- ` 列表项的 `FactText`，而 `pull`/`start` 会将整份 `MEMORY.md` 注入宿主上下文，位于标题、普通段落或缩进块中的提示词注入短语会被漏检。
- **修复建议**：将 `checkGitState` 的 `switch` 改为独立 `if` 并为 `Sequencer` 提供默认操作名；在 `checkState` 中加入 `store.LoadOperation()` 校验；将推荐元数据判断改为 `||`；对受管 Markdown 全文按行扫描可疑注入短语。

### P2-5：`resolve --apply` 缺少基线必填校验且回写丢失文件模式与文本边界检查

- **位置**：[`internal/app/resolve.go`](../../internal/app/resolve.go)（`ResolveApply` 第 110 行、`applyResolved` 第 281–283 行、`requireConflict` 第 375–392 行）、[`internal/cli/cli.go`](../../internal/cli/cli.go)（`runResolve` 第 573–586 行）
- **关联契约**：[格式保真](../02-domain-models-and-contracts.md#12-格式保真) 与 [REST 路由](../02-domain-models-and-contracts.md#42-路由)（`{conflictId, baseRevision, resolvedFiles}`）。
- **触发与机制**：
  1. `ResolveApply` 第 110 行判断 `if conflict.BaseRevision != revision || (baseRevision != "" && baseRevision != revision)`，当导入 JSON 中 `baseRevision` 为空字符串时跳过了调用方基线比对；同理 `requireConflict` 在 `conflictID == ""` 时跳过了冲突 ID 比对。
  2. `applyResolved` 第 282 行固定以 `0o644` 写入解决后的文件，丢失原有的可执行文件模式（`0755`），且未执行 `MaxTextFileBytes`、`MaxMutationBytes` 和 UTF-8 文本检查。
- **修复建议**：强制要求 `conflictId` 与 `baseRevision` 非空并精确匹配当前冲突记录；在 `applyResolved` 中执行大小与 UTF-8 校验，并保留冲突前文件权限位。

### P2-6：`wrap` 未向被包装子进程转发宿主信号

- **位置**：[`internal/cli/hook.go`](../../internal/cli/hook.go)（`runWrap` 第 272–278 行）
- **关联契约**：[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[环境变量与会话身份](../04-ui-and-cli-specification.md#2-环境变量与会话身份) 与 [核心测试矩阵](../05-validation-and-delivery.md#2-核心测试矩阵)（`H05`：「保留 stdio、退出码和信号处理」）。
- **触发与机制**：`runWrap` 直接调用 `child.Run()`，未注册 `signal.Notify` 捕获发往 `memctl wrap` 自身的 `SIGINT`、`SIGTERM`、`SIGHUP` 并转发给 `child.Process`。当外部宿主或进程管理器按 PID 向 `memctl wrap` 发送终止信号时，`memctl` 会直接退出，既不将信号转发给被包装进程，也不执行退出后的后台同步。
- **修复建议**：使用 `child.Start()` 启动子进程后注册信号监听并转发给 `child.Process`，等待 `child.Wait()` 完成后再继续收尾逻辑。

### P2-7：`init --remote` 在空目录、非默认分支与多 URL 远端场景存在边界缺陷

- **位置**：[`internal/app/remote.go`](../../internal/app/remote.go)（`initWithNewRemote` 第 34–61 行、`connectExistingRemote` 第 99–153 行）
- **关联契约**：[初始化与远端接入](../03-workflows-and-dataflows.md#1-初始化与远端接入)。
- **触发与机制**：
  1. `initWithNewRemote` 在 `LsRemote` 失败或调用 `git.Clone` 前执行 `os.Remove(repoPath)`；若用户传入事先创建的空目录，`LsRemote` 失败会删掉用户的空目录。
  2. `initWithNewRemote` 在克隆非空远端前未调用 `git.RemoteDefaultBranch`，若远端仅有非 `HEAD` 分支提交，`git clone` 会检出空分支并在 `verifyEntryFile` 报误导性的 `NO_REPO`（`"has no MEMORY.md"`），而非 `NO_UPSTREAM`。
  3. `connectExistingRemote` 读取 `remotes[0]` 而非 `git.DefaultRemote`（`origin`），且使用 `containsString(urls, opts.RemoteURL)` 判断匹配，若远端配置了未确认的额外 push URL 也会放行；同时 `AddRemote` 与 `recordApproval` 在 `Fetch` 和 `MergeBase` 之前执行，若历史不相关报错会留下已批准的远端状态。
- **修复建议**：仅清理 `init` 自身新建的目录；克隆非空远端前调用 `RemoteDefaultBranch` 校验默认分支；在 `connectExistingRemote` 中固定检查 `git.DefaultRemote` 的全部 URL 一致，并在 `Fetch`/`MergeBase` 通过后再写入远端配置与批准记录。

---

## 3. 低优先级问题（P3）

### P3-1：`WriteAtomic` 权限设置、`ListManaged` 临时文件过滤与 `Stat` 长度哈希细节

- **位置**：[`internal/vault/write.go`](../../internal/vault/write.go)（`Stat` 第 64–69 行、`ListManaged` 第 88–108 行、`WriteAtomic` 第 147–170 行）
- **触发与机制**：
  1. `WriteAtomic` 通过 `OpenFile(..., mode)` 创建临时文件，受进程 `umask` 影响且在覆盖残留同名临时文件时不会更新权限，应在 `Rename` 前显式调用 `file.Chmod(mode)`。
  2. `ListManaged` 遍历工作树时未跳过崩溃可能残留的 `.*.memctl-tmp` 隐藏临时文件。
  3. `Stat` 使用 `readBounded(file, MaxRevisionBytes)` 读取前 8 MiB 计算 `FileRevision(meta.Mode, content)`，未将完整 `info.Size()` 纳入摘要，与文件注释及 [修订标识](../02-domain-models-and-contracts.md#11-修订标识) 描述不一致。

### P3-2：`Lock.Release` 显式解锁语义偏差与 `SyncSummary` 字段缺失

- **位置**：[`internal/lock/lock.go`](../../internal/lock/lock.go)（`Release` 第 106–122 行）、[`internal/state/cache.go`](../../internal/state/cache.go)（`SyncSummary` 第 16–29 行）
- **触发与机制**：
  1. Unix `flock` 绑定在打开文件描述（open file description）上；`Lock.Release` 在 `l.file.Close()` 前显式调用 `unlockFile`（`unix.Flock(..., LOCK_UN)`），即使继承了该描述符的 Git 子进程仍在运行，也会立即解除文件锁，与 `Release` 注释不一致。直接关闭文件描述符而不调用 `LOCK_UN`，才能保证所有继承描述符关闭后才释放锁。
  2. `state.SyncSummary` 结构体缺少 [同步与恢复对象](../02-domain-models-and-contracts.md#22-同步与恢复) 规定的 `repoId` 与 `localState` 字段。

### P3-3：并发测试负载下凭据超时用例的 1 秒预算过紧

- **位置**：[`tests/integration/a4_credentials_test.go`](../../tests/integration/a4_credentials_test.go)（`TestA4CredentialTimeoutCleansUpCoversG08_5AndL01` 第 120–154 行）
- **触发与机制**：在全量并发执行 `go test ./... -count=1` 时，该用例偶发报错 `the hanging stub never recorded its pid`（单独重跑通过）。原因是测试设置 `sync_timeout_sec = 1`，而 `SyncWithOptions` 的单一超时上下文覆盖了 `Remotes`、`CurrentBranch`、`HasUpstream`、`Fetch` 连续 4 次外部 `fake-git` 进程启动；在 macOS 并发负载与首次运行临时二进制的系统校验开销下，1 秒预算可能在进入第 4 次 `fetch` 写 PID 文件前耗尽。
- **修复建议**：将该用例的 `sync_timeout_sec` 调整为 `2` 或 `3` 秒，或在正式执行 `sync --wait` 前对临时编译的桩二进制做一次预热调用。

---

## 4. 已验证达标项

- **受管路径边界（`P01`–`P05`）**：[`internal/vault/path.go`](../../internal/vault/path.go) 全程通过 Go `os.Root` 执行仓库内文件读写，配合路径段校验拦截 `..`、`.git`、控制字符与符号链接越界。
- **无损解析与写前预演（`F01`–`F02`、`W01`–`W04`）**：[`internal/vault/parse.go`](../../internal/vault/parse.go) 与 [`internal/app/mutation.go`](../../internal/app/mutation.go) 完整保留未触及行、缩进块、开放元数据键值对及 CRLF/LF 换行，并在落盘前通过重解析验证结构可逆。
- **Git 隔离与凭据脱敏（`G08.1`–`G08.7`）**：[`internal/git/exec.go`](../../internal/git/exec.go) 强制注入 `-c protocol.file.allow=never`、抑制非交互凭据弹窗、使用进程组清理超时子进程，并通过 `Redact` 统一脱敏 URL 凭据、`Authorization` 头与敏感 Token。
- **无宿主端到端验证闭环**：[`tests/integration`](../../tests/integration) 使用临时 HOME、本地 bare 远端与受控传输桩覆盖阶段 A 验收场景，不触碰真实用户配置或外部网络。

---

## 5. 收口建议

1. 在 [开发状态](../STATUS.md) 中登记阶段 A 收口修复任务 `T022`，将 `P1-1`–`P1-3` 与 `P2-1`–`P2-7`（顺带收敛 `P3-1`–`P3-3`）纳入单轮或多轮受控修复。
2. 为每项修复补充回归测试（特别是 `ResolveApply` 失败后的 `git status` 与阶段保持、非 `sync` 路径的锁描述符继承、`ReadFile` 在锁忙/恢复态下的拦截、`doctor` 对孤立 `operation.json` 和非列表行注入短语的检测，以及 `wrap` 外部信号转发）。
3. 修复全部通过 `go vet ./...`、`go test ./... -count=1` 与 `make build` 后，再启动阶段 B（`B-1`：HTTP 服务、鉴权与浏览界面）。
