# 阶段 I 归档

本文件按任务归档阶段 I（记忆架构止血与读侧、写原语演进）的完成记录。阶段定义与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-i-的实施顺序)，审查基线与分阶段方案见 [18. 实现审查与记忆架构优化方案](../18-agent-memory-review-and-optimization.md)。

## T138：P0 读写生命周期止血与工程闸门（`I-1a`）

- 状态：已完成（对应阶段 I 的 `I-1a`）。
- 目标与范围：同步 [18. 实现审查与记忆架构优化方案](../18-agent-memory-review-and-optimization.md) 索引并登记阶段 I 实施顺序（`I-1a`–`I-4`）；完成 `I-1a` 的 P0 读写生命周期止血、宿主一致性与工程闸门修复，以及 `docs/validation/` 历史失效锚点修复。
- 对应契约：[18. 实现审查与记忆架构优化方案](../18-agent-memory-review-and-optimization.md)、[05. 验收与交付](../05-validation-and-delivery.md#阶段-i-的实施顺序)、[09. MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集)、[10. 记忆更新与过期](../10-memory-lifecycle.md)、[12. 跨会话新记忆提炼](../12-session-corpus-dreaming.md#23-本地当前-agent-交互式整理协议prepare--propose--apply)。
- 主要变更：
  1. **到期占位符自动还原与整文件写 30 天删除闸门（`P0-1`）**：新增 `internal/app/expiredwrite.go`（`reconcileExpiredWrites`），在 `App.Apply` 的 `session.plan` 之后拦截或还原 Markdown 文件中的到期占位符（与磁盘到期条目投影逐字节匹配的按磁盘顺序还原，捏造、重复、篡改元数据或新建文件含占位符返回 `INVALID_ARGUMENT`），并复用 `expiredRemovals` 对删去或改写隐藏期内到期条目（含整文件删除）统一返回 `DELETION_DEFERRED`，覆盖 `write`、`memory_write`、`PUT /api/v1/files` 与 `ApplyDream`。
  2. **锁忙注入回退隐藏投影（`P0-2`）**：`internal/app/session.go` 的 `readEntryWithoutLock` 改为调用 `a.projectRead`，确保 `BUSY` 回退注入路径同样隐藏已过 `until` 日期的条目正文。
  3. **OMP 会话定位、MCP `memory_dream_prepare` 可选 `sid` 与中文帮助同步（`P0-6a`）**：`internal/corpus/session.go` 的 `LocateSession` 新增 `omp` 分支（复用 `piSessionStore`）；`internal/mcp/tools.go` 为 `memory_dream_prepare` 增加可选 `sid` 参数（服务器 `--sid` 优先，非法值返回 `INVALID_ARGUMENT`）；`internal/cli/help_data.go` 中文宿主列表补齐 `omp`、会话环境变量说明补齐 `PI_SESSION_ID` 与 `DELTA_CURRENT_THREAD_ID`；新增 `internal/corpus/hosts_test.go`（`TestEveryParsedHostIsLocatedAndSwept`）校验包内 `parsers`、`LocateSession` 与 `Backfill`（`delta` 除外）一致。
  4. **`init` 创建空 `notes/` 目录、报错文案与 `gofmt` 闸门（`P0-6b`）**：`internal/app/app.go` 在 `init`（新建及空远端首提交）时创建空 `notes/` 目录（不创建 `.gitkeep`，首提交仅 `MEMORY.md`）；`internal/vault/write.go` 的 `RequireParentDir` 缺父目录报错修正为 `parent directory "<dir>" does not exist`（`NOT_FOUND`）；格式化 `internal/corpus/corpus.go` 并新增 `internal/app/gofmt_test.go`（校验 `cmd`、`internal`）。
  5. **文档索引、阶段 I 计划与历史失效锚点修复**：更新 `docs/README.md`、`docs/01-architecture-and-modules.md`、`docs/05-validation-and-delivery.md`、`docs/09-mcp-memory-tools.md`、`docs/10-memory-lifecycle.md`、`docs/12-session-corpus-dreaming.md`、`docs/18-agent-memory-review-and-optimization.md` 及 `docs/validation/`（`stage-a-review.md`、`stage-c-review.md`、`stage-d-review.md`、`stage-g.md`、`stage-h.md`）。
- 验证日期与环境：2026-11-11，`darwin/arm64`，Go `1.27.2`，Node `v26.11.1`。
- 执行的命令与结果：
  - `gofmt -l cmd internal tests`：输出为空；`go vet ./...`：通过。
  - `go test -count=1 ./internal/... ./cmd/...`：全部通过（`31.4s`，其中 `internal/app` `30.7s`），含新增与重写的 `TestWriteAfterHideKeepsExpiredEntry`、`TestWriteRefusesUnmatchedPlaceholder`、`TestWholeFileWriteRespectsHiddenPeriod`、`TestBusyInjectionHidesExpiredEntries`、`TestEveryParsedHostIsLocatedAndSwept`、`TestDreamPrepareSessionArgument`、`TestRequireParentDirNamesMissingDirectory`、`TestGoSourcesAreGofmtFormatted`。
  - `make test`（将 Node 26 构建基线置于 `PATH` 最前）：退出码 `0`（`web` 单元测试 `104/104`、`dsh` 插件 `18/18`、`pi` 插件 `19/19`、Go 各包与集成测试全部通过）。
  - 去缓存复跑 `go test -count=1 ./tests/integration/ ./plugins/...`：全部通过（`tests/integration` `103.2s`，总计 `1m43.7s`）；`scripts/test-shards.txt` 与 `tests/integration` 的 `151` 个测试函数完全匹配。
  - 全库 Markdown（`README.md`、`AGENTS.md`、`docs/**/*.md`）相对链接与锚点校验：`0` 错误；`git diff docs/` 新增行黑话扫描：`0` 命中；`git diff --check`：通过。
- 已通过的验收 ID：`F04`、`W01`、`G01`、`H06`、`M02`、`M14`、`D07`、`R06`、`V-OMP-04`。
- 未验证项与已知边界：跨包 `adapters.Registry()` 与 `corpus`、`mcp/status.go` 的宿主注册表一致性测试在 `I-1b`（`T139`）补齐；`expiredRemovals` 按原始行集合比对（同文件两条字节完全相同的到期行只删一条时不拦截，沿用 `dream` 既有语义）；任意行（含代码块内）含未匹配占位符子串时保守拒绝为 `INVALID_ARGUMENT`。
