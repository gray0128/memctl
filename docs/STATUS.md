# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A 与阶段 B 的已完成任务历史验收记录分别归档于 [阶段 A 归档](./validation/stage-a.md) 与 [阶段 B 归档](./validation/stage-b.md)。

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
- 已完成 `T022`：收口 [阶段 A 实现审查报告](./validation/stage-a-review.md) 记录的 `P1-1`–`P1-3`、`P2-1`–`P2-7`、`P3-1`–`P3-3` 共 13 项问题，只修复既有契约违背并补齐回归证据，未新增产品能力，详见 [T022 归档](./validation/stage-a.md#t022阶段-a-收口修复)。
- 已完成 `T023`（`B-1`）：新增 `internal/server` 只读 HTTP 安全框架与 `snapshot`/`files`/`entries`/`audit`/`doctor`/`conflict` 路由，补 `internal/app` 只读查询与 `internal/git.Log`；通过 `A01`–`A03` 与 `P01`–`P03` 的 HTTP 读取场景，详见 [T023 归档](./validation/stage-b.md#t023http-安全框架与只读-apib-1)。
- 已完成 `T024`（`B-2`）：新增 `internal/watcher`（递归目录监听、150ms 防抖、有界扇出与溢出降级）、`internal/app` 写事务提交后主动失效通知与 `internal/server` 的 `GET /api/v1/events` SSE（Token 鉴权、`vault.updated`、`resync.required`、心跳与 `Server.Watch` 接线）；通过 `A05` 与 `A02` 的 SSE 鉴权场景，详见 [T024 归档](./validation/stage-b.md#t024文件监听与-sse-状态流b-2)。
- 已完成 `T025`（`B-3` 子任务 1）：新增 `web/` 前端工程（Preact、Vite、Tailwind CSS）、`web/embed.go` 内嵌入口与 `Makefile` 的 `ui` 构建链路；`internal/server` 增加内嵌首页（注入 Session Token 与只读标识，本地 `self` CSP）与 `/assets/` 静态资源路由；新增 `memctl ui` 命令（`--port`、`--no-open`、`--readonly`），启动递归监听并在 SIGINT/SIGTERM 时取消请求上下文后关闭；前端实现 Meta Token 读取、JSON API 封装、基于 `fetch` 流的 SSE 解析与断线重连全量校正及顶栏状态；通过 `R01` 的 UI 资产场景、`A02` 的首页 Token 注入与 `A03` 的只读拦截，详见 [T025 归档](./validation/stage-b.md#t025内嵌前端工程与只读视图子任务-1b-3)。
- 已完成 `T026`（`B-3` 子任务 2）：为补齐审计 Diff 数据来源，先在 [模型与契约](./02-domain-models-and-contracts.md#42-路由) 新增只读路由 `GET /api/v1/audit/{hash}/diff` 与 §4.1 的单次提交 Diff 4 MiB 限额，再实现 `internal/git.DiffCommit`（`diff-tree -m --first-parent`、禁用外部 diff 与 textconv、有界读取）、`app.AuditDiff` 与 `internal/server` 处理器；`web/src` 新增知识图谱（Cytoscape.js）、条目检索与过滤、审计 Diff 预览（Diff2Html）与诊断计数展示四个只读视图，图谱与 Diff 库按需分块并随构建内嵌；通过 `B-3` 视图联调与 `R01` 的 UI 资产场景，详见 [T026 归档](./validation/stage-b.md#t026只读业务视图子任务-2b-3)。
- 已完成阶段 B 实现审查：以 01–06 为唯一权威契约审查 `B-1`–`B-3`（`T023`–`T026`）的 `internal/server`、`internal/watcher`、`internal/app/readsvc.go`、`internal/git/log.go`/`diff.go`、`tests/integration/b1`–`b3` 与 `web/` 全量源码，并抽查阶段 A 已收口不变量；识别 `P1-1`（`audit` 的 `before` 未校验即进入 Git argv）、`P2-1`（`snapshot` 保护态缺少 `stale` 快照）、`P2-2`（`conflict` 读路径阻塞取锁）与 `P3-1`–`P3-6` 共 9 项问题，阶段 A 抽查回归成立，详见 [阶段 B 实现审查报告](./validation/stage-b-review.md)；并登记收口任务 `T027`。
- 已完成 `T027`：收口 [阶段 B 实现审查报告](./validation/stage-b-review.md) 的 `P1-1`、`P2-1`–`P2-2` 与 `P3-1`–`P3-6`。审计 `before` 增加十六进制校验并在 `git log` 加 `--end-of-options`；`snapshot` 缓存最近完整快照并在保护态返回 `stale: true`；`conflict` 改用非阻塞取锁；审计过滤分页跳转非匹配页并记录与解析设备来源；概览改用 `doctor` 计数；提交 Diff 使用字面 pathspec；顶栏展示 ahead/behind；知识图谱按内容签名复用实例，详见 [T027 完成记录](#t027阶段-b-审查收口修复)。
- 阶段 A 与阶段 B 全部已完成；尚未实现 Dreaming 与正式安装流程，`--fix` 等确定性诊断修复属 `C-1`；`G05`/`G07` 沿用 A-3/A-4 场景；05 中其余产品验收场景仍待实现与验证。

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
| T022 | 已完成 | 收口 [阶段 A 实现审查](./validation/stage-a-review.md) 的 `P1-1`–`P1-3`、`P2-1`–`P2-7` 与 `P3-1`–`P3-3`（13 项）；依赖阶段 A 全部实现；[归档证据](./validation/stage-a.md#t022阶段-a-收口修复) |
| T023 | 已完成 | 对应 `B-1`：`internal/server` 路由与 HTTP 安全框架、Session Token 鉴权、Host/Origin 校验、安全响应头与只读拦截，以及 `snapshot`/`files`/`entries`/`audit`/`doctor`/`conflict` 只读接口与分页限额；依赖阶段 A 全部完成；[归档证据](./validation/stage-b.md#t023http-安全框架与只读-apib-1) |
| T024 | 已完成 | 对应 `B-2`：`internal/watcher` 递归目录监听与防抖、写事务完成后主动失效通知、`GET /api/v1/events` SSE 有界队列与心跳、`resync.required` 溢出降级与客户端解析校正；依赖 `B-1`；[归档证据](./validation/stage-b.md#t024文件监听与-sse-状态流b-2) |
| T025 | 已完成 | 对应 `B-3` 子任务 1：`web/` 前端工程与 `web/embed.go`、`Makefile` 的 `ui` 链路、`memctl ui` 命令（`--port`/`--no-open`/`--readonly`）、首页 Token 注入、SSE 客户端解析与重连校正及顶栏状态；依赖 `B-1`、`B-2`；[归档证据](./validation/stage-b.md#t025内嵌前端工程与只读视图子任务-1b-3) |
| T026 | 已完成 | 对应 `B-3` 子任务 2：新增只读路由 `GET /api/v1/audit/{hash}/diff`、`internal/git.DiffCommit`、`app.AuditDiff` 与知识图谱/条目检索/审计 Diff/诊断四个只读视图；依赖 `T025`；[归档证据](./validation/stage-b.md#t026只读业务视图子任务-2b-3) |
| T027 | 已完成 | 收口 [阶段 B 实现审查](./validation/stage-b-review.md) 的 `P1-1`、`P2-1`–`P2-2` 与 `P3-1`–`P3-6`（9 项）；只修复既有契约违背并补齐回归证据，不新增产品能力；依赖阶段 B 全部实现；[完成记录](#t027阶段-b-审查收口修复) |

任务 ID 不代表执行顺序。阶段 A 与阶段 B 的全部交付任务（`A-1`–`A-6`、`B-1`–`B-3`）已完成并验收，阶段 B 到达停止条件；阶段 B 实现审查（`T027`）已完成收口。后续按 `C-1` → `C-2` / `C-3` → `D-1` → `D-2` 的依赖关系与 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 依次推进；阶段 C 启动前按 [停止条件](./06-development-loop.md#3-状态与推进) 等待用户确认。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T027：阶段 B 审查收口修复

- 目标与范围：收口 [阶段 B 实现审查报告](./validation/stage-b-review.md) 的 `P1-1`、`P2-1`–`P2-2` 与 `P3-1`–`P3-6`，只修复既有契约违背并补齐回归证据，不新增产品能力。`P1-1`：校验 `GET /api/v1/audit` 的 `before` 并在 `git log` 修订参数前加 `--end-of-options`；`P2-1`：为 `snapshot` 增加最近完整快照缓存与 `stale: true` 分支；`P2-2`：`GET /api/v1/conflict` 改用非阻塞取锁；`P3-1`：修正审计过滤分页游标与非法 `before` 处理；`P3-2`：在提交消息记录设备来源并在审计中解析；`P3-3`：概览计数改用 `doctor` 口径；`P3-4`：提交 Diff 使用字面 pathspec；`P3-5`：顶栏展示 ahead/behind；`P3-6`：知识图谱按内容签名复用实例。
- 契约：[REST 契约](./02-domain-models-and-contracts.md#4-rest-契约)、[修订标识](./02-domain-models-and-contracts.md#11-修订标识)、[审计与诊断对象](./02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Web UI](./04-ui-and-cli-specification.md#4-web-ui)、[受管写入事务](./03-workflows-and-dataflows.md#2-受管写入事务)。
- 验证日期与环境：2026-10-16，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 全部通过；前端 `vitest run` 6 个文件 19 项通过。新增回归：`internal/git.TestLogRejectsOptionLikeBefore`（选项型 `before` 返回 `INVALID_ARGUMENT` 且不写文件）与 `TestLogAcceptsHexCursor`（合法游标只返回更旧提交）；`internal/app.TestSnapshotServesStaleInProtectedState`（保护态返回 `stale: true` 缓存快照）与 `TestConflictStateDoesNotWaitForLock`（非阻塞取锁）；`tests/integration` 的 `TestT027AuditBeforeIsValidated`（HTTP 层非法 `before` 返回 400 且 `--output` 未写文件）、`TestT027AuditAgentPaginationAndDevice`（过滤分页跳过非匹配页、设备来源可解析）与 `TestT027SnapshotIsStaleInProtectedState`（HTTP 层保护态返回过期快照）。回归阶段 A、B 全量用例通过。
- 未验证与遗留：未开展浏览器视觉 UI/UX 验证；`P3-4` 仅修正字面 pathspec，逐文件 `git diff-tree` 调用量保持 O(提交文件数)，未改为单次调用；`P3-2` 的设备来源仅由新提交记录，历史提交仍无 `Device` 字段；仅在 macOS arm64 实机运行。
- 下一步：`T027` 完成，阶段 B 审查问题已收口；按 [停止条件](./06-development-loop.md#3-状态与推进) 阶段 C 启动前等待用户确认。

## 近期实施任务

阶段 B 实现审查与收口修复（`T027`，9 项）已完成并通过验收，报告见 [阶段 B 实现审查报告](./validation/stage-b-review.md)。阶段 B 的 [停止条件](./06-development-loop.md#3-状态与推进) 第一条仍然有效，阶段 C 待用户确认后按 `C-1` → `C-2` / `C-3` 推进。前端构建基线为 Node v26.10.0 / npm 11.19.1，`dist` 不提交，Go 构建与测试前先执行 `make ui`，跨平台与跨版本支持另行验证。
