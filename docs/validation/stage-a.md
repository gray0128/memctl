# 阶段 A 已完成任务归档

本文件按 [最小记录结构](../06-development-loop.md#1-最小记录结构) 归档阶段 A 期间已完成任务的历史验收记录，完整保留原验收事实与更正说明。当前进度、待办任务与最近一轮记录见 [开发状态](../STATUS.md)。

## 阶段 A 性能与适配记录

本节按 [性能与适配记录](../05-validation-and-delivery.md#3-性能与适配记录) 记录 `T019` 期间的本地性能基线与宿主适配状态。数值来自本机单次会话的临时环境，不是跨机承诺；未实测的宿主保持未验证。

### 本地性能基线

- 环境：macOS 26.6.2（Build 25G83），Apple M1 Pro（8 核），Go 1.27.1，Git 2.54.0，测量日期 2026-10-16。
- 仓库：本地临时记忆仓库；受支持文件 41 个、正文合计 839 字节、提交 2 个；未配置 Git hooks，`commit.gpgsign` 未设置。
- 方法：`bin/memctl` 由 `make build` 生成；每次调用使用独立进程并测量 wall-clock 毫秒；临时 HOME、`AGENT_MEMORY_BACKGROUND=0`、`GIT_CONFIG_NOSYSTEM=1`，不访问网络；测量脚本与临时仓库位于工作区外。
- 结果：3 次会话的代表值，`p50` 取 3 次 `p50` 的中位；样本量为 `version` 30、其余路径 5–20 次。

| 路径 | 命令 | p50 | p95 | max |
| --- | --- | --- | --- | --- |
| 冷进程启动 | `version` | 5.6ms | 7.3ms | 8.4ms |
| 无变化 Hook（每轮注入） | `context --stdin` | 64ms | 210ms | 1034ms |
| 无变化 Hook（`--once` 去重） | `context --once --stdin` | 24ms | 110ms | 387ms |
| 受管写入 | `write MEMORY.md --base <rev> --stdin` | 393ms | 660ms | 1360ms |
| 外部 capture | `sync --capture` | 323ms | 428ms | 531ms |
| 锁竞争 Hook | 锁被占用时 `context --stdin` | 100ms | 126ms | 206ms |
| 网络故障 | 认证失败桩下 `sync --wait` | 341ms | 787ms | 1204ms |

- 结论：`version` 的 `p50` 约 5.6ms，略高于 `version/help < 5ms` 的待测优化目标；无竞争派生与 Hook 路径远低于 8 秒业务预算，网络故障路径在 30 秒预算内以明确原因码结束。
- 清理：网络故障连续 5 次后 `pgrep -f <bin>` 无遗留进程；锁竞争路径不阻塞、不排队。清理耗时已包含在上表数值内，本轮未单独拆分。
- 噪声：无变化 Hook 与网络故障的 `max` 明显高于 `p50`，来自本机后台负载与冷页；不把单次 `max` 当作性能承诺。

### 宿主适配状态

本轮未安装任何宿主 Hook（安装器属 `C-3`），因此所有候选宿主均为「未安装、未验证」：Claude Code、Codex、Grok、Antigravity、Command Code、OpenCode、pi、dsh。参考目录给出 pi 的目标映射（`user.prompt.submit` 读记忆、`session.idle` 同步、`session.deleted` 收尾），但本机 `pi-yaml-hooks` 未加载任何 hook，未进行真实注入验证；其余宿主的配置格式、事件映射、SID 来源、stdout 上限与 timeout 均待 `C-3` 探测后记录。

协议层已在本机手工检查：在临时仓库执行 `printf '{"session_id":"manual-session","prompt":"secret prompt"}' | bin/memctl context --json --stdin --agent manual-agent`，退出码 0，stdout 为单个 `hookSpecificOutput` 对象，`hookEventName=UserPromptSubmit`，`additionalContext` 含仓库路径、来源标签、内嵌指引与 `MEMORY.md`；stdin 中的 `prompt` 未出现在 stdout 或 stderr，stderr 为空。

## T019：Hook 协议、会话身份与宿主性能实测（A-5 子任务 2）

- 目标与范围：完成 A-5 子任务 2，前置为 `T018`。新增 `internal/hookio`（有界 Hook JSON、SID 优先级、envelope 编码）、`internal/state/session.go`（`injected` 与 `syncSucceeded` 分离、14 天清理）、`internal/assets`（内嵌默认指引）、`internal/app/session.go` 与 `internal/cli/hook.go`，接线 `start`/`context`/`pull`/`tag`/`wrap`；`wrap` 透传子进程退出码与信号；不实现安装器与前端。
- 契约：[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[环境变量与会话身份](../04-ui-and-cli-specification.md#2-环境变量与会话身份)、[Hook 输入输出与时限](../04-ui-and-cli-specification.md#3-hook-输入输出与时限)、[会话开始与上下文注入](../03-workflows-and-dataflows.md#3-会话开始与上下文注入)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build` 全部通过；Hook 集成测试使用临时 HOME 与本地临时记忆仓库、`AGENT_MEMORY_BACKGROUND=0`，不访问网络或真实宿主配置。
- `H01`：`TestA5SessionIDPriorityCoversH01` 覆盖 `--sid`、`AGENT_MEMORY_SID`、`DSH_HOOK_SESSION_ID`、stdin 的 `session_id`/`sessionId`/`sessionID` 优先级、空值跳过、非字符串返回 `INVALID_ARGUMENT`（退出码 2）与无稳定 SID 时不伪造来源。
- `H02`：`TestA5ParallelSessionsDoNotCrossCoversH02` 让 alpha/sessA 与 beta/sessB 交替 `tag`/`write`，来源标签与提交元数据不串会话，`tag` 缺少 SID 返回 `NO_SESSION_ID`，未创建共享 last-sid 目录。
- `H03`：`TestA5OnceSyncRetryCoversH03` 在锁被占用时首轮 `context --once` 仍注入本地正文并记 `sync=busy`，释放锁后同一会话重试同步成功且不重复输出正文；会话记录同时反映 `syncSucceeded` 与 `injected`。
- `H04`：`TestA5HookOutputContractCoversH04` 校验 `start --json` 的 `SessionStart`、`context --json` 的 `UserPromptSubmit` envelope、纯文本 `start` 输出以及 `pull`/`sync` 的空 stdout，无 Git 输出混入 JSON。
- `H05`：`TestA5WrapPassesThroughCoversH05` 校验 `wrap` 向子进程导出 SID、agent 与仓库路径，透传退出码 3、信号终止 `128+15=143`，并在前置同步 `BUSY` 时仍返回子进程状态 7。
- `H06`：`TestA5NoRemoteMissingSIDAndOversizedEntryCoversH06` 在无远端时输出 `sync=local` 与本地正文，无稳定 SID 时每轮注入不去重，入口文件超过 2 MiB 时明确提示改用受管读取而不内联截断。
- 性能与宿主：本地性能基线与宿主适配状态归档于 [阶段 A 性能与适配记录](#阶段-a-性能与适配记录)；`version` 的 p50 约 5.6ms，未达 `< 5ms` 优化目标；宿主 Hook 未安装，无真实注入验证。
- 未验证与遗留：Windows/Linux 的 `wrap` 与锁路径未运行；未在真实宿主验证 `additionalContext` 是否被采纳；`T018` 记录已归档至 [T018 归档](#t018后台同步派生与进程生命周期a-5-子任务-1)。
- 下一步：当时剩余已登记产品待办为 `A-6`（诊断与 CLI 交付，`T020`），后因 Orca 1.4.227 移除 `orca agent launch` 插入 `T021` 修复跨会话接续入口。

## T018：后台同步派生与进程生命周期（A-5 子任务 1）

- 目标与范围：完成 A-5 子任务 1，前置为 A-4（`T017`）。新增 `internal/syncer/env.go`、`internal/syncer/spawn*.go`，重写 `internal/lock`（`TryAcquire`、可被 Git 子进程继承的锁描述符、`unix.Flock`/`windows.LockFileEx`），并在 `internal/git`、`internal/app`、`internal/cli` 接入后台派生；实现 `sync`/`end` 自动路径与受管 `write` 提交后的非交互后台同步、凭据白名单快照、锁忙立即退出与进程组/锁生命周期继承；不实现 Hook 会话身份（`T019`）。
- 契约：[写入模型](../01-architecture-and-modules.md#2-写入模型)、[仓库身份](../01-architecture-and-modules.md#31-仓库身份)、[平台与进程生命周期](../01-architecture-and-modules.md#6-平台与进程生命周期)、[前台行为](../03-workflows-and-dataflows.md#41-前台行为)、[凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理)、[环境变量](../04-ui-and-cli-specification.md#2-环境变量与会话身份)。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build` 全部通过；后台与生命周期测试使用临时 HOME、临时 bare 远端与受控 `GIT_SSH_COMMAND` 桩，并设置 `GIT_CONFIG_NOSYSTEM=1` 与受限 `GIT_ALLOW_PROTOCOL`，不访问真实网络或系统凭据库。
- `G08.2`：`TestA5BackgroundCredentialDifferenceCoversG08_2` 让前台带可用凭据标记而后台白名单快照不含该标记，后台 worker 以 `AUTH_FAILED`/`remoteState=failed` 结束，本地提交保留、工作树干净、无遗留 memctl 进程、锁可再次获取，随后前台 `sync --wait` 成功补推。
- `L01`：`TestA5KilledParentKeepsLockWithGitCoversL01` 在 Git fetch 挂起时强杀父进程，验证 Git 进程组仍持有可继承的锁描述符（第二个获取者失败），强杀该进程组后锁释放；`internal/lock` 单元测试覆盖 `TryAcquire` 互斥与 `File()`。
- `L02`：`TestA5BackgroundStormKeepsStateCoversL02` 连续触发 15 次 `end` 且网络不可用，`TryAcquire` 让 worker 串行化（并发检测标记未出现），无后台进程堆积，本地提交与工作树保持不变。
- 未验证与遗留：`G08.2` 的「终端认证成功」在前台显式路径复现，未在真实宿主终端验证；新增 `AGENT_MEMORY_BACKGROUND` 开关并同步到 04 §2、锁实现由 `gofrs/flock` 改为 `golang.org/x/sys` 并同步到 01；Windows/Linux 的 `LockFileEx`/`DETACHED_PROCESS` 未运行；真实宿主 Stop/end 风暴与宿主强杀留待 `T019` 宿主实测。
- 下一步：`T019`（A-5 子任务 2：Hook 协议、会话身份与宿主性能实测），其后按 `A-6` → `D-2` 推进；本轮按用户指示暂停接续；已完成，由 `T019` 接续。

## T017：前台远端同步（A-4）

- 目标与范围：完成 A-4，前置为 A-3 与 `T002`。新增 `internal/syncer`、`internal/git/exec.go`、`internal/git/sync.go`、`internal/git/proc_unix.go`、`internal/git/proc_other.go`、`internal/state/conflict.go`、`internal/state/cache.go`、`internal/app/remote.go`、`internal/app/sync.go`、`internal/app/resolve.go` 与 `internal/cli/tty_*.go`；实现空远端与已有远端接入及远端审批、`sync --wait` 前台同步、错误分类、非交互凭据抑制与进程组清理、CLI 多阶段 `resolve` 及冲突类型拒绝；不实现后台派生（A-5）、Hook、UI 与安装。
- 契约：[凭据与信任边界](../01-architecture-and-modules.md#34-凭据与信任边界)、[初始化与远端接入](../03-workflows-and-dataflows.md#1-初始化与远端接入)、[同步与后台任务](../03-workflows-and-dataflows.md#4-同步与后台任务)、[凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build` 全部通过；远端集成测试使用临时 HOME、临时本地 bare 远端、受控 `GIT_SSH_COMMAND` 桩与本地 `git http-backend`，并设置 `GIT_CONFIG_NOSYSTEM=1` 与受限 `GIT_ALLOW_PROTOCOL`，不访问真实远端、网络或系统凭据库。
- `G02`/`G03`：`TestA4PrivateRemoteRoundTripCoversG02` 验证空远端首推、第二设备克隆相同历史并建立 `origin/main` upstream、不产生独立根提交；`TestA4RemoteBoundaryAndErrors` 覆盖错误 URL 不当作空远端、SSH 认证失败分类为 `AUTH_FAILED`、无默认分支明确失败、取消 upstream 后按初始化协议重建。
- `G04`/`G06`：`pre-push` 钧子在 fetch 后推进远端时 push 被拒返回 `NON_FAST_FORWARD` 且 `remoteState=pending`、本地提交保留；`TestA4ManualGitStatePausesSyncCoversG06` 在用户手工 merge 冲突下让 `sync --wait` 与受管 `write` 均返回 `OPERATION_ACTIVE`。
- `C01`–`C04`：`TestA4ConflictTwoStagesCoversC01` 验证两个连续本地提交分别冲突时每阶段的三方快照与 `captured`→`resolving` 相位；`TestA4ConflictStaleCoversC02` 验证外部 HEAD 变化后旧结果返回 `CONFLICT_STALE` 且不覆盖新历史；`TestA4ConflictTypesCoversC03` 区分删除/修改、空文件与文本冲突并拒绝二进制与 rename 类型（`UNSUPPORTED`）；`TestA4ResolveAbortCoversC04` 验证跨进程读取持久化阶段并以 `--abort` 恢复原始 HEAD。
- `G08`：`G08.1` 受控 SSH 桩成功同步并传递 `SSH_AUTH_SOCK`；`G08.3` 本地 HTTP + `git http-backend` 下非交互 `credential.helper` 认证成功且凭据不出现在输出；`G08.4` 无 TTY 时 `GIT_ASKPASS`/`SSH_ASKPASS` 未被调用；`G08.5` 挂起桩在总 deadline 到期后返回 `TIMEOUT`，进程组被终止且锁可重新获取；`G08.6` 主机密钥失败返回 `AUTH_FAILED` 且不写 `known_hosts`；`G08.7` 含凭据 URL 在诊断中被脱敏为 `://***@`。
- `L01`（前台 Git 子进程）：`G08.5` 用例同时验证 deadline 到达后无遗留 Git 子进程且仓库锁可再次获取。
- 未验证与遗留：`G08.2`（终端成功但后台凭据环境不同）属 A-5，本轮未实现或验证，`G08` 作为复合项未整体标记通过；`G05`（签名与 hooks 拒绝）沿用 A-3 的缺失身份与 hook 拒绝场景，`G07` 沿用共享 `git.Inspect` 未合并索引检测，本轮未新增专项用例；真实 `SIGKILL` 父进程与宿主清理属 A-5 平台门槛；未覆盖 Windows/Linux。
- 下一步：当时剩余已登记待办为 `T018`、`T019`（共同对应 `A-5`），其后按 `A-6` → `D-2` 推进；已完成，由 `T018` 接续。

## T011：文档引号用法统一

- 目标与范围：在 `tech-doc-style-chinese` 默认规则下确定 `docs/` 与根 `README.md` 的引号基线，并把 `01`–`04` 与 `docs/README.md` 正文的弯引号改为直角引号「」，使全仓正文口径一致；只改引号与在 [方案索引](../README.md) 增加一行基线声明，不改事实、条件、机器可读内容与产品契约。
- 契约：[方案索引](../README.md)（引号基线声明）、[协作入口](../../AGENTS.md)（文档 Skill 要求）。
- 验证日期与环境：2026-10-17，macOS arm64，Git 2.54.0；执行者为主会话。
- 核验结果：通过。17 对中文正文弯引号全部改为「」；`AGENTS.md`、根 `README.md`、`05`、`06`、`STATUS.md` 与 `docs/validation/` 原有「」保持不变；02 与 `AGENTS.md` 中位于行内代码的 ASCII 引号（`If-Match: "<fileRevision>"`、`{runner: "claude" 或 "codex", baseRevision}`、`codegraph explore "<问题>"`）按要求保留。全仓 11 份 Markdown（不含 `脚本化实现参考/` 与 `.commandcode/`）本地链接 133 条、带锚点 80 条全部有效；`git diff --check` 无输出；改动文件无行尾空白。
- 未运行：纯文档任务，不运行 Go 测试、构建、平台或宿主测试。
- 遗留限制：`脚本化实现参考/` 按参考边界不修改，其正文不纳入本次统一。
- 下一步：当时剩余已登记待办为 `T017`（对应 `A-4` 前台远端同步），其后按 `A-4` → `D-2` 推进；已完成，由 T017 接续。

## T002：凭据契约

- 目标与范围：为 A-4 提供可实施契约与可复现场景，只做设计与场景定义，不把 `G08` 标为测试通过。修改 01 增加「凭据与信任边界」，03 增加「凭据环境继承与失败处理」，05 定义 `G08` 子场景，04 补充凭据交互说明；不改动源码、测试或既有错误码集合。
- 契约：[凭据与信任边界](../01-architecture-and-modules.md#34-凭据与信任边界)、[凭据环境继承与失败处理](../03-workflows-and-dataflows.md#43-凭据环境继承与失败处理)、[错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)、[CLI 退出行为](../04-ui-and-cli-specification.md#3-hook-输入输出与时限)。
- 关键决策：同步环境白名单保留 SSH、凭据、代理与 Git 配置通道变量，剔除仓库重定向变量与 Hook payload；前台仅在标准输入与标准错误均为终端时允许 Git/SSH 原生提示，Hook 与后台强制非交互并抑制 askpass；后台使用父进程传入的白名单快照且不持久化；失败区分 `AUTH_FAILED`（无可用凭据或被拒绝，含主机密钥不匹配）与 `TIMEOUT`（总 deadline 到期一律按超时），两者都保留本地提交且不记为 `synced`；该决策由 01 §3.3「自动命令使用非交互输入和总 deadline」与 04 §3.1 预算推导，未新增错误码。
- `G08` 子场景：`G08.1` SSH agent 前台成功、`G08.2` 终端成功但后台凭据环境不同、`G08.3` HTTPS helper 非交互成功、`G08.4` 交互式 helper 被抑制、`G08.5` 凭据阻塞与总超时、`G08.6` 未知主机密钥、`G08.7` 凭据泄露检查。
- 文档检查项：本地链接与锚点、术语一致性、跨文档一致性；不运行产品测试。
- 验证日期与环境：2026-10-17，macOS arm64，Git 2.54.0；执行者为主会话。
- 核验结果：通过。文档检查项逐条核对：全仓 Markdown 本地链接与标题锚点全部命中；01、03、05、04 对同步环境白名单、交互抑制、超时与错误码的表述与 02 §4.3 内部 Git 错误码一致；`G08` 子场景覆盖「终端认证成功但后台环境不同」；改动文件无行尾空白。
- 未运行：纯文档任务，不运行 Go 测试、构建、平台或宿主测试；`G08` 子场景只定义，未实现或验证；不把 `G08` 标为测试通过。
- 遗留限制：askpass 抑制可能影响以交互式 `GIT_ASKPASS` 为唯一凭据来源的 HTTPS 环境，非交互路径会以 `AUTH_FAILED` 失败并要求前台运行；白名单落地与进程组终止细节待 A-4 按平台验证。
- 下一步：当时剩余已登记待办为 `T011`（引号统一），其后按 `A-4` → `D-2` 推进；已完成，由 T011 接续。

## T007：受管写入与恢复

- 目标与范围：完成 A-3，前置为 T006。新增 `internal/lock`、`internal/state`、`internal/vault/write.go`、`internal/app/mutation.go`、`internal/app/recover.go` 及对应测试，实现仓库排他锁、锁内 Git 状态与基线校验、事务恢复记录、`write`、仅本地的 `sync --capture` 与 `recover`；不实现远端同步、Hook、UI、链接重命名与并发 UI 写入。
- 契约：[写入模型](../01-architecture-and-modules.md#2-写入模型)、[受管写入事务](../03-workflows-and-dataflows.md#2-受管写入事务)、[修订标识](../02-domain-models-and-contracts.md#1-修订标识与格式解析)、[错误与部分成功](../02-domain-models-and-contracts.md#43-错误与部分成功)。
- 验证日期与环境：2026-10-16，macOS arm64（26.6.2），Go 1.27.1，Git 2.54.0；执行者为主会话。
- 命令与结果：`go vet ./...`、`go test ./...`、`make build`、`make test` 全部通过；`bin/memctl` 在临时 `HOME` 与临时仓库下的 `init`、`read --json`、`write`、`sync --capture`、`recover` 手工闭环成功，本地流程不访问网络。
- `W01`：`TestApplyPutAndRevisionMismatch` 与 `TestA3WriteCoversW01` 验证首个写入成功、旧 `fileRevision` 写入返回 `REVISION_MISMATCH`、首个写入内容不变且提交数不增加；`TestApplyRejectsStaleBaseRevision` 覆盖旧仓库 `baseRevision`。
- `W03`：`TestApplyRejectsExternalChanges`、`TestCaptureCommitsExternalEdits` 与 `TestA3WriteCoversW03AndCapture` 验证存在 staged、unstaged 或 untracked 变化时普通写入返回 `EXTERNAL_CHANGES` 且不落盘；`sync --capture` 只提交冻结的变更清单并使工作树干净。
- `W04`：`TestRecoverRollsBackInterruptedWrite` 在多文件写入第一个文件后注入中断，`recover --rollback` 将全部前镜像与 `HEAD` 恢复一致并删除记录。
- `W05`：`TestRecoverRecognizesCommittedTransaction` 在提交成功后、完成标记前注入中断，恢复通过 `Memctl-Transaction` 标记识别既有提交，补充状态而不重复提交。
- `W06`：`TestRecoverRefusesUserEditedFile` 在恢复前手工修改受影响文件，恢复拒绝覆盖该文件、仍恢复受控路径并在 `RECOVERY_REQUIRED` 中列出待人工处理路径。
- `W07`：`TestApplyHookRejectionRollsBack` 验证 `pre-commit` 拒绝时写入返回 `GIT_FAILED`、前镜像与索引干净且无遗留记录；`TestApplyHookAddingOutOfPlanFileIsReported` 验证钩子夹带计划外文件时报告真实提交与越界路径，恢复识别该提交而不重复提交。
- `G05`–`G07`（本地部分）：`TestA3WriteRequiresIdentity` 验证缺少 Git 身份时写入在修改文件前返回 `GIT_FAILED` 且不改全局配置；`TestApplyRejectsGitOperations/merge in progress` 与 `/unmerged index without rebase directory` 分别通过操作目录与 `git ls-files --unmerged` 拦截进行中的 merge 和无 rebase 目录的未合并索引，返回 `OPERATION_ACTIVE`。
- `P01`–`P03`：`TestApplyRejectsLimitsAndBadPaths` 与 `TestA3WriteBoundaries` 验证逃逸路径、`.git`、设备名与反斜杠路径返回 `INVALID_PATH`，库外符号链接与多硬链接目标返回 `UNSUPPORTED` 且不修改库外内容，超过 2 MiB 的单文件与超过 100 个变更返回 `LIMIT_EXCEEDED`。
- `L01`（本地写入部分）：`TestApplyReportsBusyWhileLockHeld` 验证仓库锁被占用时第二个写入在预算内返回 `BUSY`；`internal/lock` 使用 `gofrs/flock` 在 Git 目录固定文件上取排他锁，释放时不删除锁文件。
- 回归：A-1 `R03` 与 A-2 的 `init`、`read`、格式解析集成与单元测试全部通过；`write` 从「未实现」改为实现后，`R03` 的占位成功检查改用仍未实现的 `resolve`。
- 未运行：`W02`（CLI/UI 并发，属 C-1）、真实 `SIGKILL` 与父/子进程生命周期测试（属 A-4/A-5 的 `L01` 完整场景）、远端同步、Hook、UI、Windows/Linux 平台测试。
- 遗留限制：中断由进程内故障注入模拟，未用真实 `SIGKILL` 验证 OS 锁释放；`capture` 不写恢复记录，中断后重新执行即可；新文件要求父目录已存在；`write` 仅支持单文件，多文件由应用服务测试覆盖；仓库修订对单个文件最多读取 8 MiB。
- 下一步：当时剩余已登记待办为 `T002`（凭据契约）与 `T011`（引号统一），其后按 `A-4` → `D-2` 推进；已完成，由 T002 接续。

## T008：记忆格式与链接解析

- 目标与范围：完成 A-2 的解析部分，前置为 T003。新增 `internal/vault/parse.go` 与 `internal/vault/link.go`，实现行级无损解析（顶层 `- ` 条目、行尾保序元数据、同一行多个 `source`、未知键、根相对 `[[path]]`、代码围栏与行内代码排除）、`entryId` 与 `WikiLink` 标识，以及同名歧义、死链和非法路径诊断；不实现受管写入（T007）与链接重命名。
- 契约：[修订标识与格式解析](../02-domain-models-and-contracts.md#1-修订标识与格式解析)、[领域对象](../02-domain-models-and-contracts.md#2-领域对象)。
- 验证日期与环境：2026-10-16，macOS arm64（26.6.2），Go 1.27.1，Git 2.54.0；执行者为主会话。
- 核验结果：通过。`go test ./internal/vault/` 的 `TestParseFileCoversF01`、`TestParseFileLosslessLineEndings`、`TestParseFileAndResolveLinksCoversF02`、`TestResolveLinksDistinguishesEntryIDs` 全部通过，覆盖 `F01`（上游规范样例、无元数据条目、重复与多个 `source`、未知键、非法日期、代码围栏与行内代码）与 `F02`（根相对链接、`.md` 省略、`agent://` 来源解析、同名歧义不猜测、死链与非法路径诊断、Markdown/SQL/脚本/二进制分类）。`Raw()` 对空内容、无结尾换行、CRLF 与混合样例逐字节等于输入；`Root.Parse` 前后文件字节不变，确认解析不写入仓库。`go vet ./...`、`go test ./...` 全部通过，回归 T006 的读取测试。
- 未运行：未实现或验证链接重命名的字节区间定点改写、图谱孤立判定与 HTTP/CLI 暴露；这些属于 T007、C-1 与后续阶段。
- 遗留限制：条目仅识别行首 `- `，`*`/`+`、缩进与 Setext 标题按未知格式保留；同名歧义只覆盖扩展名省略（`[[name]]` 匹配 `name`、`name.md`、`name.markdown`），不做跨目录按文件名搜索。
- 下一步：当时剩余已登记待办为 `T007`（A-3 受管写入与恢复）、`T002`（凭据契约）、`T011`（引号统一），其后依次推进 `A-4` 至 `D-2`；已完成，由 T007 接续。

## T016：跨会话自动接续入口（Orca 宿主）

- 目标与范围：为 [接续的执行机制](../06-development-loop.md#61-接续的执行机制) 建立可执行的宿主入口，把四要素交接与接续前置条件变成拒绝式检查。新增 `scripts/next-round.sh`，在 `06` 第 6 节补充 6.1 接续的执行机制，在 `AGENTS.md` 补充 Orca 宿主入口说明；不改变第 6 节的所有权模型与停止条件。
- 契约：[接续的执行机制](../06-development-loop.md#61-接续的执行机制)、[目录结构与依赖](../01-architecture-and-modules.md#5-目录结构与依赖)（`scripts/` 定位）、[协作入口](../../AGENTS.md)。
- 验证日期与环境：2026-10-16，macOS arm64，Git 2.54.0，Orca 1.4.226（runtime ready），bash 3.2；执行者为主会话（实现提交 `4ab1376`）。
- 核验结果：通过。`bash -n` 通过；五类拒绝路径均返回退出码 `1`（缺少任一四要素小节、缺少提示词、`--effort` 缺少 `--model`、工作树不干净、提示词文件不存在），另核对参数缺值与未知参数两条；`--dry-run` 输出完整命令参数并正确透传 `--model`、`--effort`、`--focus`；全仓 11 份 Markdown 本地链接 117 条、带锚点 61 条全部有效。
- 链路测试：`scripts/next-round.sh --prompt-file /tmp/next-round-test.md` 返回 `outcome.kind=terminal`、`outcome.handle=term_48db347d-…`、`prompt.outcome=handed-to-terminal`、`operationId=1792125071135-…`，退出码 `0`；`orca terminal read` 确认新会话收到完整交接提示词、按只读提示执行并自行结束，测试前后工作树均无改动。
- 未运行：未执行 `--retry-launch` 重放路径（本机未复现启动回执丢失）；未在 Orca 运行时不可用时验证失败分支；未核对 `--model`、`--effort` 在新会话中的最终生效结果，只验证了命令行构造。
- 遗留限制：入口只能启动新会话，不能强制旧会话停止开发；该限制已写入 [接续的执行机制](../06-development-loop.md#61-接续的执行机制)。
- 下一步：剩余已登记待办为 `T008`（A-2 格式与链接解析）、`T007`（A-3 受管写入与恢复）、`T002`（凭据契约）、`T011`（引号统一），其后依次推进 `A-4` 至 `D-2`；本轮按 [接续的执行机制](../06-development-loop.md#61-接续的执行机制) 发起下一轮会话，优先推进 `T008`。

## T015：台账冷热分离与阶段归档

- 目标与范围：在 [开发循环规范](../06-development-loop.md#1-最小记录结构) 建立 `STATUS.md` 热台账与 `docs/validation/` 按阶段冷归档的分离机制及单条记录精简规则，同步 [目录结构与依赖](../01-architecture-and-modules.md#5-目录结构与依赖) 说明，并将 `T001`–`T014` 完整历史验收记录原样移入本文件。
- 契约：[最小记录结构](../06-development-loop.md#1-最小记录结构)、[目录结构与依赖](../01-architecture-and-modules.md#5-目录结构与依赖)。
- 验证日期与环境：2026-10-16，macOS arm64，Git 2.54.0；执行者为主会话。
- 核验结果：通过。`T001`–`T014` 共 10 条已完成任务记录完整迁入 `docs/validation/stage-a.md` 且本表锚点逐一命中；全仓 11 份 Markdown（含 `docs/validation/stage-a.md`）本地链接与标题锚点全部有效；`git diff --check`、行尾空白与改动文件弯引号扫描均无匹配。
- 未运行：纯文档任务不运行产品二进制、平台或宿主测试。
- 下一步：剩余已登记待办为 `T008`（A-2 格式与链接解析）、`T007`（A-3 受管写入与恢复）、`T002`（凭据契约）、`T011`（引号统一），其后依次推进 `A-4` 至 `D-2`；下一轮建议优先推进 `T008`。

## T014：优化开发循环与补齐完整开发计划

- 目标与范围：按用户确认的规则更新 [开发循环规范](../06-development-loop.md)，在 [验收与交付](../05-validation-and-delivery.md#1-交付顺序) 补齐阶段 A–D 的完整实施计划，并同步更新 [协作入口](../../AGENTS.md) 与状态台账。不修改源码或测试入口；本轮不创建下一轮新会话。
- 契约：[开发循环规范](../06-development-loop.md)、[最小记录结构](../06-development-loop.md#1-最小记录结构)、[交付顺序](../05-validation-and-delivery.md#1-交付顺序)、[协作入口](../../AGENTS.md)。
- 文档检查项：`06` 明确单会话单轮推进、主会话默认负责全流程与按需委派子智能体、验证通过后更新进度并提交推送及同工作区创建新会话自动接续、交接要素及停止条件；`05` 完整定义阶段 A（`A-1`–`A-6`）、阶段 B（`B-1`–`B-3`）、阶段 C（`C-1`–`C-3`）、阶段 D（`D-1`–`D-2`）的前置依赖、交付边界与全部 50 个验收场景 ID 映射，无循环依赖；`AGENTS.md` 与 `STATUS.md` 关联表述一致且保留历史验收事实；本地链接与锚点全部有效、行尾空白无匹配、符合 `tech-doc-style-chinese` 排版要求。
- 验证日期与环境：2026-10-16，macOS arm64，Git 2.54.0；执行者为主会话（提交 `7f7a25e`）。
- 核验结果：通过。根 `README.md`、`AGENTS.md` 与 `docs/` 共 10 份 Markdown 的本地链接与标题锚点逐条核对全部有效；`05` 中 14 个阶段步骤覆盖核心测试矩阵全部 50 个唯一 ID，依赖有向无环；改动文件（`AGENTS.md`、`docs/05-validation-and-delivery.md`、`docs/06-development-loop.md`、`docs/STATUS.md`）行尾空白与弯引号扫描均无匹配，直角引号、中西文留白与「一段一行」合规。
- 未运行：纯文档任务不运行产品二进制、平台或宿主测试；按用户本轮范围指令不创建下一轮新会话。
- 下一步：剩余已登记待办为 T008（A-2 格式与链接解析）、T007（A-3 受管写入与恢复）、T002（凭据契约）、T011（引号统一），其后依次推进 `A-4` 至 `D-2`；下一轮建议优先推进 T008。

## T013：台账收尾与忽略本地工具状态

- 目标与范围：收尾 T010 记录的报告级措辞，并把 Agent 本地产物 `.commandcode/` 加入忽略文件。修改 `docs/STATUS.md` 的 T010 记录与本条，以及 `.gitignore`；不新增产品契约，不改动 `AGENTS.md`、源码或 T012 的索引配置。
- 契约：[最小记录结构](../06-development-loop.md#1-最小记录结构)、[目录结构与依赖](../01-architecture-and-modules.md#5-目录结构与依赖)（本地产物不提交）。
- 文档检查项：每处措辞变化对应一条验证者提示；T010 记录标明各轮验证的运行标识与结论归属；`git check-ignore` 命中 `.commandcode/` 且该目录不出现在 `git status`；本地链接与锚点有效、行尾空白无匹配。
- 验证日期与环境：2026-10-16，macOS arm64，Git 2.54.0。验证者为子智能体 `reviewer`（运行 `ecffb4a3-9099-4491-b419-af9410f05006`），只读核验，未修改、暂存或提交文件。
- 核验结果：五项通过。`.gitignore` 同时保留 T012 的 `/.codegraph/`、`/.zvec-grep/` 并新增 `/.commandcode/`，相对 HEAD 只有新增、无删除；T010 记录的四条措辞（三轮验证与 P1 归属、核验结果标注、工作区时点限定、链接枚举范围）均已落地；T013 记录与任务表自洽；T012 整节与 `AGENTS.md` 的「代码检索工具」节完整且未被本轮改动；本地链接 72 条、带锚点 30 条由验证者枚举 10 份 Markdown 逐条核对，全部有效，行尾空白无匹配，`docs/STATUS.md` 无弯引号，新增段落一段一行。
- 实现者辅助证据（验证者工具集无法复跑）：`git check-ignore -v` 分别命中 `/.commandcode/`、`/.codegraph/`、`/.zvec-grep/`；`git status --short` 不再列出 `.commandcode/`。
- 已处理验证者提示：将 T010 记录中的「空未跟踪文件」改为「截至该轮复核时为空」，因为并发写入者随后写入了内容。
- 未运行：纯文档与忽略配置任务，不运行产品测试；未做 UI 或浏览器验证。
- 下一步：建议 T008（记忆格式与链接解析）或 T011（引号基线），本轮不自动推进。

## T010：更正技能可用性记录

- 目标与范围：更正本台账中关于 `tech-doc-style-chinese` 的错误表述，并登记引号一致性任务 T011。只修改 `docs/STATUS.md`，不改动 `AGENTS.md` 的「技能不可用」回退条款，不改动其他文档内容。
- 契约：[最小记录结构](../06-development-loop.md#1-最小记录结构)（不抹去原验收事实）、[协作入口](../../AGENTS.md)（文档工作必须应用该 Skill）。
- 依据：技能实体位于 `~/.agents/skills/tech-doc-style-chinese`，`~/.pi/agent/skills/tech-doc-style-chinese` 为其软链接，`.skill-lock.json` 记录来源 `fenng/tech-doc-style-chinese`、安装于 2026-08-17、更新于 2026-10-05；`skills` CLI 不在当前 `PATH`，属管理工具缺失，不等于技能不可用。
- 文档检查项：T003 记录保留原文并追加更正与证据；T009 的既有问题条目不再以技能不可用为前提；T010、T011 的状态与证据自洽；本地链接与锚点有效、行尾空白无匹配。
- 验证日期与环境：2026-10-16，macOS arm64，Git 2.54.0。验证者为子智能体 `reviewer`，三轮均只读，未修改、暂存或提交文件：首验 `5f8e86d3-7915-4065-9f2f-5921f392535c`（五项通过，提出 4 条 P2），修复后复核 `97910fb1-f645-4825-a6f7-326d0adb5281`（确认修复，另报 1 条 P1 与 2 条 P2），P1 修正后复核 `1afeaf8c-4db3-4ecf-8bbc-8dd30822fc5e`（P1 解除，判定可标为完成）。
- 核验结果（修复后复核）：五项通过。原文保留且更正与外部证据一致（技能实体、`references/` 四个参考文件、`scripts/` 两个脚本、`.skill-lock.json` 的来源与安装更新时间、`~/.pi` 路径内容一致）；T009 条目已去掉技能不可用前提并指向 T011；T010/T011 记录与任务表自洽；截至该轮复核时工作区仅两份已跟踪文档变化（`docs/06-development-loop.md`、`docs/STATUS.md`），另有工具生成的未跟踪文件 `.commandcode/taste/taste.md`（截至该轮复核时为空），非本轮产物；`AGENTS.md` 第 3 行回退条款未改动；本地链接 69 条、带锚点 28 条由验证者枚举根 `README.md`、`AGENTS.md` 与 `docs/` 共 10 份 Markdown 逐条核对，全部有效，行尾空白无匹配，新增行无弯引号。
- 未独立证实：软链接属性与 `skills` CLI 不在当前 `PATH` 两点超出只读验证工具的核验范围，保留为实现者的环境观察。
- 已处理验证者提示：恢复 T002 段落前的空行；将 T011 章节移到 T002 结尾段落之后；在更正条目中说明 T003「确认文档替代方式」的建议失效；将状态由「进行中」改为「已完成」。处理结果由修复后复核确认（见上）。
- 未运行：纯文档任务不运行产品测试；未做 UI 或浏览器验证。
- 下一步：T011 在确定引号基线后执行；本轮不自动推进。

## T012：引入代码检索工具

- 目标与范围：为本项目引入 codegraph 与 zvec-grep（`zg`），供任意 Agent 在阅读或修改代码前检索上下文。只新增 `.gitignore` 忽略项、`AGENTS.md` 的「代码检索工具」一节和本台账记录；不修改产品契约、源码、测试或验收矩阵，不配置任何 Agent 的 MCP 文件。
- 契约：[协作入口](../../AGENTS.md)。
- 交付物：`.codegraph/`、`.zvec-grep/` 两份本地索引（均不提交）；`AGENTS.md` 使用说明；`.gitignore` 忽略 `/.codegraph/` 与 `/.zvec-grep/`。
- 验收：`codegraph status` 与 `zg status` 报告索引就绪；`codegraph explore` 与 `zg query` 返回本项目结果；`git check-ignore` 命中两份索引；`AGENTS.md` 本地链接与术语一致、行尾空白无匹配。
- 证据：`codegraph init` 索引 18 个文件（246 节点、612 边）；`zg index --embedding local/potion-code-16m-v2` 索引 41 个文件（覆盖率 100%；实体数随索引重建漂移，复核时为 237）；两条命令均退出码 `0`。
- 验证日期与环境：2026-10-16，macOS arm64；验证者为独立子智能体（`bg-1-5929e505`），只读核验，未修改、暂存或提交文件。
- 核验结果：六项通过——工具用途与入口命令同实际能力一致；`codegraph status`、`zg status`、`codegraph explore` 与 `zg query` 均返回本项目结果；`git check-ignore` 命中两份索引且两者不出现在 `git status`；`AGENTS.md` 新增节的引号、中西文留白与「一段一行」合规，全仓 71 条本地链接与 29 条锚点有效、行尾空白无匹配；`T012` 行与章节自洽；`cmd/`、`internal/`、`tests/`、`Makefile`、`go.mod` 及 `docs/01`–`05` 未改动。
- 已知现象：共享守护进程重建索引期间，`zg query` 可能瞬时返回 `ZVEC_GREP.ENGINE.LOCK.BUSY`，重试即恢复，不影响索引可用性。
- 未运行：本轮不涉及产品二进制、平台或宿主测试；未在 Windows 或 Linux 上验证工具可用性。
- 下一步：本轮已完成；MCP 接入由各 Agent 自行配置，不在本仓提交。

## T009：验证者规则

- 目标与范围：在 [开发循环规范](../06-development-loop.md#5-验证如何落地) 规定每轮验证由独立子智能体执行，并同步本台账。只修改 `06` 与 `STATUS.md`，未新增产品契约、验收 ID 或测试入口。
- 契约：[开发循环规范](../06-development-loop.md)。
- 文档检查项：规则给出验证者、交接内容、只读约束、失败后果与记录要求；`06` 的步骤表与完成条件引用同一定义，不重复声明；本地链接与锚点有效。
- 验证日期与环境：2026-10-16，macOS arm64，Git 2.54.0。验证者为子智能体 `reviewer`，五轮均只读，未修改、暂存或提交文件：首验 `dcd4d3e4-6bff-464b-b589-5f6ad45ed7ee`（六项通过，提出 3 条 P2），复审 `2d6ebf7b-fe19-478b-8617-50abb5196106`（确认提示修复，另报 2 条 P2），收尾复核 `e3f60fec-1640-4de4-bc33-3ed3dafc083a`（判定「可标为完成」），末轮复核 `cc8790f8-09e2-4d89-8c9e-6885793628aa`（独立枚举 66 条本地链接与 27 条锚点全部命中，判定「可标为完成」），终检 `241a6d10-d369-47f1-81d5-6b3e872ee9f3`（复核措辞修正与台账自洽，判定「可标为完成」）。
- 规则完整性：验证者逐条核对五项要求均有显式定义，并给出原文位置：验证者不参与实现（`06:67`）、交接内容（`06:69`–`06:74`）、只读约束（`06:74`）、失败后果（`06:78`）、记录要求（`06:76`）。
- 单一权威定义：`06` 第 5 节是唯一定义处；步骤表第 5 行（`06:25`）与完成条件（`06:59`）均为带「第 5 节」回指的摘要，未另立第二套要求。
- 跨文档一致性：全仓检索 `子智能体` 仅命中本轮新增位置；`AGENTS.md` 在循环与验证规则上只指向 06，未另立规则；检索 `自行验证`、`自我验证`、`自验`、`无需验证`、`独立验证` 未发现规则性使用（本行检索词自身除外）。
- 链接与锚点：验证者逐条核对本地链接的目标与标题锚点，未发现失效，包括 `STATUS.md` 指向 `06#5-验证如何落地` 的锚点；实现者另行执行 `python3 /tmp/linkcheck.py`，输出 `checked 66 local links across 10 files`、退出码 `0`，仅作辅助。
- 台账与排版：`T009` 记录含目标与范围、契约链接、文档检查项，与实际改动一致；验证者扫描 `docs/` 行尾空白无匹配；新增中文段落保持一段一行，未发现硬换行或中西文紧贴问题。
- 已处理验证者提示：刷新 `STATUS.md` 中随 T009 过时的「本轮」指代与下一步建议；`06` 统一「子智能体（验证者）」术语，台账标题改为「T009：验证者规则」；将 `05` 弯引号一条限定为本轮规则文件的用法差异，不再断言全仓基线；链接与锚点结论区分验证者结论与实现者辅助命令；将「跨文档一致性」一条的「无命中」改为「未发现规则性使用（本行检索词自身除外）」，并把 `AGENTS.md` 的描述收紧为「在循环与验证规则上只指向 06」。
- 验证者报告的既有问题（未处理，超出本轮范围）：`05-validation-and-delivery.md` 第 14 行使用弯引号，与本轮规则文件 `06`、`STATUS.md` 的直角引号用法不一致；仓库其他文档两种引号混用，已升级为独立任务 T011。
- 未运行：纯文档任务不运行产品测试；未做 UI 或浏览器验证。
- 下一步：建议 T008（记忆格式与链接解析）；本轮不自动推进。

## T006：本地初始化与安全读取

- 目标与范围：完成 A-2 的初始化与读取部分；新增 `internal/config`、`internal/git`、`internal/vault` 的路径与读取部分、`internal/app` 的 `Init`/`ReadFile`、`internal/cli` 的 `init` 与 `read`，以及单元与集成测试。不实现格式解析（归 T008）、受管写入与仓库锁（归 T007）、远端或凭据行为。
- 契约：[仓库身份与文件边界](../01-architecture-and-modules.md#3-仓库路径与信任边界)、[初始化流程](../03-workflows-and-dataflows.md#1-初始化与远端接入)、[修订标识与限额](../02-domain-models-and-contracts.md#1-修订标识与格式解析)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)。
- 验收：`G01`、`G05` 的本地初始化场景、`F04`、`P01`、`P02` 的读取场景、`P03` 的读取限额；`R03` 回归。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0，GNU Make 3.81。
- 命令：`make build` 与 `make test` 全部通过。`tests/integration/a2_init_read_test.go` 编译真实二进制并在临时 `HOME`、临时仓库下运行，子进程环境设置 `GIT_ALLOW_PROTOCOL=file`、`GIT_TERMINAL_PROMPT=0`，且不配置任何 remote；本地命令不访问网络。
- G01：连续两次 `init` 均退出码 `0`，第二次报告 `state: existing` 且 head 不变；`git rev-list --count HEAD` 为 `1`，`MEMORY.md` 字节不变，工作树干净，无 remote，运行状态目录未被创建。已存在的空目录报告 `state: created`；已存在文件、非空普通目录、父目录为 Git 仓库、无关 Git 仓库四类目标均返回 `INVALID_ARGUMENT`，且不修改原文件、不创建 `.git` 或 `MEMORY.md`。
- G05（本地部分）：以 `user.useConfigOnly=true` 模拟无身份环境时，`init` 退出码 `1`，stderr 为 `GIT_FAILED` 并提示配置本仓库身份，全局配置字节不变；随后写入仓库本地身份再执行 `init`，报告 `state: completed` 且提交数仍为 `1`。`pre-commit` 钩子拒绝、`commit.gpgsign=true` 配合失败签名程序两种场景均退出码 `1`，钩子文件与 `commit.gpgsign` 保持不变。
- F04：`read MEMORY.md` 输出与磁盘字节一致；`read --json` 输出 `filePath`、`fileRevision`、`rawContent`，`fileRevision` 为 `sha256:` 加 64 位十六进制、重复读取稳定、文件模式变化时改变。不存在、二进制资产、超过 2 MiB、目录分别返回 `NOT_FOUND`、`UNSUPPORTED`、`LIMIT_EXCEEDED`、`INVALID_PATH`，stdout 为空。单元测试锁定 `sha256:3d3ad089ac30852a08937bc697caebe8b0c7a39f8d15648b2a126ad61ad47f6a`，确认修订采用长度前缀的「Git 模式 + 原始字节」编码。
- P01、P02（读取）：`../outside.md`、`/etc/hosts`、`.git/config`、`.GIT/HEAD`、`NUL`、含反斜杠路径均返回 `INVALID_PATH`；符号链接及其目录组件返回 `UNSUPPORTED`，未输出库外内容；所有失败路径 stdout 为空。
- P03（读取）：等于 2 MiB 的文件可读，超出 1 字节即返回 `LIMIT_EXCEEDED`。stdin、HTTP 请求体与变更文件数上限不在本轮范围。
- 单元测试：`internal/config`、`internal/git`、`internal/vault` 覆盖默认与覆盖路径解析、`repoId` 稳定性、`Canonical` 对已存在前缀的符号链接解析、Git 重定向变量剔除与凭据 URL 脱敏、路径边界表、修订编码与有界读取。
- R03 回归：`init` 已实现，因此 R03 集成测试改用仍未实现的 `write` 保留「无占位成功结果」检查，并新增「`PATH=""` 下 `init` 退出码 `1`、stderr 含 `GIT_FAILED`、stdout 为空且不创建仓库目录」；`version/help` 的隔离要求保持不变。
- 未运行：格式与链接解析场景 `F01`、`F02`（归 T008）；写入、锁与事务场景（归 T007）；远端、Hook、UI 与平台生命周期测试；未在 Windows 或 Linux 上运行。
- 遗留限制：`fileRevision` 的文件模式取自文件系统权限位，未与 Git 索引模式比对；硬链接目标仅禁止写入，读取仍允许，多硬链接写入检查在 A-3 落地时补测。
- 下一步：建议 T008（记忆格式与链接解析）；本轮不自动推进。

## T003：工程与命令入口

- 目标与范围：完成 [阶段 A-1](../05-validation-and-delivery.md#阶段-a-的实施顺序)；新增 `go.mod`、`Makefile`、`cmd/memctl/main.go`、`internal/cli/cli.go`、`internal/cli/cli_test.go`、`tests/integration/r03_cli_test.go`，并同步根 `README.md` 与本台账。不创建空源码目录、前端目录或占位成功子命令。
- 契约：[架构与目录](../01-architecture-and-modules.md#5-目录结构与依赖)、[CLI 规范](../04-ui-and-cli-specification.md#1-cli-命令)、[阶段 A 计划](../05-validation-and-delivery.md#阶段-a-的实施顺序)。
- Go 版本与模块依据：选用当前环境受支持的稳定版 `Go 1.27.1`（`go.mod` 声明 `go 1.27.1`），满足 01 中 Go 1.22+ `ServeMux` 路由语法及后续 `os.Root` 根句柄安全文件访问要求；模块路径按仓库 `origin` 定为 `github.com/gray0128/memctl`，A-1 阶段零外部第三方依赖。
- 验收：`R03`（macOS arm64）、`DOC01`、`DOC02`。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0，GNU Make 3.81。
- R03：执行 `make build` 与 `make test` 通过。`internal/cli` 单元测试覆盖 `version`/`--version`/`-v`、`help`/`--help`/`-h`、缺少子命令、未知子命令、未知参数、`version/help` 多余参数，以及 16 个后续规划子命令的显式非零拒绝（退出码 `1`，`UNSUPPORTED`，无空实现返回 `0`）。`tests/integration/r03_cli_test.go` 在临时目录编译真实二进制，并在 `PATH=""`、指向不存在路径的 `HOME`/`AGENT_MEMORY_DIR`/`AGENT_MEMORY_STATE` 隔离环境下验证：`version` 与 `help` 退出码为 `0` 且不创建或访问任何仓库/状态目录、不依赖 Git 或 Node.js；未知命令退出码为 `2`；未实现命令退出码为 `1`。
- DOC01 与 DOC02：核对根 `README.md`、`AGENTS.md` 与 `docs/` 全部本地链接及锚点有效；源码目录仅包含已落地文件的 `cmd/memctl`、`internal/cli` 和 `tests/integration`，无空目录或 `.gitkeep`。行尾空白扫描无匹配。注：当前环境未安装 `tech-doc-style-chinese`，本次对 `README.md` 与 `docs/STATUS.md` 的更新沿用仓库既有文档风格基线并待确认。
- 更正（2026-10-16，T010）：上文「当前环境未安装 `tech-doc-style-chinese`」不成立。复核结果为技能已安装：实体在 `~/.agents/skills/tech-doc-style-chinese`，pi 通过 `~/.pi/agent/skills/tech-doc-style-chinese` 软链接引用，`.skill-lock.json` 记录来源 `fenng/tech-doc-style-chinese`、安装于 2026-08-17、更新于 2026-10-05，`references/` 与 `scripts/` 内容完整。原文保留；该轮未加载技能的原因无法从仓库复核。DOC01 与 DOC02 的结论不受影响。本更正使 T003「下一步：建议确认文档替代方式……」的前提失效：技能可用，无需再确认替代方式。
- 未运行：A-2 及之后的仓库读写、Git 同步、前端构建与跨平台测试（不属于 A-1 范围）。
- 下一步：建议确认文档替代方式后，从 T006（对应 A-2：本地初始化与读取）或 T002（凭据契约）继续；本轮不自动推进。

## T005：阶段 A 开发计划

- 范围：修改 05 的阶段计划和必要验收场景，细化本台账的近期任务；不另建计划文件，不展开 B–D 的具体任务。
- 验收：阶段 A 的每一步均有依赖、交付物和验收映射；近期任务可判断完成；凭据决策不阻塞纯本地开发；原有验收 ID 保留，文档链接有效。
- 验证：2026-10-16，macOS，使用 Ruby 内联只读检查遍历根入口及 docs 的链接和锚点，10 份文档的 57 处本地链接有效；验收矩阵共 50 个唯一 ID，原有 45 个均保留，新增 F04、G08、H06、R03、R04。检查 7 个任务引用和 6 个阶段步骤的依赖，无循环或前置顺序错误。
- 人工核对：各阶段与近期任务都有交付边界和验收映射；凭据仅阻塞远端同步，B–D 未提前拆分，复合用例按子场景验收。两份改动文件行尾空白扫描无匹配。
- 未运行：本轮只更新计划，未执行产品测试、构建或平台认证；新增场景仍待实现与验收。
- 下一步：建议获实施授权后启动 T003；本轮不自动开始编码。

## T004：文档 Skill 使用要求

- 范围：只修改 `AGENTS.md` 的文档规范入口和本台账，不复制 Skill 正文。
- 验收：指明适用文档操作、准确的 Skill 名称、编辑前读取与应用要求，以及 Skill 不可用时的处理方式；保留原有协作规则。
- 验证：2026-10-16，macOS；读取 AGENTS 核对上述验收项均满足，原有 5 条规则保留；`rg -n '[ \t]+$' AGENTS.md docs/STATUS.md` 无匹配。本轮未改变链接或产品契约，不运行产品测试。

## T001：项目入口与开发循环

- 目标：新执行者能找到权威设计、判断哪些目录已存在、按一套最小流程开发，并识别尚未完成的工作。
- 范围：根 `README.md`、`AGENTS.md`、`.gitignore`；01 的目录规划；docs 索引；06 循环规范；本台账。参考脚本不变，不生成空源码骨架。
- 契约：[目录结构](../01-architecture-and-modules.md#5-目录结构与依赖)、[开发循环](../06-development-loop.md)。
- 验收：DOC01，本地链接与锚点均有效；DOC02，目录与实际阶段一致；DOC03，流程具有开始条件、失败分支、完成条件和接续入口；DOC04，没有把规划、文档检查或参考脚本标为产品验收通过。
- 验证日期与环境：2026-10-16，macOS arm64，Go 1.27.1，Git 2.54.0。
- DOC01：解析根 README、AGENTS 与 docs 共 10 份 Markdown 的本地链接，再对照目标文件和标题锚点，49 处均有效；12 个代码块闭合，JSON 和 Go 示例语法通过检查。临时检查器在工作区外运行，本条记录保留可复核步骤，不依赖临时文件长期存在。
- DOC02：使用 `rg --files --hidden -g '!.git' -g '!.DS_Store'` 对照目录说明，已存在入口与规划源码区分一致；没有创建空源码目录。用 `git check-ignore --no-index` 抽查，8 个构建产物或本地配置路径被忽略，7 个文档、源码或示例路径未被忽略，后者退出 1 表示无匹配。
- DOC03：逐项核查 06 的步骤和状态转换，具备开始条件、验证失败回到实现、阻塞解除、完成证据和中断接续规则；根 README 与 AGENTS 都能直接找到该入口。
- DOC04：逐项核查项目阶段与测试声明，方案、候选平台和参考脚本均未标为实现完成。另以标准库对 22 条既有 API 模式进行注册和匹配探测通过，仅证明文档路由有效，不构成产品 API 验收。
- 最终差异：已读取本次新增及修改的文件，行尾空白扫描无匹配；只改入口、规范、目录规划和忽略配置，参考目录保持不变。
- 未运行：二进制、前端、平台及宿主测试，本轮纯文档范围不适用。
- 下一步：建议从 T002 或 T003 选择下一轮；本轮不自动推进。
