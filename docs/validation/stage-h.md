# 阶段 H 归档

本文件按任务归档阶段 H（跨会话新记忆提炼）的完成记录。阶段定义与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)，方案与协议见 [12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md)。

## T098：本地当前 Agent 交互式跨会话新记忆提炼方案与契约修订（`H-1`）

- 目标与范围：完成阶段 H 的 `H-1`。新增 [12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md)，定义本地 0 模型调用确定性会话蒸馏与脱敏缓存（`internal/corpus`）、在当前活跃 Agent 会话内执行的 `dream --prepare`/`--propose` 与对应 MCP 工具、Go 代码级来源绑定守卫（Provenance Guard）及验收场景；已实现的 GitHub Actions 远端 Dreaming（[11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)）保持原样不动；同步更新 `docs/01`–`05`、`docs/09`、`docs/README.md` 与 `docs/STATUS.md`。本轮只修订设计契约，不改变产品代码。
- 契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md)、[架构与模块](../01-architecture-and-modules.md#3-仓库路径与信任边界)、[模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)、[MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集)。
- 验证日期与环境：2026-10-20，darwin/arm64。
- 验证：`git diff --check` 无空白或冲突标记错误；`docs/*.md` 与根 `README.md` 相对路径链接及标题锚点核对全部可达；对照 `tech-doc-style-chinese` 规范核对术语一致性、引号用法、中西文间距与表格完整性。
- 未验证与遗留：契约与现有实现存在冲突（本地基线复用了要求远端就绪的检查、`runner` 闭集未包含 `local-agent`、事件型条目守卫缺少可判定输入、`MEMORY.md` 行数限额与既有阈值冲突、UI 无提案列举入口、`Proposal:` trailer 不存在、宿主日志路径与真实目录不符），由 `T099` 修正。

## T099：契约修订（`H-1`）

- 目标与范围：修正 `T098` 交付的方案与契约中与现有实现冲突、内部不自洽或与实测不符的部分。只修改设计文档，不改变产品代码。
- 契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md)、[架构与模块](../01-architecture-and-modules.md#4-模块职责)、[模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)、[MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集)。
- 验证日期与环境：2026-10-20，darwin/arm64。
- 改动：
  1. 本地基线路径明确不调用远端就绪检查：无 GitHub 远端、无 `.github/workflows/memctl-dream.yml` 的本地记忆库同样可以 `prepare`/`propose`（对应 `docs/12` §2.3 步骤 1）。
  2. `DreamProposal.runner` 闭集加入 `local-agent`，`docs/02` 的模型说明与接口表同步；新增 `GET /api/v1/dream` 提案列举路由与 `dream --list`，使 UI 能发现本地提案。
  3. 删除「Go 校验事件型条目未被删除」：解析器没有条目类型字段，`worker` 的 Step Guard 也只校验路径、格式与链接。改为提炼规则加 `warnings`，并在文档中说明升级为硬约束的前提。
  4. `MEMORY.md` 行数不再设 200 行硬限，沿用 `EntryBloatLines = 100` 提示阈值，避免与 `docs/10`、`docs/11`、`docs/README.md` 的既有口径冲突。
  5. 去掉提交说明中的 `Proposal:` trailer：现有 `commitMessage` 只写 `Memctl-Transaction:` 与来源 trailer，提案 ID 记录在事务记录与提案文件中。
  6. 隐私表述改为「会话摘要会进入当前 Agent 的模型上下文」，不再声称数据完全不出本机；MCP 不提供 `memory_dream_apply`，应用只保留 CLI 与 UI 两个用户入口。
  7. 限额自洽：单次整理改为 12 个会话 / 96 KiB，说明 `MaxTurnsPerSession` 是解析上限、16 KiB 是产出上限；补扫改为「先按项目预过滤再按 mtime 倒序」，新增 `MaxBackfillFileBytes` 与 `BackfillBudget`。
  8. 项目过滤键由 `cwdBase` 改为 `cwdKey`，同名克隆不再互相污染；`--project` 传名称时按唯一性判定，歧义返回 `INVALID_ARGUMENT`。
  9. `files[]` 成为唯一写入通道，`newMemories[]` 只做来源绑定并与 `files[]` 逐字比对；Go 不再注入条目或补索引链接。
  10. 消费写回改为按摘要哈希幂等：`consumedRevision` 记录消费时的内容哈希，`apply` 时哈希不符的项跳过并列入 `warnings`；已消费摘要不再因 TTL 被清理。
  11. 按本机实测更正宿主日志路径与格式：`dsh` 为 `~/.dsh/sessions/<转义目录>/session-<sid>/session.v4.jsonl.zstd`（zstd 压缩）；`opencode` 无 `.jsonl` 日志，标记为未支持；`claude-code` 从项目目录名还原 `cwdBase`；补齐 `codex`、`pi`、`antigravity` 与 `dsh` 的字段层级（`user/message`、`assistant/message`、`tool/call`、`tool/result`）。
  12. 阶段计划拆分为 `H-2a`（四个已核对宿主）与 `H-2b`（存量补扫与其余宿主探测），并新增 `D08`（摘要变化与写回中断）验收场景。
  13. `dsh` 的 zstd 解码定为引入 `github.com/klauspost/compress/zstd`（v1.20.3，纯 Go、无 cgo、BSD-3-Clause），由 `internal/corpus` 直接解码，不由插件代劳；同时明确解码器固定单协程、`MaxBackfillFileBytes` 按解压产出计算、解码失败静默跳过，并在 [架构与模块](../01-architecture-and-modules.md#技术依赖) 的技术依赖表登记该库。
- 验证：`git diff --check` 无空白或冲突标记错误；`docs/*.md` 与根 `README.md` 相对路径链接及标题锚点核对全部可达；`docs/12` 的宿主路径与格式对照本机 `~/.dsh`、`~/.claude`、`~/.codex`、`~/.pi`、`~/.gemini/antigravity` 实测样本核对；用 `github.com/klauspost/compress/zstd` v1.20.3 实际解码本机 `session.v4.jsonl.zstd` 样本（4 行与 4013 行两例）验证事件类型与字段层级；全库检索确认不再有 `memory_dream_apply`、`20 个会话`、`200 行`、`20 个未消费` 等旧表述。
- 未验证与遗留：zstd 依赖未提交到 `go.mod`，随 `H-2a` 引入；`D08` 依赖 `H-3` 的消费写回实现。

## T100：本地会话蒸馏器实现（`H-2a`）

- 目标与范围：实现 `H-2a`——引入 zstd 依赖、`internal/corpus` 框架与四个已核对宿主格式（`claude-code`、`codex`、`pi`、`dsh`）、回合裁剪与限额、正则脱敏、可疑指令过滤、14 天 TTL 与消费保留、`hookio` 的 `transcript_path`/`last_assistant_message` 解析、`sync`/`end` 的 `--transcript` 与 `AGENT_MEMORY_CORPUS`、`memctl-pi`/`memctl-dsh` 传递会话文件路径。不实现存量补扫（`H-2b`）与本地 `prepare`/`propose`（`H-3`）。
- 契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#22-本地会话采集与确定性蒸馏internalcorpus)、[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)、[架构与模块](../01-architecture-and-modules.md#4-模块职责)、[验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)。
- 验证日期与环境：2026-10-20，darwin/arm64，Go 1.27.1，Node v26.10.0。
- 交付：
  1. `go.mod` 固定 `github.com/klauspost/compress v1.20.3`（直接依赖，纯 Go、无 cgo）；该依赖经 `scripts/release.sh` 的 `go list -m all` 自动进入 `THIRD_PARTY_LICENSES.txt`。
  2. `internal/corpus` 新增 `corpus.go`（`Digest`、`Cache`、TTL 清理与消费保留）、`digest.go`（回合限额、UTF-8 安全截断、纠正信号优先保留、16 KiB 产出上限）、`redact.go`（注入块剥离与 10 条凭据正则）、`source.go`（明文尾部窗口、zstd 解压产出上限、符号链接拒绝）、`live.go`（实时采集预算与 `AGENT_MEMORY_CORPUS`）、`collect.go` 与四个宿主解析器。
  3. `dsh` 的 zstd 流用 `zstd.NewReader(file, zstd.WithDecoderConcurrency(1))` 解码；`MaxBackfillFileBytes` 约束解压产出，达到上限即停止并标记 `truncated`；解码失败静默跳过。
  4. `hookio.Input` 新增 `TranscriptPath` 与 `LastAssistantMessage`；空白或非字符串按文档规则处理。
  5. `sync`/`end` 支持 `--transcript PATH`；采集在远端路径之前执行，任何失败都不改变退出码。采集后按 `git log`（上限 50 条）检查本会话是否已有 `Session: <sid>` trailer，命中则把 `wroteMemoryInSession` 置为 `true`，供当前 Agent 降低重复提取权重。
  6. `memctl-pi` 通过 `ctx.sessionManager.getSessionFile()` 取路径；`memctl-dsh` 在会话根（`DSH_SESSIONS_DIR` → `$DSH_HOME/sessions` → `~/.dsh/sessions`）下定位 `session-<sid>/` 并取最高版本记录，取不到时不传 `--transcript`。
- 验证：
  - `go test ./internal/...` 全部通过（含新增 `internal/corpus` 13 个用例）；`node --test plugins/dsh/test/*.test.js` 18 项、`plugins/pi/test/*.test.js` 17 项通过。
  - `tests/integration` 137 项全部通过，按 `scripts/test-shards.txt` 四片运行（约 51–57 秒/片）；新增 9 项：`TestD06LiveCollectionFromStdinPayloadCoversD06`、`TestD06LiveCollectionFromExplicitTranscriptCoversD06`、`TestD06DisabledCollectorWritesNothingCoversD06`、`TestD06MissingLogKeepsExitCodeZeroCoversD06`、`TestD06DSHZstdTranscriptCoversD06`、`TestD06DigestStaysBoundedCoversD06`、`TestD06StaleDigestsArePrunedCoversD06`、`TestD06DigestIsRedactedJSONCoversD06` 与 `TestD06WroteMemoryFlagCoversD06`。
  - 用本机真实样本复核解析：`dsh` 1489 行 zstd 会话、`pi` 5.1 MB 会话、`codex` 2.9 MB rollout 均能解析出正确 `sid`、`cwdBase` 与回合；`dsh` 的 harness 注入消息（`runtime-context`、`skill-catalog`、`time-context`）与 `codex` 的 `<external_codex_apps_open_page>` 事件被剔除。
  - 手工核对：`go vet ./...` 通过；`git diff --check` 无空白错误。
- 未验证与遗留：
  - `claude-code` 与 `antigravity` 的 Hook 侧 `transcript_path` 未接真实宿主验证（`claude-code` 的解析已按本机 JSONL 样本核对，`antigravity` 属 `H-2b`）。
  - 存量多会话补扫（`MaxBackfillFilesPerRun`、`BackfillBudget`、项目预过滤）与 `antigravity` 接入属 `H-2b`。
  - `memctl-dsh` 的会话记录定位按本机 `~/.dsh/sessions/<转义目录>/session-<sid>/` 结构实现并用插件单测覆盖，未在真实 DSH 会话中复验。
  - `codex` 的 `last_assistant_message` 只在日志缺少助手文本时作为回退，未在真实受限会话中单独验证。

## T101：存量多会话补扫与 antigravity 接入（`H-2b`）

- 目标与范围：实现 `H-2b`——`internal/corpus` 的存量多会话补扫（目录级项目预过滤、`mtime` 倒序、`MaxBackfillFilesPerRun = 50`、`MaxBackfillFileBytes = 8 MiB`、`BackfillBudget = 5 s`、14 天窗口、已入池按 `(agent, sid)` 跳过、预算切断的日志不写缓存）与 `antigravity` 解析器；`commandcode`、`grok`、`opencode` 只做静默跳过。不新增 CLI 子命令：补扫入口 `corpus.Backfill` 只作为库函数供 `H-3` 的 `dream --prepare` 调用。
- 契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#2-存量补扫的选择顺序与预算)、[架构与模块](../01-architecture-and-modules.md#4-模块职责)、[验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)。
- 验证日期与环境：2026-10-20，darwin/arm64，Go 1.27.1，Node v26.10.0。
- 交付：
  1. `internal/corpus/backfill.go`：`Backfill`、`BackfillRequest` 与 `BackfillResult`；目录级预过滤按宿主分别实现——`claude-code`、`pi` 按转义目录名收窄，`dsh` 按 `~XXXX` 转义目录名定位并取最高版本记录，`codex` 读 `session_meta` 行、`antigravity` 读 `run_command` 的 `args.Cwd`，不匹配即关闭文件且不进入候选集。候选按 `mtime` 倒序，`mtime` 超出 `CorpusStateTTL` 的第一项结束整轮。
  2. 预算与限额：`budgetReader` 在墙钟预算耗尽时用哨兵错误终止读取，被切断的日志不写缓存，避免不完整摘要被下一轮当作已入池；`TimedOut` 只在预算触发时为 `true`，达到 `MaxBackfillFilesPerRun` 不算超时。`MaxBackfillFilesPerRun` 只统计蒸馏成摘要的日志；`codex` 与 `antigravity` 的项目嗅探是有界元数据读取，受时间预算约束而不占该配额。初版把嗅探也计入配额，用本机真实存储复核时 50 条其他项目的 codex rollout 直接耗尽上限、一个会话都补不进来，因此改为按文档口径只对解析计数。补扫复用 `openSessionStream`，共享 `MaxBackfillFileBytes` 的尾部窗口与 zstd 解压产出上限。
  3. `antigravity` 解析器（`internal/corpus/antigravity.go`）：`USER_INPUT` 只取 `source = "USER_EXPLICIT"` 记录的 `<USER_REQUEST>` 正文，剥离同记录内的 `<ADDITIONAL_METADATA>` 与 `<USER_SETTINGS_CHANGE>`；`PLANNER_RESPONSE` 取 `content` 与 `tool_calls[].args` 的命令/路径签名，同层 `thinking` 丢弃；其余记录类型不进入摘要。会话没有元数据行，工作目录从 `run_command` 的 `args.Cwd` 反推。
  4. `AGENT_MEMORY_CORPUS=0` 同时关闭补扫；`Backfill` 在缓存为空、`Cwd` 为空或未启用时直接返回零结果。
  5. `commandcode`、`grok`、`opencode` 不进入发现流程：前者目录存在但字段结构未核对，后者会话在 SQLite 中。`docs/12` 的宿主表按此更新「格式核对状态」列。
- 验证：
  - `go test ./internal/...` 全部通过。`internal/corpus` 新增 13 个用例：`TestBackfillCollectsCurrentProjectCoversD06`、`TestBackfillSkipsOtherProjectsCoversD06`、`TestBackfillKeepsConsumedDigestsCoversD06`、`TestBackfillMarksOversizedLogTruncatedCoversD06`、`TestBackfillStopsAtFileLimitCoversD06`、`TestBackfillStopsAtBudgetCoversD06`、`TestBackfillDisabledCoversD06`、`TestBackfillSkipsStaleSessions`、`TestBackfillRecordsWroteMemory`、`TestBackfillIsIdempotent`、`TestAntigravityDigestCoversD06`、`TestEscapeStoreNames` 与 `TestBackfillDSHStoreIsReadable`；`TestUnknownHostIsSkippedCoversD06` 的清单移除 `antigravity`。
  - `tests/integration` 138 项按 `scripts/test-shards.txt` 四片运行全部通过（约 52–55 秒/片），新增 `TestD06BackfillScansHistoryCoversD06`；`scripts/test-shards.txt` 用全量 `-v` 运行的实测耗时按同一贪心算法重新生成。
  - 用本机真实存储复核补扫：`~/.claude`、`~/.pi`、`~/.dsh`、`~/.codex`、`~/.gemini/antigravity` 五个目录在 1.8 秒内收集 25 个 memctl 会话并跳过 298 条其他项目日志；`dsh`、`antigravity`、`pi` 的会话均按 `cwdBase = memctl` 入池，超限日志标记 `truncated`。
  - `go vet ./...` 与 `git diff --check` 通过；`docs` 相对链接与锚点检查通过。
- 未验证与遗留：
  - `commandcode`、`grok` 的日志字段结构仍未核对，与 `opencode` 一样不进入补扫；新增宿主支持前必须先在本机核对样本并更新 `docs/12` 的宿主表。
  - `antigravity` 的项目判定依赖会话中出现过 `run_command`；没有执行命令的会话无法归属项目，会被跳过。这是 `H-2b` 接受的限制，`docs/12` 宿主表已记录。
  - `dream --prepare` 的补扫触发、`cwdKey` 优先排序、`--project` 过滤与 `remainingUnconsumed` 计数属 `H-3`。

## T102：本地交互式 `prepare`/`propose`、项目筛选与 MCP 工具（`H-3`）

- 目标与范围：实现 `H-3`——`internal/dream` 的本地基线固定路径（不调用远端就绪检查）、`PrepareLocal`（刷新当前会话、补扫触发、`cwdKey` 优先、`--project` 过滤与 `remainingUnconsumed`）、`ProposeLocal`（Provenance Guard、`files[]` 逐字比对、30 天删除闸门、链接校验、`local-agent` 提案持久化与 `corpusDigests`）、`ApplyDream` 的幂等消费写回与 `Store.List`；CLI 新增 `dream --prepare`/`--propose --stdin`/`--list`；MCP 新增 `memory_dream_prepare`/`memory_dream_propose`（不注册 apply）；`internal/server` 新增 `GET /api/v1/dream`。不改动 `--runner`/`--worker` 远端路径，`H-4` 的 Skill、帮助文案与 UI 展示不在本轮。
- 契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)、[模型与契约](../02-domain-models-and-contracts.md#4-rest-契约)、[MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集)、[架构与模块](../01-architecture-and-modules.md#4-模块职责)。
- 验证日期与环境：2026-10-20，darwin/arm64，Go 1.27.1，Node v26.10.0。
- 交付：
  1. `internal/dream/local.go`：`fixLocalBaseline` 只要求仓库可读、工作树干净、无未完成事务或冲突、无进行中的 Git 操作，明确不检查远端、不检查 `.github/workflows/memctl-dream.yml`。`PrepareLocal` 先刷新带稳定 `agent`/`sid` 的当前会话摘要，再调用 `corpus.Backfill` 与 `cache.Prune`；`selectSessions` 只取 `consumedBy == ""` 且 `turns` 非空的摘要，默认先排当前 `cwdKey` 再排其他项目，均按 `updatedAt` 倒序。`--project` 接受 `cwdBase` 或完整 `cwdKey`，同名歧义返回 `INVALID_ARGUMENT` 并列出候选 `cwdKey`。分批受 `MaxCorpusSessions = 12` 与 `MaxCorpusBytes = 96 KiB` 约束，字节预算只截断后续会话，不产生空批次；被截断的会话计入 `remainingUnconsumed`。
  2. `internal/corpus/session.go`：`LocateSession` 按 `agent`/`sid`/`cwd` 定位单个已存在的会话日志（`claude-code` 直接命中文件、`pi` 按文件名匹配、`dsh` 取最高版本、`antigravity` 固定路径、`codex` 在 14 天窗口内比对 `session_meta`），用于 `prepare` 刷新调用会话自身。
  3. `internal/dream/prepare_state.go`：`prepare` 把本次会话列表与每份摘要的内容哈希写入 `$STATE/repos/<repoId>/cache/dream/prepare.json`；`propose` 按该快照解析 `sessionIndices`，不从当前目录重新推导列表——重新推导会在选择顺序变化时把引用指到别的会话上。快照只含会话元数据与摘要修订，不含记忆全文，位于可重建缓存下，写入失败不作为 `prepare` 的失败。
  4. `internal/dream/propose.go`：`newMemories` 上限 30，`sessionIndices` 非空且每个下标在快照范围内；`fact` 必须单行、无 `[source:`/`[added:`、再次经过 `corpus.Clean`/`corpus.Redact` 与可疑指令检查，清洗导致内容变化时写入 `warnings`；`time_limited` 的 `until` 必须是 `YYYY-MM-DD` 且不早于所引用会话的最新 `date`；归一化重复候选自动跳过并豁免 `files[]` 比对。按首次出现顺序去重拼装 `- <fact> [source: <tag1>; source: <tag2>; added: <latestDate>]`（含可选 `; until:`），与 `files[]` 目标内容逐字比对，缺失即 `INVALID_ARGUMENT`。`files[]` 是唯一写入通道，Go 不注入条目、不创建文件、不补索引链接；`protectedPath` 与删除 `MEMORY.md` 复用远端路径的校验；未满 30 天隐藏期的 `until` 条目被删除返回 `DELETION_DEFERRED`；新引入的断链或歧义链接被拒绝。提案以 `runner: "local-agent"`、`status: "ready"` 持久化，附 `sessionsScanned`、`newMemoriesExtracted` 与 `corpusDigests{file, revision}`。
  5. `internal/dream/consume.go`：`ConsumeCorpusDigests` 只在 apply 提交成功后由 `app.ApplyDream` 调用，按提案记录重算每个摘要的内容哈希；一致才写 `consumedBy`/`consumedRevision`，不一致或文件消失则跳过并追加到提案 `warnings`。已消费项保持不变，重复调用幂等。`corpusDigests` 只记录被 `newMemories[].sessionIndices` 实际引用的摘要，批次中未被引用的会话不会因同批而被标为已消费。
  6. `internal/dream/store.go` 新增 `Store.List(limit)`：按 `createdAt` 倒序、以提案 ID 作稳定 tie-break，上限 200、默认 50，不返回内部 `planned` 与 `corpusDigests`。
  7. `internal/app`：`DreamPrepareLocal`/`DreamProposeLocal`/`DreamList` 组合入口；`ApplyDream` 在成功提交与幂等重放两条路径都执行消费写回，跳过项写入 `MutationResult.warnings`；会话 trailer 扫描从 `internal/cli` 移到 `internal/app/dreamcorpus.go`，供 CLI 与 app 共用。
  8. CLI `dream --prepare`（`--project`、`--max-bytes`、`--agent`、`--sid`、`--json`）、`dream --propose --stdin`（stdin 上限 8 MiB，拒绝未知字段）、`dream --list`（`--limit`）。八种模式互斥，模式外参数返回用法错误。
  9. MCP 新增 `memory_dream_prepare` 与 `memory_dream_propose`，参数与返回结构与文档一致；`memory_dream_apply` 不注册。`memory_dream_prepare` 把宿主会话身份传给服务，`propose` 不提供任何文件写入能力，只持久化提案。
  10. `GET /api/v1/dream?limit=` 按 `createdAt` 倒序返回 `{proposals}`，空结果为 `[]`，只读路由在 `--readonly` 下可用，响应不含 `planned`。
- 验证：
  - `go test ./internal/...` 全部通过。`internal/dream` 新增 15 个用例：`TestPrepareLocalNeedsNoRemoteCoversD07`、`TestPrepareLocalRanksCurrentProjectFirstCoversD07`、`TestPrepareLocalRefusesAmbiguousProjectCoversD07`、`TestPrepareLocalRefusesDirtyRepository`、`TestProposeLocalAssemblesMultiSessionSourceCoversD07`、`TestProposeLocalRefusesProvenanceViolationsCoversD07`（9 个子用例）、`TestProposeLocalSkipsDuplicateFactCoversD07`、`TestProposeLocalTimeLimitedUntilCoversD07`、`TestProposeLocalRefusesPrematureExpiredDeletionCoversD07`、`TestProposeLocalRefusesNewDanglingLinkCoversD07`、`TestConsumeCorpusDigestsIsReentrantCoversD08`、`TestConsumeSkipsChangedDigestCoversD08`、`TestProposeLocalRequiresPrepareSnapshotsCoversD07`、`TestDreamListOrdersByCreatedAt` 与 `TestBundleJSONShape`（共 15 项，其中 `TestProposeLocalRefusesProvenanceViolationsCoversD07` 含 9 个子用例）；`internal/mcp` 新增 6 项协议用例（工具注册与无 apply、会话身份传递、契约错误映射、结构化输入、必需基线与错误类型）。
  - `tests/integration` 144 项按 `scripts/test-shards.txt` 四片运行全部通过（约 50 秒/片），新增 6 项：`TestH3PrepareWorksWithoutRemoteCoversD07`、`TestH3PrepareProjectFilterAndAmbiguityCoversD07`、`TestH3ProposeApplyConsumesSessionsCoversD07`、`TestH3ProposeRejectsMissingAssembledLineCoversD07`（3 个子用例）、`TestH3ApplySkipsChangedDigestCoversD08` 与 `TestH3DreamListRouteCoversD07`；`TestB1RoutesCoversA01` 增加 `GET /api/v1/dream` 路由、`TestM01MCPHandshake` 更新为 5 个工具。
  - `go vet ./...` 与 `git diff --check` 通过；`scripts/test-shards.txt` 按全量 `-v` 实测耗时用同一贪心算法重新生成。
- 未验证与遗留：
  - 未在真实宿主会话内跑完整的 `prepare` → `propose` → `apply`：CLI 与 MCP 链路均由临时仓库与合成摘要覆盖，真实 DSH/pi 会话的端到端验收属 `H-4`。
  - `dream --prepare` 的补扫规模取决于本机真实存储；本轮测试用隔离 `HOME`，未复测 `T101` 的真实存储耗时。
  - `memory_dream_prepare`/`memory_dream_propose` 的 `tools/list` 描述与 JSON Schema 已实现，但 `H-4` 才更新中英帮助与 Skill 文案，二者尚未互相对齐审查。

## T102：本地交互式 `prepare`/`propose`、项目筛选与 MCP 工具（`H-3`）

- 目标与范围：完成阶段 H 的 `H-3`。新增 `internal/dream/local.go`（本地基线固定路径、`PrepareLocal` 的补扫触发与 `cwdKey` 优先排序、`--project` 过滤与同名歧义、`MaxCorpusSessions = 12` / `MaxCorpusBytes = 96 KiB` 分批与 `remainingUnconsumed`）、`prepare_state.go`（`cache/dream/prepare.json` 会话快照，供 `propose` 解析下标）、`propose.go`（Provenance Guard、`files[]` 逐字比对、30 天删除闸门、链接校验、`local-agent` 提案与 `corpusDigests`）与 `consume.go`（提交后按摘要哈希幂等的消费写回）；`Store.List` 按 `createdAt` 倒序分页列举；`internal/corpus` 新增 `LocateSession`；CLI 新增 `dream --prepare`/`--propose --stdin`/`--list`；MCP 新增 `memory_dream_prepare`/`memory_dream_propose`（不注册 apply）；`internal/server` 新增 `GET /api/v1/dream`。不改动 `--runner`/`--worker` 远端路径；`H-4` 的 Skill、帮助文案与 UI 展示不在本轮。
- 对应契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)、[02. 模型与契约](../02-domain-models-and-contracts.md#4-rest-契约)、[09. MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集)、[05. 验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)。
- 验证时间与环境：2026-10-20，darwin/arm64，Go 1.27.1，Node v26.10.0。
- 验证检查：`go test ./internal/...` 全部通过（`internal/dream` 新增 15 个用例（含 `TestProposeLocalRefusesProvenanceViolationsCoversD07` 的 9 个子用例），`internal/mcp` 新增 6 项协议用例）；`tests/integration` 144 项按 `scripts/test-shards.txt` 四片运行全部通过（约 50 秒/片），新增 `TestH3PrepareWorksWithoutRemoteCoversD07`、`TestH3PrepareProjectFilterAndAmbiguityCoversD07`、`TestH3ProposeApplyConsumesSessionsCoversD07`、`TestH3ProposeRejectsMissingAssembledLineCoversD07`、`TestH3ApplySkipsChangedDigestCoversD08` 与 `TestH3DreamListRouteCoversD07`，`TestB1RoutesCoversA01` 与 `TestM01MCPHandshake` 同步更新；`go vet ./...` 与 `git diff --check` 通过；文档相对链接与锚点检查通过。
- 遗留与下一步：真实宿主会话内的 `prepare` → `propose` → `apply` 端到端验收、中英帮助与 Skill 文案、UI「整理」视图的本地提案列表属 `H-4`；`memory_dream_prepare` 的补扫耗时未用本机真实存储复测。


## T103：配套 Skill、CLI 帮助与 UI 提案列表（`H-4`）

- 目标与范围：完成阶段 H 的 `H-4`。`skills/memctl/SKILL.md` 新增 §6「跨会话新记忆提炼」（两步协议、三类提炼信号与跨会话频次门控、`files[]` 的逐字条目行写法、父目录必须已存在、空批次与失败码处置），原「边界」改为 §7；`internal/cli/help_data.go` 的 `dream` 页补齐 `--prepare`/`--propose`/`--list` 三个模式与全部选项，新增 `Input` 段、错误码与示例，中英与 `help --json` 同源输出；`web/src` 新增 `api.dreamList`、`dreams.js` 的后端标签与本地计数器助手、`DreamPanel` 的已保存提案列表，并修复该视图缺失的 `readyHint` 导入。不改变 `--runner`/`--worker` 远端路径与任何写入契约；`propose` 不新增父目录前置校验（属既有契约，登记为后续任务）。
- 契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)、[CLI 与 UI](../04-ui-and-cli-specification.md#4-web-ui)、[验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)。
- 验证日期与环境：2026-10-20，darwin/arm64，Go 1.27.1，Node v26.10.0。
- 交付：
  1. Skill §6.1 把可提炼信号收敛为显式纠正与持久偏好、多轮排障结论、架构决策与有时效约束三类，并写明频次门控（隐式偏好需 ≥2 个不同会话）与四类禁止提炼内容。
  2. Skill §6.2 说明 `files[]` 是唯一写入通道、`newMemories[]` 只是来源绑定，条目行必须逐字写入；补充「父目录必须已存在，否则 `apply` 返回 `NOT_FOUND`」——该约束来自 [架构与模块](../01-architecture-and-modules.md#32-文件边界)，此前未写入 Skill，真实端到端验收时因此多耗一轮。
  3. Skill §6.3、§6.4 说明准备包会进入当前会话上下文、空批次不得编造来源，以及 `INVALID_ARGUMENT`、`DELETION_DEFERRED`、`UNSUPPORTED`、`PROPOSAL_STALE`、`BUSY` 的处置。
  4. `dream` 帮助页列出八个互斥模式，补齐 `--prepare`/`--propose`/`--stdin`/`--project`/`--max-bytes`/`--agent`/`--sid`/`--list`/`--limit`/`--json` 的语义与模式限制，新增 `DreamProposeInput` 输入段、`INVALID_ARGUMENT`/`DELETION_DEFERRED` 退出码说明与五条注意事项（本地路径不需要模型 Key、准备包会进入会话上下文、守卫自行拼装 `source` 等）。
  5. `web/src/api.js` 新增 `dreamList`；`dreams.js` 新增 `RUNNER_LABELS`、`runnerLabel`、`isLocalProposal`、`proposalDetail`；`DreamPanel.jsx` 在控制条之上展示已保存提案（状态徽标、`local-agent` 后端徽标、创建时间、`proposalDetail`），点击行即载入该提案的 Diff 与确认按钮；`DreamView.jsx` 的说明改为「两个后端的提案在此汇总」。
- 验证：
  - `go test ./internal/...` 全部通过；`internal/cli` 新增 `TestDreamHelpDocumentsLocalModesCoversH4`（八个模式、选项、`Input`、错误码、示例与中英关键词）。
  - `tests/integration` 144 项按 `scripts/test-shards.txt` 四片运行全部通过（约 53–56 秒/片）。
  - `vitest run`：17 个文件、100 项通过；`dreams.test.js` 新增后端标签与本地计数器用例，`api.test.js` 新增 `dreamList` 的路径与 `limit` 查询用例；`vite build` 通过。
  - `go vet ./...` 与 `git diff --check` 通过；文档相对链接与锚点检查通过。
  - 隔离端到端（临时 `HOME`、本地仓库、无远端与工作流文件）：两段 `claude-code` JSONL 被补扫入池，凭据替换为 `[REDACTED]`；三条守卫分别拒绝手写 `[source:]`、越界 `sessionIndices` 与缺少条目行的 `files[]`；`--propose` 产出 `sessionsScanned: 2`、`newMemoriesExtracted: 2` 的 `local-agent` 提案；`--apply` 提交并写回两个摘要，重复 apply 幂等；再次 `--prepare` 返回 0 个会话。
  - 真实 `~/memory` 端到端：`--prepare` 2.8 秒补扫五个宿主目录，12 个会话入批、`remainingUnconsumed: 13`；`--propose` 产出 `dp-61e33676…`（`sessionsScanned: 12`、`newMemoriesExtracted: 2`）；`--apply` 提交 `ef0eaac` 并同步 `origin/main`，三条摘要写回 `consumedBy`；`GET /api/v1/dream` 在 `--readonly` 下返回该提案与计数器。本轮为该仓库产生的两条新记忆已进入 `~/memory/notes/memctl-cli-path-rules.md` 与新建的 `notes/memctl-skill-install.md`。
- 未验证与遗留：
  - Skill 的自动触发未验证：两轮端到端验收都由本会话手工执行 `--prepare`/`--propose`/`--apply`，没有让另一个 Agent 在「整理记忆」指令下自主完成。
  - `propose` 不校验目标父目录是否存在，提案在 `apply` 时才以 `NOT_FOUND` 失败；符合「不创建父目录」的既有契约，已登记为后续任务。
  - 提案列表只显示前 20 条，未提供分页控件；超过 20 条时需用 `memctl dream --list` 查看。

## T104：`propose` 结构守卫（父目录、孤立文件与索引膨胀，`H-4` 补遗）

- 目标与范围：补齐 `docs/12` §2.3 步骤 3 中三处「文档已写、实现未跟上」的守卫。`propose` 在生成提案前按固定基线校验 `files[]`：目标文件的父目录不存在时返回 `NOT_FOUND` 并列出缺失目录（此前只到 `apply` 才失败）、新出现的不可达文件与断链一并返回 `UNSUPPORTED`、`MEMORY.md` 超过 `EntryBloatLines` 时向提案 `warnings` 追加提示而不阻断。不改动 `--runner`/`--worker` 远端路径、`apply` 语义与任何写入契约。
- 对应契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)、[01. 架构与模块](../01-architecture-and-modules.md#32-文件边界)、[04. CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)、[05. 验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)、[09. MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集)。
- 验证时间与环境：2026-10-20，darwin/arm64，Go 1.27.1，Node v26.10.0。
- 交付：
  1. `internal/vault/write.go` 新增 `Root.ListDirs`：按与 `ListManaged` 相同的排除规则列出受管目录，跳过 `.git`、`.github`、临时文件与符号链接。
  2. `internal/dream/export.go` 的 `baseline` 新增 `dirs` 集合并由 `exportBaseline` 填充，使父目录判定使用与 `apply` 相同的锁定基线。
  3. `internal/dream/propose.go` 新增 `verifyParentDirs`（只检查基线中不存在、且父目录不在 `dirs` 中的目标，返回 `NOT_FOUND`）、把 `verifyTargetLinks` 扩展为结构守卫（链接问题加新增不可达文件，返回 `UNSUPPORTED` 并标注 `ORPHAN_FILE <path>`），并在 `buildChanges` 之后按结果树的 `MEMORY.md` 行数追加膨胀警告。
  4. 文档与文案：`docs/12` §2.3 步骤 3、步骤 2 的守卫说明与 `D07` 验收、`docs/04` 的 `dream --propose` 行、`docs/05` 的 `D07` 行、`docs/09` 与 MCP 工具描述、`skills/memctl/SKILL.md` §6.2/§6.4、`memctl help dream` 的注意事项与中英测试一并更新。
- 验证：
  - `go test ./internal/... -count=1` 全部通过。新增用例：`TestProposeLocalRefusesMissingParentDirCoversD07`（拒绝后补齐目录再通过）、`TestProposeLocalRefusesNewOrphanCoversD07`（新文件被拒、基线已有的孤立文件不阻断无关提案）、`TestProposeLocalWarnsOnEntryBloatCoversD07`（超阈值追加警告、短文件不警告）与 `internal/vault` 的 `TestListDirsSkipsGitAndSymlinks`。
  - `tests/integration` 全量 144 项通过（`go test ./tests/integration -count=1 -v`，185 个 `--- PASS` 含子用例）；`TestH3ProposeRejectsMissingAssembledLineCoversD07` 扩为五个子用例并按各守卫的错误码断言；`TestH3ApplySkipsChangedDigestCoversD08` 的提案补上 `MEMORY.md` 索引，以符合新的结构守卫。
  - `go vet ./...`、`gofmt` 与 `git diff --check` 通过；`scripts/test-shards.txt` 与全量用例名单比对无增删，无需重新生成。
- 未验证与遗留：
  - 本轮未在真实宿主会话内重跑 `prepare` → `propose` → `apply`；三条守卫由隔离单元测试与真实二进制集成测试覆盖。
  - `stage-h.md` 存在两节 `T102` 重复，属 `T102` 与 `T103` 归档时的既有重复，本轮未合并。

## T115：CodeBuddy 与 WorkBuddy 会话格式接入（`H-2c`）

- 目标与范围：为 `internal/corpus` 增加两个宿主的会话蒸馏。两者共用 CodeBuddy 会话格式，因此使用一个解析器、两个 agent 名；补齐实时采集、按 `sid` 定位、存量补扫、宿主回显过滤、毫秒时间戳与工具签名脱敏。不发布新版本，不改动 `memctl sync`/`end` 的命令行契约与既有的本地整理协议。
- 对应契约：[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#22-本地会话采集与确定性蒸馏internalcorpus)、[05. 验收与交付](../05-validation-and-delivery.md#阶段-h-的实施顺序)。
- 验证时间与环境：2026-10-21，darwin/arm64，Go 1.27.1。
- 交付：
  1. 新增 `internal/corpus/codebuddy.go`：读取每行的 `sessionId`/`cwd` 与毫秒时间戳，忽略 `session-meta` 的对话语义但取其会话 ID；保留 `input_text`/`output_text` 与 `function_call` 的工具名和参数目标；丢弃 `reasoning`、工具输出、`providerData.skipRun` 回显及 `<local-command-*>`、`<command-name>`、`<command-args>`、`<task-notification>` 等宿主消息；`summary` 标记 `truncated`；用户消息先剥离注入块再取 `<user_query>` 正文。
  2. `internal/corpus/collect.go` 把 `codebuddy`、`workbuddy` 注册到同一解析器。
  3. `internal/corpus/session.go` 增加按 `sid` 定位；`internal/corpus/backfill.go` 增加 `$HOME/.codebuddy/projects` 与 `$HOME/.workbuddy/projects` 两条按项目预过滤的补扫；新增保留 `_`、空格与非 ASCII 的 `codeBuddyDir` 转义。
  4. `internal/corpus/redact.go` 与 `codex.go` 修正带属性的 `<system-reminder ...>`：此前 `data-role="user-context"` 这类形式既不被清理也不被前缀判定识别，WorkBuddy 的首条消息会整条丢失或泄漏注入块。
  5. `internal/corpus/digest.go` 的 `boundTools` 对工具签名执行 `Redact`，避免命令参数中的凭据进入摘要。
  6. 测试：`corpus_test.go` 覆盖两个宿主的摘要、`session-meta`、`summary`、毫秒日期、回显过滤、`<user_query>` 与工具脱敏；`backfill_test.go` 的 `backfillHome` 扩到七个宿主，补跨项目过滤、目录转义与 `LocateSession`。
  7. 文档：`docs/12` §2.2.1、§2.2.2、§2.2.4、§2.2.5 与 §6.2 的 `H-2c`；`docs/05` 阶段 H；本文件与 `STATUS.md`。
- 验证：
  - `go test ./internal/corpus ./internal/dream ./internal/cli` 通过；`make test` 全绿，`tests/integration` 241.7 秒、`vitest` 104 项、两个插件 Node 测试通过。
  - 真实 CodeBuddy 日志只读探针：13 个回合、日期 2026-10-21，`reasoning` 与工具输出未进入摘要；真实 WorkBuddy 日志的首条消息正确剥离带属性的注入块并取出 `<user_query>` 正文。
  - 真实 `~/memory` 仓库运行 `dream --prepare`：12 个候选中含 3 个 `codebuddy` 会话（`cwdBase=memctl`），补扫链路贯通。
  - 真实 Hook 复验：临时安装本机构建的二进制后，CodeBuddy `2.164.0` 的 headless 会话触发 `SessionStart`（`start --agent codebuddy`，3.16 秒、退出码 0）与 `Stop`（`sync --agent codebuddy`），生成含提问与回复的摘要；WorkBuddy `5.8.3` 的 sidecar 以 `CODEBUDDY_CONFIG_DIR=$HOME/.workbuddy` 启动，触发同名 Hook（`start` 8.5 秒、`sync`；sidecar 未登录，`Stop` 原因为 failed），仍生成摘要。两份摘要归档到 `/tmp/hook-verification/` 后从候选缓存移除，测试会话记录已删除，安装的二进制已还原为 `v0.11.1`。
- 未验证与遗留：
  - `-p` 与 sidecar 形态均不触发 `SessionEnd`；交互式退出的 `end` 未复验。
  - WorkBuddy 桌面 GUI 内的 Hook 行为未复验，本轮使用其 sidecar。
  - `CODEBUDDY_CONFIG_DIR` 指向其他配置根时补扫仍只查 `$HOME`；Windows 盘符与分隔符转义未验证。
  - 工具签名不记录退出状态，与现有解析器一致。

## T120：记忆写入位置契约收紧（项目事实下沉 `notes/`）

- 目标与范围：修掉「所有记忆都写进 `MEMORY.md`」的根因。不是代码缺陷：写入路径没有默认值，是契约允许 event 型条目留在入口文件、注入面只有 `MEMORY.md`、唯一护栏在 100 行才触发。本轮按 2026-10-27 用户确认收紧口径。
- 改动：`docs/10` §1 结论、§3.2 新增「写入位置」规则、§7 的事件型条目决策改为「一律写入 `notes/`，入口文件只保留索引与跨项目偏好」；`internal/assets/INSTRUCTIONS.md` 的 Writing 段改为同一规则；`skills/memctl` 的 `SKILL.md` 与 `references/memory-conventions.md` 同步；`help` 的快速入门说明与 `write` 示例改以 `notes/<topic>.md --base absent` 演示；`tests/integration/r01_r02_test.go` 增加 `cross-project preferences` 与 `notes/<topic>.md` 断言。
- 对应契约：[10. 记忆更新与过期 §3.2](../10-memory-lifecycle.md#32-更新规则)、[§7](../10-memory-lifecycle.md#7-已确认的决策与剩余问题)、[04. CLI、UI 与分发 §2](../04-ui-and-cli-specification.md#2-环境变量与会话身份)。
- 验证时间与环境：2026-10-27，darwin/arm64，Go 1.27.3。
- 验证命令与结果：`go test ./...` 全绿（`internal/app` 60.2 秒、`tests/integration` 207.1 秒）；`go vet ./internal/cli` 通过。
- 未验证与遗留：`T118` 的重启后真实验证证据已归档到 `T119`（提交 `c7a9e03`）；新指引要随下一次二进制发布才会进入宿主注入，本机 `v0.13.0` 仍是旧文本；本机 `~/.agents/skills/memctl` 是 2026-10-19 的安装副本，需重新安装才与仓库一致；`doctor`/`propose` 的入口膨胀检查仍只在 100 行时告警，未增加「同一项目条目数」提示。

## T117：批量整理入口规则写入内嵌默认指引

- 目标与范围：把批量整理的入口规则写入内嵌默认指引，避免 Agent 在「整理记忆」时直接用 `memctl write` 改写主库。只改注入文本、测试断言与契约描述，不改变命令、协议或提案机制。
- 改动：`internal/assets/INSTRUCTIONS.md` 新增「Organizing memory」小节：单条事实仍按 Writing 规则写入；整理、去重、重构与跨会话提炼先执行 `memctl dream --prepare`，再用 `--propose` 提交完整目标文件，只持久化 `ready` 提案；展示提案 id、`patchHash` 与 Diff 后等待用户审查，Agent 不自行 `--apply`，MCP 没有 apply 工具。`tests/integration/r01_r02_test.go` 增加标题与关键句断言；`04` 第 6 节、`05` 的 `R01`、`10` 第 4/6 节与 `12` §2.3 同步该规则；`skills/memctl` 原本已有同一流程，未改动。
- 对应契约：[04. CLI、UI 与分发](../04-ui-and-cli-specification.md#6-单文件打包)、[05. 验收与交付](../05-validation-and-delivery.md#2-核心测试矩阵)、[10. 记忆更新与过期](../10-memory-lifecycle.md#4-当前实现边界)、[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)。
- 验证时间与环境：2026-10-21，darwin/arm64，Go 1.27.1。
- 验证命令与结果：`go test ./tests/integration -run TestR01EmbeddedInstructionsFromEmptyHome -count=1` 通过（3.7 秒）；`make test` 全绿（`tests/integration` 254.6 秒，前端与两个插件测试通过）。
- 未验证与遗留：断言只证明注入文本包含关键句，模型是否始终遵守仍依赖运行时判断；本机 `~/.agents/skills/memctl` 仍是 2026-10-19 的旧版（无 §6），需重新安装后 Skill 侧指引才会与仓库一致。该记录在提交 `82ce51b` 中被覆盖，`T122` 补回归档。

## T122：允许并引导主题文件之间的 `[[path]]` 互链

- 目标与范围：让 Agent 在生成新记忆时知道「可以且应该」建立主题文件之间的 `[[path]]` 链接。此前所有指引只说「在索引中增加一行」，读下来等价于「只能从 `MEMORY.md` 指向主题文件」，所以图谱长期是放射状。本轮只改契约文案与 prompt，不改解析、可达性或校验逻辑。
- 改动：`docs/10` §1 增加跨主题互链结论、§3.2 第 3 条写明两个主题文件之间同样不得各存一份、§6 增加两条验收场景；`docs/11` 阶段 B 的「格式与安全边界」加入互链要求；`docs/12` §2.3 说明互链由 Agent 在 `files[]` 中给出、Go 不代生成也不把互链当作孤立标记；`docs/05` 的 `R01` 增加互链规则；`internal/assets/INSTRUCTIONS.md` 的 Writing 段补充「A link target is any managed file, not only `MEMORY.md`」；`skills/memctl/SKILL.md` §2 与 `references/memory-conventions.md` 同步；`internal/dream/worker.go` 的 stage B prompt 增加互链规则、stage C prompt 增加「不得删除既有的主题互链」。
- 对应契约：[10. 记忆更新与过期 §3.2](../10-memory-lifecycle.md#32-更新规则)、[11. GitHub Actions Dreaming §2.3](../11-github-actions-dreaming.md#23-代码控制的多步结构化调用流程)、[12. 跨会话新记忆提炼 §2.3](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)、[02 §1.2 可达性定义](../02-domain-models-and-contracts.md#12-格式保真)、[05 的 `R01`](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证时间与环境：2026-10-27，darwin/arm64，Go 1.27.3。
- 验证命令与结果：`go test ./...` 全绿（`internal/app` 75.9 秒、`internal/dream` 33.8 秒、`tests/integration` 227.1 秒）；`go vet` 通过。写盘实测：在临时仓库让 `notes/b.md` 链接 `notes/a.md` 后 `dream --propose` 返回 `ready`，证明互链不被结构守卫拦截。
- 本机记忆库：`~/memory` 的 6 个笔记文件各补一条指向相关主题的 `[[path]]`，链接总数从 11 增至 19；写入经受管事务提交，`doctor` 无新增诊断。
- 未验证与遗留：新 prompt 与内嵌指引要随下一次二进制发布才进入宿主注入与远端 worker；本机 `~/.agents/skills/memctl` 仍是旧副本，需重新安装；互链仍是建议而非硬约束，没有新增「缺少互链」类诊断（本轮按用户选择不做）。

## T123：接入 Delta 宿主（`X01`–`X03`）

- 目标与范围：让 Delta 成为受支持宿主。Delta 没有生命周期 Hook，也不接受本地 stdio MCP server，因此不能沿用 DSH/pi 的插件路线，也不能沿用 `memctl mcp`；本轮只补齐「Agent 主动调用 CLI」这条路径上缺的两块：会话身份与宿主探测。不新增命令、工具或存储路径。
- 调研结论：Delta 只支持远端 HTTPS MCP server（OAuth 2.1 或无鉴权），二进制中无 `SessionStart`/`UserPromptSubmit`/`PreToolUse`/`hooks.json`/`mcpServers`；`delta integration register` 的唯一取值是 `claude-code`，方向是把外部会话记入 Delta；官方《Agentic Safety》写明不对 Agent 施加沙箱，因此 CLI 直连 `~/memory` 可用。Delta 读取 `~/.agents/skills/`，本项目的 Skill 已生效；`npx skills add` 的宿主名是 `zed`，`delta` 会被拒绝。
- 改动：`internal/hookio.ResolveSID` 在 `DSH_SESSION_ID` 与 Hook JSON 之间加入 `DELTA_CURRENT_THREAD_ID`；`internal/adapters` 新增只探测、不写文件的 `delta` 适配器（`HostMinVersion 0.23.0`、`NoConfigReason`、`Instructions`），`planSpec` 增加「零 artifact 宿主」分支，避免落到 `not_installed`；`internal/cli/install.go` 打印 `step:` 行；`internal/mcp/status.go` 为 Delta 输出「不支持本地 stdio」的说明；`internal/cli/help_data.go` 同步 SID 回退链、`install` 的适配器列表与说明；新增 `docs/13-delta-integration.md`，同步 01/02/03/04/05/09/12、`docs/README.md`、`README.md`、`skills/memctl` 与 `internal/assets/INSTRUCTIONS.md`。
- 对应契约：[13. Delta 宿主接入](../13-delta-integration.md)、[04 §2 环境变量与会话身份](../04-ui-and-cli-specification.md#2-环境变量与会话身份)、[02 §2.3](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[03 §7 Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[05 `X01`–`X03`](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证时间与环境：2026-10-31，darwin/arm64，Delta 0.23.0（CLI 契约 `min=8,max=8`），Go 1.27.3。
- 验证命令与结果：`go build ./...`、`go vet ./...` 通过；`go test ./...` 全绿（`tests/integration` 191.8 秒）；`web` 侧 `npm test` 18 个文件 104 项通过；`plugins/dsh` 与 `plugins/pi` 的 Node 单元测试通过。新增测试覆盖 `X01`–`X03`：`TestDeltaThreadIDProvidesTheSessionIdentity`（`tag`、`context --stdin` 与不带 `--sid` 的 `write` 都取得线程 ID，提交含 `Session:` trailer）、`TestDeltaIsDetectedWithoutWritingAnyConfig`、`TestDeltaAdapterStatusReportsNoManagedConfig`、`TestM09InstallStatusReportsMCP` 的 Delta 条目与 `hookio` 的优先级单测。
- 真实环境复核：本机 `./bin/memctl install --status` 输出 `delta: configured host=delta@0.23.0` 与三条 `step:`，`mcp:` 块为 Delta 输出不支持说明；在本轮 Delta 终端内（未传 `--sid`）执行 `tag --agent delta` 得到 `agent://MacBookPro.local/delta/<thread>`，对探针仓库执行 `write` 后提交说明含该线程的 `Session:` trailer。
- 未验证与遗留：真实 Delta 会话内的端到端执行（模型是否按 Skill 主动写入、审计视图是否按线程追溯）未验证，本轮验证的是命令与适配器行为；只在 darwin/arm64 与 Delta 0.23.0 上验证；`internal/corpus` 的 Delta 会话蒸馏未实现，原因是 CLI 不暴露工作目录，无法满足先按项目收窄的规则；Delta 无 `end`，会话摘要只在主动 `sync` 时产生。

## T124：发布 `v0.14.0`

- 目标与范围：发布 `v0.14.0`，把 `T123` 的 Delta 宿主接入带入 Release。版本号按次版本递增：新增 `DELTA_CURRENT_THREAD_ID` 会话身份回退、`install` 的 `delta` 适配器与 `mcp:` 状态说明，属向后兼容的能力增量；没有命令参数、接口或数据格式变化。插件源码未改动，tarball 保持 `memctl-dsh 0.5.3` 与 `memctl-pi 0.5.2`，本轮不发布 npm。
- 对应契约：[13. Delta 宿主接入](../13-delta-integration.md)、[04 §6.1 发布入口](../04-ui-and-cli-specification.md#61-发布入口)、[05 §4 发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证时间与环境：2026-10-31，darwin/arm64，Delta 0.23.0，Go 1.27.3，Node v26.12.1 / npm 11.20.0。
- 发布前检查：`make test` 全绿（前端 18 个文件 104 项、`plugins/dsh` 与 `plugins/pi` 的 Node 测试、`tests/integration` 194.7 秒）；`scripts/balance-test-shards.sh` 重新生成 `scripts/test-shards.txt`，146 个集成测试全部有分片归属、无遗漏无冗余（新增 `TestDeltaThreadIDProvidesTheSessionIdentity` 与 `TestDeltaAdapterStatusReportsNoManagedConfig`）；`make release` 验证构建链路产出四个候选二进制与两个插件 tarball，本地产物不参与发布校验。
- 发布：推送附注标签 `v0.14.0`，[运行 41179594733](https://github.com/gray0128/memctl/actions/runs/41179594733) 的 `verify`、`build` 与 `publish` 全部成功，创建 [Release v0.14.0](https://github.com/gray0128/memctl/releases/tag/v0.14.0)，发布准备提交 `89b2e1c`。
- 附件与校验：独立目录 `/tmp/memctl-v0.14.0` 内 `shasum -a 256 -c SHA256SUMS` 六项 OK，Release 共九项附件；`RELEASE-INFO.txt` 记录 `version: v0.14.0`、`commit: 89b2e1c…`、`go1.27.1 darwin/arm64`；产物 `memctl-darwin-arm64 version` 为 `memctl v0.14.0`，`install --status` 输出 `delta: configured host=delta@0.23.0`。
- 本机升级：`mv` 原子替换 `~/.agent-memory/bin/memctl`；`version` 为 `memctl v0.14.0`，`update --check` 返回 `up to date`，`install --status` 为 `codex@0.160.0`、`pi@1.4.0 plugin@0.5.2`、`dsh@0.2.1-rc.5 plugin@0.5.3`、`delta@0.23.0`。
- 未验证与遗留：真实 Delta 会话内的端到端执行仍未验证（模型是否按 Skill 主动写入、审计视图是否按线程追溯）；平台范围与上一版相同，仅 darwin/arm64 运行时验证；`internal/corpus` 的 Delta 会话蒸馏仍未实现，其 cwd 方案可行性见 `T125` 与 [14](../14-delta-cwd-feasibility.md)。

## T125：Delta 会话蒸馏的 cwd 方案可行性调研

- 目标与范围：调研 Delta 会话蒸馏能否用「当前工作目录」确定项目归属，输出可行性报告。只做调研与文档，不实现采集器。
- 结论：cwd 方向可行，但必须先把 cwd 归一化到项目根；直接拿进程 cwd 当项目键会按线程碎片化。
- 实测证据：本机 Delta 0.23.0 上，同一仓库 `memctl` 的原仓库与两个线程 checkout 得到 3 个不同 `cwdKey`；归一化到项目根后三者收敛为同一 key，且与既有 57 条 digest 记录的 `cwdKey` 逐字节相同。归一化有两条互相独立的规则（`--git-common-dir` 截断与 `--show-toplevel` 截断），3 个真实样本结论一致。另有 202 份现有 digest、6 个 Delta 线程的样本量。
- 差距：cwd 只解决项目归属。Delta 没有会话日志文件（只有 DeltaDB 与 CLI，官方要求不直读 DB）、`thread show` 单次最多 20 条且无总数字段、assistant `buffer` 混入 `Thought N times…` 与 `Thinking` 段而 `MaxTurnTextBytes = 1024` 可能把正文整段截掉。后两项已在 `T126` 中被实测推翻。
- 改动：新增 [14. Delta cwd 可行性](../14-delta-cwd-feasibility.md)（结论、实测表格、两阶段推荐方案、6 项风险、`X10`–`X13` 验收建议）；`docs/README.md` 与 `docs/01` 目录清单登记；`docs/12` §2.2 的 `delta` 行改为指向该报告。
- 对应契约：[14. Delta cwd 可行性](../14-delta-cwd-feasibility.md)、[12 §2.2](../12-session-corpus-dreaming.md#22-本地会话采集与确定性蒸馏internalcorpus)、[13 §7](../13-delta-integration.md#7-未验证与后续)。
- 验证时间与环境：2026-10-31，darwin/arm64，Delta 0.23.0，Go 1.27.3。
- 验证命令与结果：文档链接与锚点全量校验通过（`docs/14`、`docs/12`、`docs/README.md` 均 0 问题）；实测命令见报告中各表格，均可复跑。
- 未验证与遗留：样本仅本机 6 个线程，未覆盖 monorepo 与 Windows 路径；采集器未实现；当前线程仍须 `env -u DELTA_CURRENT_THREAD_ID` 才能读出（该判断已由 `T126` 推翻）。
