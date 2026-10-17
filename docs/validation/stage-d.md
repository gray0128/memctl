# 阶段 D 已完成任务归档

本文件按 [最小记录结构](../06-development-loop.md#1-最小记录结构) 归档阶段 D 期间已完成任务的历史验收记录。当前进度、待办任务与最近一轮记录见 [开发状态](../STATUS.md)。

## T035：Dreaming 隔离生成与持久化提案预览（`D-1`）

- 目标与范围：完成 `D-1`，前置为 `C-1`（`T029`）。新增 `internal/dream`：仓库锁内校验干净工作树、无待恢复事务、无冲突与无 Git 中间态后固定 `baseHead`/`baseRevision`；经 `vault.ListManaged`/`ReadFile` 仅导出受支持文本文件到 `os.MkdirTemp` 的独立临时目录，跳过 `.git`、Git/host 配置、符号链接、二进制与超限资产，不使用 `clone --local`；释放主仓库锁后运行 runner。macOS Seatbelt 后端（`sandbox-exec`）以默认拒绝、仅临时目录可写、`process-exec` 白名单、允许读与网络的 profile 执行 runner，非 macOS 返回 `Unavailable` 并以 `UNSUPPORTED` 拒绝；限制最小环境、输出量（4 MiB）、耗时（默认 10 分钟）与进程组。从固定基线逐文件计算文本 Diff，拒绝越界路径、Git 配置修改、符号链接/非普通文件、二进制与超限结果；持久化 `Proposal`（含有序 `changes`、`diff`、`patchHash`、`warnings`）到 `$STATE/repos/<repoId>/proposals/`，删除临时目录，孤儿 `generating` 记录在新进程查询时标为 `failed`。CLI 新增 `dream --runner claude|codex` 与 `dream --show ID`；`dream --apply` 返回 `UNSUPPORTED`（属 `D-2`）。HTTP 新增 `POST /api/v1/dream:preview`（202、`{proposalId}`，异步生成并在终态发布 `dream.updated`）、`GET /api/v1/dream/{proposalId}` 与显式 `UNSUPPORTED` 的 `POST /api/v1/dream:apply`；`watcher.Invalidation` 增加 `ProposalID`/`Status` 供 SSE 负载。`--readonly` 由既有中间件拒绝预览与应用请求，未修改主库。
- 契约：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[DreamProposal 与接口](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[应用与基础设施接口](../02-domain-models-and-contracts.md#3-应用与基础设施接口)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[REST 契约](../02-domain-models-and-contracts.md#4-rest-契约)、[SSE 与缓存](../02-domain-models-and-contracts.md#5-sse-与缓存)、[模块职责与目录](../01-architecture-and-modules.md#4-模块职责)、[仓库身份与状态根](../01-architecture-and-modules.md#31-仓库身份)、[阶段步骤的子任务切分](../05-validation-and-delivery.md#阶段步骤的子任务切分)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 与 `cd web && npm test` 全部通过；`gofmt -l internal cmd tests web/embed.go` 无输出，`git diff --check` 无告警；前端未改动，`vitest` 12 文件 65 项通过；Go 全量含 `tests/integration`（208.5 秒）通过。`internal/dream` 单测覆盖：Diff 形状、路径作用域拒绝、缺沙箱 `UNSUPPORTED`、缺 runner `UNSUPPORTED`、脏库 `EXTERNAL_CHANGES`、过期基线 `REVISION_MISMATCH`、完整生成流水线（主库字节不变、临时目录清理、提案可查询）、越界/二进制/Git 配置/host Hook/符号链接被拒、孤儿 `generating` 记录标为 `failed`。`tests/integration/d1_dream_test.go` 用真实二进制与 Seatbelt 沙箱覆盖：`R02` 无 runner、`D02` 脏库拒绝、`D01` 隔离（runner 成功改导出树而写主库与 `$HOME/.claude` 被拒且目标字节不变）、`D03` 越界提案失败且主库不变、HTTP 202/GET/`dream.updated` 端到端。`A03` 由既有 `TestB1ReadOnlyCoversA03` 覆盖。
- 未验证与遗留：未在真实 claude/codex 宿主与模型端点验证，内嵌 runner 参数与凭据环境属未验证组合，[性能与适配记录](../05-validation-and-delivery.md#3-性能与适配记录) 的宿主实测字段待补；仅 macOS Seatbelt 为已验证隔离后端，Linux/Windows 返回 `UNSUPPORTED`；runner 以 scratch `HOME` 运行且不传递宿主 API key，真实宿主是否可用待验证；未开展浏览器视觉 UI/UX 验证；仅在 macOS arm64 实机运行。
- 下一步：`D-1` 完成，阶段 D 未结束（以 `D-2` 为界），按 [开发循环规范](../06-development-loop.md#6-交接与中断) 接续 `D-2` 子任务 1（Dreaming 显式应用与 UI 整理视图）。

## T036：Dreaming 显式应用与 UI 整理视图（`D-2` 子任务 1）

- 目标与范围：完成 `D-2` 子任务 1，前置为 `T035`、`T030`、`T033`。`internal/dream` 在持久化记录中保存每个 create/update 变化的原始内容与 Git 模式（`planned` 字段，不进入 `DreamProposal` API），新增 `PrepareApply`（校验 patchHash、有序变化清单、计划修订与模式不变）与 `MarkApplying`/`MarkReady`/`MarkStale`/`MarkApplied` 状态迁移；`app.ApplyDream` 按 `proposalId + patchHash` 加载提案，先在锁内按事务记录结算中断的 apply，再经受管事务 `app.Apply` 提交（`MutationRequest.DreamProposalID`），事务记录保存 `proposalId` 且在 Dreaming apply 时保留完成记录以便恢复，提交后写入 `appliedCommit` 并清理记录；基线移动返回 409 `PROPOSAL_STALE` 并标 `stale`，重复 apply 返回同一 commit，手动改过的文件拒绝覆盖（`RECOVERY_REQUIRED`）。CLI `dream --apply <ID> --patch-hash <HASH>` 替换原 `UNSUPPORTED` 占位；`POST /api/v1/dream:apply`（`{proposalId, patchHash}`）在 `--readonly` 下由既有中间件返回 `READ_ONLY`。UI `DreamPanel` 接入「整理接入」：runner 选择与生成（沙箱不可用时禁用）、按 proposalId 查询状态并轮询、固定 Diff 预览与有序变化列表、确认应用；过期提案禁止应用；adapter 安装面板不变。首页新增 `memctl-dream-sandbox` meta 注入隔离能力。同时修复 `internal/dream` 的并发竞态：`complete` 删除内存中的生成记录与写入终态记录，和 `loadRecord` 将无主 `generating` 记录归并为 `failed`，现在在 `s.mu` 下串行，并在归并前重读磁盘，避免把刚完成的 `ready` 记录覆盖为 `failed`。
- 契约：[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)（步骤 5–7）、[DreamProposal 与接口](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[应用与基础设施接口](../02-domain-models-and-contracts.md#3-应用与基础设施接口)、[REST 契约](../02-domain-models-and-contracts.md#4-rest-契约)、[错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[HTTP 安全](../04-ui-and-cli-specification.md#5-http-安全)、[阶段步骤的子任务切分](../05-validation-and-delivery.md#阶段步骤的子任务切分)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Git 2.54.0，Node v26.10.0 / npm 11.19.1；执行者为主会话。
- 命令与结果：`make ui`、`go vet ./...`、`go test ./... -count=1`、`make build` 与 `cd web && npm test` 全部通过；`gofmt -l internal cmd tests web/embed.go` 无输出，`git diff --check` 无告警；前端 `vitest run` 13 文件 72 项通过（`dreams.test.js` 覆盖状态标签、终态轮询、生成禁用、可应用判定与变化摘要；`api.test.js` 覆盖 Dreaming 三个请求的形状）。`D04` 由 `internal/app.TestApplyDreamCommitsAndIsIdempotentCoversD04`、`TestApplyDreamRecoversInterruptedApplyCoversD04` 与 `tests/integration/d2_dream_apply_test.go` 的 HTTP/CLI 用例覆盖（首次 apply 提交并写入内容、重复 apply 返回同一 commit 且提交数不变、`after-commit` 中断后重试复用既有 HEAD、CLI `dream --apply` 多次调用同一 commit）；`D02` 由 `internal/app.TestApplyDreamRefusesStaleBaselineCoversD02` 与 HTTP 旧基线用例覆盖（409 `PROPOSAL_STALE`、提案标 `stale`、目标路径与提交数不变）；`W06` 由 `TestApplyDreamRefusesToOverwriteManualEdit` 覆盖（半写后在事务外手改的文件不被覆盖，返回 `RECOVERY_REQUIRED`）；`PrepareApply` 的哈希、模式与状态校验由 `internal/dream/apply_test.go` 覆盖；`A03` 由 `TestD2DreamReadOnlyCoversA03` 覆盖（`POST /api/v1/dream:apply` 返回 403 `READ_ONLY`）。
- 未验证与遗留：真实 `claude`/`codex` 宿主与模型端点仍未验证，生成隔离仅在 macOS Seatbelt 后端验证，Linux/Windows 返回 `UNSUPPORTED`；未驱动浏览器 DOM，`DreamPanel` 交互由纯逻辑测试与 Go HTTP 联调覆盖，未开展浏览器视觉 UI/UX 验证；`reconcileDreamApply` 在完成记录已持久化但清理被占用锁跳过时只留下一个小型完成记录，不阻塞后续 apply，未新增独立清理任务。
- 下一步：`D-2` 子任务 1 完成，阶段 D 未结束（以 `D-2` 为界），按 [开发循环规范](../06-development-loop.md#6-交接与中断) 接续 `D-2` 子任务 2（跨平台发布工程与全量交付验收）。

## 阶段 D 性能与适配记录

本节按 [性能与适配记录](../05-validation-and-delivery.md#3-性能与适配记录) 记录 `T037` 发布验收期间的本地性能基线与平台/宿主适配状态。数值来自本机一次会话的临时环境与发布二进制，不是跨机承诺；未实测的宿主和平台保持未验证。

### 环境

- 硬件：MacBook Pro（`MacBookPro18,3`），Apple M1 Pro，16 GiB。
- OS：macOS 26.6.2（Build 25G83），arm64。
- 工具链：Go 1.27.1、Git 2.54.0（Apple Git-157）、Node v26.10.0、npm 11.19.1。
- 二进制：`make release` 产出的 `dist/memctl-darwin-arm64`（`-trimpath`、`-ldflags "-s -w"`、`CGO_ENABLED=0`）。
- 仓库与配置：临时 HOME 与临时记忆仓库，`AGENT_MEMORY_BACKGROUND=0`、`GIT_CONFIG_NOSYSTEM=1`、无签名与自定义 Git hooks（锁竞争场景临时注入 sleep hook）；除网络故障场景外不访问网络，网络故障使用本机拒绝认证的 SSH 桩。fixture 为 20 个 Markdown 文件、36 次提交、约 544 KiB（含 `.git`）。
- 方法：Python3 一次性测量脚本位于工作区外，每次调用使用独立进程并测 wall-clock 毫秒；`p50`/`p95`/`max` 取排序后分位。脚本对 `/usr/bin/true` 同法测得 fork/exec 基线 `p50 2.2ms`，用于判断进程启动噪声；`context --stdin` 的 `max` 受本机后台负载影响，不作为承诺。

### 本地性能基线

| 路径 | 命令 | p50 | p95 | max |
| --- | --- | --- | --- | --- |
| 进程派生基线 | `/usr/bin/true` | 2.2ms | 2.5ms | 3.0ms |
| 冷进程启动 | `version` | 7.2ms | 8.1ms | 10.3ms |
| 帮助 | `help` | 7.3ms | 8.1ms | 8.7ms |
| 无变化 Hook（每轮注入） | `context --stdin` | 83.3ms | 91.4ms | 261.1ms |
| 无变化 Hook（`--once` 去重） | `context --once --stdin` | 22.3ms | 24.0ms | 24.4ms |
| 受管写入 | `write MEMORY.md --base <rev> --stdin` | 266.8ms | 274.5ms | 276.3ms |
| 外部 capture | `sync --capture` | 153.9ms | 162.7ms | 168.3ms |
| 锁竞争 Hook | 仓库锁被占用时 `context --stdin` | 116.1ms | 129.7ms | 129.7ms |
| 网络故障 | 认证失败桩下 `sync --wait` | 157.2ms | 171.1ms | 171.1ms |

- 清理与遗留进程：认证失败路径返回后 `pgrep` 未发现残留 SSH 桩进程；该路径 `p50 157.2ms` 远低于 `sync --wait` 的 30 秒预算。
- 结论：`version`/`help` 不访问仓库、不要求 Git 或 Node.js；`version` 扣除约 2ms 的 fork/exec 基线后约 `5ms`，与 `< 5ms` 的待测优化目标同量级但未稳定达标，如实记录。无变化 Hook、去重 Hook、受管写入与外部 capture 都远低于 8 秒业务预算；仓库锁被占用时 Hook 路径未等待锁（与无竞争路径同一量级）。整体与阶段 A `T019` 基线同一量级，`write`/`capture` 更快。

### 平台矩阵

| 目标 | 构建（交叉编译） | 运行、生命周期与隔离 |
| --- | --- | --- |
| darwin/arm64 | 已验证（Mach-O arm64） | 已验证：全量测试在本机运行；发布二进制冒烟 `version`/`help`/`init`/`start`/`doctor`/`ui`/`install`/`dream` |
| darwin/amd64 | 已验证（Mach-O x86_64） | 未验证：交叉编译产物，无实机 |
| linux/amd64 | 已验证（ELF x86-64，statically linked，stripped） | 未验证：文件锁、路径语义、进程生命周期与 runner 隔离均未运行 |
| windows/amd64 | 已验证（PE32+ x86-64，console） | 未验证：同上 |

### 宿主与 adapter 适配状态

| 宿主 | 探测结果 | 模板与计划 | 真实运行 |
| --- | --- | --- | --- |
| claude-code | `claude@2.1.220` | 已内嵌 `hooks.json`，`install --dry-run` 生成 `~/.claude/settings.json` 计划 | 未验证：未在真实宿主安装 Hook 或触发事件 |
| codex | `codex@0.160.0` | 已内嵌 `hooks.json`、`features.toml` | 未验证：同上 |
| grok | `grok@1.0.60` | 已内嵌 `hooks.json`，目标路径 `$HOME/.grok/hooks/memctl.json` 未实测 | 未验证 |
| antigravity | `agy@1.4.0` | 已内嵌 `hooks.json` | 未验证 |
| commandcode | `commandcode@1.82.5` | 已内嵌 `hooks.json` | 未验证 |
| opencode | `opencode@2.0.26` | 已内嵌 `agent-memory.ts`，插件清单路径未实测 | 未验证 |
| pi | `pi@1.3.0`，`needs_attention` | 已内嵌 `hooks.yaml` | 未验证 |
| dsh | 未探测到 | 已内嵌 `hooks.json` 与插件清单 | 未验证 |

- Dreaming runner 与沙箱：仅 macOS Seatbelt 为已验证隔离后端，Linux/Windows 返回 `UNSUPPORTED`。真实 `claude`（`/opt/homebrew/bin/claude`）在隔离 scratch HOME 下未产出有效提案（`IO_FAILED: runner ... exited with status 1`），说明模型端点与凭据环境属未验证组合；`install` 探测到的宿主版本不代表 Hook 已注入或被宿主采纳。
- 结论：`D01`–`D04` 的隔离与回落语义由合成 runner 与 Seatbelt 覆盖，真实宿主、其他平台与真实模型端点的组合保持未验证，不标为已支持。
