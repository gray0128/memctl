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
- 尚未实现前端工程与 Web UI、只读诊断、Dreaming 与正式安装流程；`G05`/`G07` 沿用 A-3/A-4 场景，未新增 A-5 专项用例；05 中其余产品验收场景仍待实现与验证。

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
| T011 | 已完成 | 统一 `docs/` 与根 `README.md` 的引号用法；依赖已可用的 `tech-doc-style-chinese`；证据见下文 |
| T012 | 已完成 | 引入 codegraph 与 zvec-grep 代码检索工具并写入 `AGENTS.md`；[归档证据](./validation/stage-a.md#t012引入代码检索工具) |
| T013 | 已完成 | 收尾 T010 记录的报告级措辞并忽略 `.commandcode/` 本地工具状态；[归档证据](./validation/stage-a.md#t013台账收尾与忽略本地工具状态) |
| T014 | 已完成 | 优化 `06` 开发循环、补齐 `05` 阶段 A–D 完整计划并同步 `AGENTS.md`；[归档证据](./validation/stage-a.md#t014优化开发循环与补齐完整开发计划) |
| T015 | 已完成 | 建立台账冷热分离与按阶段归档规则，归档 `T001`–`T014` 历史记录；[归档证据](./validation/stage-a.md#t015台账冷热分离与阶段归档) |
| T016 | 已完成 | 为 `06` 第 6 节的接续提供 Orca 宿主启动入口 `scripts/next-round.sh` 与四要素校验；[归档证据](./validation/stage-a.md#t016跨会话自动接续入口orca-宿主) |
| T017 | 已完成 | 对应 A-4：前台远端同步；依赖 A-3 与 T002；证据见下文 |
| T018 | 已完成 | 对应 A-5 子任务 1：后台同步派生与进程生命周期；依赖 A-4；[归档证据](./validation/stage-a.md#t018后台同步派生与进程生命周期a-5-子任务-1) |
| T019 | 已完成 | 对应 A-5 子任务 2：Hook 协议、会话身份与宿主性能实测；依赖 T018 与目标宿主协议；证据见下文 |
| T020 | 待开始 | 对应 A-6：只读 `doctor`、CLI 候选包与隔离环境回归；依赖 A-1 至 A-5 的适用检查完成；契约见 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) |

任务 ID 不代表执行顺序。`T019`（A-5 子任务 2：Hook 协议、会话身份与宿主性能实测）已完成；近期已登记任务为 `T020`（A-6：诊断与 CLI 交付）。后续阶段步骤按 `A-6` → `B-1` → `B-2` → `B-3` → `C-1` → `C-2` / `C-3` → `D-1` → `D-2` 的依赖关系与 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 依次滚动登记并推进；每个阶段完成后按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停并通知用户。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T019：Hook 协议、会话身份与宿主性能实测（A-5 子任务 2）

- 目标与范围：完成 A-5 子任务 2，前置为 `T018`。新增 `internal/hookio`（有界 Hook JSON、SID 优先级、envelope 编码）、`internal/state/session.go`（`injected` 与 `syncSucceeded` 分离、14 天清理）、`internal/assets`（内嵌默认指引）、`internal/app/session.go` 与 `internal/cli/hook.go`，接线 `start`/`context`/`pull`/`tag`/`wrap`；`wrap` 透传子进程退出码与信号；不实现安装器与前端。
- 契约：[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)、[环境变量与会话身份](./04-ui-and-cli-specification.md#2-环境变量与会话身份)、[Hook 输入输出与时限](./04-ui-and-cli-specification.md#3-hook-输入输出与时限)、[会话开始与上下文注入](./03-workflows-and-dataflows.md#3-会话开始与上下文注入)、[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build` 全部通过；Hook 集成测试使用临时 HOME 与本地临时记忆仓库、`AGENT_MEMORY_BACKGROUND=0`，不访问网络或真实宿主配置。
- `H01`：`TestA5SessionIDPriorityCoversH01` 覆盖 `--sid`、`AGENT_MEMORY_SID`、`DSH_HOOK_SESSION_ID`、stdin 的 `session_id`/`sessionId`/`sessionID` 优先级、空值跳过、非字符串返回 `INVALID_ARGUMENT`（退出码 2）与无稳定 SID 时不伪造来源。
- `H02`：`TestA5ParallelSessionsDoNotCrossCoversH02` 让 alpha/sessA 与 beta/sessB 交替 `tag`/`write`，来源标签与提交元数据不串会话，`tag` 缺少 SID 返回 `NO_SESSION_ID`，未创建共享 last-sid 目录。
- `H03`：`TestA5OnceSyncRetryCoversH03` 在锁被占用时首轮 `context --once` 仍注入本地正文并记 `sync=busy`，释放锁后同一会话重试同步成功且不重复输出正文；会话记录同时反映 `syncSucceeded` 与 `injected`。
- `H04`：`TestA5HookOutputContractCoversH04` 校验 `start --json` 的 `SessionStart`、`context --json` 的 `UserPromptSubmit` envelope、纯文本 `start` 输出以及 `pull`/`sync` 的空 stdout，无 Git 输出混入 JSON。
- `H05`：`TestA5WrapPassesThroughCoversH05` 校验 `wrap` 向子进程导出 SID、agent 与仓库路径，透传退出码 3、信号终止 `128+15=143`，并在前置同步 `BUSY` 时仍返回子进程状态 7。
- `H06`：`TestA5NoRemoteMissingSIDAndOversizedEntryCoversH06` 在无远端时输出 `sync=local` 与本地正文，无稳定 SID 时每轮注入不去重，入口文件超过 2 MiB 时明确提示改用受管读取而不内联截断。
- 性能与宿主：本地性能基线与宿主适配状态归档于 [阶段 A 性能与适配记录](./validation/stage-a.md#阶段-a-性能与适配记录)；`version` 的 p50 约 5.6ms，未达 `< 5ms` 优化目标；宿主 Hook 未安装，无真实注入验证。
- 未验证与遗留：Windows/Linux 的 `wrap` 与锁路径未运行；未在真实宿主验证 `additionalContext` 是否被采纳；`T018` 记录已归档至 [阶段 A 归档](./validation/stage-a.md#t018后台同步派生与进程生命周期a-5-子任务-1)。
- 下一步：剩余 `A-6`（诊断与 CLI 交付，`T020`），再按 `B-1` → `D-2` 推进；阶段 A 在 `A-6` 完成后暂停自动接续并通知用户。

## 近期实施任务

### T020：诊断与 CLI 交付（A-6）

- 目标与依赖：完成 A-6，前置为 A-1 至 A-5 的适用检查完成。交付只读 `doctor`（`--json`、`--fix` 在当前基线上执行支持的确定性修复）、内嵌默认指引的包内可用性验证、CLI 候选包与隔离环境回归；列明未验证的平台与宿主。
- 交付物：`internal/doctor` 诊断实现与对应测试，`doctor` 命令接线，按 [验收与交付](./05-validation-and-delivery.md#1-交付顺序) 复跑前序相关用例，并覆盖 `F03`、`R04`、`R01` 的默认指引与 `R02` 的 Git/凭据降级场景。
- 完成检查：`doctor` 默认只读、Level 与退出码符合契约（error 返回 1，仅 warning/info 返回 0）；复制二进制到空 HOME 后内嵌指引可用；未验证的平台、宿主与降级路径如实记录于阶段报告。
