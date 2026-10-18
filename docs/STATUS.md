# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账。状态含义、完成条件与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，完整阶段顺序见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，阶段 A、阶段 B、阶段 C、阶段 D、阶段 E 与阶段 F 的已完成任务历史验收记录分别归档于 [阶段 A 归档](./validation/stage-a.md)、[阶段 B 归档](./validation/stage-b.md)、[阶段 C 归档](./validation/stage-c.md)、[阶段 D 归档](./validation/stage-d.md)（含 `T040`–`T051` 的发布后轮次记录）、[阶段 E 归档](./validation/stage-e.md)（含 `T048`、`T052`–`T059`）与 [阶段 F 归档](./validation/stage-f.md)（含 `T060`–`T077`），各阶段实现审查见 [阶段 A 实现审查报告](./validation/stage-a-review.md)、[阶段 B 实现审查报告](./validation/stage-b-review.md)、[阶段 C 实现审查报告](./validation/stage-c-review.md) 与 [阶段 D 实现审查报告](./validation/stage-d-review.md)。

## 当前事实

- 版本：`v0.6.0` 为当前二进制发布版本（2026-10-18 发布，新增 `memctl mcp` 记忆工具通道与 `install --status` 的 MCP 探测，并缩短 CI 耗时）；npm 包 `memctl-pi` 的 `0.5.0`、`0.5.1` 与 `memctl-dsh@0.5.1` 均已发布，`latest` 均为 `0.5.1`，插件源码自 `v0.5.1` 起未改动，`v0.6.0` 不发布 npm，插件 tarball 仍为 `0.5.1`。GitHub Release 附带 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制、`memctl-dsh-0.5.1.tgz`、`memctl-pi-0.5.1.tgz`、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`；`v*` 标签推送由 GitHub Actions 构建并创建 Release，打包与发布门槛见 [单文件打包](./04-ui-and-cli-specification.md#6-单文件打包) 与 [发布前检查](./05-validation-and-delivery.md#4-发布前检查)。
- 能力：阶段 A–D 已全部交付并验收；阶段 E 的 `memctl-dsh` Host 插件、内嵌 tarball/目录、dsh 原生与 legacy 插件探测及发布产物已实现，真实 DSH 会话验证完成到 `v0.4.0`；阶段 F 的 `F-1`–`F-5` 已由 `T060`、`T062` 交付并验证。已交付能力包括单文件 CLI（`init`、`read`、`write`、`sync`、`start`/`context`/`pull`/`tag`/`wrap`、`resolve`、`recover`、`doctor`、`install`、`dsh`、`pi`、`mcp`、`update`、`dream`、`ui`）、面向 Agent 的中英帮助（`help`、`<command> --help`、`help --json`）、内嵌 Web UI 的只读与写入视图、Hook 会话协议、adapter 配置安装与 Dreaming 提案生成与应用；行为契约以 [方案索引](./README.md) 下的 01–09 为准。
- 发布后改动：`T040`–`T077` 的界面重设计、宿主协议修复、DSH/pi 插件、`update`、CLI 帮助与 i18n、npm 发布、版本发布、MCP 记忆工具通道与文档一致性检查均已交付；逐项目标与验收链接见[任务总表](#任务)，详细证据见 [UI/UX 验证报告](./validation/ui-ux-verification.md)、[发布后轮次记录](./validation/stage-d.md#发布后轮次记录t040t051)、[阶段 E 归档](./validation/stage-e.md) 与 [阶段 F 归档](./validation/stage-f.md)。
- 平台：darwin/arm64 完成运行时验证（全量测试、发布二进制冒烟与浏览器验证）；macOS amd64、Linux amd64、Windows amd64 仅交叉编译通过，运行、生命周期与隔离未验证。
- 宿主（DSH）：macOS arm64 上 7 个宿主的 Hook 已完成配置，其中 5 个完成会话级验证；`install` 探测到的宿主版本不代表 Hook 已注入或被宿主采纳。`memctl-dsh` 在 DSH 0.2.1-rc.2 的隔离 profile 与本机 `desktop` 会话验证了 `systemPrompt.context` 注入、`turn/end` 后同步、轮次内受管写入（提交 `61fd68e`）、恢复、锁忙（3.1 秒内返回 `BUSY`）与断网降级、插件卸载兜底 `end`、绝对 tarball 界面安装；本机 `desktop` profile 已通过 DSH 界面 npm 包名安装升级到 `0.5.1`，bundle 与依赖一致，`install --status` 返回 `configured host=dsh@0.2.1-rc.2 plugin@0.5.1`；`agent/disposed` 因宿主没有单会话销毁入口而由插件单元测试覆盖；会话启动失败降级仅覆盖模型凭据缺失。证据见 [T052 验证记录](./validation/t052-dsh-real-session.md)、[T067 记录](./validation/stage-f.md#t067复验-dsh-界面-npm-安装与-desktop-插件升级) 与 [dsh 专用插件](./07-dsh-plugin.md#13-实现状态)。
- 宿主（pi）：`memctl-pi` 在 pi `1.3.0`/darwin arm64 标记为 `verified`，真实会话确认 `<memctl>` 注入、每轮 `sync`、收尾、恢复、分叉、后台子代理与失败降级；前台 in-process 子代理不触发会话事件，不注入记忆。证据见 [T062 验证记录](./validation/t062-pi-real-session.md) 与 [pi 专用插件](./08-pi-plugin.md#11-实现状态)。
- 未验证：
  - 平台与进程：macOS amd64、Linux amd64、Windows amd64 的运行、生命周期与隔离；Windows/Linux 进程清理。
  - DSH：`agent/disposed` 真实事件路径；绝对目录的界面安装。
  - pi：前台子代理记忆注入；`agent_settled` 在 `pi -p` 立即退出时由 `end` 兜底；`sections` 缺失的宿主级回退仅由单元测试覆盖。
  - 其他宿主：Codex `SessionEnd` 与 OpenCode `session.deleted` 未在真实会话结束路径复验；Codex 模型回复受账号额度限制；Command Code 没有 `SessionEnd`，Antigravity 的 `Stop` 语义待真实宿主确认。
  - Dreaming 与自更新：真实 runner 与模型端点（`claude` 在隔离 scratch HOME 下退出 `1`）；非 macOS 平台的 runner 隔离；`update` 仅在 darwin/arm64 真实执行。
- 已知限制：pi 前台 in-process 子代理不触发会话生命周期，不注入记忆也不单独同步；`pi -p` 的短生命周期只保证 `end` 最终同步；阶段 C 的 `P3-6`、`P3-7`、`P3-13`；阶段 D 的 `P3-11`–`P3-16` 与 `P3-1`、`P3-18` 剩余部分；组件级 DOM 测试缺失；知识图谱密集同名孤立节点簇的标签拥挤；来源 URI 使用浏览器原生标题提示；`memctl-dsh` 没有独立 GitHub 仓库或 Discover 索引；`workspace-write` 会话内 Agent 无法直接执行 `memctl read/write`，记忆更新需经 MCP 通道（`T071`–`T073`）或放宽会话沙箱；Codex 需 `default_tools_approval_mode = "approve"`，pi 需 `--exposure direct`，Grok 的 `workspace` profile 会阻断 MCP 子进程并需要 `read_write`，pi 本身没有文件沙箱。
- 发布入口：唯一入口是推送 `v*` 标签，Release 附件只由流水线产生；本地 `make release` 只用于验证构建链路，不构成发布步骤。见 [发布入口](./04-ui-and-cli-specification.md#61-发布入口)。
- 构建基线：Node v26.10.0 / npm 11.19.1；`web/dist`、`bin/`、`dist/` 不提交，Go 构建与测试前先执行 `make ui`。本机 `~/.agent-memory/bin/memctl` 已升级到 `v0.6.0`。CI 的 `verify` 分片作业与 `build` 作业并行，分别运行 `scripts/test-shard.sh` 与 `make release-dist`，`publish` 依赖两者；集成测试在同一进程内共享一次二进制构建。

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
| T052 | 已完成 | 对应阶段 E 的 `E-5`：真实 DSH 会话验证；已确认注入、成功轮次 `turn/end` 后同步、轮次内受管写入提交 `61fd68e`、恢复注入、子代理跳过、错误轮次同步、正常退出兜底、锁忙/断网降级与 tarball 界面安装；`agent/disposed` 受宿主限制，由单元测试覆盖；[验证记录](./validation/t052-dsh-real-session.md) |
| T053 | 已完成 | 发布 `v0.2.0`（含 `T048`）：插件版本同步到 `0.2.0`，全量测试、四平台候选包、插件 tarball、校验和与 CI Release；[归档记录](./validation/stage-e.md#t053发布-v020) |
| T054 | 已完成 | 统一宿主会话结束映射：补 Codex `SessionEnd`/`PreCompact`、OpenCode `session.deleted`/`session.compacted` 与 DSH 插件卸载时对仍 live 会话的 `end` 兜底；补回归与契约；[归档记录](./validation/stage-e.md#t054宿主会话结束映射与-dsh-收尾兜底) |
| T055 | 已完成 | 发布 `v0.2.1`（含 `T054`）：插件版本提升到 `0.2.1`，全量测试、四平台候选包、插件 tarball、校验和与 CI Release；[归档记录](./validation/stage-e.md#t055发布-v021) |
| T056 | 已完成 | 新增 CLI `update` 命令：查询 GitHub Releases 最新版本，按平台下载并校验 SHA-256 后原子替换当前二进制，`--check` 只报告；[归档记录](./validation/stage-e.md#t056新增-update-命令) |
| T057 | 已完成 | 发布 `v0.3.0`（含 `T056`）：插件版本同步到 `0.3.0`，全量测试、四平台候选包、插件 tarball、校验和与 CI Release；[归档记录](./validation/stage-e.md#t057发布-v030) |
| T058 | 已完成 | 面向 Agent 的 CLI 帮助与中英 i18n：`help <command>`、`<command> --help`、`help --json`、`--lang`/`MEMCTL_LANG`/locale 选择；补 `R06`；[归档记录](./validation/stage-e.md#t058面向-agent-的-cli-帮助与中英-i18n) |
| T059 | 已完成 | 发布 `v0.4.0`（含 `T058`）：插件版本同步到 `0.4.0`，全量测试、四平台候选包、插件 tarball、校验和与 CI Release；[归档记录](./validation/stage-e.md#t059发布-v040) |
| T060 | 已完成 | 对应阶段 F 的 `F-1` 至 `F-4`：`memctl-pi` 扩展、`memctl pi plugin-dir`、pi 原生与旧路径探测、版本门槛、安装提示、`make pi-plugin-*` 与 Release 审计产物；[归档记录](./validation/stage-f.md#t060实现-pi-专用插件) |
| T061 | 已完成 | 发布 `v0.5.0`（含 `T060`）：插件版本同步到 `0.5.0`，全量测试、四平台候选包、两个插件 tarball、校验和与 CI Release；[归档记录](./validation/stage-f.md#t061发布-v050) |
| T062 | 已完成 | 对应 `F-5`：本机 pi 迁移到 `memctl-pi`，真实验证注入、每轮同步、收尾、恢复、分叉、后台子代理与失败降级；前台子代理不注入登记为宿主限制；[归档记录](./validation/stage-f.md#t062真实-pi-会话验证) |
| T063 | 已完成 | 发布 `memctl-pi@0.5.1` 到 npm 官方仓库：MIT 许可证、npm 元数据、staged 审批与隔离安装验证；[归档记录](./validation/stage-f.md#t063发布-memctl-pi-到-npm) |
| T064 | 已完成 | 发布 `memctl-dsh@0.5.1` 到 npm 与 `v0.5.1` Release：MAC 二进制、两个插件 tarball、MIT 与 npm 元数据、隔离安装与适配器探测通过；[归档记录](./validation/stage-f.md#t064发布-memctl-dsh-到-npm-与-v051-release) |
| T065 | 已完成 | 删除 npm 上 `memctl-pi@0.0.0-stage` 占位版本；registry 保留 `0.5.0`、`0.5.1`，`latest` 为 `0.5.1`；[归档记录](./validation/stage-f.md#t065删除-npm-占位版本) |
| T066 | 已完成 | 文档一致性检查与整理：修正发布状态、`F-5` 状态、Makefile 与发布产物描述、DSH/pi 状态段和失效链接，补全验证记录索引；不改变产品行为；[归档记录](./validation/stage-f.md#t066文档一致性检查与整理) |
| T067 | 已完成 | 复验 DSH 界面 npm 包名安装：确认 `desktop` profile 已升级到 `memctl-dsh@0.5.1`、bundle 与依赖一致，`install --status` 为 `configured plugin@0.5.1`；更新界面安装与 desktop 版本的未验证记录；不改变产品行为；[归档记录](./validation/stage-f.md#t067复验-dsh-界面-npm-安装与-desktop-插件升级) |
| T068 | 已完成 | 按 `tech-doc-style-chinese` 检查并优化中文帮助：统一术语与引号、把命令选项与路径改为行内代码、修正与契约不符的环境变量默认值和示例描述；不改变命令行为；[归档记录](./validation/stage-f.md#t068中文帮助检查与优化) |
| T069 | 已完成 | 发布 `v0.5.2`（含 `T068`）：全量测试、四平台候选包、两个插件 tarball、校验和与 CI Release；插件版本保持 `0.5.1`；[归档记录](./validation/stage-f.md#t069发布-v052) |
| T070 | 已完成 | 调研受沙箱宿主的记忆写入通道并产出方案：确认插件在沙箱外、bash 在工作区内；实机验证 DSH 的 MCP stdio 子进程可写 `~/memory`；比较四条路线并选定 `memctl mcp`；[方案文档](./09-mcp-memory-tools.md)、[归档记录](./validation/stage-f.md#t070受沙箱宿主的记忆写入通道方案) |
| T071 | 已完成 | 实现 `memctl mcp` 子命令与 `memory_read`/`memory_write` 工具、协议处理与错误映射、帮助条目与 `M01`–`M12` 测试；[方案文档](./09-mcp-memory-tools.md#6-验收)、[归档记录](./validation/stage-f.md#t071实现-memctl-mcp-子命令) |
| T072 | 已完成 | 实现 `install --status` 的 MCP 能力探测（`M09`）与 8 个宿主的注册命令输出；6 个有 `mcp add` 的宿主已逐个注册并确认连接，Codex 受限会话完成受管写入；[方案文档](./09-mcp-memory-tools.md#52-各宿主的注册命令)、[归档记录](./validation/stage-f.md#t072mcp-探测与宿主侧实测) |
| T073 | 已完成 | 其余宿主的受限会话实测：pi、OpenCode、Command Code、Antigravity 完成 MCP 受管写入且 shell 受限；Grok 的 `workspace` profile 会阻断子进程，需 `read_write`；Claude Code 因代理未运行未实测；[方案文档](./09-mcp-memory-tools.md#62-真实验证边界)、[归档记录](./validation/stage-f.md#t073其余宿主的受限会话实测) |
| T074 | 已完成 | 构建链路提速：集成测试共享单次二进制构建、集成测试按名分片为四个并行 CI 作业、打包与测试并行、新增 `make release-dist`；修复 MCP 探测 3 秒超时在负载下误报的缺陷；[构建契约](./04-ui-and-cli-specification.md#6-单文件打包)、[归档记录](./validation/stage-f.md#t074构建链路提速) |
| T075 | 已完成 | 发布 `v0.6.0`（含 `T070`–`T074`）：全量测试、四平台候选包、两个插件 tarball、校验和与 CI Release；插件源码未改动，保持 `0.5.1`，不发 npm；[归档记录](./validation/stage-f.md#t075发布-v060) |
| T076 | 已完成 | 明确发布入口唯一为 tag 推送：`04` 新增 6.1 节说明本地 `make release` 不构成发布步骤，`05` 第 4 节同步判定依据，README 发布段改为「推标签 → 流水线产出 → 独立目录校验」；不改变产品行为；[发布入口](./04-ui-and-cli-specification.md#61-发布入口)、[归档记录](./validation/stage-f.md#t076明确发布入口为标签推送) |
| T077 | 已完成 | 文档一致性检查与更新：补入架构文档缺失的 `internal/mcp`、`internal/update` 与 `docs/09`，修正 README 与 `09` 过期的 MCP 宿主验证状态，统一插件 tarball 版本语义与 Makefile 片段，归档 `T076`；不改变产品行为；[归档记录](./validation/stage-f.md#t077文档一致性检查与更新) |
| T078 | 已完成 | 新增配套 Agent Skill `skills/memctl/`：读取与写入时机、按基线受管写入、同步与冲突恢复、沙箱宿主改用 MCP；补 README、目录规划与 skills CLI 在线安装；不改变产品行为；[Skill](../skills/memctl/SKILL.md)、[完成记录](#t078配套-agent-skill) |

任务 ID 不代表执行顺序。阶段 A–D 的全部交付任务（`A-1`–`A-6`、`B-1`–`B-3`、`C-1`–`C-3`、`D-1`–`D-2`）与 `T040`–`T051`、`T053`、`T054`、`T055` 均已完成；阶段 E 的 `E-1`–`E-4` 由 `T048` 交付，`E-5` 由 `T052` 完成；`T056` 为已完成的发布后能力补充；`T057` 为已完成的发布轮次；`T058` 为已完成的 Agent 帮助与 i18n 轮次；`T059` 为已完成的 `v0.4.0` 发布轮次；`T060` 为已完成的 pi 插件实现轮次；`T061` 为已完成的 `v0.5.0` 发布轮次；`T062` 完成 `F-5` 真实宿主验证；`T063` 为已完成的 `memctl-pi` npm 发布轮次；`T064` 为已完成的 `memctl-dsh` npm 与 `v0.5.1` Release 轮次；`T065` 删除 npm 占位版本；`T066` 完成文档一致性检查与整理；`T067` 复验 DSH 界面 npm 安装与 `desktop` 插件升级；`T068` 完成中文帮助的术语、排版与契约一致性优化；`T069` 为已完成的 `v0.5.2` 发布轮次；`T070` 完成受沙箱宿主写入通道的调研与方案；`T071` 实现 `memctl mcp` 子命令与记忆工具；`T072` 完成 `install --status` 的 MCP 探测与宿主侧注册实测；`T073` 完成其余宿主的受限会话写入实测；`T074` 完成构建链路提速；`T075` 为已完成的 `v0.6.0` 发布轮次；`T076` 明确发布入口唯一为标签推送；`T077` 完成文档一致性检查与更新；`T078` 新增配套 Agent Skill。阶段 F 的 `F-1`–`F-5` 均已交付，`memctl-pi` 与 `memctl-dsh` 均已在 npm 发布 `0.5.1`。完成记录与验收证据按任务总表的链接指向 `docs/validation/` 或本文件的最近一轮记录。

完整阶段顺序（[阶段 A](./05-validation-and-delivery.md#阶段-a-的实施顺序)、[阶段 B](./05-validation-and-delivery.md#阶段-b-的实施顺序)、[阶段 C](./05-validation-and-delivery.md#阶段-c-的实施顺序)、[阶段 D](./05-validation-and-delivery.md#阶段-d-的实施顺序)、[阶段 E](./05-validation-and-delivery.md#阶段-e-的实施顺序)）、交付边界、子任务切分点和验收映射的权威定义见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)，本表只维护执行状态。`A-2` 拆为 `T006`（初始化与读取）和 `T008`（格式解析），`A-5` 拆为 `T018`（后台同步派生与进程生命周期）和 `T019`（Hook 协议、会话身份与宿主性能实测），子任务之间范围不重叠。

## T078：配套 Agent Skill

- 目标与范围：为使用 memctl 的 Agent 提供 `skills/memctl/`，覆盖读取与写入时机、按基线受管写入、同步与冲突恢复、沙箱宿主改用 MCP 工具；通过 `skills` CLI 从 GitHub 在线安装到 `~/.agents/skills`，不改变产品行为。
- 契约：[CLI 命令](./04-ui-and-cli-specification.md#1-cli-命令)、[MCP 记忆工具](./09-mcp-memory-tools.md#4-系统如何工作)、[目录结构与依赖](./01-architecture-and-modules.md#5-目录结构与依赖)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1。
- 结构：`SKILL.md` 的 frontmatter 只有 `name` 与 `description`，name 为 `memctl`；正文按「确认环境 → 判断读写 → 受管写入 → 同步与冲突 → MCP 降级 → 边界」组织，每步给出完成标准；`references/memory-conventions.md` 与 `references/recovery.md` 由入口按需链接。
- 事实核对：Skill 中的 `read`、`write`、`sync`、`resolve`、`recover`、`mcp`、`install` 命令与参数逐一对照当前提交构建的 `help --json`；错误码取自 `internal/memerr` 并对照 `02` 4.3；冲突解决 JSON 字段对照 `app.ConflictView` 的 `baseRevision`、`conflictId` 与 `files`。
- 验收：三个 Skill 文件存在，frontmatter 可解析且无额外字段，入口链接可解析；`README` 与 `01` 第 5 节已登记 `skills/memctl/`；已用 `skills add gray0128/memctl -g -s memctl -a codex pi opencode claude-code -y` 在线安装到 `~/.agents/skills/memctl`，`~/.agents/.skill-lock.json` 记录来源 `gray0128/memctl`，`skills list -g --json` 可见，宿主技能目录识别出 `memctl` 并通过 `skill` 工具加载；全量 Markdown 链接与锚点检查无失效项；`git diff --check` 通过。
- 未验证与遗留：真实任务中的自动触发与执行未验证。
- 下一步：需要时分发到宿主 skills 目录，并用真实会话验证触发与执行。

## 近期待办与下一步

- `memctl mcp` 与 `install --status` 的 `mcp:` 块已随 `v0.6.0` 发布；本机 `desktop` profile 尚未插入 `@deepseek-ai/dsh-mcp-client` 条目，DSH 侧需手工添加后生效。
- 插件包仍为 `0.5.1`，与 `v0.6.0` 的主次版本不一致；插件源码未改动，如需对齐需单独走插件版本与 npm 发布轮次。
- 宿主侧写入已覆盖 DSH、Codex、pi、OpenCode、Command Code 与 Antigravity；Grok 需显式 `read_write`，Claude Code 因代理未运行未实测，见 [MCP 记忆工具](./09-mcp-memory-tools.md#62-真实验证边界)。
- DSH 界面 npm 包名安装与 `desktop` profile `0.5.1` 已完成；绝对目录界面安装仍未验证。
- 可选事项：独立 GitHub 仓库与 Discover 索引（`D2`、`D3`）仍为可选，不阻塞发布。
- 配套 Skill 已通过 `skills` CLI 从 `gray0128/memctl` 安装到 `~/.agents/skills/memctl`，lockfile 记录来源，宿主技能目录可识别并加载；真实任务中的自动触发与执行尚未验证。
- 构建提速的后续项：按实际 CI 耗时调整分片数；消除 `a4_credentials_test.go` 等测试的进程级环境依赖以便 `t.Parallel()`；新增或重命名集成测试后重新生成 `scripts/test-shards.txt`。
- 未验证项与已知限制见[当前事实](#当前事实)；界面重设计的视觉结论以 [第二轮 UI/UX 验证](./validation/ui-ux-verification.md#5-第二轮验证v011-发布后复验) 与 [收口复验](./validation/ui-ux-verification.md#6-第二轮问题的收口与复验t044) 为准。
