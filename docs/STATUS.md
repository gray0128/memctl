# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A、阶段 B、阶段 C、阶段 D 与阶段 E 的已完成任务历史验收记录分别归档于 [阶段 A 归档](./validation/stage-a.md)、[阶段 B 归档](./validation/stage-b.md)、[阶段 C 归档](./validation/stage-c.md)、[阶段 D 归档](./validation/stage-d.md)（含 `T040`–`T051` 的发布后轮次记录）与 [阶段 E 归档](./validation/stage-e.md)（含 `T048`），各阶段实现审查见 [阶段 A 实现审查报告](./validation/stage-a-review.md)、[阶段 B 实现审查报告](./validation/stage-b-review.md)、[阶段 C 实现审查报告](./validation/stage-c-review.md) 与 [阶段 D 实现审查报告](./validation/stage-d-review.md)。

## 当前事实

- 版本：`v0.2.0` 为当前发布版本（2026-10-17 发布）。GitHub Release 附带 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制、`memctl-dsh-0.2.0.tgz` 与 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`；`v*` 标签推送由 GitHub Actions 构建并创建 Release，打包与发布门槛见 [单文件打包](./04-ui-and-cli-specification.md#6-单文件打包) 与 [发布前检查](./05-validation-and-delivery.md#4-发布前检查)。
- 能力：阶段 A–D 已全部交付并验收；阶段 E 的 `memctl-dsh` Host 插件、内嵌 tarball/目录、dsh 原生与 legacy 插件探测及发布产物已实现，真实 DSH 会话中的注入与每轮同步已在本机 `desktop` 验证，收尾仍待复验。已交付能力包括单文件 CLI（`init`、`read`、`write`、`sync`、`start`/`context`/`pull`/`tag`/`wrap`、`resolve`、`recover`、`doctor`、`install`、`dsh`、`dream`、`ui`）、内嵌 Web UI 的只读与写入视图、Hook 会话协议、adapter 配置安装与 Dreaming 提案生成与应用；行为契约以 [方案索引](./README.md) 下的 01–07 为准。
- 发布后改动：界面重设计（`T040`）与后续发布、UI 修复（`T041`–`T046`）已依次交付；`T042` 修正 Codex、Command Code、Antigravity、OpenCode 2 与 dsh 的真实宿主协议；`T048` 增加 `memctl-dsh` 插件、`memctl dsh plugin-path`/`plugin-dir`、原生插件探测与插件 tarball 发布产物；`T053` 发布 `v0.2.0`，将插件版本同步到 `0.2.0`；`T054` 补齐 Codex、OpenCode 与 DSH 的会话结束映射。验证与修复记录见 [UI/UX 验证报告](./validation/ui-ux-verification.md)、[发布后轮次记录](./validation/stage-d.md#发布后轮次记录t040t051) 与 [阶段 E 归档](./validation/stage-e.md)。
- 平台：darwin/arm64 完成运行时验证（全量测试、发布二进制冒烟与浏览器验证）；macOS amd64、Linux amd64、Windows amd64 仅交叉编译通过，运行、生命周期与隔离未验证。
- 宿主：macOS arm64 上 7 个宿主的 Hook 已完成配置，其中 5 个完成会话级验证；`install` 探测到的宿主版本不代表 Hook 已注入或被宿主采纳。DSH 0.2.1-rc.2 已通过隔离 profile 安装 `memctl-dsh`，并在本机 `desktop` 真实会话确认 `[agent-memory]` 注入、`turn/end` 后同步与子代理跳过；`T054` 已补插件卸载时对仍 live 会话的 `end` 兜底，真实退出复验、恢复与失败路径仍未验证。
- 未验证：真实 Dreaming runner 与模型端点（`claude` 在隔离 scratch HOME 下退出 `1`）；Codex 模型回复受账号额度限制；非 macOS 平台的运行与 runner 隔离；真实 DSH 会话中 `memctl-dsh` 的 `agent/disposed` 收尾、恢复与界面安装；Codex `SessionEnd`、OpenCode `session.deleted` 与 DSH 插件卸载兜底均未在真实会话结束路径复验；`systemPrompt.context` 注入、`turn/end` 后同步、子代理跳过与错误轮次同步已在真实会话验证，失败降级仅覆盖模型凭据缺失。
- 已知限制：阶段 C 的 `P3-6`、`P3-7`、`P3-13`；阶段 D 的 `P3-11`–`P3-16` 与 `P3-1`、`P3-18` 剩余部分；组件级 DOM 测试缺失；知识图谱密集同名孤立节点簇的标签拥挤；来源 URI 使用浏览器原生标题提示；`memctl-dsh` 未发布 npm 包、独立 GitHub 仓库或 Discover 索引，当前安装通道为内嵌 tarball；Command Code 没有 `SessionEnd`，Antigravity 的 `Stop` 语义仍待真实宿主确认。
- 构建基线：Node v26.10.0 / npm 11.19.1；`web/dist`、`bin/`、`dist/` 不提交，Go 构建与测试前先执行 `make ui`。

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
| T040 | 已完成 | 将工作区既有、尚未提交的前端界面重设计纳入 v0.1.0 发布，统一 `web/src` 各视图的布局、图标、配色与排版；不改变 REST 契约、状态字段与交互流程；依赖用户「纳入发布」指令；[归档记录](./validation/stage-d.md#t040前端界面重设计) |
| T042 | 已完成 | 修复 v0.1.0 的 Codex/Command Code `--json`、Antigravity 命名 Hook 与 `--agy` envelope、OpenCode 2 插件协议、dsh profile 插件探测；补回归并发布 `v0.1.1`；依赖用户「修复并发布新版 release」指令；[归档记录](./validation/stage-d.md#t042修复真实宿主-adapter-协议并发布-v011) |
| T041 | 已完成 | 新增 GitHub 构建流水线 `.github/workflows/build.yml`，在 `main` 推送、拉取请求、`v*` 标签与手动触发时调用 `make release` 构建四平台候选包并上传产物，`v*` 标签发布 GitHub Release；依赖 `T040` 与用户「版本号 0.1.0」指令；[归档记录](./validation/stage-d.md#t041github-构建流水线与-v010-发布) |
| T043 | 已完成 | 第二轮 ego-browser 端到端 UI/UX 验证（v0.1.1）：七视图、写路径、只读、深色与窄屏；只记录问题，不改变产品代码；[验证报告](./validation/ui-ux-verification.md#5-第二轮验证v011-发布后复验) |
| T044 | 已完成 | 收口第二轮验证的 `R2-P2-1`、`R2-P2-2` 与 `R2-P3-1`–`R2-P3-4`：Diff 内边距、粘性导航偏移、冲突横幅清理、BUSY 读取重试、本地时间格式与图谱间距；[复验记录](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044) |
| T045 | 已完成 | 修复概览导航与正文未对齐、选中项左边框被裁切、指标卡不可跳转与来源 URI 截断无完整提示；[验证记录](./validation/ui-ux-verification.md#7-概览对齐指标卡跳转与来源提示t045) |
| T046 | 已完成 | 发布 `v0.1.2`（含 `T044`/`T045` 的 UI 修复）：全量测试、四平台候选包、校验和与 CI Release；[归档记录](./validation/stage-d.md#t046发布-v012) |
| T047 | 已完成 | dsh 专用插件设计：核对 DSH 事件与上下文接入点，按「添加插件」对话框的 spec（npm 名、GitHub 仓库、绝对目录、绝对 tarball）比较分发，给出插件、适配器、发布和验收方案；只写设计，不写插件代码；[设计提案](./07-dsh-plugin.md) |
| T048 | 已完成 | 实现 `memctl-dsh` Host 插件、内嵌 tarball 路径命令、适配器探测与发布产物；`D1` 取 A；[归档记录](./validation/stage-e.md#t048memctl-dsh-主机插件与发布产物) |
| T049 | 已完成 | 台账收敛：把 `T040`–`T047` 完成记录移入阶段 D 归档、重写当前事实并把任务表锚点改指归档；不改产品行为；[归档记录](./validation/stage-d.md#t049台账收敛) |
| T050 | 已完成 | 重构根 README：补充版本与平台状态、快速开始、能力表、平台与限制，精简目录与命令细节并改指契约；不改产品代码；[归档记录](./validation/stage-d.md#t050根-readme-重构) |
| T051 | 已完成 | 收口 README 建议项：补齐 Web UI 视图表的概览与诊断、修复 4 处失效链接、对本机快速开始做 smoke 验证；不改产品代码；[归档记录](./validation/stage-d.md#t051readme-建议项收口) |
| T052 | 进行中 | 对应阶段 E 的 `E-5`：真实 DSH 会话验证；已确认 `[agent-memory]` 注入、`turn/end` 后同步、子代理跳过与错误轮次同步，待验证 `agent/disposed`、恢复、更多失败降级与界面安装；[验证记录](./validation/t052-dsh-real-session.md) |
| T053 | 已完成 | 发布 `v0.2.0`（含 `T048`）：插件版本同步到 `0.2.0`，全量测试、四平台候选包、插件 tarball、校验和与 CI Release；[归档记录](./validation/stage-e.md#t053发布-v020) |
| T054 | 已完成 | 统一宿主会话结束映射：补 Codex `SessionEnd`/`PreCompact`、OpenCode `session.deleted`/`session.compacted` 与 DSH 插件卸载时对仍 live 会话的 `end` 兜底；补回归与契约；[完成记录](#t054宿主会话结束映射与-dsh-收尾兜底) |
| T055 | 进行中 | 发布 `v0.2.1`（含 `T054`）：将插件版本提升到 `0.2.1`，运行全量测试与四平台候选包，校验产物并推送 tag；[本轮定义](#t055发布-v021) |

任务 ID 不代表执行顺序。阶段 A–D 的全部交付任务（`A-1`–`A-6`、`B-1`–`B-3`、`C-1`–`C-3`、`D-1`–`D-2`）与 `T040`–`T051`、`T053`、`T054` 均已完成；阶段 E 的 `E-1`–`E-4` 由 `T048` 交付，`E-5` 为进行中的 `T052`。完成记录与验收证据按任务总表的链接指向 `docs/validation/` 或本文件的最近一轮记录。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)、[阶段 E](./05-validation-and-delivery.md#阶段-e-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T054：宿主会话结束映射与 DSH 收尾兜底

- 目标与范围：把回合结束、压缩与会话结束的边界写入 Hook 契约；补齐 Codex 的 `SessionEnd`/`PreCompact`、OpenCode 的 `session.deleted`/`session.compacted` 与 DSH 插件卸载时对仍 live 会话的 `end` 兜底；补对应模板与回归测试。不改变 `end` 的 CLI 语义，不新增 adapter capability 字段。
- 契约：[Hook 生命周期事件映射](./04-ui-and-cli-specification.md#32-宿主生命周期事件映射)、[Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装)、[dsh 同步与收尾](./07-dsh-plugin.md#54-同步与收尾)。
- 验证日期与环境：2026-10-17，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；Codex 0.160.0、OpenCode 2.0.26、DSH 0.2.1-rc.2 的模板与假宿主测试环境；全量测试在具备 macOS `sandbox-exec` 权限的受控环境运行。
- 命令与结果：`make test` 全量通过（前端 16 文件 85 项；插件 Node 14 项；Go 含 `tests/integration` 248.0 秒）。其中 `TestC3EmbeddedTemplatesCoverR01` 覆盖 Codex `SessionEnd`/`PreCompact` 与 OpenCode `session.deleted`/`session.compacted` 的安装结果，`TestT048DshPluginTarballInstall` 通过。`TestRegistryHostProtocols` 固定 Codex 与 OpenCode 命令形状；DSH 插件测试 14 项覆盖 `agent/disposed` 一次 `end`、卸载兜底一次 `end`，以及已 disposed 后不重复。
- 验收：`H11` 在模板与假宿主层面通过；`H07`、`H10`、`I01` 回归通过。真实 Codex 与 OpenCode 会话结束事件、真实 DSH 退出兜底未执行。
- 未验证与遗留：真实会话结束路径；Claude Code `StopFailure`/`PreCompact` 与 Grok `StopFailure`/`PreCompact` 未补；Antigravity `Stop` 语义待确认；Command Code 无 `SessionEnd`；插件版本未提升，未重新构建发布 tarball。
- 下一步：`T052` 重新安装插件并正常退出 DSH，确认恰好一次 `end`；再评估补齐 Claude Code 与 Grok 的缺失事件与发布补丁版本。

## T055：发布 v0.2.1

- 目标与范围：把 `T054` 的宿主会话结束映射作为 `v0.2.1` 发布；插件包版本提升到 `0.2.1`，运行全量测试与 `make release`，校验四个候选二进制和插件 tarball，推送 tag 触发 GitHub Release。不改变 `end` 的 CLI 语义或新增 adapter 能力。
- 契约：[单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](./05-validation-and-delivery.md#4-发布前检查)、[dsh 专用插件](./07-dsh-plugin.md)。
- 验收：`make test` 全量通过；`dist/SHA256SUMS` 校验全部 `OK`；tag `v0.2.1` 推送后 GitHub Actions 创建 Release。

## 近期实施任务

`T055` 进行中：把 `T054` 的宿主会话结束映射发布为 `v0.2.1`。`T054` 已完成：Codex `SessionEnd`/`PreCompact`、OpenCode `session.deleted`/`session.compacted` 与 DSH 卸载兜底已实现并通过回归。`T052` 进行中：已确认注入、`turn/end` 后同步与子代理跳过；退出应用时未观察到 `end`，剩余真实退出复验、恢复、失败路径与界面安装，详见 [T052 验证记录](./validation/t052-dsh-real-session.md)。按 [停止条件](./06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

未验证项与已知限制见 [当前事实](#当前事实)。界面重设计的视觉结论以 [第二轮 UI/UX 验证](./validation/ui-ux-verification.md#5-第二轮验证v011-发布后复验) 与 [收口复验](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044) 为准。
