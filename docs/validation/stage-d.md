# 阶段 D 已完成任务归档

本文件按 [最小记录结构](../06-development-loop.md#1-最小记录结构) 归档阶段 D 期间（`T035`–`T039`）与阶段 D 结束后、自 `v0.1.0` 起的发布和 UI 轮次（`T040`–`T047`）的已完成任务历史验收记录。当前进度、待办任务与最近一轮记录见 [开发状态](../STATUS.md)。

## T035：Dreaming 隔离生成与持久化提案预览（`D-1`）

- 目标与范围：完成 `D-1`，前置为 `C-1`（`T029`）。新增 `internal/dream`：仓库锁内校验干净工作树、无待恢复事务、无冲突与无 Git 中间态后固定 `baseHead`/`baseRevision`；经 `vault.ListManaged`/`ReadFile` 仅导出受支持文本文件到 `os.MkdirTemp` 的独立临时目录，跳过 `.git`、Git/host 配置、符号链接、二进制与超限资产，不使用 `clone --local`；释放主仓库锁后运行 runner。macOS Seatbelt 后端（`sandbox-exec`）以默认拒绝、仅临时目录可写、`process-exec` 白名单、允许读与网络的 profile 执行 runner，非 macOS 返回 `Unavailable` 并以 `UNSUPPORTED` 拒绝；限制最小环境、输出量（4 MiB）、耗时（默认 10 分钟）与进程组。从固定基线逐文件计算文本 Diff，拒绝越界路径、Git 配置修改、符号链接/非普通文件、二进制与超限结果；持久化 `Proposal`（含有序 `changes`、`diff`、`patchHash`、`warnings`）到 `$STATE/repos/<repoId>/proposals/`，删除临时目录，孤儿 `generating` 记录在新进程查询时标为 `failed`。CLI 新增 `dream --runner claude|codex` 与 `dream --show ID`；`dream --apply` 返回 `UNSUPPORTED`（属 `D-2`）。HTTP 新增 `POST /api/v1/dream:preview`（202、`{proposalId}`，异步生成并在终态发布 `dream.updated`）、`GET /api/v1/dream/{proposalId}` 与显式 `UNSUPPORTED` 的 `POST /api/v1/dream:apply`；`watcher.Invalidation` 增加 `ProposalID`/`Status` 供 SSE 负载。`--readonly` 由既有中间件拒绝预览与应用请求，未修改主库。
- 契约：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[DreamProposal 与接口](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[应用与基础设施接口](../02-domain-models-and-contracts.md#3-应用与基础设施接口)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[REST 契约](../02-domain-models-and-contracts.md#4-rest-契约)、[SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存)、[模块职责与目录](../01-architecture-and-modules.md#4-模块职责)、[仓库身份与状态根](../01-architecture-and-modules.md#31-仓库身份)、[阶段步骤的子任务切分](../05-validation-and-delivery.md#阶段步骤的子任务切分)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 与 `cd web && npm test` 全部通过；`gofmt -l internal cmd tests web/embed.go` 无输出，`git diff --check` 无告警；前端未改动，`vitest` 12 文件 65 项通过；Go 全量含 `tests/integration`（208.5 秒）通过。`internal/dream` 单测覆盖：Diff 形状、路径作用域拒绝、缺沙箱 `UNSUPPORTED`、缺 runner `UNSUPPORTED`、脏库 `EXTERNAL_CHANGES`、过期基线 `REVISION_MISMATCH`、完整生成流水线（主库字节不变、临时目录清理、提案可查询）、越界/二进制/Git 配置/host Hook/符号链接被拒、孤儿 `generating` 记录标为 `failed`。`tests/integration/d1_dream_test.go` 用真实二进制与 Seatbelt 沙箱覆盖：`R02` 无 runner、`D02` 脏库拒绝、`D01` 隔离（runner 成功改导出树而写主库与 `$HOME/.claude` 被拒且目标字节不变）、`D03` 越界提案失败且主库不变、HTTP 202/GET/`dream.updated` 端到端。`A03` 由既有 `TestB1ReadOnlyCoversA03` 覆盖。
- 未验证与遗留：未在真实 claude/codex 宿主与模型端点验证，内嵌 runner 参数与凭据环境属未验证组合，[性能与适配记录](../05-validation-and-delivery.md#3-性能与适配记录) 的宿主实测字段待补；仅 macOS Seatbelt 为已验证隔离后端，Linux/Windows 返回 `UNSUPPORTED`；runner 以 scratch `HOME` 运行且不传递宿主 API key，真实宿主是否可用待验证；未开展浏览器视觉 UI/UX 验证；仅在 macOS arm64 实机运行。
- 下一步：`D-1` 完成，阶段 D 未结束（以 `D-2` 为界），按 [开发循环规范](../06-development-loop.md#6-交接与中断) 接续 `D-2` 子任务 1（Dreaming 显式应用与 UI 整理视图）。

## T036：Dreaming 显式应用与 UI 整理视图（`D-2` 子任务 1）

- 目标与范围：完成 `D-2` 子任务 1，前置为 `T035`、`T030`、`T033`。`internal/dream` 在持久化记录中保存每个 create/update 变化的原始内容与 Git 模式（`planned` 字段，不进入 `DreamProposal` API），新增 `PrepareApply`（校验 patchHash、有序变化清单、计划修订与模式不变）与 `MarkApplying`/`MarkReady`/`MarkStale`/`MarkApplied` 状态迁移；`app.ApplyDream` 按 `proposalId + patchHash` 加载提案，先在锁内按事务记录结算中断的 apply，再经受管事务 `app.Apply` 提交（`MutationRequest.DreamProposalID`），事务记录保存 `proposalId` 且在 Dreaming apply 时保留完成记录以便恢复，提交后写入 `appliedCommit` 并清理记录；基线移动返回 409 `PROPOSAL_STALE` 并标 `stale`，重复 apply 返回同一 commit，手动改过的文件拒绝覆盖（`RECOVERY_REQUIRED`）。CLI `dream --apply <ID> --patch-hash <HASH>` 替换原 `UNSUPPORTED` 占位；`POST /api/v1/dream:apply`（`{proposalId, patchHash}`）在 `--readonly` 下由既有中间件返回 `READ_ONLY`。UI `DreamPanel` 接入「整理接入」：runner 选择与生成（沙箱不可用时禁用）、按 proposalId 查询状态并轮询、固定 Diff 预览与有序变化列表、确认应用；过期提案禁止应用；adapter 安装面板不变。首页新增 `memctl-dream-sandbox` meta 注入隔离能力。同时修复 `internal/dream` 的并发竞态：`complete` 删除内存中的生成记录与写入终态记录，和 `loadRecord` 将无主 `generating` 记录归并为 `failed`，现在在 `s.mu` 下串行，并在归并前重读磁盘，避免把刚完成的 `ready` 记录覆盖为 `failed`。
- 契约：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)（步骤 5–7）、[DreamProposal 与接口](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[应用与基础设施接口](../02-domain-models-and-contracts.md#3-应用与基础设施接口)、[REST 契约](../02-domain-models-and-contracts.md#4-rest-契约)、[错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)、[阶段步骤的子任务切分](../05-validation-and-delivery.md#阶段步骤的子任务切分)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 与 `cd web && npm test` 全部通过；`gofmt -l internal cmd tests web/embed.go` 无输出，`git diff --check` 无告警；前端 `vitest run` 13 文件 72 项通过（`dreams.test.js` 覆盖状态标签、终态轮询、生成禁用、可应用判定与变化摘要；`api.test.js` 覆盖 Dreaming 三个请求的形状）。`D04` 由 `internal/app.TestApplyDreamCommitsAndIsIdempotentCoversD04`、`TestApplyDreamRecoversInterruptedApplyCoversD04` 与 `tests/integration/d2_dream_apply_test.go` 的 HTTP/CLI 用例覆盖（首次 apply 提交并写入内容、重复 apply 返回同一 commit 且提交数不变、`after-commit` 中断后重试复用既有 HEAD、CLI `dream --apply` 多次调用同一 commit）；`D02` 由 `internal/app.TestApplyDreamRefusesStaleBaselineCoversD02` 与 HTTP 旧基线用例覆盖（409 `PROPOSAL_STALE`、提案标 `stale`、目标路径与提交数不变）；`W06` 由 `TestApplyDreamRefusesToOverwriteManualEdit` 覆盖（半写后在事务外手改的文件不被覆盖，返回 `RECOVERY_REQUIRED`）；`PrepareApply` 的哈希、模式与状态校验由 `internal/dream/apply_test.go` 覆盖；`A03` 由 `TestD2DreamReadOnlyCoversA03` 覆盖（`POST /api/v1/dream:apply` 返回 403 `READ_ONLY`）。
- 未验证与遗留：真实 `claude`/`codex` 宿主与模型端点仍未验证，生成隔离仅在 macOS Seatbelt 后端验证，Linux/Windows 返回 `UNSUPPORTED`；未驱动浏览器 DOM，`DreamPanel` 交互由纯逻辑测试与 Go HTTP 联调覆盖，未开展浏览器视觉 UI/UX 验证；`reconcileDreamApply` 在完成记录已持久化但清理被占用锁跳过时只留下一个小型完成记录，不阻塞后续 apply，未新增独立清理任务。
- 下一步：`D-2` 子任务 1 完成，阶段 D 未结束（以 `D-2` 为界），按 [开发循环规范](../06-development-loop.md#6-交接与中断) 接续 `D-2` 子任务 2（跨平台发布工程与全量交付验收）。

## T037：跨平台发布工程与全量交付验收（`D-2` 子任务 2）

- 目标与范围：完成 `D-2` 子任务 2（阶段 D 的最后一个子任务），前置为 `T036`。新增 `scripts/release.sh` 与 `Makefile` 的 `release` 目标（依赖 `test`，先构建前端并跑全量测试），按 [单文件打包](../04-ui-and-cli-specification.md#6-单文件打包) 交叉编译 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制到 `dist/`，生成 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`（Go 运行时模块与前端运行时依赖的许可证全文，构建期前端依赖只列 SPDX 标识；前端部分由 `web/scripts/npm-licenses.mjs` 生成）与 `RELEASE-INFO.txt`（版本、提交、Go/Node/npm/Git 版本与逐平台验证状态）；新增 `Makefile` 的 `install` 目标以对齐 04 已文档化入口。新增 `tests/integration/t037_release_test.go` 的路由契约测试 `TestRoutesContractCoversT037`，用 TRACE 请求逐条断言 02 §4.2 与 04 §5 的 25 条路由已注册且方法匹配。执行降级与恢复演练、阶段 A–D 全量回归与第 4 节发布前检查，并按第 3 节记录性能基线与平台/宿主适配状态。
- 契约：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[REST 契约](../02-domain-models-and-contracts.md#4-rest-契约)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)、[性能与适配记录](../05-validation-and-delivery.md#3-性能与适配记录)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[阶段步骤的子任务切分](../05-validation-and-delivery.md#阶段步骤的子任务切分)、[阶段 D 性能与适配记录](./stage-d.md#阶段-d-性能与适配记录)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64（MacBook Pro `MacBookPro18,3`，Apple M1 Pro，16 GiB），Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：
  - 发布链路：`make release` 退出码 `0`；`make test` 通过（`vite build` 72 模块，`vitest run` 13 文件 72 项，`go test ./... -count=1` 含 `tests/integration` 186.8 秒），随后生成四个二进制与三个元数据文件；由 `scripts/release.sh` 独立重跑后 `shasum -a 256 -c SHA256SUMS` 四个文件全部 `OK`。
  - 静态检查：`go vet ./...` 无输出；`gofmt -l internal cmd tests web/embed.go` 无输出；`git diff --check` 无告警；`make build` 生成 `bin/memctl`。
  - 验收回归：`R03` 由 `tests/integration/r03_cli_test.go` 覆盖真实构建入口与 `version`/`help`/未知命令；`R01` 由 `TestR01EmbeddedInstructionsFromEmptyHome` 与 `TestB3EmbeddedUIAssetsCoverR01A02` 覆盖空 HOME 下的内嵌指引、UI 资产与无外部 CDN；`R02` 由 `TestR02MissingGitIsTargeted`、`TestR02MissingCredentialsAreTargeted` 与 `TestD1DreamMissingRunnerCoversR02` 覆盖；`R04` 由 `TestA6DoctorReadOnlyDiagnosisCoversR04` 覆盖。
  - 路由契约：`TestRoutesContractCoversT037` 通过，25 条文档路由全部注册且方法匹配；`A01` 的行为用例继续覆盖读取路由。
  - 降级演练：发布二进制在 `--no-open --readonly` 下无图形浏览器仍正常服务首页，`POST /api/v1/sync` 与 `POST /api/v1/dream:apply` 均返回 403 `READ_ONLY`；无 remote 的 `sync --wait` 返回 `remote: local_only`；PATH 无 runner 的 `dream --runner claude` 返回 `UNSUPPORTED`；缺沙箱由 `internal/dream.TestServiceRejectsMissingSandbox` 覆盖；缺 Git 由 `R02` 覆盖。
  - 恢复演练：`internal/app.TestRecoverRollsBackInterruptedWrite`（`W04`）、`TestRecoverRecognizesCommittedTransaction`（`W05`）、`TestRecoverRefusesUserEditedFile`（`W06`）、`TestMoveInterruptionRollsBack`（`W04`）与 `TestApplyDreamRecoversInterruptedApplyCoversD04`（`D04`）通过；`TestA3RecoverCli` 与 `TestC3InstallRecoversInterruptedInstallCoversI02`（`I02`）通过；演练使用故障注入，未以手工删除锁或强制重置仓库作为通过条件。
  - 发布前检查逐项结论：文档命令、参数、状态字段与 REST 路由同实现一致（新增全路由契约测试）；同步与写入测试使用临时 HOME 与本地裸远端/桩，不接触真实用户配置或公网；单文件包含默认指引与全部 8 个 adapter 模板，首页与 CSP 无外部 CDN；`--readonly`、无 remote、无 runner、无沙箱、无 Git 降级路径都有明确结果；事务与配置安装恢复演练通过；视觉 UI/UX 验证未开展（未明确安排），以协议与业务测试为准。
  - 性能与适配：性能基线、平台矩阵与宿主/adapter 状态见 [阶段 D 性能与适配记录](./stage-d.md#阶段-d-性能与适配记录)。
- 未验证与遗留：macOS amd64、Linux amd64、Windows amd64 仅交叉编译通过，运行、生命周期与隔离未验证；真实宿主（claude/codex 等）未安装 Hook、未触发事件，Dreaming 真实 runner 在隔离 scratch HOME 下退出 `1`，模型端点与凭据环境未验证；仅 macOS Seatbelt 为已验证隔离后端；未开展浏览器视觉 UI/UX 验证；`P3-6`、`P3-7`、`P3-13`（见 [阶段 C 实现审查报告](./stage-c-review.md)）与组件级 DOM 测试为已知限制，本轮未引入。
- 下一步：阶段 D 结束、阶段 A–D 全部完成，触发 [停止条件](../06-development-loop.md#3-状态与推进)，暂停自动接续并通知用户确认。

## T038：阶段 D 实现审查收口修复

- 目标与范围：收口 [阶段 D 实现审查报告](stage-d-review.md) 的 `P1-1`–`P1-4`、`P2-1`–`P2-12` 与 `P3-2`–`P3-10`、`P3-17`，`P3-1`、`P3-18` 部分修复；只修复既有契约违背并补齐回归证据，不新增产品能力。`internal/dream` 导出后显式设置源模式并按归一化模式判定变化；`loadRecord` 不再删除活跃运行的工作目录并在 `Begin` 回收过期临时目录；`fixBaseline` 要求根 `MEMORY.md`；生成阶段把不可复现的模式变化归一化并告警；`store` rename 后 fsync 目录；runner `WaitDelay` 有界；读取导出文件复核类型。`internal/app` 的 `recover --rollback` 对已证明提交的 Dreaming 记录写入 `applied`；`ApplyDream` 只把 `RevisionMismatch` 提级为 `PROPOSAL_STALE`，在带 `Commit` 的 post-commit 失败与并发丢失迁移后先对账或重新读取，并在对账分支触发后台同步与同步摘要更新。`internal/dream` 新增 `statusMu` 串行化状态迁移并允许已证明提交修复 `stale`。`internal/server` 为 `POST /api/v1/dream:preview` 增加容量 1 的生成并发上限并返回 503 `BUSY`。前端 `DreamPanel` 只在沙箱确实不可用时锁定生成、查询同一 ID 会重新拉取，`sse.js` 消费 `dream.updated`。`scripts/release.sh` 按构建宿主标注运行时验证、先捕获 `go list` 输出、要求 `web/dist` 存在并对缺失许可证告警。文档收敛 `03`/`04` 的外部发送确认语义、`02` 的 `baseRevision` 可选声明与 `05` 的 runner 验证状态措辞。
- 契约：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复)、[同步步骤](../03-workflows-and-dataflows.md#42-git-同步步骤)、[修订标识](../02-domain-models-and-contracts.md#11-修订标识)、[DreamProposal](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[REST 契约](../02-domain-models-and-contracts.md#42-路由)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[仓库身份](../01-architecture-and-modules.md#31-仓库身份)、[文件边界](../01-architecture-and-modules.md#32-文件边界)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话，审查委派 4 个只读子智能体并经主会话复核。
- 命令与结果：`gofmt -l internal cmd tests web/embed.go` 无输出；`go vet ./...` 通过；`go test ./... -count=1` 全量通过（含 `tests/integration` 207.5 秒）；`cd web && npm test` 13 文件 74 项通过；`npm run build` 产出 `web/dist`；`bash -n scripts/release.sh` 通过。新增回归：`internal/dream` 的 `TestExportPreservesModeDespiteUmask`、`TestModeChangeIsNormalizedWithWarning`、`TestServiceRequiresEntryPoint`、`TestForeignLoadKeepsLiveWorkDir`；`internal/app` 的 `TestApplyDreamRefusesDirtyTreeWithoutStaling`、`TestRecoverRollbackRecordsAppliedDream`；`tests/integration` 的 `TestD2DreamPreviewBoundCoversBusy` 与扩展的 `TestD1DreamIsolationCoversD01` 硬链接别名；`web/src/dreams.test.js`、`sse.test.js` 覆盖本轮 UI 与 SSE 修复。
- 未验证与遗留：`P3-11` 至 `P3-16` 与 `P3-1`、`P3-18` 的剩余部分登记为 [阶段 D 实现审查报告](stage-d-review.md#5-已知限制) 的已知限制；真实 `claude`/`codex` 宿主与模型端点、macOS amd64/Linux amd64/Windows amd64 运行时、浏览器视觉 UI/UX 仍未验证；`scripts/release.sh` 的宿主标注与许可证检查为静态与语法验证，未在非 darwin/arm64 宿主重跑 `make release`。
- 下一步：阶段 A–D 与其实现审查收口（`T022`、`T027`、`T034`、`T038`）全部完成，触发 [停止条件](../06-development-loop.md#3-状态与推进)，暂停自动接续并等待用户确认。

## 阶段 D 性能与适配记录

本节按 [性能与适配记录](../05-validation-and-delivery.md#3-性能与适配记录) 记录 `T037` 发布验收期间的本地性能基线与平台/宿主适配状态。数值来自本机一次会话的临时环境与发布二进制，不是跨机承诺；未实测的宿主和平台保持未验证。

### 环境

- 硬件：MacBook Pro（`MacBookPro18,3`），Apple M1 Pro，16 GiB。
- OS：macOS 26.6.2（Build 25G83），arm64。
- 工具链：Go 1.27.1、Git 2.54.0（Apple Git-157）、Node v26.10.0、npm 11.19.1。
- 二进制：`make release` 产出的 `dist/memctl-darwin-arm64`（`-trimpath`、`-ldflags "-s -w"`、`CGO_ENABLED=0`）。
- 仓库与配置：临时 HOME 与临时记忆仓库，`AGENT_MEMORY_BACKGROUND=0`、`GIT_CONFIG_NOSYSTEM=1`、无签名与自定义 Git hooks（锁竞争场景临时注入 sleep hook）；除网络故障场景外不访问网络，网络故障使用本机拒绝认证的 SSH 桩。fixture 为 20 个 Markdown 文件、36 次提交、约 544 KiB（含 `.git`）。
- 方法：Python3 一次性测量脚本位于工作区外，每次调用使用独立进程并测 wall-clock 毫秒；`p50`/`p95`/`max` 取排序后分位。脚本对 `/usr/bin/true` 同法测得 fork/exec 基线 `p50 2.2ms`，用于判断进程启动噪声；`context --stdin` 的 `max` 受本机后台负载影响，不作为承诺。

### 本地性能基线

| 路径 | 命令 | p50 | p95 | max |
| --- | --- | --- | --- | --- |
| 进程派生基线 | `/usr/bin/true` | 2.2ms | 2.5ms | 3.0ms |
| 冷进程启动 | `version` | 7.2ms | 8.1ms | 10.3ms |
| 帮助 | `help` | 7.3ms | 8.1ms | 8.7ms |
| 无变化 Hook（每轮注入） | `context --stdin` | 83.3ms | 91.4ms | 261.1ms |
| 无变化 Hook（`--once` 去重） | `context --once --stdin` | 22.3ms | 24.0ms | 24.4ms |
| 受管写入 | `write MEMORY.md --base <rev> --stdin` | 266.8ms | 274.5ms | 276.3ms |
| 外部 capture | `sync --capture` | 153.9ms | 162.7ms | 168.3ms |
| 锁竞争 Hook | 仓库锁被占用时 `context --stdin` | 116.1ms | 129.7ms | 129.7ms |
| 网络故障 | 认证失败桩下 `sync --wait` | 157.2ms | 171.1ms | 171.1ms |

- 清理与遗留进程：认证失败路径返回后 `pgrep` 未发现残留 SSH 桩进程；该路径 `p50 157.2ms` 远低于 `sync --wait` 的 30 秒预算。
- 结论：`version`/`help` 不访问仓库、不要求 Git 或 Node.js；`version` 扣除约 2ms 的 fork/exec 基线后约 `5ms`，与 `< 5ms` 的待测优化目标同量级但未稳定达标，如实记录。无变化 Hook、去重 Hook、受管写入与外部 capture 都远低于 8 秒业务预算；仓库锁被占用时 Hook 路径未等待锁（与无竞争路径同一量级）。整体与阶段 A `T019` 基线同一量级，`write`/`capture` 更快。

### 平台矩阵

| 目标 | 构建（交叉编译） | 运行、生命周期与隔离 |
| --- | --- | --- |
| darwin/arm64 | 已验证（Mach-O arm64） | 已验证：全量测试在本机运行；发布二进制冒烟 `version`/`help`/`init`/`start`/`doctor`/`ui`/`install`/`dream` |
| darwin/amd64 | 已验证（Mach-O x86_64） | 未验证：交叉编译产物，无实机 |
| linux/amd64 | 已验证（ELF x86-64，statically linked，stripped） | 未验证：文件锁、路径语义、进程生命周期与 runner 隔离均未运行 |
| windows/amd64 | 已验证（PE32+ x86-64，console） | 未验证：同上 |

### 宿主与 adapter 适配状态

| 宿主 | 探测结果 | 模板与计划 | 真实运行 |
| --- | --- | --- | --- |
| claude-code | `claude@2.1.220` | 已内嵌 `hooks.json`，`install --dry-run` 生成 `~/.claude/settings.json` 计划 | 未验证：未在真实宿主安装 Hook 或触发事件 |
| codex | `codex@0.160.0` | 已内嵌 `hooks.json`、`features.toml` | 未验证：同上 |
| grok | `grok@1.0.60` | 已内嵌 `hooks.json`，目标路径 `$HOME/.grok/hooks/memctl.json` 未实测 | 未验证 |
| antigravity | `agy@1.4.0` | 已内嵌 `hooks.json` | 未验证 |
| commandcode | `commandcode@1.82.5` | 已内嵌 `hooks.json` | 未验证 |
| opencode | `opencode@2.0.26` | 已内嵌 `agent-memory.ts`，插件清单路径未实测 | 未验证 |
| pi | `pi@1.3.0`，`needs_attention` | 已内嵌 `hooks.yaml` | 未验证 |
| dsh | 未探测到 | 已内嵌 `hooks.json` 与插件清单 | 未验证 |

- Dreaming runner 与沙箱：仅 macOS Seatbelt 为已验证隔离后端，Linux/Windows 返回 `UNSUPPORTED`。真实 `claude`（`/opt/homebrew/bin/claude`）在隔离 scratch HOME 下未产出有效提案（`IO_FAILED: runner ... exited with status 1`），说明模型端点与凭据环境属未验证组合；`install` 探测到的宿主版本不代表 Hook 已注入或被宿主采纳。
- 结论：`D01`–`D04` 的隔离与回落语义由合成 runner 与 Seatbelt 覆盖，真实宿主、其他平台与真实模型端点的组合保持未验证，不标为已支持。

## T039：ego-browser 端到端 UI/UX 验证问题收口

- 目标与范围：登记并修复 [ego-browser 端到端 UI/UX 验证报告](./ui-ux-verification.md) 发现的 5 项问题，只修复既有契约违背并补齐回归证据，不新增产品能力。`P2-1`：外部写入的失效提示在写入方仍持锁时到达，客户端读到 `stale: true` 缓存后不再重试，UI 定格旧计数；新增 `web/src/refresh.js` 的 `loadFreshSnapshot` 做有界指数退避重试，`web/src/app.jsx` 的 `refresh` 与 `refreshSnapshot` 改用该函数。`P2-2`：冲突解决、中止与过期分支只清除冲突状态，未更新 `$STATE` 同步摘要，顶栏持续显示 `conflict`；`internal/app/resolve.go` 新增 `persistResolveSyncState`，无上游记为 `local_only`，有上游按 `git.AheadBehind` 写入计数，并在 `finishResolve` 用 `defer` 覆盖提前返回路径。`P3-1`：`POST /api/v1/sync` 的 `syncSummaryDTO` 改用 `*int`，`hasUpstream` 为假时输出 `null`，与 `GET /api/v1/snapshot` 一致。`P3-2`：`web/src/graph.js` 新增 `graphLabel`，节点标签改用文件名并保留 `fullPath`，`KnowledgeGraph.jsx` 增加标签省略上限、节点斥力与容器高度，选中面板显示完整路径。`P3-3`：`TopBar.jsx` 在 `sm` 以下改为纵向布局与整行状态网格。文档补充 [同步与恢复](../02-domain-models-and-contracts.md#22-同步与恢复) 的 ahead/behind 可空语义与 [Web UI](../04-ui-and-cli-specification.md#4-web-ui) 的过期重试与图谱标签说明。
- 契约：[UI/UX 验证报告](./ui-ux-verification.md)、[同步与恢复](../02-domain-models-and-contracts.md#22-同步与恢复)、[REST 契约](../02-domain-models-and-contracts.md#42-路由)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[REST 契约路由](../02-domain-models-and-contracts.md#42-路由)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话，浏览器复验使用 ego-browser。
- 命令与结果：`gofmt -l internal cmd tests web/embed.go` 无输出；`go vet ./internal/... ./cmd/...` 通过；`go test ./... -count=1` 全量通过（含 `tests/integration` 223.9 秒）；`cd web && npm test` 14 文件 79 项通过；`npm run build` 产出 `web/dist`；`go build -o bin/memctl ./cmd/memctl` 通过。新增回归：`tests/integration/t039_ui_state_test.go` 的 `TestT039SyncSummaryHasUnknownCountsWithoutUpstream`（`P3-1`）与 `TestT039ConflictResolveRefreshesSyncState`（`P2-2`）；`web/src/refresh.test.js` 覆盖 `P2-1` 的重试、退避、耗尽与错误传播；`web/src/graph.test.js` 覆盖 `P3-2` 的文件名标签与完整路径。浏览器复验：无上游同步提示「领先 未知 · 落后 未知」，与顶栏一致；UI 解决冲突后顶栏显示「远端状态 synced」「领先 0 · 落后 0」；连续 6 次外部 `memctl write` 后 UI 计数每次收敛且无过期横幅；390 px 顶栏字段单行、无横向溢出；连通子图标签不再重叠。
- 未验证与遗留：图谱中大量同名长文件名的孤立节点簇仍会拥挤，节点间距不随标签宽度自动扩张，仅由标签省略上限截断；真实 Dreaming runner、多阶段冲突递进、adapter `REVISION_MISMATCH` 重探测与跨进程并发写未在本轮浏览器验证中覆盖，沿用既有协议回归；浏览器截图仅存在于会话临时目录，未提交，视觉结论以本记录与单元测试为证。
- 下一步：本轮为用户确认的收口任务；完成后总计划无待办，触发 [停止条件](../06-development-loop.md#3-状态与推进)，暂停自动接续并等待用户确认。

## 发布后轮次记录（`T040`–`T047`）

本节按时间顺序记录阶段 D 之后的发布与 UI 轮次，字段结构与上文任务记录一致。

### T040：前端界面重设计

- 目标与范围：将工作区既有、尚未提交的前端界面重设计纳入本轮提交与 v0.1.0 发布。只调整 `web/src` 的应用外壳、导航、顶栏、概览、条目浏览、知识图谱、冲突解决、审计、诊断与整理接入视图的布局、图标、配色与排版，不改变 REST 契约、状态字段、前端交互流程或后端行为。
- 契约：[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`cd web && npm ci && npm run build && npm test` 通过（14 文件 79 项，`web/dist` 产出 `index-*.css` 78.65 kB 与 `index-*.js` 141.43 kB）；`gofmt -l internal cmd tests web/embed.go` 无输出；`go vet ./internal/... ./cmd/...` 通过；`go test ./... -count=1` 全量通过（含 `tests/integration` 256.3 秒）。
- 未验证与遗留：未做浏览器端 UI/UX 复验，视觉结论以构建与组件测试为证，交互流程沿用既有实现。
- 下一步：作为 v0.1.0 发布内容随 `T041` 提交、推送并发布。

### T041：GitHub 构建流水线与 v0.1.0 发布

- 目标与范围：新增 GitHub Actions 构建流水线 `.github/workflows/build.yml`，在 `main` 推送、拉取请求、`v*` 标签推送与手动触发时调用 `make release`，构建 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制并上传 `dist/` 构建产物；推送 `v*` 标签时用 `gh release create` 创建 GitHub Release，附带四个候选二进制与 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`。同步更新根 `README.md`、[CLI 与 UI 打包](../04-ui-and-cli-specification.md#6-单文件打包) 与 [发布前检查](../05-validation-and-delivery.md#4-发布前检查) 的 CI 说明。
- 契约：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[目录规划](../01-architecture-and-modules.md#5-目录结构与依赖)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64（本地 `scripts/release.sh`）与 GitHub Actions `macos-latest`；Go 1.27.1，Node v26.10.0 / npm 11.19.1。
- 命令与结果：本地 `VERSION=v0.1.0 scripts/release.sh` 产出四个候选二进制与三个元数据文件，`shasum -a 256 -c SHA256SUMS` 四个文件全部 `OK`；首次 GitHub Actions 运行暴露宿主机差异并修复，修复后在 `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null` 隔离环境下 `go test ./... -count=1` 全量通过（`tests/integration` 256.5 秒）；[GitHub Actions 运行 39016504087](https://github.com/gray0128/memctl/actions/runs/39016504087) 在 `macos-latest` 上 4 分 34 秒通过，[Release v0.1.0](https://github.com/gray0128/memctl/releases/tag/v0.1.0) 已发布，附带四个候选二进制与三个元数据文件；下载后 `shasum -a 256 -c SHA256SUMS` 四个文件全部 `OK`，`RELEASE-INFO.txt` 记录 runtime-verified `darwin/arm64`（构建提交 `b404eeb`），`memctl-darwin-arm64 version` 输出 `memctl v0.1.0`。
- 首次流水线运行发现的环境依赖：GitHub 托管 macOS runner 无全局 `init.defaultBranch=main`，测试用 `git init --bare` 建的裸远端默认指向 `master`，`RemoteDefaultBranch` 读不到默认分支；`newBareRemote` 改用 `git init --bare -b main` 固定裸远端 HEAD。`TestB2WatchStreamsExternalEditCoversA05` 在 watcher 注册完成前写入外部文件会丢失事件；改为在等待事件期间有界重试写入，watcher 每次突发仍发布一个 `vault.updated`。两项均为测试隔离加固，不改变产品行为。
- 未验证与遗留：流水线只在 GitHub 托管 runner 上验证；`macos-latest` 之外的平台仍只做交叉编译，运行、生命周期与隔离保持未验证。
- 下一步：本轮完成后按 [停止条件](../06-development-loop.md#3-状态与推进) 暂停自动接续并通知用户确认。


### T042：修复真实宿主 adapter 协议并发布 v0.1.1

- 目标与范围：修复真实宿主验证暴露的 adapter 协议偏差，并发布 `v0.1.1`。Codex 与 Command Code 的 SessionStart 增加 `--json`；Antigravity 改为读写 `$HOME/.gemini/config/hooks.json` 的 `agent-memory` 命名 Hook，新增 `start --agy` 的 `injectSteps` envelope 与 `sync --agy` 的 `{}` 输出，并把 `conversationId` 纳入 SID；OpenCode 插件改为兼容 OpenCode 2 的 default `{id, setup}` 导出，订阅 `session.execution.started/succeeded`，同时保留 v1 命名导出；dsh 插件清单支持 `$HOME/.dsh/profiles/*/node_modules`。同步修正 `install --status` 的模板识别，并补对应单元与集成测试。不新增业务功能，不改变记忆仓库格式、REST 契约或同步状态机。
- 契约：[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[Hook 输入输出](../04-ui-and-cli-specification.md#3-hook-输入输出与时限)、[会话身份](../04-ui-and-cli-specification.md#2-环境变量与会话身份)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；真实宿主：Codex 0.160.0、Command Code 1.84.0、Antigravity 1.4.0、OpenCode 2.0.26、pi 1.3.0（插件 2026.9.26）、Grok 1.0.60、dsh 0.2.1-rc.1（插件 0.2.15）；执行者为主会话。
- 命令与结果：`make test` 全量通过（前端 14 文件 79 项；Go 含 `tests/integration` 259.1 秒）；新增 `TestT042AntigravityHookProtocol`（`conversationId` 作为 SID、`start --agy` 输出 `injectSteps`、`sync --agy` 输出 `{}`）、`TestAntigravityNamedHookMerge`（保留 `orca-status`、命名 Hook 幂等、陈旧受管项替换）、`TestDshPluginProfileDetection`（profile 插件探测与写入）、`TestRegistryHostProtocols`（Codex/Command Code `--json`、Antigravity 路径与 envelope、OpenCode 2 导出与事件、dsh profile glob）；`TestC3EmbeddedTemplatesCoverR01` 扩展为从内嵌模板安装 Codex、Command Code、Antigravity 与 OpenCode 2 并检查关键协议。真实宿主复验：pi、Grok、Antigravity、OpenCode、Command Code 启动并触发 Hook；Codex 在 `--dangerously-bypass-hook-trust` 下 `SessionStart Completed`，会话缓存记录注入成功，模型调用因账号额度限制未完成；dsh 插件加载并注册 `dsh-hooks-authoring` 技能，`/dsh-hooks/recent` 返回 200，`memctl` 以样例载荷验证 SessionStart/SessionEnd。`VERSION=v0.1.1 scripts/release.sh` 产出四个候选二进制与三个元数据文件；[GitHub Actions 运行 39019936416](https://github.com/gray0128/memctl/actions/runs/39019936416) 在 `macos-latest` 上 4 分 0 秒通过并创建 [Release v0.1.1](https://github.com/gray0128/memctl/releases/tag/v0.1.1)，发布四个候选二进制与三个元数据文件；下载 `memctl-darwin-arm64` 校验和 `6c433b37…f6bd` 与 `SHA256SUMS` 一致，`version` 输出 `memctl v0.1.1`，该二进制已安装到 `~/.agent-memory/bin/memctl`。
- 未验证与遗留：Codex 模型回复受账号额度限制（提示 2026-10-22 恢复）；dsh 当前会话启动早于全局 `hooks.json` 写入，SessionStart 需重启或新会话触发；macOS amd64、Linux amd64、Windows amd64 仍只有交叉编译；`dsh-hooks-plugin@0.2.15` 与 DSH 0.2.1-rc.1 的 peer 版本不匹配，已按用户授权使用精确版本豁免安装。
- 下一步：`v0.1.1` 发布完成后按 [停止条件](../06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

### T043：第二轮 ego-browser 端到端 UI/UX 验证

- 目标与范围：对 `v0.1.1` 发布候选二进制复验七个视图与主要写路径，确认 `T040` 界面重设计与 `T042` 适配器修复后的视觉与交互状态；只记录问题，不改变产品代码。
- 契约：[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，`dist/memctl-darwin-arm64`（v0.1.1），ego-browser v2.1.0；临时 HOME 与临时记忆仓库，读写 `127.0.0.1:18420`、只读 `127.0.0.1:18421`；本地裸远端加 peer 克隆制造冲突。
- 命令与结果：浏览器会话遍历七个视图，执行条目编辑与 412 草稿差异、文件原文编辑、审计 Diff 与回滚、冲突解决、adapter 预览、只读、深色与 390 px；通过项见 [第二轮验证](./ui-ux-verification.md#52-通过项)。发现 2 项 P2 与 4 项 P3，登记 `T044`。
- 未验证与遗留：图谱节点拆分、Dreaming 真实 runner、多阶段冲突递进、adapter 安装应用、非 macOS arm64 平台；截图保存在临时目录，未提交。
- 下一步：等待用户确认是否按 `T044` 收口；确认前不修改产品代码。

### T044：收口第二轮 UI/UX 验证问题

- 目标与范围：修复第二轮验证的 `R2-P2-1`、`R2-P2-2` 与 `R2-P3-1`–`R2-P3-4`；代码只改 `web/src` 的样式、布局测量、只读重试与时间格式，并在 [Web UI](../04-ui-and-cli-specification.md#4-web-ui) 补充时间格式与 `BUSY` 重试说明；不改 REST 契约、状态字段与写入流程。
- 契约：[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，ego-browser v2.1.0；临时 HOME 与临时记忆仓库，读写 `127.0.0.1:18420`、只读 `127.0.0.1:18421`。
- 命令与结果：`npx vitest run` 16 文件 85 项通过；`npm run build`、`go build -o bin/memctl ./cmd/memctl`、`gofmt -l`、`go vet` 通过；ego-browser 复验审计 Diff 文本边界（426 > 414）、横幅下滚动时 header 与导航不重叠（0–103 / 103–148）、冲突解决后横幅清除、BUSY 短锁透明与长锁友好提示、`最近同步 2026-10-17 17:54`、图谱连续 3 次布局无标签压盖、只读、深色与 390 px 无回归；详见 [复验记录](./ui-ux-verification.md#6-第二轮问题的收口与复验t044)。
- 未验证与遗留：更密集知识图谱的标签拥挤仍为已知限制；未重跑 `go test ./...`（无 Go 变更）。
- 下一步：提交推送本轮修复后按停止条件暂停并等待用户确认。

### T045：概览对齐、指标卡跳转与来源提示

- 目标与范围：修复用户报告的 4 项界面问题：概览导航与正文左侧未对齐、选中导航项左边框被滚动容器裁切、概览指标卡不可跳转、来源 URI 截断后无法查看完整值。代码只改 `web/src` 的布局与交互，并在 [Web UI](../04-ui-and-cli-specification.md#4-web-ui) 补充卡片跳转与来源悬停说明；不改 REST 契约。
- 契约：[Web UI](../04-ui-and-cli-specification.md#4-web-ui)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，ego-browser v2.1.0；`bin/memctl ui` 指向真实 `~/memory` 仓库。
- 命令与结果：`npx vitest run` 16 文件 85 项通过；`npm run build` 与 `go build` 通过；ego-browser 在 1100/1315/1440/1784 px 下导航首项与 `h1` 左边界一致（24/41.5/104/276），390 px 下均为 16，选中项圆角边框完整；点击「原子条目」「图谱节点」「错误诊断」分别进入条目、知识图谱、诊断视图；条目来源 chip 的 `title` 为完整 `agent://` URI；详见 [验证记录](./ui-ux-verification.md#7-概览对齐指标卡跳转与来源提示t045)。
- 未验证与遗留：悬停提示使用浏览器原生标题，样式由浏览器决定，未做自定义浮层；组件级 DOM 测试仍缺失。
- 下一步：提交推送本轮修复后按停止条件暂停并等待用户确认。

### T046：发布 v0.1.2

- 目标与范围：把 `T044`（第二轮 UI/UX 收口）与 `T045`（概览对齐、指标卡跳转、来源提示）以补丁版本发布；不改变 REST 契约、状态字段与记忆仓库格式。
- 契约：[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.1.2` 指向 `fec40b3`。
- 命令与结果：`VERSION=v0.1.2 make release` 全量通过（前端 16 文件 85 项；Go 含 `tests/integration` 234.0 秒），生成四个候选二进制与三个元数据文件；本地 `shasum -a 256 -c SHA256SUMS` 四个文件 `OK`，`dist/memctl-darwin-arm64 version` 输出 `memctl v0.1.2`。tag 推送后 [GitHub Actions 运行 39025243278](https://github.com/gray0128/memctl/actions/runs/39025243278) 在 3 分 30 秒内通过并创建 [Release v0.1.2](https://github.com/gray0128/memctl/releases/tag/v0.1.2)；发布附件含四个二进制与三个元数据文件，下载 `memctl-darwin-arm64` 后与发布 `SHA256SUMS` 校验一致、`version` 输出 `memctl v0.1.2`。该二进制已覆盖安装到 `~/.agent-memory/bin/memctl`，本地 UI 也以 v0.1.2 运行。本地候选包与 CI 产物的校验和不同（构建环境差异），两者各自与对应 `SHA256SUMS` 一致。
- 未验证与遗留：macOS amd64、Linux amd64、Windows amd64 仍只做交叉编译；UI 视觉交互证据沿用 `T044`/`T045` 记录。
- 下一步：发布完成后按 [停止条件](../06-development-loop.md#3-状态与推进) 暂停并等待用户确认。

### T047：dsh 专用插件设计

- 目标与范围：只做设计，不写插件代码、不安装真实插件。核对 DSH 0.2.1-rc.1 的事件与上下文接入点，并按「插件 → 添加插件」对话框实际接受的 spec（npm 包名、GitHub 仓库根、绝对目录、绝对 tarball 路径）比较 npm、内嵌 tarball、Release tarball、独立 GitHub 仓库、本地 link、Discover 索引和直接改 profile 的分发方式；给出 `memctl-dsh` 的包结构、生命周期、上下文注入、同步、适配器探测和发布验收方案。
- 契约：[dsh 专用插件设计提案](../07-dsh-plugin.md)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[验收与交付](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，DSH 0.2.1-rc.1，`dsh-hooks-plugin@0.2.15`，Node v26.10.0；调研来源为 DSH 官方开发文档、DSH 0.2.1-rc.1 的 `app.asar`、`dsh-context-memory`、`dsh-hooks-plugin`、`claude-mem`、`basic-memory` 和 Claude Code 插件市场文档。
- 命令与结果：设计文档内部链接与锚点检查通过；外部参考 URL 均返回 HTTP 200（含 DSH `install-spec.ts`）；正文无弯引号，无行尾空白。纯文档任务，未运行 Go/Node 构建或宿主测试。
- 未验证与遗留：插件未实现；未在真实 DSH 会话中验证 `systemPrompt.context`、`agent/created` 时序或 `turn/end` 触发次数；`D1` 已确认 A（npm 主 + 内嵌 tarball 备用）；独立 GitHub 仓库 `D2`、Discover 市场索引 `D3` 未决定。
- 下一步：等待用户明确进入开发后再启动 `T048`；实现前按设计提案第 10 节更新目录规划、适配器契约和验收 ID。
