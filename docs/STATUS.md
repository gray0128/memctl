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
- 已完成 `T025`（`B-3` 子任务 1）：新增 `web/` 前端工程（Preact、Vite、Tailwind CSS）、`web/embed.go` 内嵌入口与 `Makefile` 的 `ui` 构建链路；`internal/server` 增加内嵌首页（注入 Session Token 与只读标识，本地 `self` CSP）与 `/assets/` 静态资源路由；新增 `memctl ui` 命令（`--port`、`--no-open`、`--readonly`），启动递归监听并在 SIGINT/SIGTERM 时取消请求上下文后关闭；前端实现 Meta Token 读取、JSON API 封装、基于 `fetch` 流的 SSE 解析与断线重连全量校正及顶栏状态；通过 `R01` 的 UI 资产场景、`A02` 的首页 Token 注入与 `A03` 的只读拦截，详见 [T025 完成记录](#t025内嵌前端工程与只读视图子任务-1b-3)。
- 尚未实现 `B-3` 子任务 2 的只读业务视图（知识图谱、条目检索、审计 Diff、诊断展示）、Dreaming 与正式安装流程；`--fix` 等确定性诊断修复属 `C-1`；`G05`/`G07` 沿用 A-3/A-4 场景；05 中其余产品验收场景仍待实现与验证。

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
| T025 | 已完成 | 对应 `B-3` 子任务 1：`web/` 前端工程与 `web/embed.go`、`Makefile` 的 `ui` 链路、`memctl ui` 命令（`--port`/`--no-open`/`--readonly`）、首页 Token 注入、SSE 客户端解析与重连校正及顶栏状态；依赖 `B-1`、`B-2`；[完成记录](#t025内嵌前端工程与只读视图子任务-1b-3) |

任务 ID 不代表执行顺序。阶段 A 的全部任务（`A-1`–`A-6`）已完成；阶段 B 的 `B-1`、`B-2` 以及 `B-3` 子任务 1（`T025`）已完成并验收，`B-3` 子任务 2（只读业务视图）为下一轮登记任务。后续按 `B-3` 子任务 2 → `C-1` → `C-2` / `C-3` → `D-1` → `D-2` 的依赖关系与 [阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分) 依次推进；阶段 B 全部完成后按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停并通知用户。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T025：内嵌前端工程与只读视图子任务 1（B-3）

- 目标与范围：完成 `B-3` 子任务 1，前置为 `B-1`（`T023`）与 `B-2`（`T024`）。新增 `web/` 前端工程（Preact、Vite、Tailwind CSS，Node.js 仅用于构建期）与 `web/embed.go` 内嵌入口；`Makefile` 增加 `ui` 目标，`test`、`build` 先构建前端再编译 Go。`internal/server` 增加 `GET /`（内嵌首页，注入 32 字节 Session Token 与只读标识，使用只允许本地 `self` 与 `frame-ancestors 'none'` 的 CSP）与 `GET /assets/`（内嵌脚本与样式，仅服务普通文件、不列目录）；新增 `memctl ui` 命令（`--port`、`--no-open`、`--readonly`），按真实监听地址校验 Host、启动 `internal/watcher` 并在 SIGINT/SIGTERM 时先取消请求基上下文再关闭，长连接 SSE 不阻塞退出。前端实现 Meta Token 读取与 JSON API 封装、基于 `fetch` 流的 SSE 解析（跨块拆分、多行 `data`、注释心跳）与每次连接/重连的全量校正，以及顶栏状态（仓库路径、本地/远端状态、最后同步、只读与连接状态）与快照概览。只读知识图谱、条目检索浏览、审计 Diff 预览与诊断展示属 `B-3` 子任务 2。
- 契约：[Web UI](./04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](./04-ui-and-cli-specification.md#5-http-安全)、[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)、[SSE 与缓存](./02-domain-models-and-contracts.md#5-sse-与缓存)、[模块职责与目录规划](./01-architecture-and-modules.md#4-模块职责)、[技术依赖](./01-architecture-and-modules.md#技术依赖)、[阶段步骤的子任务切分](./05-validation-and-delivery.md#阶段步骤的子任务切分)。
- 验证日期与环境：2026-10-16，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 全部通过；前端 `vitest run` 2 个文件 9 项通过（`createSSEParser` 的跨块、多行 `data`、CRLF、注释与分帧缓冲，`createEventStream` 的每次连接与重连全量校正、失败重试与停止，`createApi` 的 `X-Memctl-Token` 头与错误 envelope）。`R01` 的 UI 资产场景由 `TestB3EmbeddedUIAssetsCoverR01A02`（首页与 `/assets/` 均来自内嵌文件系统、页面无外部 CDN 引用、静态资源不含 Token、未知资源与目录请求返回 404）与 `TestB3UICommandEndToEndCoversR01A02A03`（仅把二进制复制到空 HOME 后运行 `memctl ui --no-open --readonly --port 0`，经真实环回端口加载页面与静态资源）覆盖。`A02` 的首页 Meta Token 注入由两个用例覆盖（页面注入的 Token 经 `X-Memctl-Token` 认证 `GET /api/v1/snapshot` 为 200，缺失 Token 为 401）。`A03` 由 `TestB3ReadOnlyUIRejectsWritesCoversA03` 与端到端用例覆盖（`--readonly` 下 POST 返回 403 `READ_ONLY`，页面路由写入被拒，Host 校验仍生效）。`A05` 的浏览器端解析与重连校正由 `web/src/sse.test.js` 覆盖，与 `B-2` 的服务端 SSE 用例衔接。`TestR03BuildAndCLIEntry` 更新为按记录工具链构建，并改用仍未实现的 `install` 校验未实现命令不返回占位成功。回归 `B-1`、`B-2` 与阶段 A 全量用例通过。
- 未验证与遗留：`B-3` 子任务 2 的只读知识图谱（Cytoscape.js）、条目检索与过滤、审计 Diff 预览（Diff2Html）与诊断展示未实现；`internal/server` 通过 `web/embed.go` 内嵌 `web/dist`，`dist` 不提交，因此 `go vet`、`go test`、`go build` 前必须先执行 `make ui`；仅在 macOS arm64 实机运行，Windows/Linux 的浏览器打开与文件监听后端未运行；前端仅以 `vitest` 验证协议与解析逻辑，未开展浏览器视觉 UI/UX 验证；无图形环境时 `memctl ui` 的自动打开降级为打印 URL。
- 下一步：`B-3` 子任务 1 已完成，仍处阶段 B 且未触发停止条件，按 [开发循环规范](./06-development-loop.md#6-交接与中断) 接续 `B-3` 子任务 2（只读业务视图）。

## 近期实施任务

`T025`（`B-3` 子任务 1）已完成并通过验收，无进行中任务。按 [开发循环规范](./06-development-loop.md#6-交接与中断)，存在可执行任务且未触发停止条件，下一轮登记并启动 `B-3` 子任务 2（只读知识图谱、条目检索浏览、审计 Diff 预览与诊断展示）；前端构建基线为 Node v26.10.0 / npm 11.19.1，`dist` 不提交，Go 构建与测试前先执行 `make ui`，跨平台与跨版本支持另行验证。
