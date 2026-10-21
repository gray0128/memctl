# 05. 验收与交付

本文件把设计约束转为可执行验收，并定义发布门槛；具体执行结果记录在 [开发状态](./STATUS.md) 与 `docs/validation/` 归档。测试使用临时 HOME、临时记忆仓库、受控 Git remote 和模拟 runner，不读写真实用户记忆、Agent 配置或远端。

## 1. 交付顺序

| 阶段 | 能力 | 开放条件 |
| --- | --- | --- |
| A：核心 CLI | init、read/write、Hook 编解码、串行 capture、同步、CLI 冲突解决和恢复、只读 doctor | 格式、事务、Git 状态、身份与 CLI 分发检查通过；具体范围见下表 |
| B：本地 UI | 内嵌资源、只读图谱、条目检索、审计、SSE | API 鉴权、路径、路由及状态通知测试通过 |
| C：受管管理 | UI 写入、移动改链、冲突解决、revert、配置安装 | 多文件中断恢复、冲突多阶段、配置基线与幂等测试通过 |
| D：Dreaming | 隔离生成、持久化预览、显式应用 | 提案基线、范围检查、过期提案和幂等应用测试通过；本地 runner 在阶段 G 迁移为 GitHub Actions 远端执行 |
| E：DSH 专用插件 | `memctl-dsh` Host 插件、内嵌 tarball/目录、原生与旧插件探测、发布产物 | 插件单元测试、包清单、隔离 profile 安装与适配状态测试通过；真实会话注入未验证时如实记录 |
| F：pi 专用插件 | `memctl-pi` 扩展、本地包目录、原生与旧路径探测、npm/本地分发 | 插件单元测试、包清单、隔离 pi 安装与适配状态矩阵通过；真实会话注入未验证时如实记录 |
| G：GitHub Actions Dreaming | 远端分支触发、多步结构化整理 worker、4 家内置模型提供商、`--init-workflow` | `.github` 路径保护、`dream/req` 与 `dream/res` 分支协议、Step Guard 校验、幂等初始化与真实 GitHub Actions 验收通过 |
| H：跨会话新记忆提炼 | 本地会话确定性蒸馏与脱敏缓存、本地当前 Agent 交互式整理（`dream --prepare`/`--propose` 与 MCP 工具）、来源绑定守卫 | 正则脱敏与可疑指令过滤、`sessionIndices` 来源绑定校验、30 天删除闸门、本地提案生成与消费闭环验收通过 |

这些阶段是实现与开放顺序，不删除原定功能。每个平台、宿主及插件版本独立记录验证结果；某一组合失败不以关闭保护机制换取「支持」。以下小节定义全项目的完整实施顺序、前置依赖、交付边界与验收映射；实际执行进度、任务拆分与验证证据统一在 [STATUS.md](./STATUS.md) 维护。`A-1` 等是阶段步骤，`F01` 等是验收场景，两者不混用。

### 阶段 A 的实施顺序

先形成无远端的本地闭环，再接入网络同步和宿主 Hook。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| A-1：工程与入口 | 确定受支持的 Go 工具链版本 | 最小 Go 工程、`version/help`、实际构建与测试入口；README 提供已验证命令 | R03 |
| A-2：本地仓库与读取 | A-1 | 无远端 `init`、格式与链接解析、`read --json`、文件修订与安全路径访问；不执行网络请求 | G01、G05 的初始化场景，F01、F02、F04，P01、P02 的读取场景，P03 |
| A-3：受管写入与恢复 | A-2 | `write`、锁内版本校验、仅本地的 `sync --capture`、事务记录与 `recover`；多文件事务由集成测试调用应用服务验证 | W01、W03–W07，G05–G07 的本地保护场景，P01–P03，L01 的本地写入场景 |
| A-4：前台远端同步 | A-3；T002 凭据契约完成 | 已有远端与空远端接入、`sync --wait`、错误分类、CLI 多阶段 `resolve`；不依赖后台派生 | G02–G08、C01–C04，L01 的前台 Git 子进程场景 |
| A-5：Hook 与后台 | A-4；目标宿主协议明确 | `start/context/pull/sync/end/tag/wrap`、会话身份、去重、非交互后台同步及平台生命周期；安装器留在 C 阶段 | H01–H06、G08、L01、L02；按第 3 节记录宿主与性能实测 |
| A-6：诊断与 CLI 交付 | A-1 至 A-5 的适用检查完成 | 只读 `doctor`、内嵌默认指引、CLI 候选包和隔离环境回归；列明未验证的平台与宿主 | F03、R04；R01 的默认指引及 R02 的 Git/凭据场景；复跑前序相关用例 |

### 阶段 B 的实施顺序

在阶段 A 的本地仓库、图谱解析与诊断能力基础上，先建立安全的本地 HTTP 只读服务与 SSE 状态流，再接入内嵌前端只读视图。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| B-1：HTTP 安全框架与只读 API | A-6 | `internal/server` 路由注册、仅绑定 `127.0.0.1`、32 字节 Session Token 鉴权、`Host`/`Origin` 校验、安全响应头与 CSP、`--readonly` 写操作拦截；实现 `snapshot`、`files`（GET）、`entries`（GET）、`audit`（GET）、`doctor`（GET）、`conflict`（GET）只读接口及分页与请求限额 | A01、A02，A03 的只读拦截场景，P01–P03 的 HTTP 读取与限额场景 |
| B-2：文件监听与 SSE 状态流 | B-1 | `internal/watcher` 递归目录监听与防抖、写事务完成后主动失效通知、`GET /api/v1/events` 有界队列与心跳、`resync.required` 溢出降级，以及客户端 fetch 流跨块拆分、多行 `data` 与断线重连全量校正 | A05，A02 的 SSE 鉴权场景 |
| B-3：内嵌前端工程与只读视图交付 | B-1、B-2；确定 Node.js/npm 构建工具链版本 | `web/` 前端工程（Preact、Vite、Tailwind CSS、Cytoscape.js、Diff2Html）、`web/embed.go`、`Makefile` 的 `ui` 构建链路、`memctl ui` 命令（`--port`、`--no-open`、`--readonly`）；完成顶栏状态、只读知识图谱、条目检索浏览、审计 Diff 预览与诊断展示，确认静态资源无外部 CDN 依赖 | R01 的内嵌 UI 资产场景，A02、A03、A05 端到端联调；回归阶段 A 用例 |

### 阶段 C 的实施顺序

将 UI/HTTP 写操作接入与 CLI 相同的受管事务、锁和版本基线，补齐多文件移动改链、冲突解决、提交回滚及 Agent 配置幂等安装。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| C-1：UI/API 受管写入、移动改链与确定性修复 | B-3、A-3 | 实现 `PUT /api/v1/files/{path...}`、`POST /api/v1/files:move`（含已解析链接定点更新与大小写重命名中间路径）、`POST/PATCH/DELETE /api/v1/entries`（含跨文件移动与图谱拆分新文件）、`POST /api/v1/doctor:fix` 与 CLI `doctor --fix`、`POST /api/v1/sync`，并接入对应 UI 编辑与拆分交互 | A04、W01–W07（含 W02 CLI/UI 并发与 W04 多文件移动中断恢复），P01–P03 的 HTTP 写入与移动场景 |
| C-2：UI/API 冲突解决与审计回滚 | C-1、A-4 | 实现 `POST /api/v1/conflict:resolve`、`POST /api/v1/conflict:abort`（与 CLI `resolve` 共用多阶段状态机）、`app.Revert` 与 `POST /api/v1/audit/{hash}/revert`（单父提交校验、拒绝根提交与 merge commit、revert 冲突自动采集中止与恢复），接入 UI「冲突解决」与「审计回滚」视图 | C01–C05，A03 的管理写接口只读拦截，A04 |
| C-3：Agent 配置探测、模板内嵌与幂等安装 | A-5、B-3 | `internal/assets` 内嵌全部支持的 adapter 模板；`internal/adapters` 实现宿主与插件版本探测、独立配置锁、配置基线校验、JSON/JSONC/YAML/TOML 保留语义与注释更新、同目录原子替换及中断恢复；实现 CLI `install`（`--all`、`--dry-run`、`--status`）与 `GET /api/v1/adapters`、`POST /api/v1/adapters/{agent}/install` 及 UI 接入面板 | I01、I02，R01 的内嵌 adapter 模板场景，A03 的安装接口只读拦截 |

### 阶段 D 的实施顺序

在受管事务与宿主适配完成后，落地隔离的 Dreaming 整理提案生成、持久化审阅、显式应用及全平台发布候选验收。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| D-1：Dreaming 远端生成与持久化提案预览（阶段 G 交付） | C-1、G-2 | `internal/dream` 锁内校验干净基线与远端前置条件、推送 `dream/req` 请求分支、轮询并校验 `dream/res` 结果树、固定基线 Diff 与范围校验、`$STATE/repos/<repoId>/proposals/` 持久化与 `dream.updated` 事件；实现 CLI `dream --runner`、`dream --show` 与 `POST /api/v1/dream:preview`、`GET /api/v1/dream/{proposalId}` | D01、D02（脏库拒绝生成部分）、D03，R02 的远端未确认与缺少工作流降级场景 |
| D-2：Dreaming 显式应用、UI 整理视图与全量发布交付 | D-1、C-2、C-3 | 实现 `app.ApplyDream`、CLI `dream --apply <ID> --patch-hash <HASH>` 与 `POST /api/v1/dream:apply`（校验原始基线、过期拒绝、受管事务提交、中断恢复与重复 apply 幂等返回），接入 UI「整理接入」提案预览与确认操作；按第 3、4 节完成跨平台发布候选包（`make release`）、校验和与许可证清单、降级与恢复演练及阶段 A–D 全量回归 | D02（旧基线提案拒绝应用部分）、D04，A03 的 Dreaming 接口只读拦截，R01–R04 全量回归及第 4 节发布前检查 |

### 阶段 E 的实施顺序

阶段 E 由 [dsh 专用插件设计](./07-dsh-plugin.md) 定义。`E-1` 至 `E-4` 由 `T048` 交付实现与可复核验收，`E-5` 由 `T052` 跟踪；真实 DSH 会话证据不可得时保留未验证状态，不用单元测试替代。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| E-1：插件包骨架 | C-3、D-2 | `plugins/dsh/` 的 `package.json`、`cordis.patch.yml`、`index.js`、locale、icon 与 Node 单元测试；无依赖与安装脚本 | I03、I04 的包清单场景 |
| E-2：生命周期与上下文 | E-1；DSH 事件契约明确 | `agent/created` 读取并注册 `systemPrompt.context`、`turn/end` 触发有界后台同步、`agent/disposed` 尽力收尾；子代理跳过；错误不阻塞会话 | H07–H10 |
| E-3：适配器探测与安装提示 | E-1、C-3 | 原生 `memctl-dsh` 与旧 `dsh-hooks-plugin` 分别探测；原生存在时不写 `hooks.json`；两者共存、缺失与版本过低给出可诊断状态和界面安装提示 | I03、I05 的适配状态场景 |
| E-4：发布产物与隔离安装 | E-1 至 E-3 | `memctl dsh plugin-path`/`plugin-dir` 从 `go:embed` 生成绝对路径；Release 附带 `memctl-dsh-<version>.tgz` 与校验和；隔离 profile 安装与 `--dump-config` 验证 | I04、I06 |
| E-5：真实宿主验证 | E-4；可用的 DSH 会话与隔离环境 | 在真实 DSH 会话验证注入快照、每轮提交、收尾、恢复、子代理与失败降级；不可执行时记录未验证项；由 `T052` 跟踪 | H07–H10 的真实会话部分 |

### 阶段 F 的实施顺序

阶段 F 由 [pi 专用插件设计](./08-pi-plugin.md) 定义。真实 pi 会话证据不可得时保留未验证状态，不用单元测试替代。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| F-1：契约与骨架 | E-4 | `plugins/pi/` 的 `package.json`、`index.js`、`README.md`、`embed.go` 与 Node 单元测试骨架；无依赖与安装脚本；`npm pack` 文件清单正确 | I09 的包清单场景 |
| F-2：生命周期与注入 | F-1；pi 扩展事件契约明确 | `session_start` 后台 `start`、`before_agent_start` 有界等待并写 `sections["memctl"]`、`agent_settled` 有界同步、`agent_end` 兼容去重、`session_shutdown` 收尾且 `reload` 不收尾；错误不阻塞会话 | H12–H15 |
| F-3：适配器探测与状态 | F-1 | 原生 `memctl-pi` 的 npm、状态目录与 settings 本地路径探测；旧 `agent-memory-*` 条目内容探测；pi 宿主与插件版本门槛；`needs_attention` 与安装提示 | I07、I08、I10 |
| F-4：分发与发布产物 | F-1 至 F-3 | `memctl pi plugin-dir` 从 `go:embed` 生成稳定目录；`make pi-plugin-test`、`make pi-plugin-pack`；Release 附带 npm 审计 tarball；隔离 `PI_CODING_AGENT_DIR` 安装与卸载 | I09 |
| F-5：真实宿主验证 | F-4；可用 pi 会话与真实或可复现模型端点 | 在真实 pi 会话验证注入、同步、收尾、恢复、分叉与失败降级；不可执行时记录未验证项 | H12–H15 的真实会话部分 |

### 阶段 G 的实施顺序

阶段 G 由 [基于 GitHub Actions 的 Dreaming 远端整理方案](./11-github-actions-dreaming.md) 定义，用远端 GitHub Actions 结构化调用替换阶段 D 的本地 CLI runner 与平台沙箱。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| G-1：方案与契约修订 | F-5 | 新增 `docs/11` 方案文档，修订 `docs/01`–`05`、`docs/README.md` 与 `docs/validation/dreaming-handoff.md`，确立 Git 分支协议、三阶段结构化调用与 4 家内置提供商契约；由 `T082` 完成 | 文档链接、锚点与契约一致性检查 |
| G-2：`.github` 边界、Worker 与远端分支编排 | G-1 | `internal/vault` 与 `internal/dream` 保护 `.github`；实现 4 个提供商适配器、三阶段结构化整理 worker（`dream --worker`）、`dream/req` 与 `dream/res` 分支编排；删除 Seatbelt 沙箱与本地 CLI runner 代码 | D01–D05、P01 的 `.github` 边界场景 |
| G-3：CLI `--init-workflow`、帮助与 Web UI 适配 | G-2 | 实现 `memctl dream --init-workflow` 与内嵌工作流模板，更新 CLI 帮助（中英与 JSON）、`server` 就绪探针与 `DreamPanel.jsx` 提供商选择器 | D04、R06、UI 构建与 HTTP 联调 |
| G-4：真实 GitHub Actions 端到端验证 | G-3；配置好 Secret 的私有 GitHub 记忆仓库 | 在真实私有 GitHub 记忆仓库触发 Actions 运行，完成生成、拉取、Diff 审阅与 `apply` 全链路验证并归档证据 | D01–D05 真实远端与模型端点验收 |

### 阶段 H 的实施顺序

阶段 H 由 [12. 跨会话新记忆提炼](./12-session-corpus-dreaming.md) 定义，在不改动既有 GitHub Actions Dreaming 的前提下，采用「本地确定性蒸馏 + 本地当前 Agent 交互式整理」补齐上游 Dreaming「跨原始会话日志自动提炼未记录新记忆」的能力。

| 步骤 | 前置依赖 | 本轮交付边界 | 对应验收 |
| --- | --- | --- | --- |
| H-1：方案与契约修订 | G-4 | 新增 `docs/12-session-corpus-dreaming.md`，修订 `docs/01`–`05`、`docs/09`、`docs/README.md` 与 `docs/STATUS.md`，确立本地确定性蒸馏、当前 Agent 交互式 `prepare`/`propose` 协议、MCP 工具与来源绑定守卫契约；由 `T098` 完成 | 文档链接、锚点与契约一致性检查 |
| H-2a：本地会话蒸馏器（已核对宿主；`T100`，已完成） | H-1 | 引入 `github.com/klauspost/compress/zstd` 并在 `go.mod` 固定版本；实现 `internal/corpus` 框架与四个已核对格式（`claude-code`、`codex`、`pi`、`dsh` 含 zstd 解码与解压产出上限），回合裁剪、正则脱敏、可疑指令过滤、14 天 TTL 缓存与已消费摘要保留规则；扩展 `hookio.ReadInput` 解析 `transcript_path` 与 `last_assistant_message`；`sync`/`end` 支持 `--transcript` 与 `AGENT_MEMORY_CORPUS`；`memctl-pi` 与 `memctl-dsh` 传递会话文件路径 | D06 中四个已核对宿主的单元测试与 CLI 集成测试 |
| H-2b：存量多会话补扫与其余宿主探测（`T101`，已完成） | H-2a | 实现按项目预过滤的补扫（`MaxBackfillFilesPerRun`、`MaxBackfillFileBytes`、`BackfillBudget`）；接入 `antigravity`；`commandcode`、`grok`、`opencode` 只做「探测失败静默跳过」，并在方案文档记录格式核对状态 | D06 的超限日志与项目优先场景 |
| H-3：本地交互式 `prepare`/`propose`、项目筛选与 MCP 工具（`T102`，已完成） | H-2a、H-2b | `internal/dream` 实现本地基线固定路径（不依赖远端就绪检查）、`PrepareLocal`（补扫触发、`cwdKey` 优先排序、`--project` 过滤与 `remainingUnconsumed` 计数）、`ProposeLocal`（Provenance Guard、`files[]` 逐字比对、30 天删除闸门与链接校验）、`ApplyDream` 的幂等消费写回与 `Store.List`；CLI 新增 `dream --prepare`、`dream --propose --stdin`、`dream --list`；`internal/mcp` 新增 `memory_dream_prepare`、`memory_dream_propose`；`internal/server` 新增 `GET /api/v1/dream` | D07、D08 单元测试、MCP 协议测试与 CLI 集成测试 |
| H-4：配套 Skill、CLI 帮助、UI 展示与端到端验收（`T103`，已完成） | H-3（已完成） | 更新 `skills/memctl/SKILL.md` 纳入本地交互式 Dreaming 指引；更新中英与 JSON 帮助；UI「整理」视图通过 `GET /api/v1/dream` 列出提案并展示 `local-agent` 的 `sessionsScanned`/`newMemoriesExtracted`；完成宿主会话内端到端验收 | D06、D07、D08、R06、前端单测与真实宿主会话回归 |

### 阶段步骤的子任务切分

`STATUS.md` 按阶段顺序滚动登记近期实施任务，不在热台账中一次性预拆全部远期任务。单一子系统且单轮可独立完成验证的步骤（`A-1`、`A-3`、`A-4`、`A-6`、`B-1`、`B-2`、`D-1`）按 1 个任务登记；同时跨越「核心实现与宿主/性能实测」「后端领域/API 与前端视图」或「功能实现与发布工程」的步骤（`A-2`、`A-5`、`B-3`、`C-1`、`C-2`、`C-3`、`D-2`），在进入该步骤时按下表拆分为 2 个边界不重叠的子任务依次推进。

| 步骤 | 子任务切分（滚动登记至 `STATUS.md`） | 子任务交付边界 | 对应验收 |
| --- | --- | --- | --- |
| `A-2` | 子任务 1（`T006`，已完成）：本地初始化与安全读取 | 无远端 `init`、`read` / `read --json`、文件修订与根内安全路径访问；不执行网络请求 | `G01`、`G05` 的初始化场景，`F04`，`P01`、`P02` 的读取场景，`P03` 读取限额 |
| `A-2` | 子任务 2（`T008`，已完成）：记忆格式与链接解析 | 行级无损解析、行尾保序元数据、`entryId`、根相对 `[[path]]` 解析与同名歧义/死链诊断 | `F01`、`F02` 的解析场景 |
| `A-5` | 子任务 1（`T018`）：后台同步派生与进程生命周期 | `sync` / `end` 与受管写入后的非交互后台同步派生、父进程凭据白名单快照传递、锁忙立即退出的防风暴机制、父进程/宿主/Git 子进程强杀清理与锁释放 | `G08.2`，`L01` 的后台与强杀场景，`L02` |
| `A-5` | 子任务 2（`T019`）：Hook 协议、会话身份与宿主性能实测 | `start`/`context`/`pull`/`tag`/`wrap`、SID 优先级解析、`--once` 同步与注入分离去重、信号与退出码透传，以及按第 3 节记录本地性能基线与可用宿主实测 | `H01`–`H06`；按第 3 节记录宿主与性能实测 |
| `B-3` | 子任务 1：前端工程脚手架、内嵌构建与连接框架 | `web/` 前端工程初始化、`web/embed.go`、`Makefile` 的 `ui` 构建链路、`memctl ui` 命令（`--port`、`--no-open`、`--readonly`）、Session Token 注入、SSE 客户端流解析与断线重连校正及顶栏状态 | `R01` 的内嵌 UI 资产场景，`A02`、`A03`、`A05` 端到端联调 |
| `B-3` | 子任务 2：只读业务视图交付 | 只读知识图谱（Cytoscape.js）、条目检索与过滤浏览、审计 Diff 预览（Diff2Html）与诊断结果展示，确认静态资源无外部 CDN 依赖 | `B-3` 全部只读视图联调；回归阶段 A 用例 |
| `C-1` | 子任务 1：受管写入、移动改链与确定性修复后端及 API | 实现 `PUT /api/v1/files/{path...}`、`POST /api/v1/files:move`（含已解析链接定点更新与大小写重命名中间路径）、`POST/PATCH/DELETE /api/v1/entries`（含跨文件移动与图谱拆分新文件）、CLI `doctor --fix` 与 `POST /api/v1/doctor:fix`、`POST /api/v1/sync` | `A04`、`W01`–`W07`（含 `W02` CLI/UI 并发与 `W04` 多文件移动中断恢复），`P01`–`P03` 的 HTTP 写入与移动场景 |
| `C-1` | 子任务 2：UI 编辑、移动改链与图谱拆分交互 | 接入文件/条目原文编辑、移动改链、图谱节点拆分新文件、手动同步与确定性修复的前端交互及版本冲突草稿保留 | `C-1` 前后端联调与 `W02`、`A04` 回归 |
| `C-2` | 子任务 1：冲突解决与审计回滚后端及 API | 实现 `POST /api/v1/conflict:resolve`、`POST /api/v1/conflict:abort`（与 CLI `resolve` 共用多阶段状态机）、`app.Revert` 与 `POST /api/v1/audit/{hash}/revert`（单父提交校验、拒绝根提交与 merge commit、revert 冲突自动采集中止与恢复） | `C01`–`C05`，`A03` 的管理写接口只读拦截，`A04` |
| `C-2` | 子任务 2：UI 冲突解决与审计回滚视图 | 接入 UI「冲突解决」（三方上下文、删除选择、阶段进度与中止）与「审计回滚」（历史 Diff、来源展示、可疑内容提示与确认回滚）视图 | `C-2` 前后端联调与 `C01`–`C05` 回归 |
| `C-3` | 子任务 1：配置模板内嵌、保留语义更新与 CLI 安装 | `internal/assets` 内嵌全部支持的 adapter 模板；`internal/adapters` 实现宿主与插件版本探测、独立配置锁、配置基线校验、JSON/JSONC/YAML/TOML 保留语义与注释更新、同目录原子替换及中断恢复；实现 CLI `install`（`--all`、`--dry-run`、`--status`） | `I01`、`I02`，`R01` 的内嵌 adapter 模板场景 |
| `C-3` | 子任务 2：HTTP 安装接口与 UI 接入面板 | 实现 `GET /api/v1/adapters`、`POST /api/v1/adapters/{agent}/install` 与 `--readonly` 拦截，接入 UI「整理接入」中的 adapter 探测、预览与安装面板 | `A03` 的安装接口只读拦截，`I01`、`I02` 端到端联调 |
| `D-2` | 子任务 1：Dreaming 显式应用与 UI 整理视图 | 实现 `app.ApplyDream`、CLI `dream --apply <ID> --patch-hash <HASH>` 与 `POST /api/v1/dream:apply`（校验原始基线、过期拒绝、受管事务提交、中断恢复与重复 apply 幂等返回），接入 UI「整理接入」提案预览与确认操作 | `D02`（旧基线提案拒绝应用部分）、`D04`，`A03` 的 Dreaming 接口只读拦截 |
| `D-2` | 子任务 2：跨平台发布工程与全量交付验收 | 按第 3、4 节完成跨平台发布候选包（`make release`）、SHA-256 校验和与第三方许可证清单、降级与恢复演练及阶段 A–D 全量回归 | `R01`–`R04` 全量回归及第 4 节发布前检查 |

### 执行与完成边界

- 阶段 A–B 未接入前端前不初始化 `web/`，不用空 `web/dist` 满足 embed；04 中包含前端的 `Makefile` 在 `B-3` 落地时接入。
- `T002`（凭据契约与 `G08` 子场景定义）可在 `A-1` 至 `A-3` 期间独立完成，仅作为 `A-4`、`A-5` 的前置依赖；局部外部依赖或待确认决策受阻时，按 [开发循环规范](./06-development-loop.md#3-状态与推进) 标记阻塞并优先推进其他前置条件已满足的独立任务。
- 阶段步骤按上表粒度在 `STATUS.md` 滚动登记并按轮次推进；同一阶段内每个会话完成一轮后自动接续下一轮，每个阶段（阶段 A、B、C、D）完成时按 [开发循环停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并通知用户。
- 未实现的命令或接口只按已落地范围提供能力并显式返回未实现或不支持错误，不让占位实现返回成功，不把跳过远端视为推送成功；能力变化时同步更新根 `README.md`。
- 复合验收项按子场景记录结果：`W02` 的 CLI/UI 并发在 `C-1` 完成；`R01` 的默认指引在 `A-6`、UI 资产在 `B-3`、adapter 模板在 `C-3` 完成；`R02` 的 Git/凭据场景在 `A-6`、Dreaming 前置条件降级场景在 `D-1`/`G-2` 完成。子场景通过不把整项提前标为全部通过。
- 单一阶段或单平台通过不代表全项目交付就绪；阶段 A–D 的功能验收、平台生命周期验证与第 4 节发布前检查分别判定。

## 2. 核心测试矩阵

| ID | 输入或故障场景 | 可验证结果 |
| --- | --- | --- |
| F01 | 上游规范样例、无元数据条目、重复 source、未知键、代码围栏 | 正确解析；未编辑内容逐字节保留 |
| F02 | Markdown、SQL、脚本、二进制资产、同名链接歧义 | 根相对链接符合规范；二进制不进入文本编辑器；歧义不猜测 |
| F03 | 与入口断开的文件环、仅间接引用文件 | 环内文件标为孤立；可达文件不误报 |
| F04 | `read` / `read --json` 读取存在、不存在、非文本及超限文件 | 原文或结构化结果符合契约；修订可用于后续写入；不支持输入明确失败，无网络访问 |
| W01 | 两个客户端读同一基线，分别提交不同文本 | 第一个成功，第二个得到版本冲突；第一个内容不丢失 |
| W02 | Agent CLI 与 UI 同时改同一文件 | 使用相同锁与版本协议；不发生无提示覆盖 |
| W03 | 外部 staged/unstaged/untracked 内容存在时触发 Hook、UI 写入 | 自动路径不提交；显式串行 capture 才接收差异 |
| W04 | 文件移动、引用更新到第 N 个文件时强杀 | 识别未完成事务；恢复后全部前镜像一致，或明确停在可诊断恢复态 |
| W05 | 提交成功后、完成标记写入前强杀 | 通过事务 ID 识别已有提交，不重复应用 |
| W06 | 恢复前用户另改一个受影响文件 | 拒绝覆盖新内容，显示需人工处理的路径 |
| W07 | Git hook 改动计划外文件或提交失败 | 不夹带未知变化；记录真实副作用，不能返回无变化假象 |
| G01 | 空目录无远端初始化、重复初始化 | 一次首笔提交，重复执行不改变已有文件和配置 |
| G02 | 第二设备接入含历史的私有远端 | 克隆相同历史，建立正确 upstream，不产生独立根提交 |
| G03 | 空远端、无 upstream、错误 URL、无默认分支、鉴权失败 | 分别进入正确分支；访问失败不当作空远端或内容冲突 |
| G04 | fetch 超时、push 断网、远端在 fetch 后再次前进 | 本地提交保留；区分网络与 non-fast-forward；无 force push |
| G05 | 缺少身份、签名失败、hooks 拒绝 | 明确失败，不改全局配置，不自动关闭签名或 hooks |
| G06 | 手工 merge/rebase/revert/cherry-pick、sequencer、未合并索引 | 普通写入和自动同步暂停；不替用户 abort 或提交冲突标记 |
| G07 | 外部 autostash 恢复后留下冲突但没有 rebase 目录 | 通过索引检测拦截，不只检查目录 |
| G08 | 终端与后台的凭据环境不同、SSH agent 不可用、HTTPS helper 需要交互或阻塞 | 按 [凭据契约](./03-workflows-and-dataflows.md#43-凭据环境继承与失败处理) 继承环境；可用凭据非交互认证成功，不可用时明确失败，阻塞按预算终止；不泄露凭据，不遗留无锁 Git 进程；子场景见 2.1 |
| C01 | 两个连续本地提交分别冲突 | 每阶段获得正确 stage 1/2/3；一次确认不假设全部完成 |
| C02 | rebase abort 后用旧 UI 提交解决结果，期间 HEAD 已变 | 返回 `CONFLICT_STALE`，不覆盖新历史 |
| C03 | 删除/修改、空文件、二进制、重命名冲突 | 删除与空文件区分；不支持类型明确拒绝 |
| C04 | 解决中关闭 UI 或重启进程、之后 continue/abort | 操作所有权及阶段可恢复；中止恢复原始 HEAD |
| C05 | 普通提交 revert 冲突、根提交、merge commit | 不支持对象被拒绝；本工具 revert 可中止；后续 Hook 不接管残留 |
| P01 | Unix/Windows 路径逃逸、编码分隔符、盘符、UNC、`.git` 与 `.github` 别名 | 读写移动均被统一路径边界拒绝；`ListManaged` 忽略 `.git` 与 `.github` |
| P02 | 符号链接、reparse point、硬链接、检查与打开之间替换目录 | 不读取或修改库外目标，不修改 Git 内部文件 |
| P03 | 超大 stdin、文件、请求体、超多变更文件 | 有界失败，无截断后保存或无界内存增长 |
| H01 | stdin 各 SID 拼写、显式 SID、环境 SID、空值、非字符串 | 优先级一致；无稳定 SID 不跨会话去重 |
| H02 | 同一 Agent 并行 A/B 会话，交替 tag/write | 来源不串会话，不使用共享 last-sid 推断 |
| H03 | 首轮锁忙或断网，下一轮 context --once | 同步可以重试，已成功注入正文不重复输出 |
| H04 | start/context JSON 与纯文本、pull/sync 空输出 | 字段、事件名和 stdout 符合契约，无 Git 输出混入 JSON |
| H05 | wrap 子程序失败、被信号中断、同步也失败 | 透传子程序状态，同步结果不掩盖原结果 |
| H06 | 无 remote、缺少稳定 SID、入口超长时执行 start/context | 本地上下文可用；无 SID 不跨会话去重或伪造来源；超长按宿主限额明确提示，不静默截断 |
| H07 | 原生 dsh 插件事件映射 | `agent/created` 注入、`turn/end` 同步、`agent/disposed` 收尾各一次 |
| H08 | 上下文注入 | 首个请求前存在 `[agent-memory]` 快照，不伪造 inbox 用户消息 |
| H09 | 并发与失败 | 多轮快速结束只保留一个有界同步；断网/锁忙不阻塞会话 |
| H10 | 子代理与生命周期 | 子代理不重复注入/同步；插件卸载清理子进程 |
| H12 | pi 原生插件生命周期 | `session_start` 注入、`agent_settled` 同步、`session_shutdown` 收尾各一次；`reload` 不执行 `end` |
| H13 | pi 上下文注入与缓存稳定 | 首个请求存在 `memctl` section；正文不变不产生新 delta；不伪造 user message；`forceSystemPrompt` 时有回退 |
| H14 | pi 并发与失败 | 多轮快速结束只保留一个有界同步；二进制缺失、超时、非法 JSON、锁忙、断网不阻塞会话 |
| H15 | pi 会话边界与子代理 | `new/resume/fork` 使用正确 SID；后台子代理获得独立 SID 与生命周期；前台 in-process 子代理不触发会话事件，不按 `parentSession` 误跳过；显式开关行为可验证 |
| H11 | 宿主会话结束映射 | `SessionEnd`、`session.deleted`、`agent/disposed` 与插件卸载兜底各触发一次 `end`；回合结束、空闲与压缩只触发 `sync`，不触发 `end` |
| A01 | 注册全部 REST 路由 | Go ServeMux 不 panic，参数可取出，方法匹配正确 |
| A02 | 缺失 Token、跨源 Origin、伪造 Host、PATCH 绕过 | 所有 API 包括读取和 SSE 都按规则拒绝 |
| A03 | 只读 UI 下直接构造 POST/PUT/PATCH/DELETE | 服务端拒绝，包括安装、同步和 Dreaming 启动 |
| A04 | 文件 If-Match 过期、跨文件基线过期、缺少前置条件 | 分别返回 412 或 428，未改变任何目标 |
| A05 | SSE 数据跨块拆分、连接中断、队列溢出、目录重命名 | 正确解析；重连全量校正；不依赖事件恰好一次送达 |
| D01 | 未配置远端、远端未确认私有、缺少 `.github/workflows/memctl-dream.yml`、未知 `runner` | 拒绝生成并返回针对性错误（`UNSUPPORTED` 或 `REMOTE_NOT_APPROVED`）；不推送分支，不修改主库或用户配置 |
| D02 | 未提交内容、生成期间主库有新提交、应用前内容改变 | 脏库拒绝生成；旧基线提案拒绝应用 |
| D03 | 结果分支父提交不匹配 `baseHead`，或包含删除 `MEMORY.md`、修改 `.github/**`/`.git/**`、路径逃逸、符号链接、超限 | 本地范围与父提交检查拒绝该结果，提案标为 `failed`，不修改主库，远端临时分支被清理 |
| D04 | 同一提案连续 apply、应用中断后重试、`--init-workflow` 幂等初始化 | apply 返回同一提交或恢复状态，不重复提交；`--init-workflow` 写入并提交工作流文件，已最新时不产生空提交 |
| D05 | `dream --worker` 对接模拟模型端点：静态候选提取、多步分批、非法 JSON 自动纠正重试、API 失败空提交回传、事件型条目保护 | 正确发送静态诊断与批次文件；单次非法 JSON 触发 1 次纠正重试后成功；API 失败推送 `status: "failed"` 空提交且密钥脱敏；本地轮询立即感知失败 |
| D06 | Hook stdin 含 `transcript_path` / `last_assistant_message`、传入 `--transcript`，或 `dream --prepare` 扫描存量宿主历史目录；日志含超长工具输出、各类 API Key/Token/密码、`<system-reminder>`、可疑提示词注入回合，或设置了 `AGENT_MEMORY_CORPUS=0`；补扫目录中混入超过 `8 MiB` 的明文日志、超过 `8 MiB` 解码产出的 `dsh` zstd 会话与最近的无关项目会话 | 实时采集与存量补扫生成的每个 `SessionDigest` 大小均 $\le 16\text{ KiB}$，所有凭据模式被替换为 `[REDACTED]`，系统块与可疑回合被剔除；超限日志标记 `truncated: true`（明文截尾、zstd 顺序解码至上限），解码失败时静默跳过；当前项目的会话优先进入候选，无关项目不挤占配额；`AGENT_MEMORY_CORPUS=0` 或日志文件缺失时不写缓存且命令退出码为 `0` |
| D07 | 存在同一项目或跨项目的多个未消费 `SessionDigest` 时，通过 CLI（`dream --prepare [--project KEY]` → `dream --propose --stdin` → `dream --apply`）或 MCP（`memory_dream_prepare` → `memory_dream_propose`）执行本地交互式整理；含跨多会话引用 `sessionIndices: [0, 1]`、超出 12 个会话返回 `remainingUnconsumed`、传入越界 `sessionIndices`、伪造 `[source:]`、`files[]` 缺少拼装后的条目行、目标文件的父目录不存在、新建主题文件但未补 `MEMORY.md` 索引、同一 basename 的两个克隆、删除未满 30 天的过期条目、`MEMORY.md` 超过 `EntryBloatLines`，以及仓库无远端或无工作流文件 | 仓库无远端、无 `.github/workflows/memctl-dream.yml` 时 `--prepare` 仍成功；`--prepare` 默认优先返回当前 `cwdKey` 项目的多个会话（指定 `project` 时仅返回该项目会话，同名歧义返回 `INVALID_ARGUMENT`）；`--propose` 正常路径生成 `runner: "local-agent"`、`status: "ready"` 的 `DreamProposal`，多会话提炼的新条目精确携带全部引用会话的 `source` 与最新 `added` 日期；`files[]` 缺少拼装行时拒绝且不生成提案；父目录不存在时返回 `NOT_FOUND` 并列出缺失目录，未补索引的新文件返回 `UNSUPPORTED` 并列出 `ORPHAN_FILE <path>`，两者均不落盘；`MEMORY.md` 超过 `EntryBloatLines` 时在提案 `warnings` 中提示但不阻断；`apply` 成功后提交主分支并将本次涉及的多个会话摘要统一标记 `consumedBy` 与 `consumedRevision`；越界下标、伪造 `source` 或触发 `DELETION_DEFERRED` 时被本地守卫拒绝且不生成提案 |
| D08 | 提案生成后、apply 前对参与整理的会话摘要重新蒸馏（`updatedAt` 与 `turns` 变化），以及 apply 提交成功后写回 `consumedBy` 前中断进程 | 摘要变化时 apply 仍可完成提交，但对应项不被标记消费并在结果与 `warnings` 中列出；写回中断后重新 apply 命中幂等路径并完成写回，已消费项保持不变；任何情况下都不以新摘要内容覆盖已记录的 `consumedRevision` |
| I01 | 支持版本安装两次、用户已有同类 Hook | 无重复项，不覆盖非本工具配置 |
| I02 | 配置预览后用户修改、安装中断、格式不支持 | 基线冲突或保留可恢复现场；不全文件强制重序列化 |
| I03 | dsh 插件安装/升级/卸载；原生与旧插件共存 | 原生存在时不写 `hooks.json`；共存时 `needs_attention`；隔离 profile 安装幂等；无安装脚本 |
| I04 | 内嵌 tarball 离线安装 | `memctl dsh plugin-path` 输出存在的绝对 `.tgz`；无 npm 网络也能在隔离 profile 安装；文件清单和版本正确 |
| I05 | DSH 兼容拒绝 | `engines.dsh` 范围外版本被拒绝，不出现半安装或静默失效 |
| I06 | 添加插件界面输入 | npm 包名、绝对 tarball、绝对目录均可解析并安装；`~`、相对路径、带子目录的 GitHub URL 被拒绝 |
| I07 | pi 原生插件探测 | 原生存在时 `configured` 且无 `hooks.yaml` 计划；原生与旧条目共存时 `needs_attention` |
| I08 | pi 安装计划与提示 | 原生缺失且无旧条目时不写 `hooks.yaml`，输出 npm 与本地目录提示；旧条目保留并标记 legacy；旧条目存在但插件缺失时 `needs_attention` |
| I09 | pi 包安装 | `npm pack` 文件清单正确；隔离 `PI_CODING_AGENT_DIR` 下本地目录安装、`pi list`、卸载可用；无依赖和安装脚本 |
| I10 | pi 版本与降级 | 低于下界或无法解析的 pi 版本不被报告为 `configured`；`sections` 缺失时回退，不静默失效 |
| M01–M12 | MCP 记忆工具：协议、工具、沙箱独立性、探测 | 逐项定义见 [MCP 记忆工具的验收矩阵](./09-mcp-memory-tools.md#61-矩阵) |
| R01 | 仅复制二进制到空 HOME，保留系统 Git | 默认指引、UI、模板可用，不依赖独立 INSTRUCTIONS.md；默认指引含条目失效的判定规则（状态型改写、事件型追加、禁止 changelog） |
| R02 | 无 Git、Dreaming 前置条件缺失（未知提供商、无远端或缺少工作流）、凭据环境缺失 | 分别给出针对性错误；不影响无关功能 |
| R03 | 干净检出后使用已记录工具链构建，执行 `version/help` 与未知命令 | 实际构建与测试入口可用；version/help 不访问仓库、不要求 Git 或 Node.js；未知命令返回用法错误，无占位成功结果 |
| R04 | 在合成诊断仓库运行 `doctor` / `doctor --json`，对比执行前后文件与 Git 状态 | 诊断级别、路径和计数符合契约；error 返回 1，仅 warning/info 返回 0；只读检查不改变记忆内容、Git 索引或 HEAD |
| R05 | `update` 在已最新、有新版本、`SHA256SUMS` 不匹配、资产缺失与不支持平台 | 已最新只查询不写文件；新版本校验通过后原子替换并保留可执行权限；校验失败、资产缺失或平台不支持时原二进制不变；`--check` 不写文件 |
| R06 | 无仓库环境执行 `help`、`help <command>`、`<command> --help`、`help --json`，以及 `--lang`、`MEMCTL_LANG` 与 `LC_ALL`/`LC_MESSAGES`/`LANG` 组合 | 每个命令页含用法、参数、输入输出、退出码和示例；中英文可切换；JSON 为 `schemaVersion` 1 且覆盖全部命令；帮助不访问仓库、Git、Node.js 或网络；未知主题或非法语言返回 `2`，已知命令页返回 `0` |
| L01 | 同步时强杀父进程、宿主、Git 子进程 | 没有遗留 Git 在锁释放后继续写；否则该组合不开放后台写入 |
| L02 | 连续触发大量 Stop/end，网络持续不可用 | 无后台进程队列无限增长；本地已提交内容完整 |

### 2.1 `G08` 凭据子场景

子场景只定义可复现构造与预期行为，不表示已经验证。执行时使用临时 HOME、临时记忆仓库和本机受控传输桩，不连接真实远端、不使用真实凭据。

| 子场景 | 可复现构造 | 预期结果 |
| --- | --- | --- |
| `G08.1` SSH agent 可用（前台显式） | 临时仓库配置受控远端，`GIT_SSH_COMMAND` 指向测试桩，桩记录 `SSH_AUTH_SOCK` 并以成功退出 | `sync --wait` 返回成功，远端收到预期提交，`remoteState=synced`，工作树干净 |
| `G08.2` 终端成功但后台凭据环境不同 | 前台显式同步在带 `SSH_AUTH_SOCK` 的环境中成功；后台派生进程的凭据环境移除或改写该变量，桩拒绝无 agent 的认证 | 后台同步以 `AUTH_FAILED` 结束，本地提交保留，`remoteState=failed`，无遗留 Git 子进程，仓库锁可再次获取；下一次前台同步成功补推 |
| `G08.3` HTTPS credential helper 非交互成功 | 配置返回固定凭据的桩 `credential.helper`，无 TTY | 认证成功，凭据不出现在 stdout、stderr、日志或状态文件 |
| `G08.4` 交互式 helper 被抑制 | 配置需要终端输入或会阻塞的 `credential.helper` 或 `GIT_ASKPASS` 桩，无 TTY | 非交互路径不调用 askpass、不弹出提示，按 `AUTH_FAILED` 在预算内失败 |
| `G08.5` 凭据阻塞与总超时 | 桩 helper 或 SSH 挂起超过总 deadline | 返回 `TIMEOUT`，Git、SSH 与 helper 进程组被终止并确认清理，锁可再次获取，无无锁写仓库的残留进程 |
| `G08.6` 未知主机密钥 | `known_hosts` 缺失或与桩主机密钥不匹配，且无 TTY | 不自动接受主机密钥，返回 `AUTH_FAILED`，不写入 `known_hosts` |
| `G08.7` 凭据泄露检查 | 在失败、超时与成功路径检查进程输出、日志和状态文件 | 不出现凭据、含凭据的 URL 或 Hook payload；含 `user:pass@` 的 URL 在诊断文本中被脱敏为 `://***@` |

## 3. 性能与适配记录

记录硬件、OS、Git/Go 版本、仓库文件数和大小、历史提交数、签名及 hooks 状态。分别测量冷进程启动、无变化 Hook、受管写入、外部 capture、锁竞争和网络故障，不用一个「冷启动」数值代表全部路径。

报告 p50、p95、最大值及清理时间。`version/help < 5ms`、无竞争派生路径 `< 50ms` 是待测优化目标；达不到时如实记录并调整目标，不省略一致性检查。完整同步遵循业务预算，网络和故障清理不纳入毫秒级宣传。

每个 adapter 记录：宿主版本、插件版本、配置格式、事件映射、SID 来源、输入字段、输出 envelope、上下文是否确实注入、stdout 上限、timeout、进程清理方式、实测命令与结果。候选列表为 Claude Code、CodeBuddy、Codex、Grok、Antigravity、Command Code、WorkBuddy、OpenCode、pi、dsh；当前方案不将其统一标为已支持，CodeBuddy 与 WorkBuddy 的 hooks 写入已实测触发，桌面应用内的 `SessionEnd` 与 MCP 配置仍待验证。

平台矩阵至少覆盖目标 macOS arm64/amd64、Linux amd64、Windows amd64。编译、文件锁、路径语义与 Git 生命周期分项记录；没有测试结果的项保持未验证。

## 4. 发布前检查

- 文档中的命令、参数、状态字段和 REST 路由与实现一致；契约测试覆盖全部路由和错误类别。
- 帮助目录覆盖全部命令；`R06` 在无仓库、空 `PATH` 的隔离环境通过，中英文与 JSON 输出和 [CLI 契约](./04-ui-and-cli-specification.md#13-面向-agent-的帮助与中英-i18n) 一致。
- 同步与写入测试不接触真实用户配置或公网远端；真实宿主验证使用明确隔离的测试账户或目录。
- 单文件包包含默认指引、工作流模板、adapter 模板、`memctl-dsh` 与 `memctl-pi` 源文件，静态资源没有外部 CDN 依赖；发布附 SHA-256 校验和、第三方许可证、`memctl-dsh-<version>.tgz` 与 `memctl-pi-<version>.tgz`（两个插件包均已发布到 npm；tarball 用于审计，不作为 pi 安装输入）。
- `--readonly`、无 remote、缺少 Dreaming 工作流、无 GUI 浏览器等降级路径都有明确结果。
- 事务与配置安装恢复演练通过；不以手工删除锁或强制重置仓库作为测试通过条件。
- 视觉 UI/UX 验证仅在明确安排时开展，不用本方案的静态检查代替浏览器实际效果验证。
- 发布唯一入口是推送 `v*` 标签，Release 附件只由流水线产生，不从本地 `dist/` 上传；本地 `make release` 只用于验证构建链路，不构成发布步骤，也不作为发布产物证据。判断依据见 [发布入口](./04-ui-and-cli-specification.md#61-发布入口)。
- GitHub 构建流水线（`.github/workflows/build.yml`）的 `verify`、`build` 与 `publish` 三个作业都调用本地同一套入口（`make test` 的分片形式与 `scripts/release.sh`）；`publish` 依赖 `verify` 与 `build`，测试不通过时不产出 Release。分片清单 `scripts/test-shards.txt` 必须覆盖 `tests/integration` 的全部测试，新增、重命名或删除测试时重新生成。`v*` 标签发布附带四个候选二进制、`memctl-dsh-<version>.tgz`、`memctl-pi-<version>.tgz` 与三个元数据文件（插件 tarball 的 `<version>` 取插件包自身版本，可能落后于二进制版本）。流水线通过属于发布候选包门槛，不替代目标平台的运行时、生命周期与隔离验证。

## 5. 上轮审查问题追踪

| 问题 | 本次设计修订 | 验收 |
| --- | --- | --- |
| P1-1：直接写入不受锁保护 | 受管版本事务，外部编辑仅串行 capture | W01–W03 |
| P1-2：abort 后不能靠预写内容解决 rebase | 固定目标重建冲突，按阶段 continue | C01–C04 |
| P1-3：错误分类与中间态不完整 | Git 实态检查、所有权、失败恢复 | G03–G07、C05 |
| P1-4：Dreaming 克隆不等于隔离 | 固定导出、runner 隔离门槛、提案基线 | D01–D04 |
| P1-5：路径边界缺失 | 统一根内访问、拒绝链接和 Git 内部路径 | P01–P03 |
| P1-6：已有远端接入缺失 | 按空/非空远端分支初始化 | G01–G03 |
| P2-7：50ms 与 5 秒等待矛盾 | Hook 非阻塞取锁，分项测量 | L02、性能记录 |
| P2-8：Setsid 与跨平台承诺不符 | 分平台实现和进程生命周期门槛 | L01、平台记录 |
| P2-9：last-sid 串会话 | 显式身份传递，不做全局兜底 | H01–H02 |
| P2-10：SSE 无法携带鉴权头 | fetch 流、所有 API 统一鉴权 | A02–A03、A05 |
| P2-11：ServeMux 路由语法无效 | 动态段与动作段分离 | A01 |
| P2-12：默认指引未内嵌 | 指引和 adapter 模板随二进制打包 | R01–R02 |

「设计修订」只表示文档给出处理办法；对应测试通过后才表示实现问题关闭。
