# 阶段 C 实现审查报告

本报告记录阶段 C（`C-1` 至 `C-3`，对应任务 `T028`–`T033`）源码、测试与前端构建的全面审查结果。审查以 [方案索引](../README.md) 下的 01–06 规范为唯一权威契约，阶段 A 与阶段 B 的历史验收分别见 [阶段 A 归档](./stage-a.md) 与 [阶段 B 归档](./stage-b.md)，两阶段的实现审查与收口见 [阶段 A 实现审查报告](./stage-a-review.md) 与 [阶段 B 实现审查报告](./stage-b-review.md)，当前进度见 [开发状态](../STATUS.md)。

## 审查结论与范围

- 审查范围：`internal/app`（`files.go`、`entries.go`、`doctorfix.go`、`revert.go`、`mutation.go` 的阶段 C 改动）、`internal/vault`（`write.go`、`link.go`、`parse.go` 的阶段 C 改动）、`internal/git`（`revert.go`、`git.go` 的 `MoveForce`）、`internal/server`（`writes.go`、`adapters.go`）、`internal/adapters` 全部源码与 `internal/assets/adapters` 8 份模板、`internal/cli`（`install.go`、`doctor.go`）、`web/src` 阶段 C 新增与改动文件，以及 `tests/integration/c1_*`、`c2_*`、`c3_*` 与各包单元测试。
- 审查边界：阶段 A、B 已由 `T022`、`T027` 评审并收口，本轮不做重复评审；对阶段 C 触碰的阶段 A、B 不变量（锁描述符继承、环境白名单、只读门禁、路径边界、预条件语义、不可信文本渲染）做回归抽查，结论见第 4 节。
- 验证环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1。
- 验证命令：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 与 `cd web && npm test` 全部通过（前端 12 个文件 65 项，Go 含 `tests/integration` 全量）；`gofmt -l` 无输出，`git diff --check` 无告警。
- 审查方式：主会话完成全量审查并保留验收责任，同时按 [开发循环规范](../06-development-loop.md#5-验证如何落地) 委派 4 个只读子智能体分别复核后端写入事务、HTTP 写接口、Agent 配置安装与前端；子智能体结论均经主会话以源码证据复核后采纳。
- 总体结论：阶段 C 已完成受管写入/移动改链、冲突解决与审计回滚、adapter 探测与保留语义安装的主链路，单一事务入口、仓库锁复用、预条件到基线的转换、只读拦截、路径边界、HTTP 分层与 diff 转义整体符合设计；但移动的仅大小写分支、前端版本冲突重试、YAML 受管身份与安装互斥存在契约违背。
- 发现问题：共识别 **4 项高优先级问题（P1，破坏契约或数据安全）**、**10 项中优先级问题（P2，行为偏差或恢复语义缺失）** 与 **13 项低优先级问题（P3，边界条件、一致性与测试稳健性）**。其中已在本轮受控修复的问题见第 6 节收口状态。

| 编号 | 级别 | 涉及模块 | 问题摘要 | 关联契约与验收项 |
| --- | --- | --- | --- | --- |
| [P1-1](#p1-1仅大小写重命名在大小写敏感文件系统上覆盖同名既有文件) | P1 | `internal/app`、`internal/vault` | 「仅大小写」重命名只按 `EqualFold` 放行，目标已有另一文件时仍执行 rename，静默覆盖并删除其内容 | [文件边界](../01-architecture-and-modules.md#32-文件边界)、[REST 路由](../02-domain-models-and-contracts.md#42-路由)、`W04`、`P01` |
| [P1-2](#p1-2前端-412-重试沿用旧-entryid必然-404) | P1 | `web/src` | 版本冲突后「用草稿覆盖当前/重试移动/重试删除/重试拆分」仍用旧 `entryId`，新文件修订下必然 `NOT_FOUND` | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[修订标识](../02-domain-models-and-contracts.md#11-修订标识)、`W02`、`A04` |
| [P1-3](#p1-3前端冲突重试失败后错误被静默丢弃) | P1 | `web/src` | 412 分支先清空编辑状态，重试失败时无状态可写，操作者看不到任何反馈 | [错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui) |
| [P1-4](#p1-4yaml-受管身份未按-agent-memory-前缀识别且不剥离行尾注释) | P1 | `internal/adapters` | YAML 受管判定用模板 id 精确集合且 id 不剥离行尾注释，用户加注释即产生重复 id，宿主校验失败后跳过整个 hooks 文件 | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、`I01` |
| [P2-1](#p2-1revert-恢复失败仍删除所有权记录) | P2 | `internal/app` | `revert` 的 `defer DeleteOperation` 在 `RECOVERY_REQUIRED` 分支仍清除所有权记录，现场对 memctl 变为「用户自己启动的操作」 | [Revert](../03-workflows-and-dataflows.md#53-revert)、`C05` |
| [P2-2](#p2-2条目文本含换行破坏单行-bullet且-entryat-失败丢弃已提交结果) | P2 | `internal/app` | `factText`/`heading`/`sources` 含换行会写出多行条目并错位元数据；失败时返回 404 但事务已提交 | [格式保真](../02-domain-models-and-contracts.md#12-格式保真)、[REST 契约](../02-domain-models-and-contracts.md#42-路由) |
| [P2-3](#p2-3仅大小写重命名的中间名残留污染受管文件并阻塞后续写入) | P2 | `internal/vault`、`internal/app` | 两次 rename 之间中断留下的 `.*.memctl-move` 被当作受管记忆文件，`recover` 不清理，后续写入按 `EXTERNAL_CHANGES` 拒绝 | [写入不变量](../01-architecture-and-modules.md#23-写入不变量)、[中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复)、`W04` |
| [P2-4](#p2-4并发安装无安装级互斥recover-会回滚另一进程正在写入的配置) | P2 | `internal/server`、`internal/adapters`、`internal/cli` | 安装请求无互斥且共用单一 `install.json`，并发 `Recover` 会还原对方已写入文件并删除日志 | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、`I01`、`I02` |
| [P2-5](#p2-5cli-人工补丁输出被空格过滤吞掉) | P2 | `internal/cli` | `install --dry-run` 用 diff 过滤规则渲染人工补丁，JSON/YAML 片段绝大多数行以空格开头而被丢弃 | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、`I02` |
| [P2-6](#p2-6home-缺失时路径模板展开为文件系统根下的绝对路径) | P2 | `internal/adapters` | `DefaultEnv` 丢弃 `os.UserHomeDir` 错误，`$HOME/...` 展开为 `/.claude/...` 等根路径，以 root 运行会真实写入 | [文件边界](../01-architecture-and-modules.md#32-文件边界)、[环境变量](../04-ui-and-cli-specification.md#2-环境变量与会话身份) |
| [P2-7](#p2-7grok-目标路径与采纳来源不一致) | P2 | `internal/adapters` | 注册表写单文件 `~/.grok/hooks.json`，项目采纳来源写目录形态 `~/.grok/hooks/*.json` | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、`R01` |
| [P2-8](#p2-8sse-失效刷新静默丢弃未提交草稿) | P2 | `web/src` | 每次失效通知重新拉取列表，条目 id 随修订变化，正在编辑的草稿无提示消失；冲突选择与拆分路径同样被重置 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存)、`W02` |
| [P2-9](#p2-9冲突视图在无冲突分支吞掉-notice-与过期提示) | P2 | `web/src` | `resolve`/`abort`/`CONFLICT_STALE` 后进入「无冲突」早退分支，`notice` 与 `remoteState` 永不显示，`stale` 也不刷新快照 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、`C02`、`C04` |
| [P2-10](#p2-10拆分默认目标已存在时语义变为追加且界面无提示) | P2 | `web/src` | 建议路径由 fact slug 生成，命中既有文件时 `PATCH` 实际追加，界面仍称「拆分新文件」 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui) |
| [P3-1](#p3-1isancestor-对无共同祖先历史返回-git_failed-而非-not_found) | P3 | `internal/git` | 用 merge-base 结果比较，无共同祖先时 Git 非零退出被映射为 `GIT_FAILED`/500 | [Revert](../03-workflows-and-dataflows.md#53-revert)、`C05` |
| [P3-2](#p3-2请求体只解析第一个-json-值尾部字节被忽略) | P3 | `internal/server` | 两处 body 解码都不校验尾部，第二个 JSON 值或超限尾字节被静默忽略 | [REST 契约](../02-domain-models-and-contracts.md#41-公共规则)、`P03` |
| [P3-3](#p3-3http-层重复实现应用层-428-判定) | P3 | `internal/server` | 前置条件判定在 HTTP 与应用层各有一份，同一错误两处维护 | [模块职责](../01-architecture-and-modules.md#4-模块职责)、`A04` |
| [P3-4](#p3-4doctorfix-的-baserevision-可选与契约必填不一致) | P3 | `internal/app`、`internal/cli` | 契约 §4.2 写 `baseRevision` 必填，实现与 CLI 均可省略，修复提交未绑定客户端看到的基线 | [REST 契约](../02-domain-models-and-contracts.md#42-路由)、`A04` |
| [P3-5](#p3-5安装恢复完全信任-journal-的-path构成任意路径写入原语) | P3 | `internal/adapters` | `Recover` 只比对内容修订，不校验 `path` 是否属于该 adapter 的注册目标 | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[仓库身份](../01-architecture-and-modules.md#31-仓库身份) |
| [P3-6](#p3-6toml-已有键检测只覆盖裸键) | P3 | `internal/adapters` | 引号键 `"hooks"` 或子表 `[features.hooks]` 不被识别，可能插入第二个 `hooks` 定义 | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、`I02` |
| [P3-7](#p3-7替换受管条目丢失条目内部的用户注释) | P3 | `internal/adapters` | 受管 YAML/JSON 条目整段替换，条目内部注释随原文被丢弃 | [Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、`I02` |
| [P3-8](#p3-8只读模式下-adapter-预览按钮仍可点击) | P3 | `web/src` | 预览走 POST，只读模式下必然 403，按钮却只按 `busy` 禁用 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、`A03` |
| [P3-9](#p3-9fileeditor-把-no_repo-的-404-误判为可新建文件) | P3 | `web/src` | 仅按 `status === 404` 分支，未初始化仓库被引导为「保存将新建该文件」 | [错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功) |
| [P3-10](#p3-10诊断修复成功后不刷新报告) | P3 | `web/src` | `doctor:fix` 返回的修复后 `report` 被忽略，列表与基线继续停留在修复前 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui)、`R04` |
| [P3-11](#p3-11来源链接缺少协议白名单与-agent-复制能力) | P3 | `web/src` | 未实现「仅安全协议导航、`agent://` 复制」，仅输出纯文本 | [Web UI](../04-ui-and-cli-specification.md#4-web-ui) |
| [P3-12](#p3-12确认按钮缺少自身只读守卫且组件层无测试) | P3 | `web/src` | 删除/拆分确认按钮只靠父按钮间接保证只读；`vite` 测试只含 `.test.js`，全部组件交互无自动化覆盖 | [HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)、[开发循环](../06-development-loop.md#5-验证如何落地) |
| [P3-13](#p3-13post-apiv1sync-返回-202-但同步已完成) | P3 | `internal/server` | 202 表示已受理未完成，实际在请求内完成最长约 30 秒的同步 | [REST 契约](../02-domain-models-and-contracts.md#42-路由) |

---

## 1. 高优先级问题（P1）

### P1-1：仅大小写重命名在大小写敏感文件系统上覆盖同名既有文件

- **位置**：[`internal/app/files.go`](../../internal/app/files.go)（`requireAbsentDestination`）、[`internal/vault/write.go`](../../internal/vault/write.go)（`RenameFile` 的 `caseOnly` 分支）、[`internal/app/mutation.go`](../../internal/app/mutation.go)（`records`、`restoreSelected`）。
- **关联契约**：[REST 路由](../02-domain-models-and-contracts.md#42-路由) 规定「仅大小写不同的重命名经事务内中间路径完成，**拒绝覆盖已有目标**」；[写入不变量](../01-architecture-and-modules.md#23-写入不变量) 要求全部前置条件通过后才落盘并记录前镜像。
- **触发与机制**：`requireAbsentDestination` 只要 `strings.EqualFold(from, to)` 就放行；`RenameFile` 判定 `caseOnly` 后跳过「目标已存在」拒绝，执行 `rename(from → .to.memctl-move)`、再 `rename(.to.memctl-move → to)`，第二次 rename 直接替换已有目标。事务记录把目标记为 `Existed:false`，`restoreSelected` 只会删除、不会恢复被覆盖的内容。
- **影响后果**：在大小写敏感文件系统（Linux ext4、大小写敏感 APFS、Windows 逐目录敏感）上，仓库同时存在 `Notes.md` 与 `notes.md` 时，移动 `Notes.md → notes.md` 会静默覆盖并最终删除后者的内容，只能靠 Git 历史找回；契约明确禁止此行为。
- **修复建议**：把「是否允许」从字符串比较改为文件身份判定，仅在目标不存在或 `os.SameFile(fromInfo, toInfo)` 为真时允许；`planMove` 与 `RenameFile` 复用同一份权威判定。同时为事务记录补上中间名，使中断恢复可清理现场。
- **本轮处置**：已修复。新增 `Root.RefuseMoveOverwrite`（`os.SameFile` 判定）供 `planMove` 与 `RenameFile` 共用；`records()` 为仅大小写移动追加中间名记录。

### P1-2：前端 412 重试沿用旧 `entryId`，必然 404

- **位置**：[`web/src/components/EntryBrowser.jsx`](../../web/src/components/EntryBrowser.jsx)（`freshBaselines`、`saveEdit`/`saveMove`/`removeEntry` 的 412 分支）、[`web/src/components/SplitPanel.jsx`](../../web/src/components/SplitPanel.jsx)（412 分支）。
- **关联契约**：[修订标识](../02-domain-models-and-contracts.md#11-修订标识) 规定 `entryId` 绑定 `文件路径 + fileRevision + 行号`，插入一行后允许重新生成；[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 要求版本冲突时保留草稿并显示当前内容与草稿差异。
- **触发与机制**：412 分支的 `freshBaselines` 只重读文件与 snapshot，不重读 `/api/v1/entries`；重试闭包仍捕获外部写入前的 `entry`。用新 `If-Match` 配旧 `entryId` 提交时，`PatchEntry`/`DeleteEntry` 在新修订上按 id 精确查找失败，返回 `NOT_FOUND`。当前内容还用旧行号取值，外部插入/删除行后可能展示另一条条目。
- **影响后果**：「用草稿覆盖当前」「用草稿重试移动」「重试删除」「用草稿重试拆分」在真实版本冲突后无法成功，操作者只能放弃草稿；冲突差异展示本身也可能误导。
- **修复建议**：412 后按文件重读 `GET /api/v1/entries`，按 `rawLine`/`factText` 与行号就近匹配取得新的 `{id, fileRevision, lineNumber}` 再重试；匹配不到时禁用重试并提示刷新后重建草稿。
- **本轮处置**：已修复（见第 6 节）。

### P1-3：前端冲突重试失败后错误被静默丢弃

- **位置**：[`web/src/components/EntryBrowser.jsx`](../../web/src/components/EntryBrowser.jsx)（`saveEdit`/`saveMove` 的重试闭包）、[`web/src/components/SplitPanel.jsx`](../../web/src/components/SplitPanel.jsx)（重试闭包）。
- **关联契约**：[错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功) 要求 409/412 时保留草稿并按具体状态恢复；[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 要求显示失败或过期状态。
- **触发与机制**：412 分支先 `setActive(null)`，重试再次失败时走 `setActive(current => current ? … : current)`，`current` 为 null，错误被丢弃，也没有写入 `actionError`/`notice`。
- **影响后果**：操作者点击重试后界面毫无变化，可无限重复点击；真实的 404/409/422/503 原因不可见，违反「保留草稿并给出可恢复状态」。
- **修复建议**：为冲突面板增加独立的错误状态与渲染通道，重试抛出的任何错误都可见（含 `code`）。
- **本轮处置**：已修复（见第 6 节）。

### P1-4：YAML 受管身份未按 `agent-memory-` 前缀识别，且不剥离行尾注释

- **位置**：[`internal/adapters/mergeyaml.go`](../../internal/adapters/mergeyaml.go)（`yamlItemID`、`unquoteYAMLScalar`、受管判定与重建）。
- **关联契约**：[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装) 规定「YAML 条目以 `id` 前缀 `agent-memory-` 识别」；`I01` 要求重复安装不增加重复项。
- **触发与机制**：受管判定写成 `templateIDs[id]` 精确集合，`id` 取值不剥离行尾注释也不处理锚点/流式写法。用户在受管项首行加注释（`- id: agent-memory-sync # 不要删`）后，解析出的 id 不等于模板 id，条目被保留，模板三条又被追加，文件中出现两个有效 id 相同的条目；反方向，旧版 `agent-memory-*` 条目不会被识别为受管项而长期并存。
- **影响后果**：宿主侧把重复 id 判为校验错误并跳过整个 hooks 文件，用户自己的 hook 与本工具 hook 一起失效，命令仍报 `applied`；旧受管项会重复触发同步。
- **修复建议**：受管判定改为 `id` 前缀匹配；id 在比较前剥离行尾注释；重建时删除全部受管条目再按模板追加；无法解析但提及 `agent-memory-` 的条目转人工补丁而不是继续写。
- **本轮处置**：已修复。`mergeyaml.go` 改为前缀识别、注释剥离，并对未解析到 id 却提及受管前缀的条目返回人工补丁。

---

## 2. 中优先级问题（P2）

### P2-1：`revert` 恢复失败仍删除所有权记录

- **位置**：[`internal/app/revert.go`](../../internal/app/revert.go)（`defer session.store.DeleteOperation()` 与三个 `RECOVERY_REQUIRED` 返回分支）。
- **关联契约**：[Revert](../03-workflows-and-dataflows.md#53-revert) 规定「中止或恢复检查失败时保留所有权记录并进入恢复态」。
- **触发与机制**：`git revert` 失败后，`git revert --abort` 失败、HEAD 未回到原提交或工作树仍脏时返回 `RECOVERY_REQUIRED`，但注册的 `defer` 仍无条件删除 `operation.json`。
- **影响后果**：`originalHead`/`targetHead`/`operationID` 丢失，残留的 `REVERT_HEAD` 与未合并索引被当作「用户自己启动的 revert」，`doctor` 无法解释现场，`resolve --abort` 也无法接管，只剩人工处理。
- **修复建议**：只在提交已验证成功、或冲突已 abort 且 HEAD/工作树核对通过后清除记录；`RECOVERY_REQUIRED` 与无法证明恢复的路径保留记录。
- **本轮处置**：已修复。移除无条件 `defer`，在已证实的恢复点显式 `DeleteOperation`，并新增可注入的 `revert-abort` 故障点用于回归。

### P2-2：条目文本含换行破坏单行 bullet，且 `entryAt` 失败丢弃已提交结果

- **位置**：[`internal/app/entries.go`](../../internal/app/entries.go)（`CreateEntry`、`PatchEntry`、`buildEntryLine`、`entryAt`）。
- **关联契约**：[格式保真](../02-domain-models-and-contracts.md#12-格式保真) 规定条目为单行 bullet、元数据保真；[REST 契约](../02-domain-models-and-contracts.md#42-路由) 规定返回 `{entry, result}`；§4.3 禁止在已形成本地提交时只报「失败」。
- **触发与机制**：`factText`/`sources`/`heading` 只做 `TrimSpace`，含 `\n` 时直接拼进单行 bullet；行号用 `strings.Count` 或附加行号计算，指向被拆出的非 bullet 行，`entryAt` 返回 `NOT_FOUND`，但事务已提交且响应体只有 404。UI 条目编辑框回车即可触发。
- **影响后果**：客户端把 404 当作「未写入」重试会重复插入条目；PATCH 场景静默产生结构损坏的条目行与错位元数据，响应内容不等于写入内容。
- **修复建议**：在 `CreateEntry`/`PatchEntry` 入口拒绝含 `\r`/`\n` 的 `factText`、`sources`、`heading`；`entryAt` 失败路径保留已提交的 `MutationResult`。
- **本轮处置**：已修复。新增 `requireSingleLine` 前置校验，`entryAt` 失败时返回已提交的 `result`。

### P2-3：仅大小写重命名的中间名残留污染受管文件并阻塞后续写入

- **位置**：[`internal/vault/write.go`](../../internal/vault/write.go)（`ListManaged`、`RenameFile`、`memctlMoveSuffix`）、[`internal/app/mutation.go`](../../internal/app/mutation.go)（`restoreSelected`）、[`internal/app/recover.go`](../../internal/app/recover.go)（`describe`、`RecoverRollback`）。
- **关联契约**：[写入不变量](../01-architecture-and-modules.md#23-写入不变量) 与 [中断与恢复](../03-workflows-and-dataflows.md#21-中断与恢复) 要求中断可识别并恢复、恢复后全部前镜像一致。
- **触发与机制**：两次 `Rename` 之间被强杀或回退 rename 也失败时，留下 `.notes.md.memctl-move`。事务记录只含 `from`/`to`，`RecoverRollback` 只重写 `from` 前镜像并删除记录，中间产物原样保留；`ListManaged` 只跳过 `.memctl-tmp`，把该中间产物当作受管记忆文件。
- **影响后果**：恢复后工作树不干净，之后所有受管写入与同步按 `EXTERNAL_CHANGES` 拒绝；中间产物进入 snapshot、修订摘要与诊断，并可能被 `sync --capture` 提交进历史；下一次同文件仅大小写重命名返回 `BUSY`，而 `recover` 已无事务可报。
- **修复建议**：`ListManaged` 同时跳过 `.*.memctl-move`；事务记录补上中间名，使回滚能删除残留。
- **本轮处置**：已修复。`ListManaged` 跳过移动中间名；`records()` 为仅大小写移动追加中间名，`describe` 将其判为 `planned` 并在恢复时删除。

### P2-4：并发安装无安装级互斥，`Recover` 会回滚另一进程正在写入的配置

- **位置**：[`internal/server/adapters.go`](../../internal/server/adapters.go)（真实安装分支）、[`internal/cli/install.go`](../../internal/cli/install.go)（`Recover`）、[`internal/adapters/journal.go`](../../internal/adapters/journal.go)（单一固定 `install.json`、`Recover`）、[`internal/adapters/install.go`](../../internal/adapters/install.go)（仅单文件级锁）。
- **关联契约**：[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装) 规定每个配置路径独立锁、单一 `install.json` 与中断恢复语义；HTTP 接入复用同一实现，不重复实现锁。
- **触发与机制**：每个请求独立 goroutine，安装路由无安装级锁；两个并发安装都会先 `Recover()`，B 读到 A 正在维护的同一 journal，把 A 刚写入且仍等于安装后修订的文件还原并删除日志（A 继续写、结果与磁盘不一致），两个 agent 并发时还会互相覆盖同一 journal。
- **影响后果**：响应报 `applied` 而磁盘是安装前内容；并发安装静默回滚对方受管条目；真实崩溃时的恢复依据被对方删除。
- **修复建议**：以状态根下 `adapters/install.lock` 为粒度增加安装级锁，覆盖「恢复 + 计划 + 基线校验 + 全部写入」，CLI 与 HTTP 复用同一把锁。
- **本轮处置**：已修复。新增 `Env.AcquireInstallLock`，HTTP 真实安装与 CLI 非 dry-run 安装均持有该锁完成恢复与写入。

### P2-5：CLI 人工补丁输出被空格过滤吞掉

- **位置**：[`internal/cli/install.go`](../../internal/cli/install.go)（`printPlan`）。
- **关联契约**：[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装) 规定无法可靠保留时在计划中给出人工补丁；`I02` 要求保留可恢复现场、提示人工处理。
- **触发与机制**：`printPlan` 对 `artifact.Patch` 统一按 diff 预览过滤（跳过空行与 `line[0]==' '`），而 `ActionManual` 的补丁是原始模板片段，JSON/YAML 绝大多数行以空格开头，被整段丢弃；非 dry-run 只打印 `manual <path>` 一行。
- **影响后果**：`install --dry-run` 在 CLI 这一交付面上拿不到可粘贴的 hooks 片段，契约要求的人工补丁实际不可用（HTTP/UI 的 `patch` 字段不受影响）。
- **修复建议**：按 `artifact.Action` 分支渲染，人工补丁原样输出整段。
- **本轮处置**：已修复。`printPlan` 对 `ActionManual` 原样打印 `patch:` 段落。

### P2-6：HOME 缺失时路径模板展开为文件系统根下的绝对路径

- **位置**：[`internal/adapters/detect.go`](../../internal/adapters/detect.go)（`DefaultEnv`、`expand`）、[`internal/adapters/install.go`](../../internal/adapters/install.go)（`planSpec`）。
- **关联契约**：[文件边界](../01-architecture-and-modules.md#32-文件边界) 与 [环境变量](../04-ui-and-cli-specification.md#2-环境变量与会话身份) 要求配置路径来自可解析的 HOME/状态根；[仓库身份](../01-architecture-and-modules.md#31-仓库身份) 要求状态根仅当前用户可写。
- **触发与机制**：`os.UserHomeDir` 的错误被丢弃，`Env.Home` 可为空串，`$HOME/...` 展开为 `/.claude/settings.json` 等根路径；`config.Load` 对同一情形会报错，适配器侧没有对应校验。
- **影响后果**：非特权用户得到误导路径与 `EACCES`；以 root 运行时（容器/CI 常见）会真实创建 `/.claude/`、`/.codex/` 等库外绝对路径。
- **修复建议**：`Plan`/`Apply` 入口在 HOME 为空时显式失败，不使用空串展开。
- **本轮处置**：已修复。`planSpec` 在 `env.Home == ""` 时返回 `INVALID_ARGUMENT`。

### P2-7：Grok 目标路径与采纳来源不一致

- **位置**：[`internal/adapters/registry.go`](../../internal/adapters/registry.go)（grok 的 `PathTemplate`）。
- **关联契约**：[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装) 要求 8 个宿主各内嵌版本匹配模板；`R01` 要求模板可用。
- **触发与机制**：注册表把 Grok 目标定为单文件 `$HOME/.grok/hooks.json`，项目采纳来源 [`脚本化实现参考/README.md`](../../脚本化实现参考/README.md) 写目录形态 `~/.grok/hooks/*.json`；同表其余 7 个宿主路径与实现一致。
- **影响后果**：若宿主只读取 `~/.grok/hooks/`，安装会「成功返回 configured」但 Grok 永不加载该 hook（静默不生效），且该风险未登记在遗留清单。
- **修复建议**：与真实宿主文档对齐为目录形态并给出明确文件名，或在文档中显式登记该路径未确认；不要两处并存。
- **本轮处置**：已修复为 `$HOME/.grok/hooks/memctl.json`，并登记为未经真实宿主验证。

### P2-8：SSE 失效刷新静默丢弃未提交草稿

- **位置**：[`web/src/components/EntryBrowser.jsx`](../../web/src/components/EntryBrowser.jsx)（列表 effect 依赖 `refreshKey`、编辑态按 `active.entry.id` 匹配）、[`web/src/components/SplitPanel.jsx`](../../web/src/components/SplitPanel.jsx)（effect 内清空 `active`/`conflict`/`notice`）、[`web/src/components/ConflictView.jsx`](../../web/src/components/ConflictView.jsx)（重新拉取后 `setChoices(initialChoices(...))`）。
- **关联契约**：[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 要求保留未保存草稿；[SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存) 规定事件只通知失效、不作为事实来源。
- **触发与机制**：任何外部写入或重连都会递增 `refreshKey` 并重拉列表，条目 id 随文件修订全变，正在编辑的 `<li>` 因 id 不匹配不再渲染；冲突视图的文本合并选择与拆分路径在一轮失效通知后被重置为默认值。
- **影响后果**：正是 `W02` 要覆盖的 CLI/UI 并发场景下，未保存草稿无提示消失，违反「保留未保存草稿」。
- **修复建议**：草稿打开期间跳过该次列表重拉，或在重拉后按 `rawLine`/`factText` 重新绑定并提示；`ConflictView` 在 `conflictId` 未变时合并而非替换 `choices`。
- **本轮处置**：已修复（见第 6 节）。

### P2-9：冲突视图在「无冲突」分支吞掉 notice 与过期提示

- **位置**：[`web/src/components/ConflictView.jsx`](../../web/src/components/ConflictView.jsx)（`conflict === null` 早退分支与 `notice` 渲染位置）。
- **关联契约**：[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 的冲突解决失败或过期状态列；§4.3 要求返回 `remoteState`。
- **触发与机制**：`resolve` 成功、`abort` 成功、`CONFLICT_STALE` 都 `setConflict(null)`，随后走「当前没有待解决的冲突」早退分支，`notice` 永不渲染；`stale` 分支也不调用 `onChanged`。
- **影响后果**：`remoteState=pending`（推送未成功）在视图完全不可见；`CONFLICT_STALE` 的说明丢失且快照不刷新，`C02`、`C04` 的可恢复状态不可见。
- **修复建议**：把 notice 提升到早退分支之前渲染，`CONFLICT_STALE` 同时刷新快照。
- **本轮处置**：已修复（见第 6 节）。

### P2-10：拆分默认目标已存在时语义变为追加，且界面无提示

- **位置**：[`web/src/splits.js`](../../web/src/splits.js)（`suggestSplitPath`）、[`web/src/components/SplitPanel.jsx`](../../web/src/components/SplitPanel.jsx)。
- **关联契约**：[Web UI](../04-ui-and-cli-specification.md#4-web-ui) 知识图谱行规定「拆分条目为新文件」；[REST 契约](../02-domain-models-and-contracts.md#42-路由) 规定 `moveToFilePath` 目标不存在时创建该文件。
- **触发与机制**：建议路径由 fact slug 生成，不检查目标是否存在；`PATCH` 在目标已存在时把条目追加到该文件，界面仍称「拆分条目为新文件」。
- **影响后果**：操作者以为创建了新文件，实际并入既有主题文件，需事后人工再拆；不丢数据但违背视图表述。
- **修复建议**：用 snapshot/图谱文件集合校验建议路径，命中既有文件时提示「目标已存在，将追加到该文件」或强制改名。
- **本轮处置**：已修复（见第 6 节）。

---

## 3. 低优先级问题（P3）

### P3-1：`IsAncestor` 对无共同祖先历史返回 `GIT_FAILED` 而非 `NOT_FOUND`

- **位置**：[`internal/git/revert.go`](../../internal/git/revert.go)（`IsAncestor`）、[`internal/git/exec.go`](../../internal/git/exec.go)。
- **触发与机制**：`IsAncestor` 用 `MergeBase` 的输出比较；无共同祖先时 `git merge-base` 非零退出且无输出，被映射为 `GIT_FAILED`。现有用例使用 `checkout -b`（与 HEAD 同源），未覆盖真正独立历史。
- **影响后果**：`Revert` 对不可达 hash 返回 500 而非契约要求的 404。
- **修复建议**：改用 `git merge-base --is-ancestor` 的退出码语义（0/1 分别为是/否，其余为错误）。
- **本轮处置**：已修复。`git.Error` 增加 `ExitCode`，`IsAncestor` 按退出码判定。

### P3-2：请求体只解析第一个 JSON 值，尾部字节被忽略

- **位置**：[`internal/server/writes.go`](../../internal/server/writes.go)（`decodeBody`）、[`internal/server/adapters.go`](../../internal/server/adapters.go)（`decodeOptionalBody`）。
- **触发与机制**：`json.Decoder.Decode` 只消费一个 JSON 值，两处都没有二次 `Decode`/EOF 校验；`MaxBytesReader` 只在实际读取超限时报错，位于首个值之后的超限字节永远不被读取。
- **影响后果**：`POST /api/v1/adapters/{agent}/install` 的首个 JSON 值后追加 9 MiB 垃圾返回 200 而非 413，第二个 JSON 文档被静默丢弃；内存仍有界，非高危。
- **修复建议**：解码成功后追加一次 `Decode`，非 `io.EOF` 即拒绝；该次读取同时使超限尾部正确落到 413。
- **本轮处置**：已修复。新增 `requireBodyEOF`，`decodeOptionalBody` 同步处理。

### P3-3：HTTP 层重复实现应用层 428 判定

- **位置**：[`internal/server/writes.go`](../../internal/server/writes.go) 的 PUT/PATCH/DELETE 前置条件分支与应用层同码同文案的判定。
- **触发与机制**：同一前置条件有两份事实源，当前无行为差异，但任一处改文案即漂移，也不符合「`internal/server` 不重复实现应用层职责」。
- **影响后果**：维护期出现「同一错误两套消息」的风险。
- **修复建议**：HTTP 只做头到基线的映射，缺失时交由 app 统一返回 428。
- **本轮处置**：已修复。移除 HTTP 层重复分支。

### P3-4：`doctor:fix` 的 `baseRevision` 可选与契约必填不一致

- **位置**：[`internal/app/doctorfix.go`](../../internal/app/doctorfix.go)、[`internal/cli/doctor.go`](../../internal/cli/doctor.go)。
- **触发与机制**：契约 §4.2 把该路由输入写作 `{issueCodes, baseRevision}`（未标 `?`），实现将 `BaseRevision` 记为可选且 CLI 不传，修复提交未绑定客户端看到的基线。
- **影响后果**：入口文件被提交删除时，不带基线的修复仍会返回 200 并产生新提交；风险限于「未绑定基线的写」。
- **修复建议**：把 `BaseRevision` 变为必填，CLI 传报告 `revision`。
- **本轮处置**：已修复。`DoctorFix` 要求 `BaseRevision`，CLI 先诊断再以 `report.Revision` 修复。

### P3-5：安装恢复完全信任 journal 的 `path`，构成任意路径写入原语

- **位置**：[`internal/adapters/journal.go`](../../internal/adapters/journal.go)（`recoverRecord`）。
- **触发与机制**：恢复只校验「当前内容是否等于安装后修订」，不校验 `path` 是否属于该 agent 的注册目标、也不校验 `Agent`；删除分支只需内容修订相等。
- **影响后果**：任何能写状态根 `adapters/install.json` 的本机进程可借安装路径删除或覆盖任意路径；不构成跨权限提权，但把「只管理稳定标识条目」的组件变成任意路径写入原语。
- **修复建议**：恢复前用注册表在该 agent 下的目标路径集合白名单比对，不匹配的记录标为 `skipped`。
- **本轮处置**：已修复。新增 `allowedArtifactPaths` 白名单校验。

### P3-6：TOML 已有键检测只覆盖裸键

- **位置**：[`internal/adapters/mergetoml.go`](../../internal/adapters/mergetoml.go)。
- **触发与机制**：`[features]` 中同义键 `"hooks" = false` 或子表 `[features.hooks]` 不被正则识别，代码会再插入 `hooks = true`；表体内以 `[` 开头的续行也可能提前截断扫描区间。
- **影响后果**：产出同一键定义两次或与子表冲突的 `config.toml`，TOML 解析器拒绝，Codex 读取配置失败。
- **修复建议**：把「表内出现任何 `hooks` 词元（含引号键、点号键、子表头）」作为人工补丁条件，或插入前严格重解析自检。
- **本轮处置**：遗留。已登记为已知限制，本轮不扩大合并器改写范围。

### P3-7：替换受管条目丢失条目内部的用户注释

- **位置**：[`internal/adapters/mergeyaml.go`](../../internal/adapters/mergeyaml.go)、[`internal/adapters/mergejson.go`](../../internal/adapters/mergejson.go)。
- **触发与机制**：受管条目/元素内容与模板不同时整段替换，条目内部的注释随原文丢弃；这些位置不触发人工补丁分支。
- **影响后果**：升级受管 hook 时用户加在受管块内的说明性注释静默消失，与「保留原有行列、注释」的承诺有落差；不损坏语义且有备份。
- **修复建议**：替换区间内检测到注释时转人工补丁，或在文档中显式声明该限制。
- **本轮处置**：遗留。已登记为已知限制（有备份可恢复）。

### P3-8：只读模式下 adapter 预览按钮仍可点击

- **位置**：[`web/src/components/AdapterView.jsx`](../../web/src/components/AdapterView.jsx)、[`web/src/api.js`](../../web/src/api.js)。
- **触发与机制**：预览走 `POST .../install {dryRun:true}`，只读模式下服务端必然 403；按钮只按 `busy` 禁用。
- **影响后果**：只读会话里预览入口永远失败并显示错误，与「安装入口已禁用」的文案自相矛盾；不构成只读绕过。
- **修复建议**：按钮同时按 `api.readOnly` 禁用。
- **本轮处置**：已修复（见第 6 节）。

### P3-9：`FileEditor` 把 `NO_REPO` 的 404 误判为可新建文件

- **位置**：[`web/src/components/FileEditor.jsx`](../../web/src/components/FileEditor.jsx)。
- **触发与机制**：仅按 `error.status === 404` 分支，而 `NOT_FOUND` 与 `NO_REPO` 都映射为 404；库路径不存在或无入口时被引导为「保存将新建该文件」。
- **影响后果**：操作者被引导去「新建」一个属于未初始化仓库的文件，随后保存失败。
- **修复建议**：按 `error.code === 'NOT_FOUND'` 判定。
- **本轮处置**：已修复（见第 6 节）。

### P3-10：诊断修复成功后不刷新报告

- **位置**：[`web/src/components/DiagnosticsView.jsx`](../../web/src/components/DiagnosticsView.jsx)。
- **触发与机制**：`POST /api/v1/doctor:fix` 返回修复后的 `report`，组件只用 `result.result.commit` 生成提示，`report` 状态仍是修复前对象。
- **影响后果**：修复后列表继续显示已修复问题，再次点击只能得到基线过期提示；断线时窗口被拉长。
- **修复建议**：用响应中的 `report` 更新状态或触发一次刷新。
- **本轮处置**：已修复（见第 6 节）。

### P3-11：来源链接缺少协议白名单与 `agent://` 复制能力

- **位置**：[`web/src/components/EntryBrowser.jsx`](../../web/src/components/EntryBrowser.jsx)（`SourceList`）。
- **触发与机制**：代码库内没有协议判定或链接构造，来源字段既不导航也不提供复制。
- **影响后果**：安全底线（不任意唤起外部程序、按不可信文本渲染）已满足，但 [Web UI](../04-ui-and-cli-specification.md#4-web-ui) 的「仅安全协议导航、`agent://` 默认复制」未实现，且缺少后续复用的白名单函数。
- **修复建议**：新增纯函数 `sourceProtocol`（仅 `https` 导航、`agent://` 复制、其余纯文本），组件按结果渲染。
- **本轮处置**：已修复（见第 6 节）。

### P3-12：确认按钮缺少自身只读守卫且组件层无测试

- **位置**：[`web/src/components/EntryBrowser.jsx`](../../web/src/components/EntryBrowser.jsx)（确认删除）、[`web/src/components/SplitPanel.jsx`](../../web/src/components/SplitPanel.jsx)（确认拆分）、[`web/vite.config.js`](../../web/vite.config.js)。
- **触发与机制**：两处确认按钮只由已禁用的父按钮进入，只读 gating 完全依赖这条间接路径；`include` 仅 `src/**/*.test.js` 且 `environment: 'node'`，仓库内无任何 `*.test.jsx`。
- **影响后果**：只读回归风险与交互分支回归风险只能靠人工发现，P1-2、P1-3、P2-8 正落在这些无覆盖分支上。
- **修复建议**：补 `disabled={api.readOnly}`；扩展测试 include 并为关键分支补组件测试。
- **本轮处置**：部分修复。确认按钮补只读守卫；组件级 DOM 测试未引入（属遗留，见第 6 节）。

### P3-13：`POST /api/v1/sync` 返回 202 但同步已完成

- **位置**：[`internal/server/writes.go`](../../internal/server/writes.go)（`handleSync`）。
- **触发与机制**：先执行完整 `SyncWait`，再返回 202 与最终摘要；202 语义上表示已受理未完成。
- **影响后果**：按「202 → 轮询 snapshot」实现的客户端仍可工作，长同步期间其他写请求按设计得到 503，属协议语义不精确。
- **修复建议**：改为后台任务 + 202，或修订契约措辞为「同步完成返回最终摘要」。
- **本轮处置**：遗留。已在 `T028` 记录，本轮仅登记，不改同步执行模型。

---

## 4. 阶段 A、B 契约回归抽查

- **锁描述符继承**：[`internal/app/mutation.go`](../../internal/app/mutation.go) `beginWrite`/`beginLocked` 继续以 `git.WithInheritedFile` 构造 `session.ctx`；阶段 C 新增的 `PutFile`/`MoveFile`/`Entries`/`DoctorFix`/`Revert` 与 `stagePlans` 均使用该 `ctx`，抽查未发现新的裸 `ctx` Git 调用。
- **环境白名单**：[`internal/git/exec.go`](../../internal/git/exec.go) `childEnv` 仍统一走 `SyncEnv`；新增的 `MoveForce` 通过 `runWithEnv` 只追加 `GIT_LITERAL_PATHSPECS=1`，未绕过白名单。
- **只读门禁**：[`internal/server/server.go`](../../internal/server/server.go) 的安全中间件在路由前拒绝只读模式下的全部非 GET/HEAD；阶段 C 的 9 条写路由与安装接口均被覆盖。
- **路径边界与预条件**：阶段 C 写路由继续走 `vault.ValidateRelPath` 与 `os.Root`；`If-Match`/`If-None-Match`/`baseRevision` 到基线的转换与既有 `A04`/`W01` 语义一致；`audit` 的 `before` 与 `{hash}` 仍受十六进制白名单与 `--`/`--end-of-options` 防护。
- **不可信文本渲染**：[`web/src/diff.js`](../../web/src/diff.js) 经 Diff2Html 转义；阶段 C 新增的条目、来源、冲突三方、adapter 补丁均以文本节点渲染，未发现可执行注入。
- **结论**：抽查项均保持修复后的行为，未发现阶段 C 引入的阶段 A、B 回归。

---

## 5. 已验证达标项

- **受管写入单一入口**：`PutFile`、`MoveFile`、`CreateEntry`、`PatchEntry`、`DeleteEntry`、`DoctorFix`、`Revert` 与冲突解决全部经 `beginWrite`/`beginProtected` + `requireWriteBaseline` + `commit`，未发现绕过仓库锁或基线校验的写入路径；HTTP 层只做请求形状与基线转换。
- **暂存范围与真实副作用校验**：`stagePlans` 用字面 pathspec（`GIT_LITERAL_PATHSPECS=1`）只暂存计划清单，提交前用 `StagedPaths` 集合相等校验，提交后比对 `CommittedPaths`；`MoveForce` 用 `mv -f --`。
- **路径与链接保真**：写入、移动、条目编辑与诊断修复统一 `ValidateRelPath`+`os.Root`，拒绝 `..`、绝对路径、`.git`、盘符、NUL 与硬链接；移动只重写 `ParseFile` 解析到源文件的链接字节区间，代码围栏与行内代码不动。
- **HTTP 安全与限额**：仅绑定环回、`Host`/`Origin` 校验、常量时间 Token 比较、安全响应头与本地 CSP 保持；请求体 8 MiB、单文件 2 MiB、单次变更 8 MiB 与 100 文件等限额超限即拒绝。
- **adapter 安装语义**：JSON/JSONC 与 YAML 的定点合并保留无关键、注释与用户条目，陈旧受管项被替换，二次执行为 no-op（修复后 YAML 以 `agent-memory-` 前缀识别）；配置基线在持锁后重读比对，同目录原子替换，备份位于状态根，恢复只覆盖仍与安装后修订一致的文件，`--status`/`--dry-run` 不执行恢复。
- **状态与来源**：写入后只得到 `configured`，不产生 `verified`；`capabilities` 按注册顺序去重；`deviceLabel` 记录规范化设备来源。
- **不可信文本与 diff 转义**：审计 Diff 经 Diff2Html 转义，冲突三方、条目、来源与补丁以文本节点渲染。
- **构建与测试链路**：`make ui` 先于 Go 构建；`web/dist` 不提交；阶段 C 新增用例把 `doctor:fix` 与安装请求纳入既有 HTTP 隔离 fixture。

---

## 6. 收口状态（T034）

本轮在完成审查后按 [开发循环规范](../06-development-loop.md#2-一轮怎么执行) 第 3–5 步先对齐契约再修复，并为本轮修复补充回归测试。修复任务登记为 `T034`，范围只覆盖本报告识别的问题，不新增产品能力。

| 编号 | 处置 | 证据 |
| --- | --- | --- |
| P1-1 | 已修复 | `vault.RefuseMoveOverwrite`（`os.SameFile`）；`records()` 中间名记录；`internal/app`、`internal/vault` 用例 |
| P1-2 | 已修复 | 412 后按文件重读条目并重新绑定 `entryId`；`edits.js` 匹配函数与前端单测 |
| P1-3 | 已修复 | 冲突面板独立错误通道与渲染 |
| P1-4 | 已修复 | `mergeyaml.go` 前缀识别 + 注释剥离 + 无法解析转人工；`merge_test.go` 新增用例 |
| P2-1 | 已修复 | 移除无条件 `defer DeleteOperation`；`revert-abort` 故障注入用例 |
| P2-2 | 已修复 | `requireSingleLine`；`entryAt` 失败保留 `result`；`entries_test.go` 新增用例 |
| P2-3 | 已修复 | `ListManaged` 跳过 `.memctl-move`；事务记录中间名 |
| P2-4 | 已修复 | `Env.AcquireInstallLock`；HTTP 与 CLI 复用 |
| P2-5 | 已修复 | `printPlan` 分支渲染人工补丁；`internal/cli/install_test.go` |
| P2-6 | 已修复 | `planSpec` 拒绝空 HOME；`install_test.go` 新增用例 |
| P2-7 | 已修复 | grok 路径改为 `$HOME/.grok/hooks/memctl.json`，登记未实测 |
| P2-8 | 已修复 | 草稿打开期间跳过列表重拉并保留选择 |
| P2-9 | 已修复 | notice 提升到早退分支之前；`stale` 触发刷新 |
| P2-10 | 已修复 | 拆分目标存在时提示「追加」并允许改名 |
| P3-1 | 已修复 | `git.Error.ExitCode` + `merge-base --is-ancestor`；`revert_test.go` 新增独立历史用例 |
| P3-2 | 已修复 | `requireBodyEOF`；`c1_write_test.go` 新增尾部 JSON 用例 |
| P3-3 | 已修复 | 删除 HTTP 层重复 428 分支 |
| P3-4 | 已修复 | `DoctorFix` 要求 `BaseRevision`；CLI 传 `report.Revision`；调用方测试同步更新 |
| P3-5 | 已修复 | `allowedArtifactPaths` 白名单；`install_test.go` 新增用例 |
| P3-6 | 遗留 | 已登记，TOML 合并器保持现状 |
| P3-7 | 遗留 | 已登记，替换受管条目仍有注释损失（有备份） |
| P3-8 | 已修复 | 预览按钮按 `api.readOnly` 禁用 |
| P3-9 | 已修复 | 按 `error.code === 'NOT_FOUND'` 判定 |
| P3-10 | 已修复 | 用响应 `report` 更新状态 |
| P3-11 | 已修复 | `sourceProtocol` 白名单与 `agent://` 复制 |
| P3-12 | 部分修复 | 确认按钮补只读守卫；组件级 DOM 测试仍缺（遗留） |
| P3-13 | 遗留 | 已登记，不改同步执行模型 |

遗留项与已知限制统一记录在 `T034` 完成记录与本报告，不把未运行的检查标记为通过。

本轮新增回归覆盖：`vault` 移动目标判定与中间名、`app` 的 `revert` 恢复失败保留所有权与独立历史 `NOT_FOUND`、条目换行拒绝、`git` 退出码、`server` 请求体尾部拒绝、`adapters` 的 YAML 前缀识别 / 空 HOME / 恢复白名单 / 人工补丁输出，以及 `web` 的 412 重试重绑定与来源协议分类。验证日期 2026-10-17，环境 macOS 26.6.2 arm64 / Go 1.27.1 / Git 2.54.0 / Node v26.10.0；执行者为主会话，4 个只读子智能体提供第二视角，结论由主会话复核。

## 7. 收口建议

1. 本轮修复已通过 `go vet ./...`、`go test ./... -count=1`、`make ui`、`make build` 与 `cd web && npm test` 后收口（结果见 [开发状态](../STATUS.md)）。
2. P1-1 的完整复现需要大小写敏感文件系统（Linux CI 或大小写敏感 APFS 卷）；macOS 默认大小写不敏感卷只覆盖「同一文件的仅大小写重命名」路径，该平台差异记入遗留。
3. P3-6、P3-7 与 P3-12 的组件级测试建议在后续阶段与 Dreaming UI 一并补齐；P3-13 的契约措辞在阶段 D 发布工程中统一。
4. `C-3` 完成后已触发阶段边界停止条件；用户确认后按 `D-1` → `D-2` 依赖顺序推进。
