# Dreaming 交接文档

> 状态：本文档是历史基线。`D-1`、`D-2` 的本地沙箱实现已通过合成验收，真实本地 CLI runner 因隔离环境凭据问题未跑通；`T082` 选定架构演进方案并由 `T083` 完成代码迁移（见 [11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)），本地 CLI runner 与 macOS Seatbelt 沙箱已从代码中删除，整理改为依托记忆仓库的 GitHub Actions 运行 `memctl dream --worker`。本文件保留当时的问题记录与实测证据，供追溯 `G-2` 的取舍依据；当前实现状态以 [阶段 G 归档](./stage-g.md) 为准。记录日期 2026-10-19，macOS arm64，Go 1.27.1，`memctl v0.6.0`。

## 1. 一页结论

| 项目 | 状态 |
| --- | --- |
| 隔离生成、固定基线 Diff、提案持久化 | 已实现，自动化测试通过 |
| 显式应用、幂等、过期拒绝、中断恢复 | 已实现，自动化测试通过 |
| CLI、HTTP、UI 三个入口 | 已实现，自动化测试通过 |
| 合成 runner 端到端 | 通过 |
| **真实 runner 端到端** | **未通过**，三个候选 runner 全部卡在凭据 |
| 非 macOS 平台 | `UNSUPPORTED`，无沙箱后端 |
| 凭据如何进入沙箱 | **未实现，也未在契约中定义** |

## 2. 业务语义

Dreaming 是用户显式触发的一次性记忆整理：把受管记忆文件导出到隔离临时目录，在其中运行一个本地模型 CLI 就地改写，再由 memctl 从固定基线计算 Diff，生成待确认的提案。确认前主仓库一个字节都不会变。

三条承诺分别成立、互不推导：

1. **确认前不修改主库** —— 由导出与提案机制保证。
2. **提案内容可复核** —— 由固定基线 Diff 与 `patchHash` 保证。
3. **模型判断正确** —— **不作承诺**。解析检查只能验证格式、链接与范围，不能证明无语义损失。

## 3. 流程

```text
CLI dream --runner R          POST /api/v1/dream:preview
        │                              │
        └──────────┬───────────────────┘
                   ▼
   ① fixBaseline（持仓库锁，随后释放）
      拒绝：无 MEMORY.md / 未完成事务 / 冲突中 / Git 操作中
            / 索引锁 / 工作树不干净 / baseRevision 不匹配
      固定 baseHead + baseRevision
      导出受管文本到 $TMPDIR/memctl-dream-*/repo
                   ▼
   ② runRunner（沙箱内，独立进程组）
      seatbelt (deny default) + 仅允许写 workDir
      HOME=workDir/home  TMPDIR=workDir/tmp
      超时 10 分钟；stdout/stderr 各 4 MiB
                   ▼
   ③ scanWorkTree + buildChanges
      逐字节比对基线与结果 → create / update / delete
      拒绝：越界路径、软链、非普通文件、Git/凭据路径、超限
      模式变化归一化为 warning
                   ▼
   ④ 持久化提案（status=ready）+ patchHash，删除临时目录
                   ▼
   ⑤ ApplyDream（持仓库锁）
      patchHash 校验 → 记录重算校验 → 计划内容重算校验
      基线移动 → stale；工作树脏 → 回退 ready 可重试
      受管事务提交 → 记录 appliedCommit → 后台同步
```

## 4. 代码地图

### 4.1 `internal/dream`（核心，1903 行不含测试）

| 文件 | 职责 | 关键函数 |
| --- | --- | --- |
| `service.go` | 服务装配、基线固定、生成编排、状态对账 | `NewService`:50 `Begin`:84 `Run`:129 `Load`:176 `fixBaseline`:295 `sweepWorkDirs`:255 |
| `run.go` | 沙箱内启动 runner、环境构造、输出限幅 | `runRunner`:27 `runnerEnv`:105 |
| `runner.go` | runner 注册表与 prompt | `defaultRunners`:29 `resolveRunner`:48 |
| `export.go` | 基线导出 | `exportBaseline`:40 `writeExportFile`:100 |
| `scope.go` | 导出与提案的路径禁区 | `protectedPath` |
| `diff.go` | 变更集计算、链接校验、`patchHash` | `buildChanges`:32 `linkWarnings`:137 `scanWorkTree`:168 `patchHash`:258 |
| `apply.go` | 应用前校验与状态迁移 | `PrepareApply`:23 `verifyPlanned`:99 `setStatus`:149 |
| `store.go` | 提案持久化（原子写 + 目录 fsync） | `OpenStore`:38 `Save`:47 `Load`:62 |
| `sandbox.go` | `Sandbox` 接口与 `Unavailable` | — |
| `sandbox_darwin.go` | Seatbelt 后端与 profile 生成 | `platformSandbox` `seatbeltProfile` |
| `sandbox_other.go` | 非 darwin 返回 `Unavailable` | — |
| `textdiff.go` | 统一 Diff 渲染 | `unifiedDiff` |
| `proc_unix.go` / `proc_other.go` | 进程组创建与终止 | `setProcessGroup` `killProcessGroup` |

### 4.2 上层

| 位置 | 职责 | 入口 |
| --- | --- | --- |
| `internal/app/dream.go` | 服务单例与转发 | `dreamService`:18 `DreamBegin`:46 `DreamRun`:52 `DreamProposal`:57 `DreamSandboxAvailable`:67 |
| `internal/app/dreamapply.go` | 应用编排、对账、幂等 | `ApplyDream`:26 `reconcileDreamApply`:186 |
| `internal/server/dream.go` | 三个 HTTP 处理器 | `handleDreamPreview`:38 `handleDreamProposal`:90 `handleDreamApply`:102 |
| `internal/cli/dream.go` | CLI 三模式 | `runDream`:16 |
| `web/src/components/DreamPanel.jsx` | 「整理接入」面板 | 1 秒轮询 + SSE |
| `web/src/dreams.js` | 状态文案与可应用判定 | `canGenerate` `canApply` |

## 5. 契约要点

### 5.1 边界与限额（`internal/dream/dream.go`）

| 常量 | 值 | 作用 |
| --- | --- | --- |
| `MaxChanges` | 100 | 单份提案最多文件数 |
| `MaxChangeBytes` | 8 MiB | 提案计划内容总量 |
| `MaxRunnerOutput` | 4 MiB | runner stdout / stderr 各自上限 |
| `DefaultTimeout` | 10 分钟 | 单次 runner 执行预算 |
| `MaxPatchBytes` | 4 MiB | 存储的 Diff；超出写截断说明，有序变更表仍完整 |
| `DefaultMaxConcurrentDreams` | 1 | HTTP 并发生成上限，超出返回 `BUSY` |

### 5.2 状态机

```text
generating ──成功──> ready ──确认──> applying ──提交──> applied
     │                 │                 │
     └──失败──> failed  └──基线移动──> stale
                       applying ──事务失败──> ready（可重试）
```

- `applied` 是终态：再次应用返回原 `appliedCommit`，不重复提交。
- `stale` 不能回到 `ready`；只能重新生成。
- 孤儿 `generating`（进程内无对应 run）在读取时对账为 `failed`，错误文本固定为 `generation was interrupted before it completed`。
- 提案是审阅记录不是缓存，**永不自动清理**。

### 5.3 存储布局

```text
$STATE/repos/<repoId 去掉 sha256: 前缀>/proposals/dp-<32 hex>.json
```

`record` 在 `Proposal` 之外还有两个内部字段：`workDir`（临时目录，终态后删除）、`instanceId`（对账用）；另有 `planned`：create/update 的精确字节与 Git 模式，供应用时重算校验，**不返回给客户端**。

### 5.4 `patchHash`

覆盖 `action + path + baseRevision + plannedRevision` 的有序序列，字段以 8 字节大端长度前缀编码，避免拼接碰撞。不覆盖渲染后的 patch 文本，因此 Diff 截断不影响校验。应用时会从持久化的有序变更表重算一遍，不一致即拒绝，防止记录被篡改。

## 6. 入口与参数

### 6.1 CLI

```bash
memctl dream --runner claude|codex
memctl dream --show <proposalId>
memctl dream --apply <proposalId> --patch-hash <hash>
```

三者互斥，缺省或多给返回退出码 `2`。`--patch-hash` 只在 `--apply` 时允许。

### 6.2 HTTP

| 方法 | 路径 | 请求 | 响应 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/dream:preview` | `{runner, baseRevision?}` | `202 {proposalId}`，生成在后台 goroutine |
| `GET` | `/api/v1/dream/{proposalId}` | — | `DreamProposal` |
| `POST` | `/api/v1/dream:apply` | `{proposalId, patchHash}` | `MutationResult` |

`--readonly` 下 `internal/server/server.go:268` 的中间件拒绝一切非 `GET`/`HEAD`，包含 preview 与 apply。

### 6.3 SSE

事件名 `dream.updated`，载荷 `{proposalId, status}`，仅在生成终态后发出。UI 同时以 1 秒间隔轮询，SSE 只是加速。

## 7. 隔离机制的真实边界

这一节是理解后续所有问题的前提。

### 7.1 Seatbelt profile

`(deny default)` 之后放行：

- `process-fork`、`signal`
- `process-exec`：runner 所在目录、`/usr`、`/bin`、`/sbin`、`/System`、`/Library`、`/opt`、`/usr/local`
- `file-read*`：**整个文件系统**
- `file-write*`：仅 `workDir` 与 `/dev/{null,zero,stdout,stderr,tty}`
- `network*`、`mach*`、`sysctl-read`、`ipc-posix*`、`system-socket`

因此 runner **可以读**任意路径的文件，但**只能写** `workDir`。隔离目标是「不写用户配置与主库」，不是「不读」。

### 7.2 runner 环境

`runnerEnv` 只传白名单：

```text
HOME=workDir/home   TMPDIR=workDir/tmp   PWD=workDir/repo
PATH LANG LC_ALL TERM USER SHELL（存在才传）
```

**不传任何凭据变量**。代码注释明确这是有意为之：

> Host credential variables are deliberately not passed: a host-specific runner environment is exactly what a verified runner/platform combination has to establish before its runner is treated as supported.

`docs/03` §6 第 2 步对应写的是「临时目录可写，主仓库和用户配置不可写」。

### 7.3 由此产生的硬约束

runner 必须在**没有真实 HOME、没有凭据变量**的条件下自行完成认证。这是当前唯一的失败根因，见 §9。

## 8. 当前 prompt 全文

`internal/dream/runner.go:77`，12 行，无任何分类规则：

```text
You are organizing a personal memory repository that is checked out read-only-except-for-this-directory.

The current directory contains the memory files. MEMORY.md is the entry point and other Markdown files are linked from it with [[name]] links.

Task:
- Reorganize the memory: merge duplicates, improve headings, group related facts, fix obviously stale or contradictory entries, and keep the root-relative [[link]] syntax valid.
- Preserve every fact you are not confident about; do not invent new facts.
- Edit the files in place. Do not create binary files, symlinks, or directories outside this working directory.
- Do not run destructive commands and do not attempt to modify anything outside this directory.

When you are done, stop. memctl will compute the difference itself.
```

**已知缺陷**：`docs/03` §6 写的是「静态规则识别候选，AI 生成整理建议」，但代码里没有静态候选识别，`doctor` 的诊断结果也没有传入 prompt。判断「哪里重复、哪里过期」完全由模型通读决定。

**已知冲突**：prompt 的 `fix obviously stale or contradictory entries` 与 `docs/10` 的「事件型条目只增不改」直接冲突。模型在没有分类标准时会删除决策记录，这符合它看到的指令。

## 9. 真实 runner 验证结果（关键阻塞）

2026-10-19 在本机逐条实测，全部命令行执行、未改产品代码。

| # | runner | 条件 | 结果 |
| --- | --- | --- | --- |
| 1 | `claude` 2.1.303 | 隔离 HOME + 复制 `.claude`（547 MB）+ `.claude.json` | `Not logged in · Please run /login` |
| 2 | `claude` | 真实 HOME，无沙箱 | 挂起超过 120 秒无输出，已终止 |
| 3 | `codex` 0.160.0 | 隔离 HOME + 复制 `~/.codex/auth.json` | 认证通过，`You've hit your usage limit` |
| 4 | `pi` 1.3.0 | 真实 HOME，无沙箱，`--no-session -ne` | 成功写入，退出 `0` |
| 5 | `pi` | 真实 HOME，Seatbelt 只放行 `workDir` | `EPERM: mkdir ~/.pi/agent/sessions/...` |
| 6 | `pi` | 真实 HOME，Seatbelt 额外放行 `~/.pi/agent` 可写 | 成功写入，退出 `0` |
| 7 | `pi` | **隔离 HOME + 暂存 `auth.json`+`models.json`（8 KB）+ `PI_CODING_AGENT_DIR`** | **成功写入，退出 `0`** |
| 8 | `dsh` 0.2.1-rc.2 | 复制整个 `~/.dsh`（601 MB），隔离 HOME | `MISSING_CREDENTIAL` |
| 9 | `dsh` | 真实 `DSH_HOME`，Seatbelt 下 | 打开 `cordis.yml` 失败 |

结论：

- `claude` 的凭据在 macOS Keychain，复制配置目录无效；无沙箱时也不通（本机无代理）。
- `codex` 认证可过，但账号额度耗尽，**需要用户恢复额度才能作为验收对象**。
- `pi` 是唯一可用的 runner，且实验 7 证明**完全隔离下也能跑通**，只需两个 8 KB 文件。
- `dsh` 的凭据在宿主进程的 credentials service 内，独立进程拿不到。

附带发现：`pi` 在本机装了 `memctl-pi` 扩展，Dreaming 下它会触发一次 `memctl start` / `sync`，污染输出并可能与仓库争锁。**任何方案下都必须加 `-ne`**（`--no-extensions`）。实验 4、6、7 均已加。

## 10. 待决事项：凭据如何进入沙箱

### 10.1 三个选项

| 方案 | 做法 | 代价 |
| --- | --- | --- |
| A. 受控暂存 | runner 声明凭据文件路径，启动前复制到 `workDir/home`，随 `workDir` 删除 | 需实现复制与清理；凭据在临时目录短暂存在 |
| B. 本机凭据 | 保留真实 `HOME`，Seatbelt 按 runner 加写白名单 | **违反 `docs/03` §6「用户配置不可写」**，需改契约 |
| C. 环境变量 | 显式传 `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | 需用户提供 key，与「本地 CLI 不等于本地模型」的告知冲突 |

### 10.2 建议

选 A。理由：

1. 实验 7 已证明可行，不需要新增产品能力，只是补一个已缺的环节。
2. 不改动现有隔离承诺与 `D01` 验收口径。
3. B 放弃的写权限范围远超实际需要——runner 被 prompt 注入影响时能改的不只是记忆，还有 shell、SSH、其他工具凭据。
4. C 把「用本机的 agent」变成「配一把 API key」，与产品定位不符。

### 10.3 选 A 需要的改动

1. `Runner` 增加凭据声明，例如 `Credentials []CredentialFile{Src, DstRel}`。
2. `runRunner` 在启动前复制、设置权限 `0600`、收尾与 `removeWorkDir` 一起删除。
3. `pi` 注册项：`pi -p <prompt> --no-session -ne`，并设 `PI_CODING_AGENT_DIR=<workDir>/home/.pi/agent`。
4. 文档补：`docs/03` §6 增加「凭据暂存」说明；`docs/05` 的 `D01` 增加「暂存凭据随工作目录清理」子场景。
5. 测试补：凭据复制与清理的单元测试；真实 runner 验收记录按 `docs/validation/t0xx-dreaming-real-runner.md` 归档。

## 11. 待决事项：ACP 是否作为接入方式

已调研（[ACP 文档](https://agentclientprotocol.com/)）：

- ACP 是 JSON-RPC over stdio，客户端启动 agent 子进程。
- `fs/read_text_file` 与 `fs/write_text_file` 由**客户端**执行，理论上可代理并拦截 runner 的写入。
- 但现成适配器都不用它：`pi-acp` 的 Limitations 原文为 *No ACP filesystem delegation (`fs/*`) and no ACP terminal delegation (`terminal/*`). pi reads/writes and executes locally.*；`codex-acp` 与 `claude-agent-acp` 也只声明权限与终端，未声明文件委托。
- ACP 不负责凭据分发，`initialize` 与 `session/new` 都没有凭据字段。

结论：ACP 方向正确（「用本机已有 agent」的标准化形式），但**解决不了当前阻塞**，且会引入 Node 依赖、MVP 状态适配器与 runner 层重写。建议登记为未来方向，不在本轮实施。

## 12. 测试覆盖

### 12.1 单元测试（`internal/dream`）

`dream_test.go`：`TestProtectedPath`、`TestUnifiedDiffShape`、`TestServiceRejectsMissingSandbox`、`TestServiceRejectsMissingRunner`、`TestServiceRefusesDirtyRepository`、`TestServiceRefusesStaleBaseRevision`、`TestServiceGenerationPipeline`、`TestServiceRejectsOutOfScopeProposal`、`TestServiceReconcilesOrphanedGeneration`、`TestExportPreservesModeDespiteUmask`、`TestModeChangeIsNormalizedWithWarning`、`TestServiceRequiresEntryPoint`、`TestForeignLoadKeepsLiveWorkDir`

`apply_test.go`：`TestPrepareApplyBuildsOrderedPlan`、`TestPrepareApplyRejectsStaleAndModeChange`、`TestMarkAppliedIsTerminal`

### 12.2 集成测试（`tests/integration`）

| 测试 | 覆盖验收 |
| --- | --- |
| `TestD1DreamMissingRunnerCoversR02` | 无 runner 的定向错误，不回退 |
| `TestD1DreamDirtyRepositoryRefusesCoversD02` | 脏库拒绝生成 |
| `TestD1DreamIsolationCoversD01` | 沙箱阻止写主库与 `~/.claude`；硬链接与越界文件被拒 |
| `TestD1DreamOutOfScopeRejectedCoversD03` | 越界提案被拒且不改主库 |
| `TestD1DreamHTTPPreviewCoversD01AndEvent` | 预览 202 + `dream.updated` |
| `TestD2DreamHTTPApplyCoversD04` | HTTP 应用与重复应用幂等 |
| `TestD2DreamHTTPStaleCoversD02` | 旧基线拒绝应用 |
| `TestD2DreamReadOnlyCoversA03` | 只读模式拦截 |
| `TestD2DreamCLIApplyCoversD04` | CLI 应用 |
| `TestD2DreamPreviewBoundCoversBusy` | 并发上限返回 `BUSY` |

**所有集成测试使用 shell 脚本桩代替真实 runner**（`writeStub`，见 `tests/integration/a4_remote_test.go:45`），这是「真实 runner 未验证」的直接原因：自动化从未调用过真实模型 CLI。

## 13. 已知问题清单

| # | 问题 | 影响 | 状态 |
| --- | --- | --- | --- |
| 1 | 真实 runner 无法在隔离环境下认证 | 功能不可用 | 阻塞，见 §10 |
| 2 | `codex` 账号额度耗尽 | 该 runner 无法验收 | 需用户处理 |
| 3 | 非 macOS 无沙箱后端 | Linux/Windows 返回 `UNSUPPORTED` | 设计如此，未计划 |
| 4 | prompt 无记忆分类规则 | 可能删除事件型条目 | 待改，见 §8 |
| 5 | 无静态候选识别 | `docs/03` §6 的表述与实现不符 | 待澄清或实现 |
| 6 | `pi` 扩展会触发 `memctl start` | 污染输出、可能与仓库争锁 | 已确定加 `-ne`，未写入代码 |
| 7 | 提案永不清理 | 状态目录随使用增长 | 设计如此，未评估 |

## 14. 后续决议与实施顺序（`T082` 已定案）

经 `T082` 方案评审，决定**不采用 §10 的本地凭据暂存方案**，改为迁移至 **GitHub Actions 远端整理架构**，完整设计与契约见 [11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)：

1. **删除本地 runner 与 Seatbelt 沙箱**：移除 `claude`/`codex` 本地 CLI runner 及 `sandbox_darwin.go` 等本地子进程沙箱实现。
2. **代码控制的有界结构化调用**：新增 `memctl dream --worker`，在 GitHub Actions 虚拟机的工作流中按「阶段 1（静态扫描与分簇）→ 阶段 2（受控循环结构化调用 + Step Guard）→ 阶段 3（跨文件一致性收敛）」执行。
3. **内置四个模型提供商**：支持 `deepseek`、`minimax`、`openai`、`anthropic`，用户通过 `--runner <provider>` 指定并在仓库 GitHub Actions Secrets 配置对应 API Key。
4. **阶段 G 实施顺序**：`G-1`（方案与契约修订，`T082` 已完成）→ `G-2`（删除本地沙箱与 CLI runner，实现 `--worker` 与 4 个 Provider）→ `G-3`（实现 Git 分支投递/轮询、`--init-workflow`、`.github` 保护边界与 UI 同步）→ `G-4`（`D01`–`D05` 集成测试与真实 GitHub Actions 验收）。

## 15. 复现命令（`D-1`/`D-2` 历史基线）

```bash
# 单元与集成测试（不接触真实 runner）
go test ./internal/dream/... ./internal/app/... -count=1
go test ./tests/integration/ -run 'TestD1Dream|TestD2Dream' -count=1

# 真实 runner 实验（隔离 HOME + 暂存凭据）
mkdir -p /tmp/p/work/{home/.pi/agent,repo,tmp}
cp ~/.pi/agent/auth.json ~/.pi/agent/models.json /tmp/p/work/home/.pi/agent/
cd /tmp/p/work/repo && printf -- '- fact\n' > MEMORY.md
REAL=$(cd /tmp/p/work && pwd -P)
HOME=$REAL/home TMPDIR=$REAL/tmp PI_CODING_AGENT_DIR=$REAL/home/.pi/agent \
  sandbox-exec -p "$(cat ./seatbelt.profile)" /path/to/pi -p "append '- new' to MEMORY.md" --no-session -ne
```

`seatbelt.profile` 按 `internal/dream/sandbox_darwin.go` 的 `seatbeltProfile` 生成，注意 `workDir` 必须使用 `pwd -P` 解析后的真实路径，否则 `/tmp` 与 `/private/tmp` 不一致会触发 `EPERM`。

## 16. 相关文档索引

| 文档 | 内容 |
| --- | --- |
| [11. GitHub Actions Dreaming](../11-github-actions-dreaming.md) | 远端整理架构、Git 分支协议、有界结构化调用与四家 Provider 规格 |
| [03. 流程与恢复](../03-workflows-and-dataflows.md#6-dreaming-提案) | 七步流程契约与承诺边界 |
| [02. 模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入) | `DreamProposal` 字段与状态语义 |
| [04. CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令) | `dream` 命令、HTTP 安全、只读拦截 |
| [05. 验收与交付](../05-validation-and-delivery.md#2-核心测试矩阵) | `D01`–`D05` 验收场景、阶段 G 交付边界 |
| [10. 记忆更新与过期](../10-memory-lifecycle.md) | 三档记忆规则，与 prompt 冲突之处见 §8 |
| 阶段 D 归档 | `T035`、`T036`、`T037`、`T038` 的实现与审查记录 |
| `tests/integration/t037_release_test.go` | 发布前的路由覆盖检查，含三个 dream 路由 |
