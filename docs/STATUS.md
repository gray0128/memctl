# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A 已完成任务的历史验收记录归档于 [阶段 A 归档](./validation/stage-a.md)。

## 当前事实

- 已完成阶段 A-1（`T003`）：建立 `go.mod`、`Makefile`、`cmd/memctl` 与 `internal/cli` 最小命令入口，支持 `version/help`、用法错误返回（退出码 `2`）及未实现子命令显式拒绝（退出码 `1`），通过 `R03` 验收（macOS arm64，Go 1.27.1）。
- 已完成阶段 A-2 的初始化与读取部分（`T006`）：`init` 仅本地创建或复用记忆仓库，`read` 输出原文或 `{filePath, fileRevision, rawContent}`，读取经根句柄并有界；通过 `G01`、`G05` 的本地初始化场景、`F04`、`P01`、`P02` 的读取场景和 `P03` 的读取限额，`R03` 回归通过。
- 已完成阶段 A-2 的解析部分（`T008`）：`internal/vault` 提供行级无损解析（顶层 `- ` 条目、行尾保序元数据、多个 `source`、未知键、根相对 `[[path]]`、代码围栏与行内代码排除）、`entryId` 与链接标识，以及同名歧义、死链和非法路径诊断；通过 `F01`、`F02` 的解析场景，未编辑内容逐字节保留且解析不写入仓库。
- 已完成阶段 A-3（`T007`）：新增 `internal/lock`、`internal/state`、`internal/vault/write.go` 与 `internal/app` 事务服务，提供仓库排他锁、锁内 Git 状态与基线校验、多文件事务与恢复记录、`write`、`sync --capture` 与 `recover`；通过 `W01`、`W03`–`W07`、`G05`–`G07` 的本地保护场景、`P01`–`P03` 与 `L01` 的本地写入场景，回归 A-1、A-2。
- 已完成 `T002`（凭据契约）：在 01 定义凭据与信任边界及同步环境白名单，在 03 定义环境继承、交互抑制、总超时与失败分类，在 05 定义 `G08.1`–`G08.7` 可复现子场景，并在 04 补充凭据交互说明；为 `A-4` 提供可实施契约，未把 `G08` 标为测试通过。
- 已完成 `T015`：建立 `STATUS.md` 热台账与 `docs/validation/` 按阶段冷归档的分离机制，将 `T001`–`T014` 完整历史验收记录移入 [阶段 A 归档](./validation/stage-a.md)。
- 已完成 `T014`：更新 [开发循环规范](./06-development-loop.md)、[验收与交付](./05-validation-and-delivery.md#1-交付顺序) 与 [协作入口](../AGENTS.md)，将开发循环调整为「每个会话完成一轮任务、默认由主会话负责全流程并按需委派子智能体、验证通过后更新台账并提交推送、存在可执行任务时创建新会话自动接续」，并在 `05` 补齐阶段 A–D（`A-1`–`A-6`、`B-1`–`B-3`、`C-1`–`C-3`、`D-1`–`D-2`）完整计划。
- 已完成 `T012`–`T013`：引入 `codegraph` 与 `zvec-grep` 代码检索工具并忽略 `/.codegraph/`、`/.zvec-grep/`、`/.commandcode/` 本地工具状态。
- 已完成 `T016`：在 [开发循环规范](./06-development-loop.md#61-接续的执行机制) 第 6 节补充接续执行机制，并提供 Orca 宿主启动入口 `scripts/next-round.sh`，校验四要素交接与「已提交推送」前置条件后启动新会话。
- 已完成 `T011`（文档引号用法统一）：按 `tech-doc-style-chinese` 默认基线将 `01`–`04` 与 `docs/README.md` 正文的弯引号统一为直角引号「」，并在 [方案索引](./README.md) 声明引号基线；未新增或改变产品契约，未修改源码与 `脚本化实现参考/`。
- 已完成阶段 A-4（`T017`）：新增 `internal/syncer` 与 `internal/git`、`internal/state`、`internal/app`、`internal/cli` 的远端相关实现，提供空远端与已有远端接入及远端审批、`sync --wait` 前台同步、错误分类（`AUTH_FAILED`/`NETWORK_UNAVAILABLE`/`NON_FAST_FORWARD`/`TIMEOUT`/`NO_UPSTREAM`/`CONTENT_CONFLICT`）、非交互凭据抑制与 Git 进程组清理、CLI 多阶段 `resolve` 与不支持冲突类型拒绝；通过 `G02`–`G04`、`G06`、`C01`–`C04`、`L01` 前台 Git 子进程场景与 `G08.1`、`G08.3`–`G08.7`。
- 已完成阶段 A-5 子任务 1（`T018`）：新增 `internal/syncer` 后台派生与凭据白名单快照，重写 `internal/lock`（`TryAcquire`、可被 Git 子进程继承的锁描述符），实现 `sync`/`end` 自动路径与受管 `write` 提交后的非交互后台同步、锁忙立即退出与进程组/锁生命周期继承；通过 `G08.2`、`L01` 后台与强杀场景、`L02`。
- 已完成阶段 A-5 子任务 2（`T019`）：新增 `internal/hookio`、`internal/assets`、`internal/state/session.go` 与 `internal/app/session.go`，接线 `start`/`context`/`pull`/`tag`/`wrap`；提供 Hook JSON 有界读取与 SID 优先级解析、`injected` 与 `syncSucceeded` 分离的会话级去重、14 天缓存清理、内嵌默认指引与 envelope 编码，以及 `wrap` 的子进程退出码与信号透传；通过 H01–H06，并归档本机性能基线与宿主适配状态。
- 已完成阶段 A-6（`T020`）：新增 `internal/doctor` 只读诊断与 `internal/app/doctor.go`、`internal/cli/doctor.go` 接线，把写入事务与诊断共用的仓库修订摘要抽到 `internal/vault.RepoRevision`；`doctor` 支持 `--json`，按契约以 error 返回退出码 `1`、仅 warning/info 返回 `0`，不改变记忆内容、Git 索引或 HEAD；通过 `F03`、`R04`、`R01` 的默认指引场景与 `R02` 的 Git/凭据场景。CLI `doctor --fix` 按阶段计划属 `C-1`，本轮显式返回 `UNSUPPORTED`。
- 已完成 `T021`：适配 Orca CLI 1.4.227 重构 `scripts/next-round.sh`，按 `orca-cli` 完全所有权交接规范改用 `orca terminal create` + `orca terminal wait --for tui-idle` + `orca terminal send --enter --json` 三步流程启动新会话并投递交接提示词，支持 `--terminal <handle> --retry-request <id>`（兼容别名 `--retry-launch`）对同一次提示词投递做幂等重放，并同步更新 `AGENTS.md` 与 [开发循环规范](./06-development-loop.md#61-接续的执行机制)。
- 已完成 `T022`：收口 [阶段 A 实现审查报告](./validation/stage-a-review.md) 记录的 `P1-1`–`P1-3`、`P2-1`–`P2-7`、`P3-1`–`P3-3` 共 13 项问题，只修复既有契约违背并补齐回归证据，未新增产品能力，详见 [T022 完成记录](#t022阶段-a-收口修复)。
- 尚未实现前端工程与 Web UI、Dreaming 与正式安装流程；`--fix` 等确定性诊断修复属 `C-1`；`G05`/`G07` 沿用 A-3/A-4 场景，未新增 A-5 专项用例；05 中其余产品验收场景仍待实现与验证。

## 任务

| ID | 状态 | 目标与依赖 |
| --- | --- | --- |
| T001 | 已完成 | 建立项目入口、目录职责、开发循环和本台账；[归档证据](./validation/stage-a.md#t001项目入口与开发循环) |
| T002 | 已完成 | 补齐凭据契约与 G08 场景；A-4 的前置设计任务，不阻塞本地开发；[归档证据](./validation/stage-a.md#t002凭据契约) |
| T003 | 已完成 | 对应 A-1：建立最小 Go 工程、`version/help` 和实际构建测试入口；[归档证据](./validation/stage-a.md#t003工程与命令入口) |
| T004 | 已完成 | 在 AGENTS 中明确文档工作必须使用 `tech-doc-style-chinese`；[归档证据](./validation/stage-a.md#t004文档-skill-使用要求) |
| T005 | 已完成 | 补充阶段 A 的实施顺序、依赖、交付物和近期任务验收条件；[归档证据](./validation/stage-a.md#t005阶段-a-开发计划) |
| T006 | 已完成 | 对应 A-2：本地初始化与读取；依赖 T003；[归档证据](./validation/stage-a.md#t006本地初始化与安全读取) |
| T007 | 已完成 | 对应 A-3：受管写入与恢复；依赖 T006；[归档证据](./validation/stage-a.md#t007受管写入与恢复) |
| T008 | 已完成 | 对应 A-2：记忆格式与链接解析；依赖 T003；[归档证据](./validation/stage-a.md#t008记忆格式与链接解析) |
| T009 | 已完成 | 在 `06` 规定每轮验证由独立子智能体执行（已由 T014 替代）；[归档证据](./validation/stage-a.md#t009验证者规则) |
| T010 | 已完成 | 更正台账中关于 `tech-doc-style-chinese` 可用性的错误表述，并登记 T011；[归档证据](./validation/stage-a.md#t010更正技能可用性记录) |
| T011 | 已完成 | 统一 `docs/` 与根 `README.md` 的引号用法；依赖已可用的 `tech-doc-style-chinese`；[归档证据](./validation/stage-a.md#t011文档引号用法统一) |
| T012 | 已完成 | 引入 codegraph 与 zvec-grep 代码检索工具并写入 `AGENTS.md`；[归档证据](./validation/stage-a.md#t012引入代码检索工具) |
| T013 | 已完成 | 收尾 T010 记录的报告级措辞并忽略 `.commandcode/` 本地工具状态；[归档证据](./validation/stage-a.md#t013台账收尾与忽略本地工具状态) |
| T014 | 已完成 | 优化 `06` 开发循环、补齐 `05` 阶段 A–D 完整计划并同步 `AGENTS.md`；[归档证据](./validation/stage-a.md#t014优化开发循环与补齐完整开发计划) |
| T015 | 已完成 | 建立台账冷热分离与按阶段归档规则，归档 `T001`–`T014` 历史记录；[归档证据](./validation/stage-a.md#t015台账冷热分离与阶段归档) |
| T016 | 已完成 | 为 `06` 第 6 节的接续提供 Orca 宿主启动入口 `scripts/next-round.sh` 与四要素校验；[归档证据](./validation/stage-a.md#t016跨会话自动接续入口orca-宿主) |
| T017 | 已完成 | 对应 A-4：前台远端同步；依赖 A-3 与 T002；[归档证据](./validation/stage-a.md#t017前台远端同步a-4) |
| T018 | 已完成 | 对应 A-5 子任务 1：后台同步派生与进程生命周期；依赖 A-4；[归档证据](./validation/stage-a.md#t018后台同步派生与进程生命周期a-5-子任务-1) |
| T019 | 已完成 | 对应 A-5 子任务 2：Hook 协议、会话身份与宿主性能实测；依赖 T018 与目标宿主协议；[归档证据](./validation/stage-a.md#t019hook-协议会话身份与宿主性能实测a-5-子任务-2) |
| T020 | 已完成 | 对应 A-6：只读 `doctor`、内嵌默认指引与隔离环境回归；依赖 A-1 至 A-5 的适用检查完成；[归档证据](./validation/stage-a.md#t020诊断与-cli-交付a-6) |
| T021 | 已完成 | 适配 Orca 1.4.227 重构 `scripts/next-round.sh`，改用 `orca terminal create` + `wait` + `send` 完全交接流并支持 `--terminal` + `--retry-request` 重放；消除接续阻塞；[归档证据](./validation/stage-a.md#t021修复跨会话接续入口以适配-orca-14227) |
| T022 | 已完成 | 收口 [阶段 A 实现审查](./validation/stage-a-review.md) 的 `P1-1`–`P1-3`、`P2-1`–`P2-7` 与 `P3-1`–`P3-3`（13 项）；依赖阶段 A 全部实现；[完成记录](#t022阶段-a-收口修复) |

任务 ID 不代表执行顺序。阶段 A 的全部任务（`A-1`–`A-6`）已完成，当前无进行中任务与待开始任务，按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并通知用户。后续在用户确认后按 `B-1` → `B-2` → `B-3` → `C-1` → `C-2` / `C-3` → `D-1` → `D-2` 的依赖关系与 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 依次滚动登记并推进；每个阶段完成后按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停并通知用户。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T020：诊断与 CLI 交付（A-6）

- 目标与范围：完成 A-6，前置为 A-1 至 A-5 的适用检查完成。新增 `internal/doctor` 只读诊断与 `internal/app/doctor.go`、`internal/cli/doctor.go` 接线，把写入事务与诊断共用的仓库修订摘要抽到 `internal/vault.RepoRevision`；`doctor` 支持 `--json`。CLI `doctor --fix` 与 `POST /api/v1/doctor:fix` 按阶段计划属 `C-1`，A-6 的 `--fix` 显式返回 `UNSUPPORTED`，不返回占位成功，所有 `DiagnosticIssue.autoFixable` 恒为 `false`。
- 契约：[诊断对象与级别](./02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)、[Hook 输入输出与时限](./04-ui-and-cli-specification.md#3-hook-输入输出与时限)、[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)、[阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分)；完整记录见 [T020 归档](./validation/stage-a.md#t020诊断与-cli-交付a-6)。
- 验证日期与环境：2026-10-16，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./... -count=1`、`make build` 全部通过。`F03` 与 `R04` 由 `TestA6DoctorOrphanRingCoversF03`、`TestA6DoctorReadOnlyDiagnosisCoversR04` 覆盖（孤立环标记、死链为 `error` 返回 1、仅 `warning`/`info` 返回 0、执行前后文件与 Git 状态一致）；`R01` 默认指引由 `TestR01EmbeddedInstructionsFromEmptyHome` 覆盖（空 HOME 仅复制二进制即输出内嵌指引）；`R02` 的 Git/凭据场景由 `TestR02MissingGitIsTargeted`、`TestR02MissingCredentialsAreTargeted` 覆盖。
- 未验证与遗留：`doctor` 仅在 macOS arm64 实机运行，Windows/Linux 的 Git 中间态、`index.lock` 与路径语义未运行；真实宿主注入未验证（安装属 `C-3`）；runner 场景按 05 留待 `D-1`。
- 下一步：阶段 A 全部完成，按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并通知用户；待用户确认后再登记并启动 `B-1`（本地 HTTP 安全框架与只读 API）。

## T022：阶段 A 收口修复

- 目标与范围：按 [阶段 A 实现审查报告](./validation/stage-a-review.md) 收口全部 13 项问题，不新增产品能力。`P1`/`P2` 行为修复包括：`resolve --apply` 在重启变基前完成冲突类型、路径覆盖、动作枚举、文件模式、大小与 UTF-8 的静态校验，并在失败时恢复 `captured` 阶段；持锁会话的全部 Git 子进程继承锁描述符；前台 Git 子进程改用单一凭据白名单；`ReadFile` 在锁忙、待恢复事务、待解决冲突与 Git 中间态时拒绝读取；`Classify` 保留 `NO_UPSTREAM` 等契约错误码且同步成功后清零 ahead/behind；本地提交在有远端时返回并记录 `pending`；`sync --capture` 解析 Agent/SID 并触发后台同步；`doctor` 并列报告多重 Git 状态、孤立操作记录、单词推荐元数据与全文可疑短语；`wrap` 向被包装进程转发终止信号；`init --remote` 不再删除调用方提供的空目录，克隆前校验远端默认分支，连接已有远端时校验全部 URL 并校验共同历史后才写入远端配置与批准记录。`P3` 收口包括 `WriteAtomic` 显式设置模式、`ListManaged` 过滤临时文件、`Stat` 在截断读取时把完整长度并入摘要、`Lock.Release` 只关闭描述符不做显式解锁、`SyncSummary` 补 `repoId`/`localState`，并把凭据超时用例的总预算从 2 秒放宽到 5 秒。
- 契约：[凭据与信任边界](./01-architecture-and-modules.md#34-凭据与信任边界)、[写入不变量](./01-architecture-and-modules.md#23-写入不变量)、[中断与恢复](./03-workflows-and-dataflows.md#21-中断与恢复)、[Git 同步步骤](./03-workflows-and-dataflows.md#42-git-同步步骤)、[UI 与 CLI 共用的解决过程](./03-workflows-and-dataflows.md#52-ui-与-cli-共用的解决过程)、[初始化与远端接入](./03-workflows-and-dataflows.md#1-初始化与远端接入)、[审计、诊断、整理与接入](./02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[同步与恢复对象](./02-domain-models-and-contracts.md#22-同步与恢复)。本轮把 `01` §3.4 与 `03` §4.3 对前台凭据环境的表述统一为「前台与后台 Git 子进程使用同一份白名单」，并把身份覆盖变量并入该白名单；`G08.2` 用例改用标记文件而不是环境变量区分前后台凭据环境，以符合新的白名单边界。
- 验证日期与环境：2026-10-16，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./... -count=1`、`make build` 全部通过。回归证据：`TestPlanResolvedValidatesBeforeTouchingTheRepository`、`TestA4ResolveRejectsInvalidInputCoversC01`、`TestA4ResolvePreservesFileModeCoversC02`（`C01`、`C03`）、`TestLocalGitChildrenInheritLockCoversL01`、`TestKilledWriteParentKeepsLockWithGitCoversL01`、`TestReleaseKeepsLockWhileInheritedDescriptorIsOpen`（`L01`）、`TestSyncEnvDropsRepositoryRedirectsAndUnknownVariables`、`TestA5BackgroundCredentialDifferenceCoversG08_2`（`G08.2`）、`TestReadFileRefusesProtectedStates`、`TestApplyReportsPendingRemoteState`、`TestA4LocalCommitReportsPendingAndResetsCountsCoversG02`（`G02`）、`TestA4InitRemoteBoundariesCoversG02G04`（`G02`、`G04`）、`TestCheckReportsEveryGitStateAndOrphanOperation`、`TestCheckScansWholeTextForSuspiciousPhrases`、`TestCheckFlagsEntryMissingOneRecommendedMetadata`（`F03`、`R04`）、`TestA5WrapForwardsTerminationSignalsCoversH05`（`H05`）、`TestWriteAtomicSetsModeDespiteUmask`、`TestListManagedSkipsAtomicTempFiles`（`W01`–`W07`）。
- 未验证与遗留：审查报告 `P3-3` 写的 `sync_timeout_sec` 在当前测试中不存在，实际受限的是用例传入的总预算，已按报告意图放宽；严格凭据白名单的实测仅在 macOS arm64，Windows/Linux 的 Git 子进程环境与 `wrap` 信号转发位置未运行；`G05`/`G07` 仍沿用 A-3/A-4 用例，未新增专项；`doctor --fix` 仍属 `C-1`。
- 下一步：阶段 A 已全部收口，按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并通知用户；待用户确认后按 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 登记并启动 `B-1`（`internal/server` 只读 API 与 HTTP 安全框架）。

## 近期实施任务

阶段 A 与 `T022` 收口均已完成，当前无进行中任务与待开始任务。按 [停止条件](./06-development-loop.md#3-状态与推进)，本轮不自动创建下一轮会话；待用户确认后按 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 将 `B-1`（`internal/server` 只读 API 与 HTTP 安全框架）登记为下一轮任务，`B-2`、`B-3` 随后滚动登记。
