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
  11. 按本机实测更正宿主日志路径与格式：`dsh` 为 `~/.dsh/sessions/<转义目录>/session-<sid>/session.v4.jsonl.zstd`（zstd 压缩，需引入 zlib 之外的依赖或由插件解压）；`opencode` 无 `.jsonl` 日志，标记为未支持；`claude-code` 从项目目录名还原 `cwdBase`；补齐 `codex`、`pi`、`antigravity` 的字段层级。
  12. 阶段计划拆分为 `H-2a`（四个已核对宿主）与 `H-2b`（存量补扫与其余宿主探测），并新增 `D08`（摘要变化与写回中断）验收场景。
- 验证：`git diff --check` 无空白或冲突标记错误；`docs/*.md` 与根 `README.md` 相对路径链接及标题锚点核对全部可达；`docs/12` 的宿主路径与格式对照本机 `~/.dsh`、`~/.claude`、`~/.codex`、`~/.pi`、`~/.gemini/antigravity` 实测样本核对；全库检索确认不再有 `memory_dream_apply`、`20 个会话`、`200 行`、`20 个未消费` 等旧表述。
- 未验证与遗留：`dsh` 会话日志的解码路径（引入 `klauspost/compress/zstd` 或由插件解压）尚未定案，列入 `H-2a` 实现前决策；`D08` 依赖 `H-3` 的消费写回实现。
