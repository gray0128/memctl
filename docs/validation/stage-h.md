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
