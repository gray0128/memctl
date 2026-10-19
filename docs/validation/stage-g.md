# 阶段 G 归档

本文件按任务归档阶段 G（GitHub Actions Dreaming 迁移）的完成记录。阶段定义与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-g-的实施顺序)，方案与协议见 [11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)。

## T083：GitHub Actions Dreaming 代码迁移（`G-2`、`G-3`）

- 目标与范围：把阶段 D 的本地 CLI runner 与 macOS Seatbelt 沙箱执行路径整体替换为 GitHub Actions 远端整理，落实 [11. GitHub Actions Dreaming](../11-github-actions-dreaming.md) 的分支协议、三阶段有界结构化 worker 与四家内置模型提供商，并同步 CLI 帮助与 Web UI。范围覆盖 `G-2`（`.github` 边界、worker、分支编排）与 `G-3`（`--init-workflow`、帮助、UI）；`G-4` 真实 GitHub Actions 端到端不在本轮。
- 契约：[11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)、[架构与模块](../01-architecture-and-modules.md#32-文件边界)、[模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#阶段-g-的实施顺序)。
- 验证日期与环境：2026-10-19，macOS arm64，Go 1.27.1，Node v26.10.0。
- 实现：
  1. 删除 `internal/dream` 的本地 runner、Seatbelt 沙箱与进程组工具（`run.go`、`runner.go`、`sandbox*.go`、`proc_*.go`），`export.go` 改为在内存中导出基线，不再落地临时执行目录。
  2. `internal/vault` 把 `.github` 与 `.git` 同等纳入路径边界：`ValidateRelPath` 拒绝、`ListManaged` 跳过，常规读写、MCP 与 UI 编辑无法触达工作流文件。
  3. `internal/dream/provider.go` 实现 `deepseek`（`json_object`）、`minimax`（非流式 + 围栏剥离）、`openai`（`json_schema` strict）、`anthropic`（`tools` + 强制 `tool_choice`）四个提供商，仅用 `net/http`，错误码区分鉴权、限流与网络失败，响应正文写入前脱敏。
  4. `internal/dream/worker.go` 实现三阶段流程：静态候选（`ENTRY_BLOAT`、`ORPHAN_FILE`、死链、缺失元数据、到期 `until`、重复条目）→ 分批结构化调用与 Step Guard（Schema、路径、批次范围、链接新增问题、有界重试）→ 索引对齐；事件型条目规则写入固定 system prompt。
  5. `internal/dream/remote.go` 实现 `dream/req`/`dream/res` 协议：推送基线、`ls-remote` 探测、单 ref 拉取到 `refs/memctl/dream/<id>`、父提交唯一性校验、结果树在内存中按不可信输入重算 Diff 与 `patchHash`、终态清理远端与本地临时引用。
  6. `internal/dream/workflow.go` 实现 `--init-workflow`（幂等写入并提交内嵌模板）与 `--worker`（校验 checkout 等于基线、运行 worker、以固定机器人身份提交空树结果并推送 `dream/res`）。
  7. CLI `dream` 扩为五种互斥模式，`--runner` 限定四家提供商；帮助条目（中英与 JSON）同步；Web UI 提供商选择器改为四家提供商，就绪状态改由 `memctl-dream-sandbox` 与新增的 `memctl-dream-reason` meta 驱动，未就绪时给出 `dream --init-workflow` 提示。
- 验证：`go test ./...` 全量通过（含 `internal/dream`、`internal/app`、`internal/server`、`internal/cli` 与 `tests/integration`）；`go vet ./...` 无输出；`cd web && npm test`（86 项）通过；`npm run build` 通过；全量 Markdown 相对链接与锚点检查无失效项；`git diff --check` 无输出。对应验收：`D01`（未配置远端、未确认私有、缺少工作流、未知提供商均拒绝且不推送分支）、`D02`（脏库拒绝生成）、`D03`（父提交不等于基线、修改 `.github`、删除 `MEMORY.md`、超出批次均拒绝并置 `failed`）、`D04`（重复 apply 幂等、`--init-workflow` 幂等）、`D05`（静态候选随批次下发、单次非法 JSON 自动纠正、提供商失败空提交回传且密钥脱敏）、`P01`（`.github` 别名被路径边界与 `ListManaged` 同时拒绝）。
- 未验证与遗留：`G-4` 真实私有 GitHub 仓库 + 真实模型端点的端到端运行；Windows/Linux 上的 Git 引用转发与 `git` 命令行为只经交叉编译检查；`memctl-dsh` 与 `memctl-pi` 未涉及。
- 下一步：按 `G-4` 在真实私有 GitHub 记忆仓库配置 Actions Secret，跑通生成、拉取、Diff 审阅与 `apply` 全链路并归档证据。
