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
- 尚未实现 Hook 与后台同步（A-5）、前端工程与 Web UI、诊断、Dreaming 与安装；`G05`/`G07` 未新增 A-4 专项用例，`G08.2`（后台派生凭据）属 A-5；05 中其余产品验收场景仍待实现与验证。

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
| T018 | 待开始 | 对应 A-5 子任务 1：后台同步派生与进程生命周期；依赖 A-4；契约见 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) |
| T019 | 待开始 | 对应 A-5 子任务 2：Hook 协议、会话身份与宿主性能实测；依赖 T018 与目标宿主协议；契约见 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) |

任务 ID 不代表执行顺序。`T017`（A-4 前台远端同步）已完成；近期已登记任务为 `T018`、`T019`（共同对应 `A-5` Hook 与后台）。后续阶段步骤按 `A-5` → `A-6` → `B-1` → `B-2` → `B-3` → `C-1` → `C-2` / `C-3` → `D-1` → `D-2` 的依赖关系与 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 依次滚动登记并推进。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T017：前台远端同步（A-4）

- 目标与范围：完成 A-4，前置为 A-3 与 `T002`。新增 `internal/syncer`、`internal/git/exec.go`、`internal/git/sync.go`、`internal/git/proc_unix.go`、`internal/git/proc_other.go`、`internal/state/conflict.go`、`internal/state/cache.go`、`internal/app/remote.go`、`internal/app/sync.go`、`internal/app/resolve.go` 与 `internal/cli/tty_*.go`；实现空远端与已有远端接入及远端审批、`sync --wait` 前台同步、错误分类、非交互凭据抑制与进程组清理、CLI 多阶段 `resolve` 及冲突类型拒绝；不实现后台派生（A-5）、Hook、UI 与安装。
- 契约：[凭据与信任边界](./01-architecture-and-modules.md#34-凭据与信任边界)、[初始化与远端接入](./03-workflows-and-dataflows.md#1-初始化与远端接入)、[同步与后台任务](./03-workflows-and-dataflows.md#4-同步与后台任务)、[凭据环境继承与失败处理](./03-workflows-and-dataflows.md#43-凭据环境继承与失败处理)、[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build` 全部通过；远端集成测试使用临时 HOME、临时本地 bare 远端、受控 `GIT_SSH_COMMAND` 桩与本地 `git http-backend`，并设置 `GIT_CONFIG_NOSYSTEM=1` 与受限 `GIT_ALLOW_PROTOCOL`，不访问真实远端、网络或系统凭据库。
- `G02`/`G03`：`TestA4PrivateRemoteRoundTripCoversG02` 验证空远端首推、第二设备克隆相同历史并建立 `origin/main` upstream、不产生独立根提交；`TestA4RemoteBoundaryAndErrors` 覆盖错误 URL 不当作空远端、SSH 认证失败分类为 `AUTH_FAILED`、无默认分支明确失败、取消 upstream 后按初始化协议重建。
- `G04`/`G06`：`pre-push` 钩子在 fetch 后推进远端时 push 被拒返回 `NON_FAST_FORWARD` 且 `remoteState=pending`、本地提交保留；`TestA4ManualGitStatePausesSyncCoversG06` 在用户手工 merge 冲突下让 `sync --wait` 与受管 `write` 均返回 `OPERATION_ACTIVE`。
- `C01`–`C04`：`TestA4ConflictTwoStagesCoversC01` 验证两个连续本地提交分别冲突时每阶段的三方快照与 `captured`→`resolving` 相位；`TestA4ConflictStaleCoversC02` 验证外部 HEAD 变化后旧结果返回 `CONFLICT_STALE` 且不覆盖新历史；`TestA4ConflictTypesCoversC03` 区分删除/修改、空文件与文本冲突并拒绝二进制与 rename 类型（`UNSUPPORTED`）；`TestA4ResolveAbortCoversC04` 验证跨进程读取持久化阶段并以 `--abort` 恢复原始 HEAD。
- `G08`：`G08.1` 受控 SSH 桩成功同步并传递 `SSH_AUTH_SOCK`；`G08.3` 本地 HTTP + `git http-backend` 下非交互 `credential.helper` 认证成功且凭据不出现在输出；`G08.4` 无 TTY 时 `GIT_ASKPASS`/`SSH_ASKPASS` 未被调用；`G08.5` 挂起桩在总 deadline 到期后返回 `TIMEOUT`，进程组被终止且锁可重新获取；`G08.6` 主机密钥失败返回 `AUTH_FAILED` 且不写 `known_hosts`；`G08.7` 含凭据 URL 在诊断中被脱敏为 `://***@`。
- `L01`（前台 Git 子进程）：`G08.5` 用例同时验证 deadline 到达后无遗留 Git 子进程且仓库锁可再次获取。
- 未验证与遗留：`G08.2`（终端成功但后台凭据环境不同）依赖 A-5 后台派生，本轮未实现或验证，`G08` 作为复合项未整体标记通过；`G05`（签名与 hooks 拒绝）沿用 A-3 的缺失身份与 hook 拒绝场景，`G07` 沿用共享 `git.Inspect` 未合并索引检测，本轮未新增专项用例；真实 `SIGKILL` 父进程与宿主清理属 A-5 平台门槛；未覆盖 Windows/Linux。
- 下一步：剩余已登记待办为 `T018`、`T019`（共同对应 `A-5` Hook 与后台），其后按 `A-6` → `D-2` 推进；本轮按用户指示暂停接续。

## 近期实施任务

### T018：后台同步派生与进程生命周期（A-5 子任务 1）

- 目标与依赖：完成 A-5 的后台同步与进程生命周期部分，前置为 A-4（`T017`）。实现 `sync` / `end` 自动路径与受管 `write` 提交后的非交互后台同步派生、父进程凭据环境白名单快照传递、锁忙立即退出的防风暴机制，以及父进程/宿主/Git 子进程强杀后的进程组清理与锁释放；Hook 会话身份、`start`/`context`/`pull`/`tag`/`wrap` 留待 `T019`。
- 交付物：后台派生与同步工作进程实现、`sync`（无 `--wait`/`--capture`）与 `end` 命令入口及对应集成测试；覆盖 `G08.2`、`L01`（后台与强杀场景）、`L02`。
- 完成检查：后台凭据环境隔离与补推（`G08.2`）、强杀父进程/子进程后无无锁写仓库残留（`L01`）、连续触发 `sync`/`end` 且网络不可用时无后台进程无限排队（`L02`）全部通过。

### T019：Hook 协议、会话身份与宿主性能实测（A-5 子任务 2）

- 目标与依赖：完成 A-5 的 Hook 协议、会话身份及宿主与性能实测部分，前置为 `T018` 与明确的目标宿主协议。交付 `start`/`context`/`pull`/`tag`/`wrap`、SID 优先级解析、`--once` 同步与注入分离去重、信号与退出码透传；安装器留待 C-3。
- 交付物：实现、对应测试，以及按 [验收与交付](./05-validation-and-delivery.md#3-性能与适配记录) 的宿主与性能实测记录；覆盖 `H01`–`H06` 及第 3 节性能与适配记录。
- 完成检查：Hook 输入输出协议、会话身份优先级、并行会话不串源、`--once` 去重、`wrap` 退出码与信号透传场景全部通过，并在 [阶段 A 归档](./validation/stage-a.md) 或阶段报告记录宿主版本、命令与结果。
