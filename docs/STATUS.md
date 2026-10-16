# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A 已完成任务的历史验收记录归档于 [阶段 A 归档](./validation/stage-a.md)。

## 当前事实

- 已完成阶段 A-1（`T003`）：建立 `go.mod`、`Makefile`、`cmd/memctl` 与 `internal/cli` 最小命令入口，支持 `version/help`、用法错误返回（退出码 `2`）及未实现子命令显式拒绝（退出码 `1`），通过 `R03` 验收（macOS arm64，Go 1.27.1）。
- 已完成阶段 A-2 的初始化与读取部分（`T006`）：`init` 仅本地创建或复用记忆仓库，`read` 输出原文或 `{filePath, fileRevision, rawContent}`，读取经根句柄并有界；通过 `G01`、`G05` 的本地初始化场景、`F04`、`P01`、`P02` 的读取场景和 `P03` 的读取限额，`R03` 回归通过。
- 已完成阶段 A-2 的解析部分（`T008`）：`internal/vault` 提供行级无损解析（顶层 `- ` 条目、行尾保序元数据、多个 `source`、未知键、根相对 `[[path]]`、代码围栏与行内代码排除）、`entryId` 与链接标识，以及同名歧义、死链和非法路径诊断；通过 `F01`、`F02` 的解析场景，未编辑内容逐字节保留且解析不写入仓库。
- 已完成阶段 A-3（`T007`）：新增 `internal/lock`、`internal/state`、`internal/vault/write.go` 与 `internal/app` 事务服务，提供仓库排他锁、锁内 Git 状态与基线校验、多文件事务与恢复记录、`write`、`sync --capture` 与 `recover`；通过 `W01`、`W03`–`W07`、`G05`–`G07` 的本地保护场景、`P01`–`P03` 与 `L01` 的本地写入场景，回归 A-1、A-2。
- 已完成 `T015`：建立 `STATUS.md` 热台账与 `docs/validation/` 按阶段冷归档的分离机制，将 `T001`–`T014` 完整历史验收记录移入 [阶段 A 归档](./validation/stage-a.md)。
- 已完成 `T014`：更新 [开发循环规范](./06-development-loop.md)、[验收与交付](./05-validation-and-delivery.md#1-交付顺序) 与 [协作入口](../AGENTS.md)，将开发循环调整为「每个会话完成一轮任务、默认由主会话负责全流程并按需委派子智能体、验证通过后更新台账并提交推送、存在可执行任务时创建新会话自动接续」，并在 `05` 补齐阶段 A–D（`A-1`–`A-6`、`B-1`–`B-3`、`C-1`–`C-3`、`D-1`–`D-2`）完整计划。
- 已完成 `T012`–`T013`：引入 `codegraph` 与 `zvec-grep` 代码检索工具并忽略 `/.codegraph/`、`/.zvec-grep/`、`/.commandcode/` 本地工具状态。
- 已完成 `T016`：在 [开发循环规范](./06-development-loop.md#61-接续的执行机制) 第 6 节补充接续执行机制，并提供 Orca 宿主启动入口 `scripts/next-round.sh`，校验四要素交接与「已提交推送」前置条件后启动新会话。
- 尚未实现远端同步（A-4）、Hook（A-5）、前端工程或 Web UI；05 中其余产品验收场景仍待实现与验证。

## 任务

| ID | 状态 | 目标与依赖 |
| --- | --- | --- |
| T001 | 已完成 | 建立项目入口、目录职责、开发循环和本台账；[归档证据](./validation/stage-a.md#t001项目入口与开发循环) |
| T002 | 待开始 | 补齐凭据契约与 G08 场景；A-4 的前置设计任务，不阻塞本地开发 |
| T003 | 已完成 | 对应 A-1：建立最小 Go 工程、`version/help` 和实际构建测试入口；[归档证据](./validation/stage-a.md#t003工程与命令入口) |
| T004 | 已完成 | 在 AGENTS 中明确文档工作必须使用 `tech-doc-style-chinese`；[归档证据](./validation/stage-a.md#t004文档-skill-使用要求) |
| T005 | 已完成 | 补充阶段 A 的实施顺序、依赖、交付物和近期任务验收条件；[归档证据](./validation/stage-a.md#t005阶段-a-开发计划) |
| T006 | 已完成 | 对应 A-2：本地初始化与读取；依赖 T003；[归档证据](./validation/stage-a.md#t006本地初始化与安全读取) |
| T007 | 已完成 | 对应 A-3：受管写入与恢复；依赖 T006；证据见下文 |
| T008 | 已完成 | 对应 A-2：记忆格式与链接解析；依赖 T003；[归档证据](./validation/stage-a.md#t008记忆格式与链接解析) |
| T009 | 已完成 | 在 `06` 规定每轮验证由独立子智能体执行（已由 T014 替代）；[归档证据](./validation/stage-a.md#t009验证者规则) |
| T010 | 已完成 | 更正台账中关于 `tech-doc-style-chinese` 可用性的错误表述，并登记 T011；[归档证据](./validation/stage-a.md#t010更正技能可用性记录) |
| T011 | 待开始 | 统一 `docs/` 与根 `README.md` 的引号用法；依赖已可用的 `tech-doc-style-chinese` |
| T012 | 已完成 | 引入 codegraph 与 zvec-grep 代码检索工具并写入 `AGENTS.md`；[归档证据](./validation/stage-a.md#t012引入代码检索工具) |
| T013 | 已完成 | 收尾 T010 记录的报告级措辞并忽略 `.commandcode/` 本地工具状态；[归档证据](./validation/stage-a.md#t013台账收尾与忽略本地工具状态) |
| T014 | 已完成 | 优化 `06` 开发循环、补齐 `05` 阶段 A–D 完整计划并同步 `AGENTS.md`；[归档证据](./validation/stage-a.md#t014优化开发循环与补齐完整开发计划) |
| T015 | 已完成 | 建立台账冷热分离与按阶段归档规则，归档 `T001`–`T014` 历史记录；[归档证据](./validation/stage-a.md#t015台账冷热分离与阶段归档) |
| T016 | 已完成 | 为 `06` 第 6 节的接续提供 Orca 宿主启动入口 `scripts/next-round.sh` 与四要素校验；[归档证据](./validation/stage-a.md#t016跨会话自动接续入口orca-宿主) |

任务 ID 不代表执行顺序。近期已登记任务建议先完成 `T002`（在 `A-4` 开始前），`T011` 在确定引号基线后执行。后续阶段步骤按 `A-4` → `A-5` → `A-6` → `B-1` → `B-2` → `B-3` → `C-1` → `C-2` / `C-3` → `D-1` → `D-2` 的依赖关系依次登记并推进。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），两者范围不重叠。

## T007：受管写入与恢复

- 目标与范围：完成 A-3，前置为 T006。新增 `internal/lock`、`internal/state`、`internal/vault/write.go`、`internal/app/mutation.go`、`internal/app/recover.go` 及对应测试，实现仓库排他锁、锁内 Git 状态与基线校验、事务恢复记录、`write`、仅本地的 `sync --capture` 与 `recover`；不实现远端同步、Hook、UI、链接重命名与并发 UI 写入。
- 契约：[写入模型](./01-architecture-and-modules.md#2-写入模型)、[受管写入事务](./03-workflows-and-dataflows.md#2-受管写入事务)、[修订标识](./02-domain-models-and-contracts.md#1-修订标识与格式解析)、[错误与部分成功](./02-domain-models-and-contracts.md#43-错误与部分成功)。
- 验证日期与环境：2026-10-16，macOS arm64（26.6.2），Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build`、`make test` 全部通过；`bin/memctl` 在临时 `HOME` 与临时仓库下的 `init`、`read --json`、`write`、`sync --capture`、`recover` 手工闭环成功，本地流程不访问网络。
- `W01`：`TestApplyPutAndRevisionMismatch` 与 `TestA3WriteCoversW01` 验证首个写入成功、旧 `fileRevision` 写入返回 `REVISION_MISMATCH`、首个写入内容不变且提交数不增加；`TestApplyRejectsStaleBaseRevision` 覆盖旧仓库 `baseRevision`。
- `W03`：`TestApplyRejectsExternalChanges`、`TestCaptureCommitsExternalEdits` 与 `TestA3WriteCoversW03AndCapture` 验证存在 staged、unstaged 或 untracked 变化时普通写入返回 `EXTERNAL_CHANGES` 且不落盘；`sync --capture` 只提交冻结的变更清单并使工作树干净。
- `W04`：`TestRecoverRollsBackInterruptedWrite` 在多文件写入第一个文件后注入中断，`recover --rollback` 将全部前镜像与 `HEAD` 恢复一致并删除记录。
- `W05`：`TestRecoverRecognizesCommittedTransaction` 在提交成功后、完成标记前注入中断，恢复通过 `Memctl-Transaction` 标记识别既有提交，补充状态而不重复提交。
- `W06`：`TestRecoverRefusesUserEditedFile` 在恢复前手工修改受影响文件，恢复拒绝覆盖该文件、仍恢复受控路径并在 `RECOVERY_REQUIRED` 中列出待人工处理路径。
- `W07`：`TestApplyHookRejectionRollsBack` 验证 `pre-commit` 拒绝时写入返回 `GIT_FAILED`、前镜像与索引干净且无遗留记录；`TestApplyHookAddingOutOfPlanFileIsReported` 验证钩子夹带计划外文件时报告真实提交与越界路径，恢复识别该提交而不重复提交。
- `G05`–`G07`（本地部分）：`TestA3WriteRequiresIdentity` 验证缺少 Git 身份时写入在修改文件前返回 `GIT_FAILED` 且不改全局配置；`TestApplyRejectsGitOperations/merge in progress` 与 `/unmerged index without rebase directory` 分别通过操作目录与 `git ls-files --unmerged` 拦截进行中的 merge 和无 rebase 目录的未合并索引，返回 `OPERATION_ACTIVE`。
- `P01`–`P03`：`TestApplyRejectsLimitsAndBadPaths` 与 `TestA3WriteBoundaries` 验证逃逸路径、`.git`、设备名与反斜杠路径返回 `INVALID_PATH`，库外符号链接与多硬链接目标返回 `UNSUPPORTED` 且不修改库外内容，超过 2 MiB 的单文件与超过 100 个变更返回 `LIMIT_EXCEEDED`。
- `L01`（本地写入部分）：`TestApplyReportsBusyWhileLockHeld` 验证仓库锁被占用时第二个写入在预算内返回 `BUSY`；`internal/lock` 使用 `gofrs/flock` 在 Git 目录固定文件上取排他锁，释放时不删除锁文件。
- 回归：A-1 `R03` 与 A-2 的 `init`、`read`、格式解析集成与单元测试全部通过；`write` 从「未实现」改为实现后，`R03` 的占位成功检查改用仍未实现的 `resolve`。
- 未运行：`W02`（CLI/UI 并发，属 C-1）、真实 `SIGKILL` 与父/子进程生命周期测试（属 A-4/A-5 的 `L01` 完整场景）、远端同步、Hook、UI、Windows/Linux 平台测试。
- 遗留限制：中断由进程内故障注入模拟，未用真实 `SIGKILL` 验证 OS 锁释放；`capture` 不写恢复记录，中断后重新执行即可；新文件要求父目录已存在；`write` 仅支持单文件，多文件由应用服务测试覆盖；仓库修订对单个文件最多读取 8 MiB。
- 下一步：剩余已登记待办为 `T002`（凭据契约）与 `T011`（引号统一），其后按 `A-4` → `D-2` 推进；本轮验证通过并提交推送后按 [接续的执行机制](./06-development-loop.md#61-接续的执行机制) 创建下一轮会话。

## 近期实施任务

### T002：凭据契约

- 目标与依赖：为 A-4 提供可实施契约，可在本地开发之前或期间单独完成；此任务只完成设计与场景定义，不把 G08 标为测试通过。
- 交付物：01 的凭据与信任边界、03 的环境继承和失败处理流程、05 中 G08 的可复现子场景，必要时同步 04 的用户操作说明。
- 完成检查：SSH/HTTPS 路径分别明确，终端与后台环境差异、交互提示、总超时和错误分类均有确定处理规则及预期结果；影响结果的未决选项获得确认。

当前设计复用系统 Git/SSH 环境，但还没有可实施的后台凭据继承清单和提示抑制协议。该任务需明确 `SSH_AUTH_SOCK`、HTTPS helper、ASKPASS 等环境如何继承或限制，如何处理交互式 helper，以及鉴权失败与超时如何区分。

结果写回 [架构](./01-architecture-and-modules.md)、[同步流程](./03-workflows-and-dataflows.md) 和 [验收矩阵](./05-validation-and-delivery.md)，不在台账定义第二套凭据规则。覆盖「终端认证成功但后台环境不同」的场景；未经验证，不宣称后台一定复用成功。

### T011：文档引号用法统一

- 目标与依赖：先确定 `docs/` 与根 `README.md` 的引号基线，再统一现有正文；依赖已可用的 `tech-doc-style-chinese`。
- 交付物：一次文档一致性修改与对应链接、锚点、行尾空白检查；不新增产品契约，不改变事实、条件与机器可读内容。
- 完成检查：全仓正文引号用法一致；若决定保留混用，则在 [方案索引](./README.md) 说明基线并给出依据。
