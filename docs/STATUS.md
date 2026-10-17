# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A、阶段 B、阶段 C 与阶段 D 的已完成任务历史验收记录分别归档于 [阶段 A 归档](./validation/stage-a.md)、[阶段 B 归档](./validation/stage-b.md)、[阶段 C 归档](./validation/stage-c.md) 与 [阶段 D 归档](./validation/stage-d.md)，各阶段实现审查见 [阶段 A 实现审查报告](./validation/stage-a-review.md)、[阶段 B 实现审查报告](./validation/stage-b-review.md)、[阶段 C 实现审查报告](./validation/stage-c-review.md) 与 [阶段 D 实现审查报告](./validation/stage-d-review.md)。

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
- 已完成阶段 D 的 `D-2` 子任务 2（`T037`）：新增 `scripts/release.sh` 与 `Makefile` 的 `release`（依赖 `test`）和 `install` 目标，产出 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制及 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`；新增 `web/scripts/npm-licenses.mjs` 生成前端许可证清单与 `tests/integration/t037_release_test.go` 的全路由契约测试；完成降级与恢复演练、阶段 A–D 全量回归与第 4 节发布前检查，详见 [T037 完成记录](./validation/stage-d.md#t037跨平台发布工程与全量交付验收d-2-子任务-2) 与 [阶段 D 性能与适配记录](./validation/stage-d.md#阶段-d-性能与适配记录)。
- 已完成 `T038`：收口 [阶段 D 实现审查报告](./validation/stage-d-review.md) 的 `P1-1`–`P1-4`、`P2-1`–`P2-12` 与 `P3-2`–`P3-10`、`P3-17`（26 项），`P3-1`、`P3-18` 部分修复，`P3-11`–`P3-16` 登记为已知限制；修复导出 umask、中断 apply 恢复语义、状态迁移并发、跨进程生成查询、模式变化归一化、生成并发上限与发布元数据真实性，并补齐回归；详见 [T038 归档](./validation/stage-d.md#t038阶段-d-实现审查收口修复)。
- 已完成 `T039`：登记并修复 [ego-browser 端到端 UI/UX 验证](./validation/ui-ux-verification.md) 发现的 5 项问题（`P2-1` 快照过期不重试、`P2-2` 冲突解决后同步摘要未刷新、`P3-1` 无上游 ahead/behind 返回 `0`、`P3-2` 图谱标签重叠、`P3-3` 窄屏顶栏错位）；前端新增 `web/src/refresh.js` 的有界过期重试、图谱标签改用文件名并保留完整路径、顶栏窄屏单列布局，后端在 `internal/app/resolve.go` 持久化解决后的同步摘要、`syncSummaryDTO` 改用可空计数；通过 `tests/integration/t039_ui_state_test.go`、`web/src/refresh.test.js` 与 `web/src/graph.test.js` 回归；详见 [T039 归档记录](./validation/stage-d.md#t039ego-browser-端到端-uiux-验证问题收口)。
- 已完成 `T040`：将工作区既有、尚未提交的前端界面重设计纳入 v0.1.0 发布，统一 `web/src` 的应用外壳、导航、顶栏、概览、条目浏览、知识图谱、冲突解决、审计、诊断与整理接入视图的布局、图标、配色与排版，不改变 REST 契约、状态字段与交互流程；前端 14 文件 79 项测试与 `go test ./... -count=1` 全量通过，详见 [T040 完成记录](#t040前端界面重设计)。
- 已完成 `T041`：新增 GitHub 构建流水线 `.github/workflows/build.yml`，在 `main` 推送、拉取请求、`v*` 标签推送与手动触发时调用 `make release` 构建四平台候选包并上传 `dist/` 产物，推送 `v*` 标签时创建 GitHub Release；`v0.1.0` 标签已推送并在 `macos-latest` 上 4 分 34 秒通过，[Release v0.1.0](https://github.com/gray0128/memctl/releases/tag/v0.1.0) 附带四个候选二进制与 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`，校验和全部通过，详见 [T041 完成记录](#t041github-构建流水线与-v010-发布)。
- 已完成 `T042`：修复 v0.1.0 在真实宿主验证中暴露的 adapter 协议偏差（Codex/Command Code 的 `--json`、Antigravity 的 `~/.gemini/config/hooks.json` 命名 Hook 与 `--agy` envelope、OpenCode 2 的 default `setup` 导出与 `session.execution.*` 事件、dsh 的 profile 插件探测），补齐回归并发布 [v0.1.1](https://github.com/gray0128/memctl/releases/tag/v0.1.1)；依赖用户「修复并发布新版 release」指令。
- 已完成第二轮 ego-browser 端到端 UI/UX 验证（`T043`，2026-10-17，v0.1.1 发布候选）：七个视图与条目编辑、412 草稿差异、文件编辑、审计回滚、冲突解决、只读、深色与 390 px 窄屏路径通过；发现 2 项 P2（审计 Diff 行首裁切、同步横幅遮挡导航）与 4 项 P3，已由 `T044` 收口，复验见 [收口与复验](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044)。
- 已完成 `T045`：修复概览与正文左侧未对齐、选中导航项左边框被滚动容器裁切、指标卡不可跳转与来源 URI 截断后无法查看完整值；概览卡片可点击或键盘跳转到条目、知识图谱与诊断，来源 URI 悬停显示完整值，详见 [验证记录](./validation/ui-ux-verification.md#7-概览对齐指标卡跳转与来源提示t045)。
- 已完成 `T046`：发布 [v0.1.2](https://github.com/gray0128/memctl/releases/tag/v0.1.2)，包含 `T044`/`T045` 的 UI 修复；本地与 CI 全量测试通过，四个候选二进制与三个元数据文件已发布，`~/.agent-memory/bin/memctl` 与本地 UI 已更新到 v0.1.2，详见 [完成记录](#t046发布-v012)。
- 已完成 `T047`（设计，2026-10-17）：新增 [dsh 专用插件设计提案](./07-dsh-plugin.md)，核对 DSH 0.2.1-rc.1 的事件与上下文接入点，并按「插件 → 添加插件」对话框实际接受的 spec（npm 包名、GitHub 仓库根、绝对目录、绝对 tarball）比较分发方式；用户已确认 `D1` 取 A（npm 主通道加内嵌 tarball 备用）。本轮只完善方案，不写插件代码、不安装真实插件；`T048` 暂缓，等待用户明确进入开发。
- 阶段 A、B、C、D 的交付与实现审查收口（`T022`、`T027`、`T034`、`T038`）均已完成；跨平台发布候选包、校验和与许可证清单已产出并接入 GitHub 构建流水线，`v0.1.0`、`v0.1.1` 与 `v0.1.2` 已发布；第 4 节发布前检查逐项有结论；macOS amd64、Linux amd64、Windows amd64 的运行时支持、真实 Dreaming runner 与 Codex 模型额度受限后的完整回复未验证；真实宿主 Hook 已在 macOS arm64 的 7 个宿主完成配置，其中 5 个完成会话级验证；阶段 D 审查发现的 `P3-11`–`P3-16`、`P3-1`/`P3-18` 剩余部分、`P3-6`、`P3-7`、`P3-13` 与组件级 DOM 测试为已知限制；`G05`/`G07` 沿用 A-3/A-4 场景。

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
| T037 | 已完成 | 对应 `D-2` 子任务 2：跨平台发布工程与全量交付验收（`make release` 候选包、SHA-256 校验和与第三方许可证清单、降级与恢复演练、阶段 A–D 全量回归与第 4 节发布前检查）；依赖 `T036`；[归档证据](./validation/stage-d.md#t037跨平台发布工程与全量交付验收d-2-子任务-2) |
| T038 | 已完成 | 收口 [阶段 D 实现审查](./validation/stage-d-review.md) 的 `P1-1`–`P1-4`、`P2-1`–`P2-12`、`P3-2`–`P3-10` 与 `P3-17`，`P3-1`、`P3-18` 部分修复，`P3-11`–`P3-16` 登记为已知限制；只修复既有契约违背并补齐回归证据，不新增产品能力；依赖阶段 D 全部实现；[归档证据](./validation/stage-d.md#t038阶段-d-实现审查收口修复) |
| T039 | 已完成 | 收口 [ego-browser 端到端 UI/UX 验证](./validation/ui-ux-verification.md) 发现的 5 项问题（`P2-1`、`P2-2`、`P3-1`–`P3-3`）：快照过期有界重试、冲突解决后刷新同步摘要、无上游 ahead/behind 返回 `null`、图谱标签改用文件名、窄屏顶栏布局；依赖 `T038` 与用户确认的收口指令；[归档记录](./validation/stage-d.md#t039ego-browser-端到端-uiux-验证问题收口) |
| T040 | 已完成 | 将工作区既有、尚未提交的前端界面重设计纳入 v0.1.0 发布，统一 `web/src` 各视图的布局、图标、配色与排版；不改变 REST 契约、状态字段与交互流程；依赖用户「纳入发布」指令；[完成记录](#t040前端界面重设计) |
| T042 | 已完成 | 修复 v0.1.0 的 Codex/Command Code `--json`、Antigravity 命名 Hook 与 `--agy` envelope、OpenCode 2 插件协议、dsh profile 插件探测；补回归并发布 `v0.1.1`；依赖用户「修复并发布新版 release」指令；[完成记录](#t042修复真实宿主-adapter-协议并发布-v011) |
| T041 | 已完成 | 新增 GitHub 构建流水线 `.github/workflows/build.yml`，在 `main` 推送、拉取请求、`v*` 标签与手动触发时调用 `make release` 构建四平台候选包并上传产物，`v*` 标签发布 GitHub Release；依赖 `T040` 与用户「版本号 0.1.0」指令；[完成记录](#t041github-构建流水线与-v010-发布) |
| T043 | 已完成 | 第二轮 ego-browser 端到端 UI/UX 验证（v0.1.1）：七视图、写路径、只读、深色与窄屏；只记录问题，不改变产品代码；[验证报告](./validation/ui-ux-verification.md#5-第二轮验证v011-发布后复验) |
| T044 | 已完成 | 收口第二轮验证的 `R2-P2-1`、`R2-P2-2` 与 `R2-P3-1`–`R2-P3-4`：Diff 内边距、粘性导航偏移、冲突横幅清理、BUSY 读取重试、本地时间格式与图谱间距；[复验记录](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044) |
| T045 | 已完成 | 修复概览导航与正文未对齐、选中项左边框被裁切、指标卡不可跳转与来源 URI 截断无完整提示；[验证记录](./validation/ui-ux-verification.md#7-概览对齐指标卡跳转与来源提示t045) |
| T046 | 已完成 | 发布 `v0.1.2`（含 `T044`/`T045` 的 UI 修复）：全量测试、四平台候选包、校验和与 CI Release；[完成记录](#t046发布-v012) |
| T047 | 已完成 | dsh 专用插件设计：核对 DSH 事件与上下文接入点，按「添加插件」对话框的 spec（npm 名、GitHub 仓库、绝对目录、绝对 tarball）比较分发，给出插件、适配器、发布和验收方案；只写设计，不写插件代码；[设计提案](./07-dsh-plugin.md) |
| T048 | 待开始 | 实现 `memctl-dsh` Host 插件、内嵌 tarball 路径命令、适配器探测与发布产物；`D1` 已确认 A；等待用户明确进入开发；[设计提案](./07-dsh-plugin.md) |

任务 ID 不代表执行顺序。阶段 A、B、C、D 的全部交付任务（`A-1`–`A-6`、`B-1`–`B-3`、`C-1`–`C-3`、`D-1`–`D-2`）已完成并验收，阶段 A、B、C、D 的实现审查与收口（`T022`、`T027`、`T034`、`T038`）均已完成；用户确认的 `T040`、`T041` 已完成并发布 `v0.1.0`，`T042` 已完成并交付 `v0.1.1`，`T043` 已完成第二轮 UI/UX 验证，`T044` 已收口该轮发现的问题，`T045` 已完成概览与来源提示修复，`T046` 已发布 `v0.1.2`，`T047` 已完成 dsh 专用插件设计并确认 `D1` 取 A；`T048` 暂缓，等待用户明确进入开发，按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。


## T040：前端界面重设计

- 目标与范围：将工作区既有、尚未提交的前端界面重设计纳入本轮提交与 v0.1.0 发布。只调整 `web/src` 的应用外壳、导航、顶栏、概览、条目浏览、知识图谱、冲突解决、审计、诊断与整理接入视图的布局、图标、配色与排版，不改变 REST 契约、状态字段、前端交互流程或后端行为。
- 契约：[Web UI](./04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](./04-ui-and-cli-specification.md#5-http-安全)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`cd web && npm ci && npm run build && npm test` 通过（14 文件 79 项，`web/dist` 产出 `index-*.css` 78.65 kB 与 `index-*.js` 141.43 kB）；`gofmt -l internal cmd tests web/embed.go` 无输出；`go vet ./internal/... ./cmd/...` 通过；`go test ./... -count=1` 全量通过（含 `tests/integration` 256.3 秒）。
- 未验证与遗留：未做浏览器端 UI/UX 复验，视觉结论以构建与组件测试为证，交互流程沿用既有实现。
- 下一步：作为 v0.1.0 发布内容随 `T041` 提交、推送并发布。

## T041：GitHub 构建流水线与 v0.1.0 发布

- 目标与范围：新增 GitHub Actions 构建流水线 `.github/workflows/build.yml`，在 `main` 推送、拉取请求、`v*` 标签推送与手动触发时调用 `make release`，构建 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制并上传 `dist/` 构建产物；推送 `v*` 标签时用 `gh release create` 创建 GitHub Release，附带四个候选二进制与 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`。同步更新根 `README.md`、[CLI 与 UI 打包](./04-ui-and-cli-specification.md#6-单文件打包) 与 [发布前检查](./05-validation-and-delivery.md#4-发布前检查) 的 CI 说明。
- 契约：[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](./05-validation-and-delivery.md#4-发布前检查)、[目录规划](./01-architecture-and-modules.md#5-目录结构与依赖)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64（本地 `scripts/release.sh`）与 GitHub Actions `macos-latest`；Go 1.27.1，Node v26.10.0 / npm 11.19.1。
- 命令与结果：本地 `VERSION=v0.1.0 scripts/release.sh` 产出四个候选二进制与三个元数据文件，`shasum -a 256 -c SHA256SUMS` 四个文件全部 `OK`；首次 GitHub Actions 运行暴露宿主机差异并修复，修复后在 `GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null` 隔离环境下 `go test ./... -count=1` 全量通过（`tests/integration` 256.5 秒）；[GitHub Actions 运行 39016504087](https://github.com/gray0128/memctl/actions/runs/39016504087) 在 `macos-latest` 上 4 分 34 秒通过，[Release v0.1.0](https://github.com/gray0128/memctl/releases/tag/v0.1.0) 已发布，附带四个候选二进制与三个元数据文件；下载后 `shasum -a 256 -c SHA256SUMS` 四个文件全部 `OK`，`RELEASE-INFO.txt` 记录 runtime-verified `darwin/arm64`（构建提交 `b404eeb`），`memctl-darwin-arm64 version` 输出 `memctl v0.1.0`。
- 首次流水线运行发现的环境依赖：GitHub 托管 macOS runner 无全局 `init.defaultBranch=main`，测试用 `git init --bare` 建的裸远端默认指向 `master`，`RemoteDefaultBranch` 读不到默认分支；`newBareRemote` 改用 `git init --bare -b main` 固定裸远端 HEAD。`TestB2WatchStreamsExternalEditCoversA05` 在 watcher 注册完成前写入外部文件会丢失事件；改为在等待事件期间有界重试写入，watcher 每次突发仍发布一个 `vault.updated`。两项均为测试隔离加固，不改变产品行为。
- 未验证与遗留：流水线只在 GitHub 托管 runner 上验证；`macos-latest` 之外的平台仍只做交叉编译，运行、生命周期与隔离保持未验证。
- 下一步：本轮完成后按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并通知用户确认。


## T042：修复真实宿主 adapter 协议并发布 v0.1.1

- 目标与范围：修复真实宿主验证暴露的 adapter 协议偏差，并发布 `v0.1.1`。Codex 与 Command Code 的 SessionStart 增加 `--json`；Antigravity 改为读写 `$HOME/.gemini/config/hooks.json` 的 `agent-memory` 命名 Hook，新增 `start --agy` 的 `injectSteps` envelope 与 `sync --agy` 的 `{}` 输出，并把 `conversationId` 纳入 SID；OpenCode 插件改为兼容 OpenCode 2 的 default `{id, setup}` 导出，订阅 `session.execution.started/succeeded`，同时保留 v1 命名导出；dsh 插件清单支持 `$HOME/.dsh/profiles/*/node_modules`。同步修正 `install --status` 的模板识别，并补对应单元与集成测试。不新增业务功能，不改变记忆仓库格式、REST 契约或同步状态机。
- 契约：[Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)、[Hook 输入输出](./04-ui-and-cli-specification.md#3-hook-输入输出与时限)、[会话身份](./04-ui-and-cli-specification.md#2-环境变量与会话身份)、[发布前检查](./05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；真实宿主：Codex 0.160.0、Command Code 1.84.0、Antigravity 1.4.0、OpenCode 2.0.26、pi 1.3.0（插件 2026.9.26）、Grok 1.0.60、dsh 0.2.1-rc.1（插件 0.2.15）；执行者为主会话。
- 命令与结果：`make test` 全量通过（前端 14 文件 79 项；Go 含 `tests/integration` 259.1 秒）；新增 `TestT042AntigravityHookProtocol`（`conversationId` 作为 SID、`start --agy` 输出 `injectSteps`、`sync --agy` 输出 `{}`）、`TestAntigravityNamedHookMerge`（保留 `orca-status`、命名 Hook 幂等、陈旧受管项替换）、`TestDshPluginProfileDetection`（profile 插件探测与写入）、`TestRegistryHostProtocols`（Codex/Command Code `--json`、Antigravity 路径与 envelope、OpenCode 2 导出与事件、dsh profile glob）；`TestC3EmbeddedTemplatesCoverR01` 扩展为从内嵌模板安装 Codex、Command Code、Antigravity 与 OpenCode 2 并检查关键协议。真实宿主复验：pi、Grok、Antigravity、OpenCode、Command Code 启动并触发 Hook；Codex 在 `--dangerously-bypass-hook-trust` 下 `SessionStart Completed`，会话缓存记录注入成功，模型调用因账号额度限制未完成；dsh 插件加载并注册 `dsh-hooks-authoring` 技能，`/dsh-hooks/recent` 返回 200，`memctl` 以样例载荷验证 SessionStart/SessionEnd。`VERSION=v0.1.1 scripts/release.sh` 产出四个候选二进制与三个元数据文件；[GitHub Actions 运行 39019936416](https://github.com/gray0128/memctl/actions/runs/39019936416) 在 `macos-latest` 上 4 分 0 秒通过并创建 [Release v0.1.1](https://github.com/gray0128/memctl/releases/tag/v0.1.1)，发布四个候选二进制与三个元数据文件；下载 `memctl-darwin-arm64` 校验和 `6c433b37…f6bd` 与 `SHA256SUMS` 一致，`version` 输出 `memctl v0.1.1`，该二进制已安装到 `~/.agent-memory/bin/memctl`。
- 未验证与遗留：Codex 模型回复受账号额度限制（提示 2026-10-22 恢复）；dsh 当前会话启动早于全局 `hooks.json` 写入，SessionStart 需重启或新会话触发；macOS amd64、Linux amd64、Windows amd64 仍只有交叉编译；`dsh-hooks-plugin@0.2.15` 与 DSH 0.2.1-rc.1 的 peer 版本不匹配，已按用户授权使用精确版本豁免安装。
- 下一步：`v0.1.1` 发布完成后按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

## T043：第二轮 ego-browser 端到端 UI/UX 验证

- 目标与范围：对 `v0.1.1` 发布候选二进制复验七个视图与主要写路径，确认 `T040` 界面重设计与 `T042` 适配器修复后的视觉与交互状态；只记录问题，不改变产品代码。
- 契约：[Web UI](./04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](./04-ui-and-cli-specification.md#5-http-安全)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，`dist/memctl-darwin-arm64`（v0.1.1），ego-browser v2.1.0；临时 HOME 与临时记忆仓库，读写 `127.0.0.1:18420`、只读 `127.0.0.1:18421`；本地裸远端加 peer 克隆制造冲突。
- 命令与结果：浏览器会话遍历七个视图，执行条目编辑与 412 草稿差异、文件原文编辑、审计 Diff 与回滚、冲突解决、adapter 预览、只读、深色与 390 px；通过项见 [第二轮验证](./validation/ui-ux-verification.md#52-通过项)。发现 2 项 P2 与 4 项 P3，登记 `T044`。
- 未验证与遗留：图谱节点拆分、Dreaming 真实 runner、多阶段冲突递进、adapter 安装应用、非 macOS arm64 平台；截图保存在临时目录，未提交。
- 下一步：等待用户确认是否按 `T044` 收口；确认前不修改产品代码。

## T044：收口第二轮 UI/UX 验证问题

- 目标与范围：修复第二轮验证的 `R2-P2-1`、`R2-P2-2` 与 `R2-P3-1`–`R2-P3-4`；代码只改 `web/src` 的样式、布局测量、只读重试与时间格式，并在 [Web UI](./04-ui-and-cli-specification.md#4-web-ui) 补充时间格式与 `BUSY` 重试说明；不改 REST 契约、状态字段与写入流程。
- 契约：[Web UI](./04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](./04-ui-and-cli-specification.md#5-http-安全)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，ego-browser v2.1.0；临时 HOME 与临时记忆仓库，读写 `127.0.0.1:18420`、只读 `127.0.0.1:18421`。
- 命令与结果：`npx vitest run` 16 文件 85 项通过；`npm run build`、`go build -o bin/memctl ./cmd/memctl`、`gofmt -l`、`go vet` 通过；ego-browser 复验审计 Diff 文本边界（426 > 414）、横幅下滚动时 header 与导航不重叠（0–103 / 103–148）、冲突解决后横幅清除、BUSY 短锁透明与长锁友好提示、`最近同步 2026-10-17 17:54`、图谱连续 3 次布局无标签压盖、只读、深色与 390 px 无回归；详见 [复验记录](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044)。
- 未验证与遗留：更密集知识图谱的标签拥挤仍为已知限制；未重跑 `go test ./...`（无 Go 变更）。
- 下一步：提交推送本轮修复后按停止条件暂停并等待用户确认。

## T045：概览对齐、指标卡跳转与来源提示

- 目标与范围：修复用户报告的 4 项界面问题：概览导航与正文左侧未对齐、选中导航项左边框被滚动容器裁切、概览指标卡不可跳转、来源 URI 截断后无法查看完整值。代码只改 `web/src` 的布局与交互，并在 [Web UI](./04-ui-and-cli-specification.md#4-web-ui) 补充卡片跳转与来源悬停说明；不改 REST 契约。
- 契约：[Web UI](./04-ui-and-cli-specification.md#4-web-ui)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，ego-browser v2.1.0；`bin/memctl ui` 指向真实 `~/memory` 仓库。
- 命令与结果：`npx vitest run` 16 文件 85 项通过；`npm run build` 与 `go build` 通过；ego-browser 在 1100/1315/1440/1784 px 下导航首项与 `h1` 左边界一致（24/41.5/104/276），390 px 下均为 16，选中项圆角边框完整；点击「原子条目」「图谱节点」「错误诊断」分别进入条目、知识图谱、诊断视图；条目来源 chip 的 `title` 为完整 `agent://` URI；详见 [验证记录](./validation/ui-ux-verification.md#7-概览对齐指标卡跳转与来源提示t045)。
- 未验证与遗留：悬停提示使用浏览器原生标题，样式由浏览器决定，未做自定义浮层；组件级 DOM 测试仍缺失。
- 下一步：提交推送本轮修复后按停止条件暂停并等待用户确认。

## T046：发布 v0.1.2

- 目标与范围：把 `T044`（第二轮 UI/UX 收口）与 `T045`（概览对齐、指标卡跳转、来源提示）以补丁版本发布；不改变 REST 契约、状态字段与记忆仓库格式。
- 契约：[发布前检查](./05-validation-and-delivery.md#4-发布前检查)、[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.1.2` 指向 `fec40b3`。
- 命令与结果：`VERSION=v0.1.2 make release` 全量通过（前端 16 文件 85 项；Go 含 `tests/integration` 234.0 秒），生成四个候选二进制与三个元数据文件；本地 `shasum -a 256 -c SHA256SUMS` 四个文件 `OK`，`dist/memctl-darwin-arm64 version` 输出 `memctl v0.1.2`。tag 推送后 [GitHub Actions 运行 39025243278](https://github.com/gray0128/memctl/actions/runs/39025243278) 在 3 分 30 秒内通过并创建 [Release v0.1.2](https://github.com/gray0128/memctl/releases/tag/v0.1.2)；发布附件含四个二进制与三个元数据文件，下载 `memctl-darwin-arm64` 后与发布 `SHA256SUMS` 校验一致、`version` 输出 `memctl v0.1.2`。该二进制已覆盖安装到 `~/.agent-memory/bin/memctl`，本地 UI 也以 v0.1.2 运行。本地候选包与 CI 产物的校验和不同（构建环境差异），两者各自与对应 `SHA256SUMS` 一致。
- 未验证与遗留：macOS amd64、Linux amd64、Windows amd64 仍只做交叉编译；UI 视觉交互证据沿用 `T044`/`T045` 记录。
- 下一步：发布完成后按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停并等待用户确认。

## T047：dsh 专用插件设计

- 目标与范围：只做设计，不写插件代码、不安装真实插件。核对 DSH 0.2.1-rc.1 的事件与上下文接入点，并按「插件 → 添加插件」对话框实际接受的 spec（npm 包名、GitHub 仓库根、绝对目录、绝对 tarball 路径）比较 npm、内嵌 tarball、Release tarball、独立 GitHub 仓库、本地 link、Discover 索引和直接改 profile 的分发方式；给出 `memctl-dsh` 的包结构、生命周期、上下文注入、同步、适配器探测和发布验收方案。
- 契约：[dsh 专用插件设计提案](./07-dsh-plugin.md)、[Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装)、[验收与交付](./05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，DSH 0.2.1-rc.1，`dsh-hooks-plugin@0.2.15`，Node v26.10.0；调研来源为 DSH 官方开发文档、DSH 0.2.1-rc.1 的 `app.asar`、`dsh-context-memory`、`dsh-hooks-plugin`、`claude-mem`、`basic-memory` 和 Claude Code 插件市场文档。
- 命令与结果：设计文档内部链接与锚点检查通过；外部参考 URL 均返回 HTTP 200（含 DSH `install-spec.ts`）；正文无弯引号，无行尾空白。纯文档任务，未运行 Go/Node 构建或宿主测试。
- 未验证与遗留：插件未实现；未在真实 DSH 会话中验证 `systemPrompt.context`、`agent/created` 时序或 `turn/end` 触发次数；`D1` 已确认 A（npm 主 + 内嵌 tarball 备用）；独立 GitHub 仓库 `D2`、Discover 市场索引 `D3` 未决定。
- 下一步：等待用户明确进入开发后再启动 `T048`；实现前按设计提案第 10 节更新目录规划、适配器契约和验收 ID。

## 近期实施任务

阶段 A、B、C、D 已全部完成并通过验收，各阶段实现审查与收口（`T022`、`T027`、`T034`、`T038`）均已完成；`T039` 记录已归档于 [阶段 D 归档](./validation/stage-d.md)，`T040`、`T041` 记录见本文件的 [T040 完成记录](#t040前端界面重设计) 与 [T041 完成记录](#t041github-构建流水线与-v010-发布)；`T042` 见 [修复真实宿主 adapter 协议](#t042修复真实宿主-adapter-协议并发布-v011)。`T043` 第二轮 UI/UX 验证见 [验证报告](./validation/ui-ux-verification.md#5-第二轮验证v011-发布后复验)，`T044` 已收口，`T045` 已修复概览对齐与来源提示，`T046` 见 [发布 v0.1.2](#t046发布-v012)，`T047` 见 [dsh 专用插件设计](#t047dsh-专用插件设计)。审查报告见 [阶段 D 实现审查报告](./validation/stage-d-review.md)，性能与适配记录见 [阶段 D 性能与适配记录](./validation/stage-d.md#阶段-d-性能与适配记录)。`T042` 已完成并发布 `v0.1.1`，`T043` 已完成验证，`T044` 已完成收口，`T045` 已完成概览对齐与来源提示修复，`T046` 已发布 `v0.1.2`，`T047` 已完成设计并确认 `D1` 取 A；`T048` 暂缓，等待用户明确进入开发，按 [开发循环规范](./06-development-loop.md#3-状态与推进) 的停止条件暂停自动接续并通知用户确认，不自行发起下一轮。

剩余未验证与已知限制：`T048` 未开始，`memctl-dsh` 的 `systemPrompt.context` 注入、`agent/created` 时序与 `turn/end` 触发次数尚未在真实 DSH 会话验证；macOS amd64、Linux amd64、Windows amd64 的运行时、生命周期与隔离（GitHub 构建流水线只在 `macos-latest` 上运行，其余平台仍为交叉编译）；Codex 模型额度限制下的完整回复、dsh 重启后的 SessionStart 事件与 Dreaming 真实 runner；图谱密集同名孤立节点簇的标签拥挤；`P3-6`、`P3-7`、`P3-13` 与组件级 DOM 测试。`T040` 的前端界面重设计已由 [第二轮 UI/UX 验证](./validation/ui-ux-verification.md#5-第二轮验证v011-发布后复验) 覆盖，发现的问题已由 [收口与复验](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044) 处理，修复后浏览器视觉交互结论以该报告为准。前端构建基线为 Node v26.10.0 / npm 11.19.1，`web/dist` 与 `bin/`、`dist/` 不提交，Go 构建与测试前先执行 `make ui`。
