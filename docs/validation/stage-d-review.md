# 阶段 D 实现审查报告

本报告记录阶段 D（`D-1`、`D-2`，对应任务 `T035`–`T037`）源码、测试、前端与发布工程的全面审查结果。审查以 [方案索引](../README.md) 下的 01–06 规范为唯一权威契约，阶段 A、B、C 的历史验收分别见 [阶段 A 归档](./stage-a.md)、[阶段 B 归档](./stage-b.md)、[阶段 C 归档](./stage-c.md)，三阶段的实现审查与收口见 [阶段 A 实现审查报告](./stage-a-review.md)、[阶段 B 实现审查报告](./stage-b-review.md) 与 [阶段 C 实现审查报告](./stage-c-review.md)，当前进度见 [开发状态](../STATUS.md)。

## 审查结论与范围

- 审查范围：`internal/dream`（`dream.go`、`service.go`、`export.go`、`diff.go`、`textdiff.go`、`run.go`、`runner.go`、`sandbox*.go`、`scope.go`、`proc*.go`、`apply.go`、`store.go`）、`internal/app`（`dream.go`、`dreamapply.go`、`recover.go` 及相关事务提交路径）、`internal/server`（`dream.go`、`events.go`、`ui.go`、`server.go` 的生成并发上限）、`internal/cli/dream.go`、`web/src`（`dreams.js`、`api.js`、`components/DreamPanel.jsx`、`sse.js`）、`scripts/release.sh`、`Makefile`、`web/scripts/npm-licenses.mjs`、`tests/integration/d1_*`、`d2_*`、`t037_release_test.go` 与各包单元测试。
- 审查边界：阶段 A、B、C 已由 `T022`、`T027`、`T034` 评审并收口，本轮不做重复评审；对阶段 D 触碰的既有不变量（仓库锁与事务记录、恢复语义、只读门禁、路径边界、SSE、发布门槛、不可信文本渲染）做回归抽查，结论见第 4 节。
- 验证环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1。
- 审查方式：主会话完成全量审查并保留验收责任，同时按 [开发循环规范](../06-development-loop.md#5-验证如何落地) 委派 4 个只读子智能体分别复核生成管线、显式应用、HTTP/CLI/UI 与发布工程；子智能体结论均经主会话以源码或实机证据复核后采纳。
- 总体结论：阶段 D 的隔离生成、固定基线 Diff、持久化提案、显式应用、UI 整理视图与跨平台发布链路整体符合设计，`patchHash` 绑定、受管事务复用、`--readonly` 拦截与路由契约测试成立；但导出模式、应用状态迁移、跨进程生成查询、模式变化归一化、并发上限与发布元数据真实性与契约存在偏差。
- 发现问题：共识别 **4 项高优先级问题（P1，破坏契约或数据安全）**、**12 项中优先级问题（P2，行为偏差或恢复语义缺失）** 与 **18 项低优先级问题（P3，边界条件、一致性与测试稳健性）**。其中已在本轮受控修复的问题见第 4 节收口状态，登记为已知限制的项在本节末列出。

| 编号 | 级别 | 涉及模块 | 问题摘要 | 关联契约与验收项 |
| --- | --- | --- | --- | --- |
| [P1-1](#p1-1导出写入受-umask-影响未改动文件变成幻影更新) | P1 | `internal/dream` | 导出文件创建模式被进程 umask 改写，未改动文件变成 mode 更新，提案不可应用 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、`fileRevision`、`D03` |
| [P1-2](#p1-2已应用提案在文档化-recover-路径后被打成-stale) | P1 | `internal/app` | 中断 apply 后执行 `recover --rollback` 删除提交证据，重试把已应用提案标为 stale | [中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复)、[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、`D04`、`W05` |
| [P1-3](#p1-3提案状态迁移无同步已应用状态可被回退) | P1 | `internal/dream`、`internal/app` | 状态迁移为无同步 `Load→Save`，并发 apply 可把 `applied` 覆盖为 `applying`/`stale` | [修订标识](../02-domain-models-and-contracts.md#11-修订标识)、`D04` |
| [P1-4](#p1-4发送到外部模型缺少可追溯的确认语义) | P1 | 文档、`web/src` | `03` 要求首次选择 runner 时确认外部发送，`04` 无对应 flag，实现与 UI 均无说明 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令) |
| [P2-1](#p2-1跨进程查询生成中提案会破坏活跃运行) | P2 | `internal/dream` | 其他进程查询 `generating` 记录会标 failed 并删除活跃运行的工作目录 | [DreamProposal](../02-domain-models-and-contracts.md#23-审计诊断整理与接入) |
| [P2-2](#p2-2真实模式变化进入-ready-提案但-apply-必然拒绝) | P2 | `internal/dream` | 执行位变化生成 `ready` 提案，应用时 `UNSUPPORTED`，整份提案无法应用 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、`D03` |
| [P2-3](#p2-3external_changes-被当作基线过期) | P2 | `internal/app` | 工作树脏被等同基线移动，合法提案被打成终态 `stale` | `D02` |
| [P2-4](#p2-4提交已发生时的-post-commit-失败被误标-ready) | P2 | `internal/app` | hook 改动提交路径集后返回错误，提案回到 `ready` 而 HEAD 已有提交 | `W07`、`D04` |
| [P2-5](#p2-5中断-reconcile-分支跳过-post-commit-记账与后台同步) | P2 | `internal/app` | 恢复出的提交不触发后台同步、不更新同步摘要 | [同步步骤](../03-workflows-and-dataflows.md#42-git-同步步骤) |
| [P2-6](#p2-6缺少-memorymd-的普通-git-仓库仍会被导出) | P2 | `internal/dream` | 指向普通 Git 工作树时导出全部文本并发送模型，而不是 `NO_REPO` | [仓库身份](../01-architecture-and-modules.md#31-仓库身份) |
| [P2-7](#p2-7dreampreview-未强制-baserevision) | P2 | `internal/server`、文档 | 路由输入列出 `baseRevision` 但未校验，文档未声明可选 | [REST 路由](../02-domain-models-and-contracts.md#42-路由) |
| [P2-8](#p2-8dreampanel-把任何-unsupported-当作缺沙箱并永久禁用) | P2 | `web/src` | 缺少 runner 的 `UNSUPPORTED` 被当成沙箱不可用，生成被永久禁用 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、`D01` |
| [P2-9](#p2-9生成任务并发无上限) | P2 | `internal/server` | 每次 preview 无上限派生沙箱 runner | [HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全) |
| [P2-10](#p2-10release-info-硬编码-darwinarm64-为已验证) | P2 | `scripts/release.sh` | 在非 darwin/arm64 构建宿主上仍声称该平台已运行验收 | [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查) |
| [P2-11](#p2-11go-list-失败被吞掉产出无-go-许可证的完整-release) | P2 | `scripts/release.sh` | 进程替换失败不触发 `set -e`，许可证文件可不含任何 Go 条目 | [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) |
| [P2-12](#p2-12直接运行-release-可内嵌过期前端) | P2 | `scripts/release.sh` | 缺少 `web/dist` 新鲜度检查，可直接重跑并打包旧 UI | [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) |
| [P3-1](#p3-1重复-apply-结果缺少-required-字段) | P3 | `internal/app` | 幂等重复 apply 返回空 `syncState`，缺 `transactionId`/`revision` | [MutationResult](../02-domain-models-and-contracts.md#22-同步与恢复) |
| [P3-2](#p3-2sse-客户端丢弃-dreamupdated) | P3 | `web/src` | 客户端只处理 `vault.updated`/`resync.required`，`dream.updated` 被丢弃 | [SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存) |
| [P3-3](#p3-3查询当前提案-id-永久停留在加载态) | P3 | `web/src` | 查询同一 proposalId 时 effect 不重跑，界面停在加载 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui) |
| [P3-4](#p3-4超时终止可能无限阻塞) | P3 | `internal/dream` | 逃逸进程组的后代保持管道打开，`Wait` 无二次上限 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) |
| [P3-5](#p3-5读取导出文件未复核类型) | P3 | `internal/dream` | 目录遍历与打开之间被换成链接/特殊文件会跟随读取 | [文件边界](../01-architecture-and-modules.md#32-文件边界)、`D03` |
| [P3-6](#p3-6硬链接别名写入) | P3 | `internal/dream` | 潜在硬链接逃逸；Seatbelt 已默认拒绝 `file-link`，本轮补回归 | `D01` |
| [P3-7](#p3-7提案-store-未-fsync-目录) | P3 | `internal/dream` | rename 后未落盘目录项，掉电可能丢失刚写入的提案 | [DreamProposal](../02-domain-models-and-contracts.md#23-审计诊断整理与接入) |
| [P3-8](#p3-8孤儿临时目录泄漏) | P3 | `internal/dream` | 崩溃残留的 `memctl-dream-*`（含记忆内容）不再被清理 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) |
| [P3-9](#p3-9许可证缺失降级占位仍退出-0) | P3 | `scripts/release.sh` | 缺许可证文件时输出占位文本并以 0 退出 | [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) |
| [P3-10](#p3-10go-list--m-all-标签夸大) | P3 | `scripts/release.sh` | 模块图被标注为「bundled into the release binary」 | [依赖与许可证](../04-ui-and-cli-specification.md#6-单文件打包) |
| [P3-11](#p3-11链接警告只覆盖改动文件) | P3 | `internal/dream` | 删除被引用文件时，未改动引用方不产生死链警告 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) |
| [P3-12](#p3-12无提交仓库的-unborn-head-硬失败) | P3 | `internal/dream` | 无 HEAD 时返回原始 `GIT_FAILED`，未使用契约的空值 HEAD | [修订标识](../02-domain-models-and-contracts.md#11-修订标识) |
| [P3-13](#p3-13保护路径规则比文档更宽且读取错误硬失败) | P3 | `internal/dream` | `*.key`/`.config` 等路径使整次生成失败，而不是跳过该文件 | [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) |
| [P3-14](#p3-14patchhash-不一致返回-400-而非-412) | P3 | `internal/dream` | 哈希不一致返回 `INVALID_ARGUMENT`，与适配器基线不一致的 `REVISION_MISMATCH` 语义不同 | [错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功) |
| [P3-15](#p3-15npm-嵌套重复版本许可证归属) | P3 | `web/scripts/npm-licenses.mjs` | 嵌套重复版本被归到提升版本的许可证 | [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) |
| [P3-16](#p3-16可复现性声明缺证据) | P3 | 文档、`scripts/release.sh` | 同一运行内生成的校验和无法证明两次构建一致 | [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) |
| [P3-17](#p3-17文档声明已验证-runner) | P3 | `docs/05` | 计划称「已验证 runner」，实际仅有合成 runner 与未验证宿主 | `R02`、`D01` |
| [P3-18](#p3-18应用不可删的空变更集与取消失败) | P3 | `internal/dream`、`internal/app` | 空变更集 `apply` 返回参数错误；stale 后 `MarkApplied` 失败时调用方只拿到错误 | `D04` |

---

## 1. 高优先级问题（P1）

### P1-1：导出写入受 umask 影响，未改动文件变成幻影更新

- **位置**：`internal/dream/export.go`（导出写入）。
- **关联契约**：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) 步骤 4 要求从固定基线计算 Diff 并包含模式变化；`fileRevision` 由 Git 模式与字节共同决定。
- **触发与机制**：`os.WriteFile(destination, content, file.Mode.Perm())` 的创建模式被进程 umask 掩码。`umask 077` 下导出的 `0644` 变成 `0600`，`buildChanges` 逐文件比较 `old.fsMode == new.fsMode` 时把每个未改动文件判为 `update`，`PlannedRevision` 与 `BaseRevision` 相同、patch 为空。
- **影响**：提案变化清单不实，且受管事务对这些路径 `git add` 无实际改动，提交路径集不匹配而回滚，整份提案不可应用。
- **修复建议**：导出后显式 `Chmod` 到源模式；模式比较改用 `vault.GitMode` 归一化。
- **本轮处置**：已修复。新增 `writeExportFile` 在写入后设置精确模式；`buildChanges` 以归一化模式判定变化。

### P1-2：已应用提案在文档化 `recover` 路径后被打成 `stale`

- **位置**：`internal/app/recover.go`（`RecoverRollback` 的已解决分支）。
- **关联契约**：[中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复) 与 [Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) 步骤 7 要求提交后中断先补齐状态再重试；`D04` 要求重复 apply 幂等。
- **触发与机制**：`commit` 在 `DreamProposalID` 路径下保留完成记录以便对账。若进程在写 `applied` 前中断，用户按文档运行 `recover --rollback`，`describe` 通过 `Memctl-Transaction` trailer 认定提交已发生并删除记录，但未更新提案。重试时对账找不到记录，`requireWriteBaseline` 因 HEAD 前移返回 `RevisionMismatch`，`ApplyDream` 将提案标为终态 `stale`。
- **影响**：已应用的提案永久显示 `stale`，`appliedCommit` 丢失，用户被引导重新生成并可能重复应用内容。
- **修复建议**：`RecoverRollback` 在删除已证明提交的记录前，按 `ProposalID` 写入 `applied` + commit。
- **本轮处置**：已修复。`RecoverRollback` 的已解决分支对带 `ProposalID` 的记录调用 `MarkApplied`；新增 `TestRecoverRollbackRecordsAppliedDream` 覆盖。

### P1-3：提案状态迁移无同步，已应用状态可被回退

- **位置**：`internal/dream/apply.go`（`setStatus`、`PrepareApply`）。
- **关联契约**：[修订标识](../02-domain-models-and-contracts.md#11-修订标识) 要求校验在取得锁后针对磁盘状态执行；`D04` 要求重复 apply 返回同一提交、不重复提交。
- **触发与机制**：`setStatus` 是完整文件的 `Load→Save`，无锁也无 CAS。两个重叠 apply 中，落后方可能把 `applied` 覆盖为 `applying`，随后成功写入 `stale`；或读到旧状态后返回 `INVALID_ARGUMENT`。
- **影响**：破坏「已应用不可回退」的不变量，重复 apply 在并发下可能报错而非返回原提交。
- **修复建议**：序列化状态迁移，并在丢失迁移后重新读取，遇 `applied` 返回幂等结果。
- **本轮处置**：已修复。`dream.Service` 新增 `statusMu` 串行化 `PrepareApply` 与 `setStatus`；`MarkApplied` 允许修复被误标 `stale` 的记录（仅已证明提交可达）；`ApplyDream` 在 `PrepareApply`/`MarkApplying` 失败或基线错误后重新读取，遇 `applied` 返回同一提交。

### P1-4：发送到外部模型缺少可追溯的确认语义

- **位置**：`internal/cli/dream.go`、`internal/server/dream.go`、`web/src/components/DreamPanel.jsx`、`docs/03`、`docs/04`。
- **关联契约**：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) 步骤 5 要求首次选择 runner 时确认全库内容会发送到外部模型。
- **触发与机制**：`dream --runner` 与 UI 生成按钮直接启动模型；`docs/03` 要求确认，`docs/04` 的 CLI 契约没有对应 flag，两者冲突。
- **影响**：受支持记忆文本在无说明的情况下发送给 provider，与项目对远端私有性显式确认的处理不一致。
- **修复建议**：新增一次性确认（flag/prompt 与 UI 复选）并记录；或在 `03`、`04` 明确 runner 选择即发送确认，并在 CLI 帮助与 UI 文案中说明。
- **本轮处置**：已修复（文档收敛方案）。`03` 步骤 5 与 `04` `dream` 行明确「选择 runner 即确认将导出的受支持文本发送给该 runner 的模型端点，本地 CLI 不等于本地模型」；UI 面板新增同义说明。未新增 flag，避免扩大首版 CLI 契约。

---

## 2. 中优先级问题（P2）

### P2-1：跨进程查询生成中提案会破坏活跃运行

- **位置**：`internal/dream/service.go`（`loadRecord`、`removeWorkDir`）。
- **关联契约**：[DreamProposal](../02-domain-models-and-contracts.md#23-审计诊断整理与接入) 要求生成中断标失败且提案可查询；[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) 要求运行结束后删除临时执行环境。
- **触发与机制**：进程 A 正在生成提案 X，进程 B 执行 `dream --show X` 或 `GET /api/v1/dream/X`。B 的 `pending` 为空，`loadRecord` 把 X 标为 `failed` 并删除 A 的 `memctl-dream-*` 目录。
- **影响**：A 运行中的工作目录被删除，健康生成被误报失败。
- **本轮处置**：已修复。`loadRecord` 不再删除工作目录，仅标记终态，拥有者随后的终态写入覆盖该标记；新增 `sweepWorkDirs` 只在 `Begin` 回收早于 2 倍隔离预算的目录；新增 `TestForeignLoadKeepsLiveWorkDir`。

### P2-2：真实模式变化进入 `ready` 提案但 apply 必然拒绝

- **位置**：`internal/dream/diff.go`、`internal/dream/service.go`（`plannedFiles`）、`internal/dream/apply.go`（`verifyPlanned`）。
- **关联契约**：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案) 要求 `ready` 提案经确认后按 `proposalId + patchHash` 应用；受管写入保留文件模式。
- **触发与机制**：runner 新建可执行文件或 `chmod` 既有文件时，`buildChanges` 记录计划修订为新的 Git 模式；`verifyPlanned` 在 apply 时以 `UNSUPPORTED` 拒绝整个提案。
- **影响**：用户审阅并确认的提案必然失败，同一提案中的其他有效变更也无法应用。
- **本轮处置**：已修复。生成阶段把计划模式归一化为受管写入可复现的值（更新保留基线模式，新建为 `100644`），并以 warning 说明被忽略的模式变化；`TestModeChangeIsNormalizedWithWarning` 覆盖。

### P2-3：`EXTERNAL_CHANGES` 被当作基线过期

- **位置**：`internal/app/dreamapply.go`（`dreamBaselineError`）。
- **关联契约**：`D02` 只在基线移动时标记 `PROPOSAL_STALE`。
- **触发与机制**：工作树脏（未跟踪文件、遗留临时文件）时 `requireWriteBaseline` 先返回 `EXTERNAL_CHANGES`，`dreamBaselineError` 将其视为基线过期并写入终态 `stale`。
- **影响**：可重试的脏库错误摧毁有效提案，必须重新运行模型。
- **本轮处置**：已修复。只有 `RevisionMismatch` 视为基线移动；`EXTERNAL_CHANGES` 返回错误并保持 `ready`；新增 `TestApplyDreamRefusesDirtyTreeWithoutStaling`。

### P2-4：提交已发生时的 post-commit 失败被误标 `ready`

- **位置**：`internal/app/dreamapply.go`（apply 错误分支）。
- **关联契约**：`W07` 要求记录真实副作用；`D04` 要求不重复提交。
- **触发与机制**：pre-commit hook 改动提交路径集时 `samePathSet(committed, paths)` 失败，返回带 `Commit` 的错误；旧逻辑直接 `MarkReady`，提案可在 HEAD 已有提交的情况再次进入应用流程。
- **影响**：放弃记录证据后可能重复应用；错误路径也丢弃 `result.Commit`。
- **本轮处置**：已修复。错误结果携带 `Commit` 时先 `reconcileDreamApply`，仅在无已证明提交时回退 `ready`。

### P2-5：中断 reconcile 分支跳过 post-commit 记账与后台同步

- **位置**：`internal/app/dreamapply.go`（`reconcileDreamApply`、found 分支）。
- **关联契约**：[同步步骤](../03-workflows-and-dataflows.md#42-git-同步步骤) 与 Dreaming 步骤 7 要求记录 `appliedCommit` 并唤起同步。
- **触发与机制**：对账出的提交只写提案状态，不触发后台同步，也不更新同步摘要。
- **影响**：该提交可能长期不推送，UI 持续显示旧 head/`synced`。
- **本轮处置**：已修复。found 分支调用 `triggerBackgroundSync`；对账出的结果调用 `persistLocalCommit` 标记 `pending`。

### P2-6：缺少 `MEMORY.md` 的普通 Git 仓库仍会被导出

- **位置**：`internal/dream/service.go`（`fixBaseline`）。
- **关联契约**：[仓库身份](../01-architecture-and-modules.md#31-仓库身份) 要求根 `MEMORY.md` 存在，普通项目目录不是记忆仓库。
- **触发与机制**：`AGENT_MEMORY_DIR` 指向普通 Git 工作树时，`fixBaseline` 只检查 Git 状态与根句柄，随后导出全部文本并发送模型。
- **影响**：误配置会把项目文件发送到外部模型，而同一配置的受管写入返回 `NO_REPO`。
- **本轮处置**：已修复。`fixBaseline` 在导出前检查入口文件，缺少时返回 `NO_REPO`；新增 `TestServiceRequiresEntryPoint`。

### P2-7：`dream:preview` 未强制 `baseRevision`

- **位置**：`internal/server/dream.go`、`docs/02` 与 `docs/04`。
- **关联契约**：[REST 路由](../02-domain-models-and-contracts.md#42-路由) 的输入列出 `baseRevision`，未声明可选。
- **触发与机制**：路由不要求非空 `baseRevision`，UI 与 CLI 恒传空串；服务端在锁内固定自己的基线，因此没有损坏风险，但输入契约与实际不符。
- **影响**：调用方可以不带基线启动生成，合约描述与实现不一致。
- **本轮处置**：已修复（文档对齐）。`02` 将该字段标为可选并说明省略时服务端固定当前基线；`04` 与 CLI/UI 行为一致，未新增必填要求。

### P2-8：`DreamPanel` 把任何 `UNSUPPORTED` 当作缺沙箱并永久禁用

- **位置**：`web/src/components/DreamPanel.jsx`。
- **关联契约**：[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 与 `memctl-dream-sandbox` meta 是沙箱能力信号。
- **触发与机制**：缺少 runner 时服务端返回 `UNSUPPORTED`，组件据此永久置 `sandboxUnavailable`，即使 meta 为 `available`。
- **影响**：界面声称沙箱不可用，安装 runner 后仍需整页刷新。
- **本轮处置**：已修复。新增 `previewFailureDisablesSandbox`，仅在沙箱确实不可用时锁定；其他 `UNSUPPORTED` 只显示消息；`dreams.test.js` 覆盖。

### P2-9：生成任务并发无上限

- **位置**：`internal/server/dream.go`、`internal/server/server.go`。
- **关联契约**：[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全) 要求 API 限制并发生成任务。
- **触发与机制**：每次 preview 都派生一个沙箱 runner，仓库锁在导出后即释放，无法串行化。
- **影响**：单个本地客户端可产生无界临时目录与 runner 进程，无 `BUSY` 降级路径。
- **本轮处置**：已修复。`Server` 新增容量 1 的 `dreamRun` 槽位，占用时返回 503 `BUSY`；`TestD2DreamPreviewBoundCoversBusy` 覆盖。

### P2-10：`RELEASE-INFO` 硬编码 darwin/arm64 为已验证

- **位置**：`scripts/release.sh`。
- **关联契约**：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) 要求逐平台验证状态；[发布前检查](../05-validation-and-delivery.md#4-发布前检查) 禁止把交叉编译当运行验收。
- **触发与机制**：脚本无条件声明 `darwin/arm64 runtime-verified on the build host`。
- **影响**：在 Linux/Intel 宿主上产出虚假验证声明。
- **本轮处置**：已修复。按 `uname` 推断 `HOST_TARGET`，只有构建宿主标记为运行时验证，并输出 `build host` 行。

### P2-11：`go list` 失败被吞掉，产出无 Go 许可证的「完整」release

- **位置**：`scripts/release.sh`。
- **关联契约**：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) 要求 Go 运行时模块许可证全文。
- **触发与机制**：`done < <(go list ...)` 的进程替换失败不触发 `set -e`，许可证文件不含任何 Go 条目仍以 0 退出。
- **影响**：发布合规制品静默缺失。
- **本轮处置**：已修复。先用普通赋值捕获 `go list` 输出（失败即中止），再以 here-string 迭代。

### P2-12：直接运行 `release.sh` 可内嵌过期前端

- **位置**：`scripts/release.sh`。
- **关联契约**：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) 要求构建顺序为先前端后二进制。
- **触发与机制**：缺少 `web/dist` 新鲜度检查；缺失会因 embed 编译失败而暴露，过期则静默打包。
- **影响**：发布包含旧 UI。
- **本轮处置**：已修复。构建前要求 `web/dist/index.html` 存在，否则报错退出。

---

## 3. 低优先级问题（P3）

### P3-1：重复 apply 结果缺少 required 字段

- **位置**：`internal/app/dreamapply.go`（`dreamAppliedResult`、`cachedSyncState`）。
- **关联契约**：[MutationResult](../02-domain-models-and-contracts.md#22-同步与恢复)。
- **本轮处置**：部分修复。重复 apply 的 `syncState` 改为读取持久化同步摘要（无记录时为 `local_only`），并校验 `patchHash`；`transactionId` 与 `revision` 因幂等路径不开启事务仍缺省，登记为已知限制。

### P3-2：SSE 客户端丢弃 `dream.updated`

- **位置**：`web/src/sse.js`。
- **关联契约**：[SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存)。
- **本轮处置**：已修复。`dream.updated` 现在也触发失效回调；`sse.test.js` 覆盖。

### P3-3：查询当前提案 ID 永久停留在加载态

- **位置**：`web/src/components/DreamPanel.jsx`（`query`）。
- **本轮处置**：已修复。查询总是递增 reload key，同一 ID 也会重新拉取。

### P3-4：超时终止可能无限阻塞

- **位置**：`internal/dream/run.go`。
- **本轮处置**：已修复。设置 `cmd.WaitDelay = 5s`，逃逸进程组的后代无法无限持有管道。

### P3-5：读取导出文件未复核类型

- **位置**：`internal/dream/diff.go`（`readWorkFile`）。
- **本轮处置**：已修复。读取前 `Lstat`、打开后 `Stat` 均要求普通文件。

### P3-6：硬链接别名写入

- **位置**：`internal/dream/sandbox_darwin.go`、`internal/dream/diff.go`。
- **本轮处置**：已核实免疫。实机验证 Seatbelt 默认拒绝 `link(2)`（`Operation not permitted`），主库内容不变；`TestD1DreamIsolationCoversD01` 扩展为先硬链接再写入，链接失败时不产生旁路文件。

### P3-7：提案 store 未 fsync 目录

- **位置**：`internal/dream/store.go`。
- **本轮处置**：已修复。rename 后同步提案目录项。

### P3-8：孤儿临时目录泄漏

- **位置**：`internal/dream/service.go`。
- **本轮处置**：已修复。`Begin` 回收早于 2 倍隔离预算的 `memctl-dream-*` 目录。

### P3-9：许可证缺失降级占位仍退出 0

- **位置**：`scripts/release.sh`。
- **本轮处置**：已修复。缺失许可证时输出占位文本并向 stderr 告警，记录缺失数量。

### P3-10：`go list -m all` 标签夸大

- **位置**：`scripts/release.sh`。
- **本轮处置**：已修复。章节标题改为「Go module dependencies listed by `go list -m all`」。

### P3-11：链接警告只覆盖改动文件

- **位置**：`internal/dream/diff.go`（`linkWarnings`）。
- **本轮处置**：登记为已知限制。删除被引用文件时未改动引用方不产生死链警告；不影响主库安全，后续可扩展为对受影响路径的反向引用解析。

### P3-12：无提交仓库的 unborn HEAD 硬失败

- **位置**：`internal/dream/service.go`（`git.Head`）。
- **本轮处置**：登记为已知限制。干净但无提交的仓库返回原始 `GIT_FAILED`；常规 `init` 已产生首笔提交，影响面小。

### P3-13：保护路径规则比文档更宽且读取错误硬失败

- **位置**：`internal/dream/scope.go`、`internal/dream/export.go`。
- **本轮处置**：登记为已知限制。`*.key`、`.config` 等命名会被排除，特定仓库可能整体生成失败；保持保守拒绝，后续可细化为逐文件告警跳过。

### P3-14：`patchHash` 不一致返回 400 而非 412

- **位置**：`internal/dream/apply.go`。
- **本轮处置**：登记为已知限制。`02` 未为该字段指定错误码；当前 `INVALID_ARGUMENT` 不误导自动重试，后续可评估改为 `REVISION_MISMATCH`。

### P3-15：npm 嵌套重复版本许可证归属

- **位置**：`web/scripts/npm-licenses.mjs`。
- **本轮处置**：登记为已知限制。当前仅构建期依赖存在嵌套重复版本且均为 MIT，输出无差异。

### P3-16：可复现性声明缺证据

- **位置**：`docs/04`、`scripts/release.sh`。
- **本轮处置**：登记为已知限制。同一运行内生成的 `SHA256SUMS` 只能证明文件自洽，不能证明两次构建一致；未新增两次构建比对记录。

### P3-17：文档声明已验证 runner

- **位置**：`docs/05`。
- **本轮处置**：已修复。`D-1` 计划中的「已验证 runner（`claude`/`codex`）」改为「候选 runner（真实宿主未验证）」，与 [阶段 D 归档](./stage-d.md) 的实测结论一致。

### P3-18：应用不可删的空变更集与取消失败

- **位置**：`internal/dream/apply.go`、`internal/app/dreamapply.go`。
- **本轮处置**：部分修复。空变更集 `apply` 返回参数错误属预期；并发下 `MarkApplied` 失败时 `ApplyDream` 会先重新读取并返回已应用结果，只有真实不一致才返回错误。

---

## 4. 收口状态与回归抽查

- 本轮已修复 P1-1 至 P1-4、P2-1 至 P2-12、P3-2 至 P3-10 与 P3-17，`P3-1`、`P3-18` 部分修复；`P3-11` 至 `P3-16` 登记为已知限制。
- 新增回归：`internal/dream` 的 umask 导出、模式归一化、入口文件、跨进程查询保护；`internal/app` 的脏树不提级 stale、`recover --rollback` 记录已应用；`tests/integration` 的生成并发上限与硬链接别名；前端 `dreams.test.js`、`sse.test.js`。
- 阶段 A–C 不变量抽查：仓库锁与事务记录继承（`lock`/`state` 未改）、恢复语义（`recover` 仅在已证明提交时更新提案状态）、只读门禁（`--readonly` 覆盖 preview/apply）、路径边界（导出与提案范围校验）、SSE 鉴权与事件格式、发布门槛（校验和与许可证），均保持成立。
- 命令与结果：`gofmt -l internal cmd tests web/embed.go` 无输出；`go vet ./...` 通过；`go test ./... -count=1` 全量通过（含 `tests/integration`）；`cd web && npm test` 13 文件 74 项通过；`npm run build` 产出 `web/dist`；`bash -n scripts/release.sh` 通过。

## 5. 已知限制

- `P3-11` 至 `P3-16` 与 `P3-1`、`P3-18` 的剩余部分：链接警告范围、unborn HEAD、保护路径粒度、哈希错误码、npm 嵌套许可证归属、可复现性证据、幂等结果 `transactionId`/`revision`。
- 真实 `claude`/`codex` 宿主与模型端点未验证；`Linux`/`Windows` 运行、真实宿主 Hook 与 runner 隔离仍为未验证组合。
- 组件级 DOM 测试未开展；`DreamPanel` 交互由纯逻辑测试与 Go HTTP 联调覆盖。
