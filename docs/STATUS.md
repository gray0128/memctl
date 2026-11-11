# 开发状态

本文件是开发任务状态、当前事实与近期事项的热台账，按归一化换行后的总长度受 **`12,000` UTF-8 字符**硬上限约束（由 `internal/app/status_budget_test.go` 与 `scripts/next-round.sh` 强制校验）。状态含义与归档规则见 [开发循环规范](./06-development-loop.md#1-最小记录结构)，阶段计划见 [验收与交付](./05-validation-and-delivery.md#1-交付顺序)，历史归档与审查报告见 [历史阶段归档索引](#历史阶段归档索引t001t146)。

## 当前事实

- **版本与发布**：当前版本 `v0.17.0`（2026-11-11 发布，含阶段 I `I-1a`–`I-4`；历次演进见阶段归档与 [Releases](https://github.com/gray0128/memctl/releases)），插件 `memctl-pi@0.6.0`、`memctl-dsh@0.5.3`。发布物含四平台（`darwin/arm64`、`darwin/amd64`、`linux/amd64`、`windows/amd64`）二进制、双插件 tarball、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`。唯一发布入口是推送 `v*` 标签（CI 构建并经 `npm-publish` OIDC 发布未上架插件），本地 `make release` 仅验证构建，见 [04 §6](./04-ui-and-cli-specification.md#6-单文件打包)、[§6.1](./04-ui-and-cli-specification.md#61-发布入口) 与 [05 §4](./05-validation-and-delivery.md#4-发布前检查)。
- **已交付核心能力**：阶段 A–I 全部交付并验收，权威契约见 [方案索引](./README.md) 下的 `00`–`18`（规范图以 [00](./00-goal-and-requirements.md) 为根，`06`、`14`、`16`、`18` 与本台账不进图）：24 个 CLI 子命令与中英/JSON 帮助（[04](./04-ui-and-cli-specification.md#1-cli-命令)）；6 个 MCP 工具（`memory_read`、`memory_entries` 标 `readOnlyHint: true` 与 `destructiveHint: false`，其余 4 工具无注解，见 [09](./09-mcp-memory-tools.md)、[16](./16-mcp-tool-annotations.md)）；Web UI 七视图；锁内无基线追加（`add`/`memory_append`，记录 `Session-Origin` 并同事务维护 `MEMORY.md` 索引）、块级 `DetailLines`、分号转义、稳定 `EntryID`、到期隐藏与 30 天 `DELETION_DEFERRED`、`EVENT_PROTECTED` 守卫及 `UNCONFIRMED_HYPOTHESIS` 提醒（[10](./10-memory-lifecycle.md)、[18](./18-agent-memory-review-and-optimization.md)）；`BUSY` 时 `refs/heads/<branch>` 无锁快照只读回退、`200` 行 / `25 KiB` 注入预算与当前项目记忆预加载；中英文分词 BM25、一跳邻居 `relatedFiles`、`--project` 与 `--include-transcripts` 检索；`corpus` 八宿主蒸馏/补扫与 `delta` 采集、本地 `dream --prepare`/`--propose`（含 `operations[]`；远端轨道维护冻结）及 `TOPIC_BLOAT`/`NEAR_DUPLICATE` 诊断（[11](./11-github-actions-dreaming.md)、[12](./12-session-corpus-dreaming.md)、[15](./15-delta-corpus-adaptation.md)）。
- **平台与宿主验证状态**：`darwin/arm64` 完成运行时全量验证，其余三平台仅交叉编译通过。宿主侧：DSH `0.2.1-rc.2`–`rc.5` 完成生命周期、降级、UI 安装与 MCP 验证，本机 `desktop` 为 `0.5.3`（[T052](./validation/t052-dsh-real-session.md)、[T096](./validation/t096-dsh-plugin-0-5-2-verification.md)、[07](./07-dsh-plugin.md#13-实现状态)）；pi `1.3.0`/`1.4.0` 完成全生命周期验证（[T062](./validation/t062-pi-real-session.md)、[08](./08-pi-plugin.md#11-实现状态)）；OMP `18.17.0`–`18.18.8` 已验 `--agent omp`、`systemPrompt` 回退、蒸馏及 MCP 6 工具（[17](./17-omp-integration.md#6-实现状态与验证记录t134)）；CodeBuddy `2.164.0` 与 WorkBuddy `5.8.3` 已验 hooks 与蒸馏；Delta `0.23.0` 已验身份、探测与 `thread watch` 采集（[13](./13-delta-integration.md)）；Antigravity 已验 `SessionStart` 仪表盘注入与 `brain` 补扫；MCP 受限写入覆盖 DSH、Codex、pi、OMP、OpenCode、Command Code 与 Antigravity（[09 §6.2](./09-mcp-memory-tools.md#62-真实验证边界)）。
- **未验证项**：非 `darwin/arm64` 运行、生命周期、进程清理与 `update`；DSH `agent/disposed` 真实路径、绝对目录安装及 `0.5.3` 桌面复验；OMP 交互式 `agent_settled`/`/quit`/`V-OMP-02`/`V-OMP-05`/npm 安装；Codex `SessionEnd`、OpenCode `session.deleted`、CodeBuddy/WorkBuddy 交互式 `SessionEnd` 与 WorkBuddy 桌面 GUI；Antigravity `Stop` 语义及 `claude-code`/`antigravity` Hook `transcript_path` 实时传入；Delta 共享/子智能体/归档/Cloud Runner 线程与未登录态采集；远端 Actions Dreaming 带文件改动提案的 `apply`；ThinkRail 撤销 `memory_read` 授权后的免审复验及 Codex、Claude Code、Command Code、CodeBuddy、WorkBuddy、DSH 对 `readOnlyHint` 的消费行为。
- **已知结构性限制**：MCP 传入 `sid` 标为 `Session-Origin: model`，服务端无法跨进程证明会话归属；同文件两条字节相同到期行只删一条时集合比对不拦截，含未匹配到期占位符子串时拒绝为 `INVALID_ARGUMENT`；OMP 无 `sections` 只能替换整段 `systemPrompt`，`omp -p --no-session` 无转录；pi 前台 in-process 子代理不触发会话事件；`commandcode`/`grok`/`opencode` 不进自动补扫，`antigravity` 缺工作目录时跳过归属，`antigravity`/`codebuddy`/`workbuddy` 暂不提取 `toolOutcomes`；沙箱 `workspace-write` 须经 MCP 读写 `~/memory`（Grok 需 `read_write`）；阶段 C/D 的 P3 遗留与组件级 DOM 测试缺失见审查报告。
- **构建与升级基线**：Go `1.27+`、Node `v26.10.0+` / npm `11.19.1+`（默认 `node` 为 v22 时需将 Node 26 置于 `PATH` 最前）。`web/dist`、`bin/`、`dist/` 不入库，Go 构建测试前先执行 `make ui`。替换运行中的 `~/.agent-memory/bin/memctl` 必须用 `memctl update` 或同目录临时文件 `mv` 原子替换，禁止直接 `cp` 覆盖。

## 任务

### 历史阶段归档索引（`T001`–`T146`）

阶段顺序与验收映射见 [交付顺序](./05-validation-and-delivery.md#1-交付顺序)。更早任务（`T001`–`T146`）均按阶段归档于 `docs/validation/`：

| 阶段 | 任务范围 | 核心里程碑 | 归档与审查 |
| --- | --- | --- | --- |
| 阶段 A：核心 CLI | `A-1`–`A-6`（`T001`–`T022`） | 初始化与读取、受管写入与恢复、远端同步、Hook 与后台同步、`doctor` | [阶段 A](./validation/stage-a.md)、[审查](./validation/stage-a-review.md) |
| 阶段 B：只读 UI | `B-1`–`B-3`（`T023`–`T027`） | HTTP Token 鉴权、文件监听与 SSE、Preact 内嵌与四视图 | [阶段 B](./validation/stage-b.md)、[审查](./validation/stage-b-review.md) |
| 阶段 C：受管管理 | `C-1`–`C-3`（`T028`–`T034`） | UI/API 写入与改链、`doctor --fix`、冲突解决与 `revert`、adapter 安装 | [阶段 C](./validation/stage-c.md)、[审查](./validation/stage-c-review.md) |
| 阶段 D：Dreaming 与首发 | `D-1`–`D-2`（`T035`–`T047`、`T049`–`T051`） | Dreaming 预览与 `apply`、四平台发布、UI 重设计、`v0.1.0`–`v0.1.2` | [阶段 D](./validation/stage-d.md)、[审查](./validation/stage-d-review.md)、[UI 验证](./validation/ui-ux-verification.md) |
| 阶段 E：DSH 插件与 CLI | `E-1`–`E-5`（`T048`、`T052`–`T059`） | `memctl-dsh`、结束事件、`update`、中英/JSON 帮助、`v0.2.0`–`v0.4.0` | [阶段 E](./validation/stage-e.md)、[T052](./validation/t052-dsh-real-session.md) |
| 阶段 F：pi、MCP 与 Skill | `F-1`–`F-5`（`T060`–`T082`） | `memctl-pi`、`memctl mcp` 与沙箱实测、CI 四分片、Skill、`v0.5.0`–`v0.6.0` | [阶段 F](./validation/stage-f.md)、[T060](./validation/t060-pi-plugin.md)、[T062](./validation/t062-pi-real-session.md) |
| 阶段 G：远端整理与过期 | `G-1`–`G-4`（`T083`–`T097`、`T105`–`T114`、`T116`、`T118`、`T119`、`T121`） | Actions Dreaming、`entries` 检索、到期隐藏与 30 天闸门、CodeBuddy/WorkBuddy、MCP 溯源、`v0.7.0`–`v0.13.1` | [阶段 G](./validation/stage-g.md)、[T096](./validation/t096-dsh-plugin-0-5-2-verification.md)、[交接](./validation/dreaming-handoff.md) |
| 阶段 H：跨会话提炼与多宿主 | `H-1`–`H-4`（`T098`–`T104`、`T115`、`T117`、`T120`、`T122`–`T137`） | `corpus` 蒸馏、本地 `prepare`/`propose`、Delta/OMP 接入、规范图、只读注解、OIDC、`v0.14.0`–`v0.16.0` | [阶段 H](./validation/stage-h.md) |
| 阶段 I：架构止血与原语 | `I-1a`–`I-4`（`T138`–`T146`） | P0 差集守卫、P1 `add` 与快照读及仪表盘、P2 分词 BM25 与 `operations[]`、P3 `DetailLines` 与 `EntryID`、`v0.17.0` | [阶段 I](./validation/stage-i.md) |

### 近期与当前任务表

| ID | 状态 | 目标与依赖 |
| --- | --- | --- |
| T145 | 已完成 | 发布 `v0.17.0`、原子升级本机二进制并刷新全局 `memctl` Skill；依赖 `T144`；[归档](./validation/stage-i.md#t145发布-v0170原子升级本机二进制并刷新全局-memctl-skill) |
| T146 | 已完成 | `v0.17.0` 阶段 I 隔离与真实宿主复验，并将阶段 I 规范受管写入 `~/memory` 且远端同步；依赖 `T145`；[归档](./validation/stage-i.md#t146v0170-阶段-i-端到端实测本机真实宿主复验与真实记忆仓库受管写入同步) |
| T147 | 已完成 | 精简 `docs/STATUS.md` 热台账并建立最大字符数（`MaxStatusChars = 12000`）三层门禁，归档 `T118` 与 `T146`，根目录 `待办.md` 改名为 `TODO.md` 三栏结构；依赖 `T146`；[本轮记录](#最近一轮完成记录t147) |

## 最近一轮完成记录（T147）

- 状态：已完成。
- 目标与范围：治理 `docs/STATUS.md` 膨胀至 `67,600` UTF-8 字符问题；补齐 `T118`（[阶段 G](./validation/stage-g.md#t118mcp-写入的会话溯源m14)）与 `T146`（[阶段 I](./validation/stage-i.md)）归档确保 `T001`–`T146` 均在 `docs/validation/` 可检索；将 `docs/STATUS.md` 精简至 `6,500`–`8,500` UTF-8 字符区间内，建立 `internal/app/status_budget_test.go`、`scripts/next-round.sh`（`MAX_STATUS_CHARS=12000`）与 `docs/06` / `AGENTS.md` 三层门禁；同步将根目录 `待办.md` 改名为 `TODO.md` 三栏结构。
- 对应契约：[06](./06-development-loop.md#1-最小记录结构)、[05](./05-validation-and-delivery.md#1-交付顺序)、[AGENTS.md](../AGENTS.md#跨会话与轮次接续)。
- 验证日期与环境：2026-11-11，`darwin/arm64`，Go `1.27.2`，Node `v26.11.1`。
- 执行的命令与结果：精简前 `TestStatusMarkdownCharBudget` 报 `got 67600 UTF-8 chars, max allowed is 12000`，精简后通过；`gofmt -l cmd internal tests` 为空；`go vet ./...` 与 `go test -count=1 ./internal/app -run 'TestStatusMarkdownCharBudget|TestCheckStatusCharBudgetBoundary|TestNextRoundScriptEnforcesStatusBudget|TestGoSourcesAreGofmtFormatted'` 通过；`scripts/next-round.sh --check-only --dry-run` 对当前 `docs/STATUS.md` 与 `12,000` 字符文件退出码 `0`、对 `12,001` 字符退出码 `1`；全库 Markdown 链接锚点、黑话/路径扫描及 `git diff --check` 均通过（`0` 错误）。
- 已通过的验收项：`TestStatusMarkdownCharBudget`、`TestCheckStatusCharBudgetBoundary`、`TestNextRoundScriptEnforcesStatusBudget`、`TestGoSourcesAreGofmtFormatted` 及全库链接锚点检查。
- 未验证项与已知边界：本轮未改 `memctl` 二进制或插件代码。

## 近期待办与下一步

- **npm OIDC 配置**（[04 §6.1](./04-ui-and-cli-specification.md#61-发布入口)）：在 npmjs.com 为 `memctl-pi` 与 `memctl-dsh` 配置 Trusted Publisher（`gray0128/memctl`、`build.yml`），下次插件升版推送 `v*` 标签时验证 OIDC 自动发布与 provenance。
- **宿主交互式与 MCP 审批补验**（[16](./16-mcp-tool-annotations.md)、[17](./17-omp-integration.md)）：推进 [当前事实](#当前事实) 未验证项（OMP 交互式 `agent_settled`/`/quit`/`V-OMP-02`/`V-OMP-05`/npm 安装、ThinkRail 与多宿主 `readOnlyHint` 审批、Delta 多类线程采集、CodeBuddy/WorkBuddy 交互式退出与 GUI、DSH `0.5.3` 桌面复验）。
- **远端 Dreaming 与测试并行化**（[11](./11-github-actions-dreaming.md)）：按需复验远端轨道带文件改动提案的 `dream --apply` 并清理 `gray0128/memctl-dream-diag`；消除 `a4_credentials_test.go` 等测试的进程级环境变量依赖以启用 `t.Parallel()`。
