# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A、阶段 B、阶段 C 与阶段 D 的已完成任务历史验收记录分别归档于 [阶段 A 归档](./validation/stage-a.md)、[阶段 B 归档](./validation/stage-b.md)、[阶段 C 归档](./validation/stage-c.md) 与 [阶段 D 归档](./validation/stage-d.md)，阶段 C 实现审查见 [阶段 C 实现审查报告](./validation/stage-c-review.md)。

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
- 已完成 `T027`：收口 [阶段 B 实现审查报告](./validation/stage-b-review.md) 的 `P1-1`、`P2-1`–`P2-2` 与 `P3-1`–`P3-6`。审计 `before` 增加十六进制校验并在 `git log` 加 `--end-of-options`；`snapshot` 缓存最近完整快照并在保护态返回 `stale: true`；`conflict` 改用非阻塞取锁；审计过滤分页跳转非匹配页并记录与解析设备来源；概览改用 `doctor` 计数；提交 Diff 使用字面 pathspec；顶栏展示 ahead/behind；知识图谱按内容签名复用实例，详见 [T027 归档](./validation/stage-b.md#t027阶段-b-审查收口修复)。
- 已完成阶段 C 的 `C-1` 子任务 1（`T028`）：新增 `app.PutFile`、`app.MoveFile`、`app.CreateEntry`、`app.PatchEntry`、`app.DeleteEntry`、`app.DoctorFix` 与 `vault.Root.RenameFile`、`vault.RewriteLinks`，注册 `PUT /api/v1/files/{path...}`、`POST /api/v1/files:move`、`POST/PATCH/DELETE /api/v1/entries`、`POST /api/v1/doctor:fix`、`POST /api/v1/sync`，并实现 CLI `doctor --fix`；写入全部复用受管事务、仓库锁与版本基线，保持 `--readonly`、`X-Memctl-Token`、Host/Origin 校验与统一路径边界不变；通过 `A04`、`W01`–`W04`（含 `W02` CLI/UI 并发与 `W04` 多文件移动中断恢复）与 `P01`–`P03` 的 HTTP 写入与移动场景，详见 [T028 归档](./validation/stage-c.md#t028受管写入移动改链与确定性修复后端及-apic-1-子任务-1)。
- 已完成阶段 C 的 `C-1` 子任务 2（`T029`）：前端接入 `T028` 的文件与条目原文编辑、条目移动改链、图谱节点拆分新文件、手动同步与确定性修复，并在 412 `REVISION_MISMATCH` 时保留草稿、并列展示服务器当前内容与草稿行差异；新增纯逻辑模块 `web/src/writes.js`、`web/src/edits.js`、`web/src/splits.js` 与组件 `FileEditor`、`SplitPanel`、`DraftDiff`；通过 `C-1` 前后端联调（`TestC1UiEditFlowCoversIntegration`）与 `W02`、`A04` 回归，前端 `vitest` 9 个文件 42 项通过，详见 [T029 归档](./validation/stage-c.md#t029ui-编辑移动改链与图谱拆分交互c-1-子任务-2)。
- 已完成 `T033`（`C-3` 子任务 2）：实现 `GET /api/v1/adapters`、`POST /api/v1/adapters/{agent}/install`（`{dryRun?, expectedConfigRevisions?}`）与 `--readonly` 安装接口拦截，接入 UI「整理接入」adapter 探测、预览与安装面板；复用 `adapters.Plan`/`Apply`/`Recover`，不重复实现合并逻辑；详见 [T033 归档](./validation/stage-c.md#t033http-安装接口与-ui-接入面板c-3-子任务-2)。
- 已完成阶段 C 实现审查与 `T034` 收口：以 01–06 为唯一权威契约审查 `C-1`–`C-3`（`T028`–`T033`）的 `internal/app`、`internal/vault`、`internal/git`、`internal/server`、`internal/adapters`、`internal/cli`、`web/src` 与 `tests/integration/c1`–`c3`，识别 4 项 P1、10 项 P2 与 13 项 P3，详见 [阶段 C 实现审查报告](./validation/stage-c-review.md)；修复 23 项并补回归测试，`P3-12` 部分修复，`P3-6`、`P3-7`、`P3-13` 作为已知限制登记，详见 [T034 归档](./validation/stage-c.md#t034阶段-c-实现审查收口修复)。
- 已完成阶段 D 的 `D-1`（`T035`）：新增 `internal/dream`（锁内干净基线、受支持文件独立临时目录导出、macOS Seatbelt 受限执行、固定基线 Diff 与范围校验、`$STATE/repos/<repoId>/proposals/` 持久化）、CLI `dream --runner`/`dream --show`、`POST /api/v1/dream:preview`（202）与 `GET /api/v1/dream/{proposalId}` 及终态 `dream.updated` 事件；`dream --apply` 与 `POST /api/v1/dream:apply` 显式返回 `UNSUPPORTED`（属 `D-2`）；通过 `D01`、`D02` 的脏库拒绝生成、`D03`、`R02` 的无 runner 与缺少沙箱降级场景，`A03` 只读拦截由既有中间件覆盖，详见 [T035 归档](./validation/stage-d.md#t035dreaming-隔离生成与持久化提案预览d-1)。
- 已完成阶段 D 的 `D-2` 子任务 1（`T036`）：新增 `internal/dream` 的 `PrepareApply` 与 `applying`/`ready`/`stale`/`applied` 状态迁移（持久化每个 create/update 变化的原始内容与 Git 模式），`app.ApplyDream` 在锁内按事务记录结算中断的 apply、经 `app.Apply` 提交（事务记录保存 `proposalId`）并回写 `appliedCommit`，CLI `dream --apply ID --patch-hash HASH`、`POST /api/v1/dream:apply`（`--readonly` 返回 `READ_ONLY`）与 UI「整理接入」的提案生成、固定 Diff 预览、状态查询与确认应用（沙箱不可用禁用生成、过期提案禁止应用）均已落地；通过 `D02` 的旧基线拒绝应用、`D04` 的重复/中断重试幂等、`A03` 的只读拦截与 `W06` 的手改保护场景，详见 [T036 归档](./validation/stage-d.md#t036dreaming-显式应用与-ui-整理视图d-2-子任务-1)。
- 已完成阶段 D 的 `D-2` 子任务 2（`T037`）：新增 `scripts/release.sh` 与 `Makefile` 的 `release`（依赖 `test`）和 `install` 目标，产出 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制及 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`；新增 `web/scripts/npm-licenses.mjs` 生成前端许可证清单与 `tests/integration/t037_release_test.go` 的全路由契约测试；完成降级与恢复演练、阶段 A–D 全量回归与第 4 节发布前检查，详见 [T037 完成记录](#t037跨平台发布工程与全量交付验收d-2-子任务-2) 与 [阶段 D 性能与适配记录](./validation/stage-d.md#阶段-d-性能与适配记录)。
- 阶段 A、B、C 全部已完成并通过验收；阶段 D 的 `D-1`、`D-2`（`T035`–`T037`）已完成，阶段 D 结束，阶段 A–D 全部完成；跨平台发布候选包、校验和与许可证清单已产出，第 4 节发布前检查逐项有结论；macOS amd64、Linux amd64、Windows amd64 的运行时支持、真实宿主 Hook 与真实 Dreaming runner 未验证；`P3-6`、`P3-7`、`P3-13` 与组件级 DOM 测试为已知限制；`G05`/`G07` 沿用 A-3/A-4 场景。

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
| T027 | 已完成 | 收口 [阶段 B 实现审查](./validation/stage-b-review.md) 的 `P1-1`、`P2-1`–`P2-2` 与 `P3-1`–`P3-6`（9 项）；只修复既有契约违背并补齐回归证据，不新增产品能力；依赖阶段 B 全部实现；[归档证据](./validation/stage-b.md#t027阶段-b-审查收口修复) |
| T028 | 已完成 | 对应 `C-1` 子任务 1：受管写入（`PUT /api/v1/files/{path...}`）、移动改链（`POST /api/v1/files:move`）、条目增删改与跨文件移动（`POST/PATCH/DELETE /api/v1/entries`）、确定性修复（`POST /api/v1/doctor:fix` 与 CLI `doctor --fix`）与手动同步（`POST /api/v1/sync`）；依赖 `B-3`、`A-3`；[归档证据](./validation/stage-c.md#t028受管写入移动改链与确定性修复后端及-apic-1-子任务-1) |
| T029 | 已完成 | 对应 `C-1` 子任务 2：接入文件与条目原文编辑、条目移动改链、图谱节点拆分新文件、手动同步与确定性修复的前端交互，并在版本冲突时保留草稿并展示当前内容与草稿差异；复用 `T028` 写接口，不新增后端契约；依赖 `T028`；[归档证据](./validation/stage-c.md#t029ui-编辑移动改链与图谱拆分交互c-1-子任务-2) |
| T030 | 已完成 | 对应 `C-2` 子任务 1：实现 `POST /api/v1/conflict:resolve`、`POST /api/v1/conflict:abort`（与 CLI `resolve` 共用多阶段状态机）、`app.Revert` 与 `POST /api/v1/audit/{hash}/revert`（单父提交校验、拒绝根提交与 merge commit、revert 冲突自动采集中止与恢复）；依赖 `C-1`（`T029`）与 `A-4`（`T017`）；[归档证据](./validation/stage-c.md#t030冲突解决与审计回滚后端及-apic-2-子任务-1) |
| T031 | 已完成 | 对应 `C-2` 子任务 2：在 `web/src` 接入 UI「冲突解决」（三方上下文、文本合并、删除选择、阶段进度与中止）与「审计回滚」（历史 Diff、可解析来源展示、可疑内容提示与确认后回滚）视图；调用 `T030` 与既有只读/写接口，不新增后端契约；依赖 `T030`；[归档证据](./validation/stage-c.md#t031ui-冲突解决与审计回滚视图c-2-子任务-2) |
| T032 | 已完成 | 对应 `C-3` 子任务 1：`internal/assets` 内嵌全部支持的 adapter 模板；`internal/adapters` 实现宿主与插件版本探测、独立配置锁、配置基线校验、JSON/JSONC/YAML/TOML 保留语义与注释更新、同目录原子替换及中断恢复；实现 CLI `install`（`--all`、`--dry-run`、`--status`）；不含 HTTP 安装接口与 UI 面板（属 `C-3` 子任务 2）；依赖 `A-5`、`B-3`；[归档证据](./validation/stage-c.md#t032配置模板内嵌与保留语义更新c-3-子任务-1) |
| T033 | 已完成 | 对应 `C-3` 子任务 2：实现 `GET /api/v1/adapters`、`POST /api/v1/adapters/{agent}/install`（`{dryRun?, expectedConfigRevisions?}`）与 `--readonly` 安装接口拦截，接入 UI「整理接入」adapter 探测、预览与安装面板；复用 `adapters.Plan`/`Apply`/`Recover`，不重复实现合并逻辑；依赖 `T032`、`B-1`、`B-3`；[归档证据](./validation/stage-c.md#t033http-安装接口与-ui-接入面板c-3-子任务-2) |
| T034 | 已完成 | 收口 [阶段 C 实现审查](./validation/stage-c-review.md) 的 `P1-1`–`P1-4`、`P2-1`–`P2-10`、`P3-1`–`P3-5` 与 `P3-8`–`P3-11`，`P3-12` 部分修复；`P3-6`、`P3-7`、`P3-13` 登记为已知限制；只修复既有契约违背并补齐回归证据，不新增产品能力；依赖阶段 C 全部实现；[归档证据](./validation/stage-c.md#t034阶段-c-实现审查收口修复) |
| T035 | 已完成 | 对应 `D-1`：Dreaming 隔离生成与持久化提案预览；依赖 `C-1`（`T029`）；[归档证据](./validation/stage-d.md#t035dreaming-隔离生成与持久化提案预览d-1) |
| T036 | 已完成 | 对应 `D-2` 子任务 1：Dreaming 显式应用与 UI 整理视图（`app.ApplyDream`、CLI `dream --apply`、`POST /api/v1/dream:apply`、UI 提案预览与确认）；依赖 `T035`、`T030`、`T033`；[归档证据](./validation/stage-d.md#t036dreaming-显式应用与-ui-整理视图d-2-子任务-1) |
| T037 | 已完成 | 对应 `D-2` 子任务 2：跨平台发布工程与全量交付验收（`make release` 候选包、SHA-256 校验和与第三方许可证清单、降级与恢复演练、阶段 A–D 全量回归与第 4 节发布前检查）；依赖 `T036`；[完成记录](#t037跨平台发布工程与全量交付验收d-2-子任务-2) |

任务 ID 不代表执行顺序。阶段 A、B、C、D 的全部交付任务（`A-1`–`A-6`、`B-1`–`B-3`、`C-1`–`C-3`、`D-1`–`D-2`）已完成并验收，阶段 A、B、C 的实现审查与收口（`T022`、`T027`、`T034`）均已完成；阶段 D 以 `D-2` 为界，`T037` 完成后 `D-2` 完成，阶段 A–D 全部结束，按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T037：跨平台发布工程与全量交付验收（`D-2` 子任务 2）

- 目标与范围：完成 `D-2` 子任务 2（阶段 D 的最后一个子任务），前置为 `T036`。新增 `scripts/release.sh` 与 `Makefile` 的 `release` 目标（依赖 `test`，先构建前端并跑全量测试），按 [单文件打包](./04-ui-and-cli-specification.md#6-单文件打包) 交叉编译 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制到 `dist/`，生成 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`（Go 运行时模块与前端运行时依赖的许可证全文，构建期前端依赖只列 SPDX 标识；前端部分由 `web/scripts/npm-licenses.mjs` 生成）与 `RELEASE-INFO.txt`（版本、提交、Go/Node/npm/Git 版本与逐平台验证状态）；新增 `Makefile` 的 `install` 目标以对齐 04 已文档化入口。新增 `tests/integration/t037_release_test.go` 的路由契约测试 `TestRoutesContractCoversT037`，用 TRACE 请求逐条断言 02 §4.2 与 04 §5 的 25 条路由已注册且方法匹配。执行降级与恢复演练、阶段 A–D 全量回归与第 4 节发布前检查，并按第 3 节记录性能基线与平台/宿主适配状态。
- 契约：[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)、[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)、[REST 契约](./02-domain-models-and-contracts.md#4-rest-契约)、[HTTP 安全](./04-ui-and-cli-specification.md#5-http-安全)、[性能与适配记录](./05-validation-and-delivery.md#3-性能与适配记录)、[发布前检查](./05-validation-and-delivery.md#4-发布前检查)、[阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分)、[阶段 D 性能与适配记录](./validation/stage-d.md#阶段-d-性能与适配记录)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64（MacBook Pro `MacBookPro18,3`，Apple M1 Pro，16 GiB），Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：
  - 发布链路：`make release` 退出码 `0`；`make test` 通过（`vite build` 72 模块，`vitest run` 13 文件 72 项，`go test ./... -count=1` 含 `tests/integration` 186.8 秒），随后生成四个二进制与三个元数据文件；由 `scripts/release.sh` 独立重跑后 `shasum -a 256 -c SHA256SUMS` 四个文件全部 `OK`。
  - 静态检查：`go vet ./...` 无输出；`gofmt -l internal cmd tests web/embed.go` 无输出；`git diff --check` 无告警；`make build` 生成 `bin/memctl`。
  - 验收回归：`R03` 由 `tests/integration/r03_cli_test.go` 覆盖真实构建入口与 `version`/`help`/未知命令；`R01` 由 `TestR01EmbeddedInstructionsFromEmptyHome` 与 `TestB3EmbeddedUIAssetsCoverR01A02` 覆盖空 HOME 下的内嵌指引、UI 资产与无外部 CDN；`R02` 由 `TestR02MissingGitIsTargeted`、`TestR02MissingCredentialsAreTargeted` 与 `TestD1DreamMissingRunnerCoversR02` 覆盖；`R04` 由 `TestA6DoctorReadOnlyDiagnosisCoversR04` 覆盖。
  - 路由契约：`TestRoutesContractCoversT037` 通过，25 条文档路由全部注册且方法匹配；`A01` 的行为用例继续覆盖读取路由。
  - 降级演练：发布二进制在 `--no-open --readonly` 下无图形浏览器仍正常服务首页，`POST /api/v1/sync` 与 `POST /api/v1/dream:apply` 均返回 403 `READ_ONLY`；无 remote 的 `sync --wait` 返回 `remote: local_only`；PATH 无 runner 的 `dream --runner claude` 返回 `UNSUPPORTED`；缺沙箱由 `internal/dream.TestServiceRejectsMissingSandbox` 覆盖；缺 Git 由 `R02` 覆盖。
  - 恢复演练：`internal/app.TestRecoverRollsBackInterruptedWrite`（`W04`）、`TestRecoverRecognizesCommittedTransaction`（`W05`）、`TestRecoverRefusesUserEditedFile`（`W06`）、`TestMoveInterruptionRollsBack`（`W04`）与 `TestApplyDreamRecoversInterruptedApplyCoversD04`（`D04`）通过；`TestA3RecoverCli` 与 `TestC3InstallRecoversInterruptedInstallCoversI02`（`I02`）通过；演练使用故障注入，未以手工删除锁或强制重置仓库作为通过条件。
  - 发布前检查逐项结论：文档命令、参数、状态字段与 REST 路由同实现一致（新增全路由契约测试）；同步与写入测试使用临时 HOME 与本地裸远端/桩，不接触真实用户配置或公网；单文件包含默认指引与全部 8 个 adapter 模板，首页与 CSP 无外部 CDN；`--readonly`、无 remote、无 runner、无沙箱、无 Git 降级路径都有明确结果；事务与配置安装恢复演练通过；视觉 UI/UX 验证未开展（未明确安排），以协议与业务测试为准。
  - 性能与适配：性能基线、平台矩阵与宿主/adapter 状态见 [阶段 D 性能与适配记录](./validation/stage-d.md#阶段-d-性能与适配记录)。
- 未验证与遗留：macOS amd64、Linux amd64、Windows amd64 仅交叉编译通过，运行、生命周期与隔离未验证；真实宿主（claude/codex 等）未安装 Hook、未触发事件，Dreaming 真实 runner 在隔离 scratch HOME 下退出 `1`，模型端点与凭据环境未验证；仅 macOS Seatbelt 为已验证隔离后端；未开展浏览器视觉 UI/UX 验证；`P3-6`、`P3-7`、`P3-13`（见 [阶段 C 实现审查报告](./validation/stage-c-review.md)）与组件级 DOM 测试为已知限制，本轮未引入。
- 下一步：阶段 D 结束、阶段 A–D 全部完成，触发 [停止条件](./06-development-loop.md#3-状态与推进)，暂停自动接续并通知用户确认。

## 近期实施任务

阶段 A、B、C、D 已全部完成并通过验收，各阶段实现审查与收口（`T022`、`T027`、`T034`）均已完成。阶段 D 的 `D-1`（`T035`）、`D-2` 子任务 1（`T036`）与子任务 2（`T037`）已完成：Dreaming 的隔离生成、持久化预览、显式应用与 UI 整理视图，以及跨平台发布候选包、SHA-256 校验和与第三方许可证清单均已落地；`T035`、`T036` 归档于 [阶段 D 归档](./validation/stage-d.md)，`T037` 记录见 [T037 完成记录](#t037跨平台发布工程与全量交付验收d-2-子任务-2)，性能与适配记录见 [阶段 D 性能与适配记录](./validation/stage-d.md#阶段-d-性能与适配记录)。阶段 D 以 `D-2` 为界，`T037` 完成后阶段 D 结束，按 [开发循环规范](./06-development-loop.md#3-状态与推进) 的停止条件暂停自动接续并通知用户确认，不自行发起下一轮。

剩余未验证与已知限制：macOS amd64、Linux amd64、Windows amd64 的运行时、生命周期与隔离；真实宿主 Hook 注入与 Dreaming 真实 runner；浏览器视觉 UI/UX；`P3-6`、`P3-7`、`P3-13` 与组件级 DOM 测试。前端构建基线为 Node v26.10.0 / npm 11.19.1，`web/dist` 与 `bin/`、`dist/` 不提交，Go 构建与测试前先执行 `make ui`。
