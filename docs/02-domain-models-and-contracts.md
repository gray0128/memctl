# 02. 模型与契约

本文定义领域对象与外部协议。字段表是实现依据，不再以缺少关联类型的 Go Struct 片段代替完整契约。文件访问边界见 [架构文档](./01-architecture-and-modules.md#3-仓库路径与信任边界)。

## 1. 修订标识与格式解析

### 1.1 修订标识

| 标识 | 定义 | 用途 |
| --- | --- | --- |
| `repoId` | 见架构中的仓库身份 | 区分本机仓库状态 |
| `fileRevision` | `sha256:` 加文件原始字节和 Git 文件模式的完整 SHA-256；输入采用长度前缀编码 | 检查单文件读取基线 |
| `absent` | 目标路径不存在的固定字面量 | 新建文件的前置条件 |
| `revision` | 仓库 HEAD、受管路径清单及各文件修订的确定性摘要；无 HEAD 时使用明确空值 | 多文件请求、提案和快照的基线 |
| `entryId` | 文件路径、`fileRevision` 和行号的完整 SHA-256，采用长度前缀编码 | 只定位特定文件修订中的条目 |
| `conflictId` | 随机生成的不可复用快照 ID | 绑定一次冲突阶段 |
| `proposalId` | 随机生成的提案 ID | 查询和确认已生成的整理结果 |

摘要不是授权凭据。`entryId` 不承诺跨编辑稳定；插入一行后允许重新生成 ID。客户端发送条目变更时同时提供基线，不能只凭旧行号定位当前文件。所有校验在取得仓库锁后针对磁盘当前状态执行，不信任 UI 缓存。

快照从同一次受控读取生成。Git 操作或恢复期间返回最近完整快照并标记 `stale: true`；没有完整快照时返回 `BUSY`，不把 rebase 中间树当成当前记忆。

### 1.2 格式保真

解析器识别单行 bullet、可选行尾元数据和根相对 `[[path]]`。未知元数据保留原文和顺序；一行多个 `[source: ...]` 分别保留。没有元数据的 bullet 是合法记忆，普通段落、代码围栏、缩进和未知格式不自动改写。

Markdown 链接省略 `.md`，其他资产保留扩展名；代码围栏和行内代码中的 `[[...]]` 不作为图谱链接。存在同名歧义时报告诊断，不猜测目标。重命名只修改已解析且解析到该目标的链接字节区间。

孤立文件统一定义为从 `MEMORY.md` 出发不可达的受管文件，包括与入口断开的环；不是简单的入度为零。根入口自身不作为孤立文件。

## 2. 领域对象

所有时间使用 RFC 3339、日期使用 `YYYY-MM-DD`。可选字段缺失与空字符串不混用；未知计数使用 `null`，不伪装为零。

### 2.1 记忆快照

| 对象 | 必需字段 | 可选字段及说明 |
| --- | --- | --- |
| `VaultSnapshot` | `repoId`, `repoPath`, `revision`, `stale`, `files`, `graph`, `syncState`, `health`, `hiddenExpired` | `entryPoint` 为根文件路径；`hiddenExpired` 为因 `until` 到期而排除的条目总数；未初始化时使用错误响应 |
| `MemoryFile` | `relPath`, `fileRevision`, `kind`, `sizeBytes`, `updatedAt`, `editable` | `wikiKey`, `title`, `lineCount`, `indexedInRoot`, `entries`, `outgoingLinks`, `backlinks` |
| `MemoryEntry` | `id`, `filePath`, `fileRevision`, `lineNumber`, `rawLine`, `factText`, `sources`, `metadata` | `heading`, `addedDate`, `inlineLinks`；`metadata` 是保序键值数组，不用 map 丢弃重复键 |
| `SourceRef` | `raw`, `scheme` | `device`, `agent`, `sessionId` 仅在合法 `agent://` 来源可解析时提供 |
| `WikiLink` | `sourceFile`, `lineNumber`, `rawTarget`, `exists` | `resolvedPath` 在无歧义时提供 |
| `KnowledgeGraph` | `nodes`, `edges` | 节点包含路径、类型、条目数、是否孤立；边包含起点、目标和是否失效 |

`kind` 为 `entrypoint | markdown | sql | script | other`。非文本文件不提供伪造的 `rawContent`；文本原文由单文件读取接口返回，避免首屏传输全库正文。

### 2.2 同步与恢复

| 对象 | 字段 |
| --- | --- |
| `SyncSummary` | `repoId`, `branch`, `head`, `hasRemote`, `hasUpstream`, `remoteState`, `localState`, `aheadCount`, `behindCount`, `lastAttemptAt`, `lastSuccessAt`, `reasonCode`, `conflictId?`, `operationId?` |
| `OperationState` | `operationId`, `kind`, `owner`, `originalHead`, `targetHead?`, `phase`, `startedAt` |
| `ConflictState` | `conflictId`, `repoId`, `baseRevision`, `originalHead`, `expectedHead`, `upstreamHead`, `replayCommit`, `indexRevision`, `operationId?`, `phase`, `files`, `detectedAt` |
| `ConflictFile` | `path`, `kind`, `base`, `onto`, `replayed` |
| `ConflictSide` | `exists`, `blobId?`, `mode?`, `content?` |
| `MutationResult` | `transactionId`, `revision`, `commit?`, `changedFiles`, `syncState` |

`localState` 为 `clean | external_changes | operation_active | recovery_required`。`remoteState` 为 `local_only | pending | running | synced | conflict | failed`。本地提交成功和远端推送成功是两件事；`synced` 只描述最后一次确认时刻，不表示远端从此不再变化。`aheadCount` 与 `behindCount` 在没有上游时为 `null`，不用 `0` 表示未知。

`OperationState.kind` 为 `mutation | rebase | revert`。`owner` 区分本工具创建的操作与外部操作；单凭 PID 或文件存在不能授权自动 abort。状态恢复时核对 Git 实际状态、原始 HEAD 和目标 HEAD。

冲突三方取自失败时未合并索引的 stage 1、2、3，在 abort 之前采集。rebase 的 `onto` 是远端基线及已重放提交，`replayed` 是当前正在重放的本地提交，不使用容易颠倒的 ours/theirs 或「本地最终 HEAD」替代它们。删除用 `exists: false` 表示，不与空文件混淆。二进制、重命名等不支持的冲突返回类型和人工处理提示，不伪造文本三方。

`phase` 为 `captured | resolving`。`expectedHead` 在 captured 阶段等于中止后恢复的原始 HEAD，在 resolving 阶段等于当前停止点的 HEAD；`baseRevision` 同样绑定该阶段实际工作树。`indexRevision` 对冲突时全部索引项的路径、stage、模式和 blob ID 做确定性摘要：captured 阶段用于重建后比对，resolving 阶段用于继续前比对，避免误用第一轮的基线。

旧式 Hook 摘要可继续输出 `ok | local | cached | failed | busy | norepo`；它只用于上下文头部，不用于替代上述结构化状态。`pending` 对应结构化详情，不能映射成 `ok`。

### 2.3 审计、诊断、整理与接入

| 对象 | 字段与语义 |
| --- | --- |
| `AuditCommit` | `hash`, `parents`, `timestamp`, `subject`, `files`, `agent?`, `sessionId?`, `device?`, `alerts`；来源只取自提交说明末尾的 `Agent:`、`Session:`、`Device:` trailer，缺失时显示「未记录来源」，不根据作者或提交内容推断 |
| `FileDiff` | `path`, `additions`, `deletions`, `patch`, `binary` |
| `DiagnosticIssue` | `code`, `severity`, `filePath?`, `lineNumber?`, `message`, `suggestion`, `autoFixable` |
| `DoctorReport` | `revision`, `checkedAt`, `issues`, `counts`；首版不提供没有计算规则的 0–100 分 |
| `DreamProposal` | `proposalId`, `repoId`, `baseRevision`, `baseHead`, `runner`, `status`, `patchHash?`, `diff?`, `changes?`, `warnings`, `createdAt`, `appliedCommit?`, `error?` |
| `AdapterStatus` | `agent`, `hostVersion?`, `pluginVersion?`, `configPaths`, `status`, `capabilities`, `reason?` |
| `AdapterInstallPlan` | `agent`, `hostVersion?`, `pluginVersion?`, `status`, `applicable`, `reason?`, `artifacts`；每项为 `{path, format, action, currentRevision?, plannedRevision?, note?, patch?}`，`action` 为 `create \| update \| skip \| manual` |

诊断级别为 `error | warning | info`。首版 `DiagnosticIssue.code` 为 `ENTRY_MISSING`、`FILE_UNREADABLE`、`INVALID_LINK`、`MISSING_LINK`、`AMBIGUOUS_LINK`、`ORPHAN_FILE`、`ENTRY_BLOAT`、`MISSING_RECOMMENDED_METADATA`、`SUSPICIOUS_INSTRUCTION`、`TRANSACTION_PENDING`、`CONFLICT_PENDING`、`GIT_OPERATION_ACTIVE`、`GIT_UNMERGED_INDEX`、`GIT_INDEX_LOCKED`。死链、不可访问路径、读失败、未完成事务与待解决冲突报告 `error`；同名歧义、与入口断开的孤立文件、入口膨胀与可疑指令模式报告 `warning`；缺少推荐元数据报告 `info`。可疑指令模式只是需审阅的线索，不把合法 SQL、脚本示例认定为恶意行为。首版只有 `ENTRY_MISSING` 标记 `autoFixable: true`：重建缺失的入口文件不引入新事实。其余诊断需要人工判断，`doctor --fix` 与 `POST /api/v1/doctor:fix` 对没有确定性修复的 `issueCodes` 返回 `UNSUPPORTED`，不猜测修复；空 `issueCodes` 选择当前存在的全部确定性修复。

`DreamProposal.status` 为 `generating | ready | applying | applied | stale | failed`；`DreamProposal.runner` 为所选的内置模型提供商标识符（`deepseek | minimax | openai | anthropic`）。提案在当前进程结束或系统重启后仍可查询；生成阶段中断标记失败，不伪造可继续的执行状态。已经应用的提案再次应用返回原 `appliedCommit`，不能重复提交。

`AdapterStatus.status` 为 `unsupported | not_installed | configured | verified | needs_attention`。写入配置只得到 `configured`；只有宿主实测成功才是 `verified`。`AdapterStatus.capabilities` 为该 adapter 当前接入路径的受管能力，取值 `hooks`（命令 Hook 配置）、`config`（宿主功能开关配置）、`plugin`（需随宿主加载的插件文件），按注册顺序去重；原生插件已安装时只报告 `plugin`，旧路径只报告 `hooks`。它只描述本工具维护的对象，不表示宿主已实测可用。

dsh 同时支持原生插件 `memctl-dsh` 和旧 hooks 插件 `dsh-hooks-plugin`。原生插件存在时 `pluginVersion` 取该插件清单的版本，`artifacts` 为空，适配器不写 `hooks.json`；旧插件存在时 `pluginVersion` 取旧插件版本，`artifacts` 保留 `hooks.json` 合并计划。两者同时存在、只存在旧插件且未写配置、或两者都不存在时，状态和可应用性按 [Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装) 处理；`needs_attention` 不允许被当作已接入。

pi 同时支持原生插件 `memctl-pi` 和旧 `pi-yaml-hooks` + `agent-memory-*` 条目。原生插件通过 npm 清单、`$STATE/pi/package.json` 或 `~/.pi/agent/settings.json` 的本地 `packages` 声明探测；存在时 `pluginVersion` 取原生插件版本，`artifacts` 为空，`capabilities` 为 `[plugin]`。旧路径是否在用由 `hooks.yaml` 中实际的 `agent-memory-*` 条目决定，`pi-yaml-hooks` 包存在但无这些条目时不构成旧接入；旧路径的 `capabilities` 为 `[hooks]`。原生与旧条目同时存在、只有旧条目但缺少旧插件、两者都不存在、pi 版本低于 `0.86.1` 或原生插件版本低于最低支持版本时报告 `needs_attention`；`install pi` 不再默认创建 `hooks.yaml`。

`AdapterInstallPlan.artifacts[].currentRevision` 是预览时读取的配置内容修订，安装请求以它作为配置基线。`AdapterInstallResult` 是 `POST /api/v1/adapters/{agent}/install` 的响应：`dryRun`, `status`: `AdapterStatus`, `plan`: `AdapterInstallPlan`, `applied`: `[{path, action, backupPath?}]`, `manual`: `[{path, action, note?}]`, `skipped`: `[path]`, `recovery?`: `{recovered, restored, removed, skipped}`, `pendingRecovery`；`status` 是本次请求结束后的状态，`plan` 是被应用或将应用的预览。

## 3. 应用与基础设施接口

以下是包级职责契约，不是可独立编译的完整源码。类型在所属领域包定义；调用方通过 `context.Context` 传递取消和截止时间。

| 接口 | 输入 → 输出 | 关键行为 |
| --- | --- | --- |
| `app.ReadFile` | 路径 → 文件、原文、修订 | 有界读取完整版本 |
| `app.Apply` | `MutationRequest` → `MutationResult` | 锁内检查、规划、应用、提交、登记同步 |
| `app.Capture` | 显式确认 → `MutationResult` | 接收外部串行编辑，暂存范围来自预检清单 |
| `syncer.Run` | 已持锁仓库上下文、deadline → `SyncSummary` | 上下文由 app 传入；不嵌套持锁，不反向依赖 app |
| `app.Resolve` | 冲突 ID、阶段结果 → 状态或下一冲突 | 绑定指定重放阶段 |
| `app.Revert` | 提交 hash、仓库基线 → `MutationResult` | 验证父提交数，失败保留恢复信息 |
| `dream.Preview` | 模型提供商（`runner`）、固定基线 → 提案 ID | 触发远端 GitHub Actions 整理并拉取校验结果树；确认前不修改主仓库 |
| `adapters.Plan` / `adapters.Apply` | 探测环境、agent → 安装计划 / 安装结果 | 每配置路径独立锁与基线校验；无法安全合并时给人工补丁，不写文件 |
| `app.ApplyDream` | 提案 ID、patchHash → `MutationResult` | 校验持久化提案后执行受管事务 |
| `lock.Acquire` | context、资源键 → release 或错误 | 仓库操作使用同一锁；非重入 |
| `git.Inspect` | 仓库 → Git 状态或错误 | 检查索引、rebase、merge、revert、cherry-pick、sequencer 和 Git 锁 |
| `git.CommitPaths` | 字面路径清单、提交元数据 → hash 或错误 | 不扩大暂存范围 |
| `git.Fetch` / `Rebase` / `Continue` / `Abort` / `Push` | 显式 remote/ref/operation → 结果或错误 | 不修改应用状态；是否恢复由应用服务决定 |

`MutationRequest` 包含 `baseRevision?` 和 `changes[]`。每项变化为 `{path, expectedRevision, action, rawContent?}`，`action` 为 `put | delete`，创建时基线为 `absent`。单文件可仅使用文件基线；移动、改链、整理和诊断修复使用仓库基线，服务端重新生成完整受影响清单。不能由客户端指定任意 Git 命令、refspec 或绝对路径。

## 4. REST 契约

### 4.1 公共规则

前缀为 `/api/v1`，JSON 使用 UTF-8。所有 API 请求含 GET 和 SSE 都遵循 [HTTP 安全](./04-ui-and-cli-specification.md#5-http-安全)。JSON Body 未声明的字段返回参数错误，避免拼写错误被静默忽略。

已有文件的 PUT、条目 PATCH/DELETE 使用 `If-Match: "<fileRevision>"`；新建文件使用 `If-None-Match: *`。条目创建使用 Body 中的 `expectedRevision`，目标不存在时为 `absent`。跨文件移动使用 `baseRevision`，同一请求的所有前置条件都通过才修改。

本次设计的默认资源限额为：Hook stdin 1 MiB、单文件文本 2 MiB、单次 JSON 请求 8 MiB、一次变更最多 100 个文件、一次文本变更总量 8 MiB、审计单页默认 50 条且最多 200 条、单次提交 Diff 响应 4 MiB。它们是实现与测试的初始边界，不是实测性能结论；超限明确拒绝，不静默截断后保存。大型文件仍可由 Git 同步。

### 4.2 路由

表中 `GET` 不改变记忆，写入请求共享应用事务。通配参数占据完整路径段，固定动作段可以使用冒号。

| 方法与路径 | 输入 | 输出及行为 |
| --- | --- | --- |
| `GET /api/v1/snapshot` | 无 | `VaultSnapshot` |
| `GET /api/v1/files/{path...}` | 根相对路径 | `{file, rawContent, hiddenExpired}`，附带文件 `ETag`；`hiddenExpired` 为本次读取被隐藏的到期条目数 |
| `PUT /api/v1/files/{path...}` | 版本头、`{rawContent}` | `MutationResult` |
| `POST /api/v1/files:move` | `{fromPath, toPath, baseRevision}` | `MutationResult`；同一事务内移动文件并只重写已解析到源文件的链接；仅大小写不同的重命名经事务内中间路径完成，拒绝覆盖已有目标 |
| `GET /api/v1/entries` | `q, agent, device, file, from, to, limit, offset` | `{revision, items, total}`；默认 50、最多 200 条，变更后重新分页 |
| `POST /api/v1/entries` | `{filePath, heading?, factText, sources?, expectedRevision}` | `{entry, result: MutationResult}` |
| `PATCH /api/v1/entries/{id}` | 版本头、`{filePath, factText, moveToFilePath?, baseRevision?}` | `{entry, result}`；移动时要求仓库基线，目标不存在时创建该文件（图谱拆分），源文件只移除该条目并保留其余内容与元数据 |
| `DELETE /api/v1/entries/{id}` | 版本头、`?filePath=` | `MutationResult` |
| `POST /api/v1/sync` | 无 | 202、`SyncSummary`；完整双向同步，通过 snapshot 查询后续状态 |
| `GET /api/v1/conflict` | 无 | `ConflictState`，无冲突时 204 |
| `POST /api/v1/conflict:resolve` | `{conflictId, baseRevision, resolvedFiles}` | 状态及可能出现的新冲突；不保证一轮完成 |
| `POST /api/v1/conflict:abort` | `{conflictId}` | 中止本工具拥有的解决过程，返回恢复后状态 |
| `GET /api/v1/audit` | `limit, agent, before?` | `{commits, nextCursor?}`；只返回提交元数据与文件清单，不含 Diff；游标分页且不返回总数，UI 只能按已访问页编号 |
| `GET /api/v1/audit/{hash}/diff` | 无 | `{hash, files}`，`files` 为 `FileDiff[]`；只读预览该提交的文本改动，二进制文件为 `binary: true` 且 `patch` 为空 |
| `POST /api/v1/audit/{hash}/revert` | `{baseRevision}` | `MutationResult` |
| `GET /api/v1/doctor` | 无 | `DoctorReport` |
| `POST /api/v1/doctor:fix` | `{issueCodes, baseRevision}` | `{report, result}`；仅应用确定性修复 |
| `POST /api/v1/dream:preview` | `{runner: "deepseek" \| "minimax" \| "openai" \| "anthropic", baseRevision?}` | 202、`{proposalId}`；`baseRevision` 可选，省略时服务端在锁内固定当前基线，提供时按预条件校验；远端未配置、未确认私有或缺少工作流文件时同步返回错误 |
| `GET /api/v1/dream/{proposalId}` | 无 | `DreamProposal` |
| `POST /api/v1/dream:apply` | `{proposalId, patchHash}` | `MutationResult` |
| `GET /api/v1/adapters` | 无 | `{adapters: AdapterStatus[]}`；只探测与预览，不写文件 |
| `GET /api/v1/mcp` | 无 | `{executable, available, version?, tools, reason?, registrations: [{agent, command?, instruction?}]}`；探测当前二进制的 MCP 通道并给出每个宿主的注册动作，只读，不写宿主配置 |
| `POST /api/v1/adapters/{agent}/install` | `{dryRun?, expectedConfigRevisions?}` | `AdapterInstallResult`；`dryRun` 只返回计划，实际安装要求每个已存在目标文件的配置基线 |
| `GET /api/v1/events` | Token 请求头 | SSE，客户端使用 fetch 流读取 |

`resolvedFiles` 为 `{path, action: "put" | "delete", rawContent?}[]`，覆盖当前阶段全部冲突文件，且不能包含阶段外路径。不支持的冲突类型返回 `UNSUPPORTED`。`baseRevision` 在初次恢复时绑定原工作树，在后续阶段由新的冲突快照重新提供，不能复用旧阶段请求。

`expectedConfigRevisions` 为 `{<绝对配置路径>: <fileRevision>}`。`dryRun: true` 只返回计划，不执行中断恢复。实际安装先恢复未完成的安装现场，再按最新配置重算计划，对每个 `currentRevision` 非空的目标文件要求 `expectedConfigRevisions` 提供同一修订（缺失返回 `PRECONDITION_REQUIRED`，不一致返回 `REVISION_MISMATCH`），随后按 [Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装) 写入。本次请求确实恢复了中断现场时，预览基线已被恢复动作取代，该请求不再校验 `expectedConfigRevisions`，而以恢复后的内容重算计划并直接写入，恢复结果在 `recovery` 字段返回。未注册的 `agent` 返回 `UNSUPPORTED`；`--readonly` 下该路由返回 `READ_ONLY`。

### 4.3 错误与部分成功

错误响应为 `{error: {code, message, retryable, transactionId?, recovery?, committedHash?}}`。`message` 不包含完整用户 prompt、Token、凭据或未脱敏 remote URL。

| HTTP | code | 调用方动作 |
| --- | --- | --- |
| 400 | `INVALID_ARGUMENT`, `INVALID_PATH` | 修正参数，不自动重试 |
| 401 | `AUTH_REQUIRED` | 重新打开当前 UI 会话 |
| 403 | `ORIGIN_DENIED`, `READ_ONLY`, `REMOTE_NOT_APPROVED` | 检查访问来源、只读模式或远端授权 |
| 404 | `NOT_FOUND`, `NO_REPO` | 刷新资源或初始化仓库 |
| 409 | `EXTERNAL_CHANGES`, `OPERATION_ACTIVE`, `CONFLICT_STALE`, `CONTENT_CONFLICT`, `PROPOSAL_STALE`, `RECOVERY_REQUIRED` | 保留用户草稿，按具体状态恢复 |
| 412 | `REVISION_MISMATCH` | 重新读取并显示差异，不盲目重发 |
| 413 | `LIMIT_EXCEEDED` | 缩小输入，不能截断替代 |
| 422 | `UNSUPPORTED` | 改用支持的路径或人工处理 |
| 428 | `PRECONDITION_REQUIRED` | 补齐版本条件 |
| 503 | `BUSY`, `NETWORK_UNAVAILABLE` | 稍后显式重试或等待下次 Hook |
| 500 | `GIT_FAILED`, `IO_FAILED` | 读取恢复信息；不自动重复写入 |

内部 Git 错误另区分 `NETWORK_UNAVAILABLE`、`AUTH_FAILED`、`NO_UPSTREAM`、`NON_FAST_FORWARD`、`CONTENT_CONFLICT`、`OPERATION_ACTIVE`、`TIMEOUT`、`GIT_FAILED`。通过退出结果与 Git 实际状态组合判断；未能证明是内容冲突时不生成冲突快照。

文件修改已形成本地提交时，后台推送失败不令写接口返回「保存失败」；返回提交 hash 和 `pending/failed` 远端状态。写入中断且尚不能确认是否提交时返回事务 ID 和恢复指引，不宣称整次操作没有副作用。确认存在重复应用风险时先查询状态，再决定是否重试。

## 5. SSE 与缓存

前端用 `fetch('/api/v1/events', {headers: {'X-Memctl-Token': token}})` 读取 `text/event-stream`，不用不能设置自定义请求头的原生 `EventSource`。解析器处理跨网络块拆分、多行 data、空行分隔和断线重连。

```text
event: vault.updated
data: {"revision":"sha256:...","changedFiles":["MEMORY.md"]}

event: sync.status
data: {"remoteState":"pending","reasonCode":"NETWORK_UNAVAILABLE"}

event: sync.conflict
data: {"conflictId":"...","files":["MEMORY.md"]}

event: dream.updated
data: {"proposalId":"...","status":"ready"}

```

事件只通知失效，不作为事实来源或可靠消息队列。重连后刷新 snapshot，并查询仍在等待的提案。队列溢出或监听事件丢失时发送 `resync.required`，客户端重新读取；服务端不无限缓存事件。

监听器递归登记已有和新建目录，处理 rename、remove、原子替换及 overflow；150ms 防抖只是合并通知的初始参数，不是事务完成信号。仓库写事务结束后主动发送失效通知，不能只等待文件监听。
