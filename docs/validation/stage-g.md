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

## T084：发布 `v0.7.0`

- 目标与范围：把 `T083` 的 GitHub Actions Dreaming 迁移随二进制发布，更新版本台账与发布说明；不改变产品行为。
- 契约：[发布入口](../04-ui-and-cli-specification.md#61-发布入口)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-19，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1。
- 改动：`README.md` 与 `STATUS.md` 的当前版本改为 `v0.7.0`；`scripts/release.sh` 的 `RELEASE-INFO.txt` 说明补入内嵌 Actions 工作流模板与 Dreaming 的真实前置条件；同步 `07`/`08` 的插件 tarball 版本表述；修复 `hostVersionTimeout` 由 3 秒提升到 10 秒，避免并行编译负载下把正常宿主误判为 `cannot parse the version`（`I01` 曾因此间歇失败）。
- 验证：`make test` 全量通过（单元包、两个插件 Node 测试、全部集成测试）；`go vet ./...` 无输出；`git diff --check` 无输出。
- 发布结果：提交 `8bababd`，标签 `v0.7.0`；工作流运行 [39220876567](https://github.com/gray0128/memctl/actions/runs/39220876567) 的 `verify`、`build`、`publish` 全部成功，Release 于 2026-10-19T08:15:46Z 创建，附件为四个候选二进制、`memctl-dsh-0.5.1.tgz`、`memctl-pi-0.5.1.tgz`、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`；附件下载到独立目录 `/tmp/memctl-v0.7.0` 后 `shasum -a 256 -c SHA256SUMS` 六项全部 OK，`memctl-darwin-arm64 version` 输出 `memctl v0.7.0`、`help dream` 显示五种模式；本机 `~/.agent-memory/bin/memctl` 已替换为该附件（SHA-256 与 `SHA256SUMS` 一致）。本地 `make release` 未参与发布。
- 未验证与遗留：`G-4` 真实 GitHub Actions 与模型端点端到端仍未执行；插件源码未改动，npm 不发布新版本。

## T085：修复工作流校验和步骤并发布 `v0.7.1`

- 目标与范围：修复 `T083` 内嵌工作流模板中的真实缺陷——`SHA256SUMS` 记录的是发布资产名 `memctl-linux-amd64`，模板却把下载保存为 `/tmp/memctl`，导致 `sha256sum -c` 找不到文件，`v0.7.0` 的首次真实 Actions 运行在「安装 memctl Linux 二进制」步骤失败。本轮只改模板与文档，不改 worker 与传输协议。
- 契约：[11. GitHub Actions Dreaming](../11-github-actions-dreaming.md#25-工作流文件与自部署配置)、[发布入口](../04-ui-and-cli-specification.md#61-发布入口)。
- 现象与证据：`v0.7.0` 在 `~/memory`（`gray0128/agent-memory-repo`）安装工作流后触发运行 [39222156552](https://github.com/gray0128/agent-memory-repo/actions/runs/39222156552)，日志为 `sha256sum: memctl-linux-amd64: No such file or directory`、`FAILED open or read`，作业在 12 秒内结束；本地同步侧因缺少 `dream/res` 结果而持续等待，直到取消。
- 修复：模板把二进制与 `SHA256SUMS` 下载到 `/tmp/memctl-dream/`，文件名与资产名一致，在同一目录执行校验后从该路径调用 `--worker`；文档 §2.5 的模板片段同步。新增 `internal/assets/assets_test.go`，断言下载名、校验命令、执行路径、版本占位符、触发分支与 `contents: write`，防止模板再次与发布资产命名脱节。
- 验证：`go test ./internal/assets/ ./internal/dream/` 通过；`go vet ./...` 无输出；用修复后的二进制在临时仓库执行 `dream --init-workflow`，渲染出的步骤与断言一致。
- 未验证与遗留：修复后的工作流需随补丁版本发布后才能在真实 Actions 中复验；`G-4` 的真实模型调用仍未执行（仓库尚无提供商 Secret）。

## T086：修复 worker 在 Actions 中的仓库定位并发布 `v0.7.2`

- 目标与范围：修复 `--worker` 在 GitHub Actions 中定位记忆仓库的方式。`v0.7.1` 的两次真实运行都在「运行 Dreaming worker」步骤失败，报 `GIT_FAILED: git remote failed: fork/exec /usr/bin/git: no such file or directory`。根因是 `memctl dream --worker` 沿用本地配置的默认仓库 `~/memory`，而 runner 的工作目录是 `$GITHUB_WORKSPACE`，`git` 于是在一个不存在的目录里启动，Go 把 `chdir` 失败报成了可执行文件缺失。
- 契约：[11. GitHub Actions Dreaming](../11-github-actions-dreaming.md#25-工作流文件与自部署配置)、[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)。
- 证据：诊断仓库 `gray0128/memctl-dream-diag` 的运行 [39224706290](https://github.com/gray0128/memctl-dream-diag/actions/runs/39224706290) 中，同一作业内 `command -v git`、`/usr/bin/git`、`git --version` 全部正常，只有 worker 步骤报该错误；`~/memory` 版本 [39224378495](https://github.com/gray0128/agent-memory-repo/actions/runs/39224378495) 表现相同。以 `GITHUB_WORKSPACE=/tmp/diagmem` 在本地复现同一环境后，修复前失败、修复后推进到推送结果分支。
- 修复：`--worker` 先解析工作仓库——显式 `AGENT_MEMORY_DIR` 优先，否则使用 `GITHUB_WORKSPACE`，两者都缺失或目录不存在时返回 `INVALID_ARGUMENT`/`NO_REPO`，不再静默回退到默认路径；新增 `internal/cli/dream_worker_test.go` 覆盖三种分支。
- 验证：`go test ./internal/cli/ ./internal/dream/ ./internal/assets/` 通过；`go vet ./...` 无输出；本地以 Actions 环境变量组合执行 `/tmp/memctl-fix2 dream --worker`，错误从「Git 可执行文件缺失」变为「无法推送结果分支」（本地裸仓库无推送凭据），确认仓库定位已修复。
- 复验结果（`G-4`，2026-10-19）：`v0.7.2` 发布后，`~/memory`（`gray0128/agent-memory-repo`）完成首次真实端到端运行：
  1. 工作流更新到 `MEMCTL_DREAM_VERSION: v0.7.2` 并推送。
  2. `memctl dream --runner deepseek` 推送 `dream/req/dp-9047541efb4805869af58535518426a8/deepseek`；运行 [39226384745](https://github.com/gray0128/agent-memory-repo/actions/runs/39226384745) 成功：删除请求分支 → 校验并安装 `memctl-linux-amd64` → `--worker` 以 `deepseek-chat` 执行 1 步 → 推送结果提交 `476969a`。
  3. 本地拉取结果、重算 Diff 与 `patchHash`，提案 `dp-9047541efb4805869af58535518426a8` 状态 `ready`，`changes` 为 0，`warnings` 为 4 条（入口索引行缺元数据、`notes/memctl-todo-audit.md` 的历史状态条目按事件型保留、无到期条目、worker 未产生改动）。
  4. 空提案执行 `--apply` 返回 `INVALID_ARGUMENT: proposal ... has no changes to apply`，主库未产生提交（`HEAD` 仍为 `9bfbf0e`）。
  5. 本地无残留 `refs/memctl/dream/*`，远端无残留 `dream/req/*` 与 `dream/res/*`。
- 未验证与遗留：`G-4` 只验证了「运行成功但模型无改动」的路径，尚未验证一次带实际文件变更的提案被 `apply` 提交；诊断仓库 `gray0128/memctl-dream-diag` 需要 `delete_repo` scope 才能删除（`gh auth refresh -h github.com -s delete_repo`）。

## T087：工作流版本绑定与漂移拦截

- 目标与范围：把「工作流固定 worker 版本」从安装时的隐含约定变成可检查的不变量。提出问题的背景是：`--init-workflow` 把当时的 memctl 版本写进工作流后，升级 memctl 会让本地校验规则与 Actions 中运行的 worker 版本错位，而就绪检查只验证文件存在，不读内容，漂移完全无声。
- 契约：[11. GitHub Actions Dreaming](../11-github-actions-dreaming.md#25-工作流文件与自部署配置) 的「版本绑定规则」、[CLI 与 UI](../04-ui-and-cli-specification.md#5-http-安全)。权威定义只写在 `11`，其余位置链接引用。
- 决策：继续固定版本，不改成 `latest`。`latest` 是可变指针，无法用 `SHA256SUMS` 固定内容，同一份记忆在不同时间会被不同版本的 worker 处理，Step Guard 与输出 Schema 会随上游静默漂移，提案失去可复现性；工作流本身是仓库中被审阅、被提交的产物，固定值才可审计。代价是升级后需要刷新工作流，因此由就绪检查强制提示。
- 实现：`dream.Service.Version` 由应用服务注入（CLI `dream`/`ui` 通过 `App.SetVersion`）；就绪检查读取工作流的 `MEMCTL_DREAM_VERSION`，与当前版本不一致或仍是占位符时返回新增的 `WORKFLOW_STALE`，`dream --runner` 在仓库锁内以 `UNSUPPORTED` 拒绝生成并提示 `memctl dream --init-workflow`。开发构建（`v2.0.0-dev`）没有 Release 可下载，豁免校验并保留占位符。
- 验证：`go test ./internal/dream/ ./internal/app/ ./internal/cli/ ./internal/server/` 与 `make test` 全量通过；新增 `TestWorkflowVersionDriftBlocksGenerationCoversD01`（占位符、版本不一致、刷新后恢复、开发构建豁免）与 `TestWorkflowPinnedVersionParsing`；`cd web && npm test` 86 项通过。用 `-X main.version=v0.7.2` 构建的二进制在临时仓库实测：占位符工作流被拒绝，`--init-workflow` 后写入 `v0.7.2` 并通过就绪检查进入等待。
- 未验证与遗留：真实环境中尚未出现一次「升级 memctl 后未刷新工作流」的实际拦截；UI 侧只通过 meta 的 `unavailable` 与原因文案呈现，未做浏览器验证（本轮未要求）。

## T088：发布 `v0.7.3` 与版本漂移真实验收

- 目标与范围：发布带 `WORKFLOW_STALE` 拦截的 `v0.7.3`，并在真实仓库上验证拦截与刷新流程；不改变产品行为（`T087` 已实现）。
- 契约：[发布入口](../04-ui-and-cli-specification.md#61-发布入口)、[版本绑定规则](../11-github-actions-dreaming.md#25-工作流文件与自部署配置)。
- 发布结果：提交 `2d8d50e`，标签 `v0.7.3`，运行 [39228807441](https://github.com/gray0128/memctl/actions/runs/39228807441) 的 `verify`、`build`、`publish` 全部成功；附件下载到 `/tmp/memctl-v0.7.3` 后 6 项校验和全部 OK，`memctl-darwin-arm64 version` 输出 `memctl v0.7.3`。
- 真实验收：本机升级到 `v0.7.3` 后，`~/memory` 的工作流仍固定 `v0.7.2`，`memctl dream --runner deepseek` 被拦截为 `UNSUPPORTED: ... pins v0.7.2 but this memctl is v0.7.3; run memctl dream --init-workflow to refresh it`；执行 `--init-workflow` 并推送后版本一致，随后运行 [39229750039](https://github.com/gray0128/agent-memory-repo/actions/runs/39229750039) 成功，提案 `dp-0774eb99…` 状态 `ready`（模型仍判定无需改动，0 处变更）。
- 附带修复（运维侧，非产品缺陷）：直接 `cp` 覆盖正在运行的 `memctl`（`memctl ui` 进程持有该 inode）会触发 macOS SIGKILL，表现为二进制文件校验和正确但一执行就退出；改用「写临时文件 + `mv` 原子替换」后正常。`internal/update` 已采用 `os.Rename`，不受影响；手工升级应按同样方式替换。
- 未验证与遗留：带实际文件变更的提案被 `apply` 提交仍未在真实环境验证。诊断仓库 `gray0128/memctl-dream-diag2` 已创建并配好工作流，但没有对应 Secret；删除两个诊断仓库需要 `delete_repo` scope。

## T089：新增条目检索通道（`entries` 与 `memory_entries`）

- 目标与范围：补齐「Agent 按内容查询记忆」的缺口。此前只有 hook 注入（给出 `MEMORY.md` 全文）与按路径读取（`memctl read` / `memory_read`），子串检索能力只存在于 Web UI 使用的 `GET /api/v1/entries`，CLI 与 MCP 都没有入口；沙箱宿主的 bash 又读不到 `~/memory`，Agent 因此无法自行定位事实。
- 契约：[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令) 的命令表、[MCP 记忆工具](../09-mcp-memory-tools.md#44-工具集) 的工具表与 `M13`。
- 实现：把已有的 `App.ListEntries` 暴露为两条通道，不新增存储、索引或检索算法。
  1. `memctl entries`：`--q`（大小写不敏感子串）、`--file`、`--agent`、`--device`、`--from`、`--to`、`--limit`、`--offset`、`--json`；文本输出为 `path:line\tfact` 加 `--- N of M entries` 尾部，JSON 输出 `{revision, items, total}`。
  2. MCP `memory_entries`：同一组参数与同一返回结构，参数原样下发到 `ListEntries`；负 `limit`/`offset` 在调用前拒绝，契约失败以 `isError` 携带错误码返回。
- 边界：`q` 只匹配已存储的条目正文，不建立索引、不做语义或向量检索；`limit` 上限 200、默认 50，与 HTTP 路由一致；只读，不写仓库、不产生提交。
- 验证：`make test` 全量通过；新增 `TestEntriesCLIFilters`、`TestEntriesCLIUsageErrors`、`TestEntriesRejectsPageBeyondLimit`、`TestM13MemoryEntries`（MCP 层）与 `TestM13MCPEntriesFindsFactThenReads`（真实二进制端到端：`memory_entries` 定位 → `memory_read` 读取 → 确认 HEAD 与工作树未变 → CLI 与 MCP 返回同一 `total`）。`docs/09` 的 `M02` 与工具表同步为三个工具。
- 未验证与遗留：各宿主侧只更新了文档与帮助文本，未逐个宿主重跑「模型实际调用 `memory_entries`」；宿主注册的命令没变（仍是同一个 `memctl mcp`），已有注册无需改动。

- 真实仓库冒烟（2026-10-19）：`~/memory` 上执行 `entries --q 沙箱` 返回 `MEMORY.md:8` 与 `notes/dsh-memory-updates.md:4` 并给出 `--- 2 of 3 entries; continue with --offset 2`；`entries --agent dsh --limit 2` 返回 `--- 2 of 19 entries`，来源筛选生效。

## T090：审计视图来源、分页与时间轴修正

- 目标与范围：修正审计视图的三个可观察问题。截图中 `memctl dream: install the Dreaming workflow` 显示「未知来源」、长历史没有翻页入口、时间轴竖线与圆点错位且时间格式不可扫读。
- 契约：[审计对象](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[审计路由](../02-domain-models-and-contracts.md#42-路由)、[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[受管写入事务](../03-workflows-and-dataflows.md#2-受管写入事务)、[Revert](../03-workflows-and-dataflows.md#53-revert)。
- 来源（现象 1）：根因不是解析失败，而是这些提交没有写来源 trailer。`git.Commit` 直接提交的三处路径（`init` 首个提交、`dream --init-workflow`、`dream --apply` 通过受管事务）与 `git revert --no-edit` 生成的提交都不带 `Agent:`/`Session:`/`Device:`。
  1. 新增 `git.Source` 与 `git.AppendTrailers`，作为唯一 trailer 生成实现；`commitMessage` 改为复用它，输出保持一致。
  2. `App.SetCommitSource` 与 `app.CommitSource(getenv)` 由 CLI `init`、`dream` 与 `ui` 入口设置；`dream.Service.CommitSource` 供工作流安装提交使用。
  3. 无调用方来源时 `localSource` 把 Agent 替换为 `memctl`，因此库级调用同样产生可归属提交；根提交与 dream 提交都记录设备。
  4. revert 在 Git 生成提交后用 `git.AmendMessage` 追加 trailer，`This reverts commit <id>` 正文保留，返回的 hash 是重写后的值。
  5. 前端把「未知来源」改为「未记录来源」，并给出悬停说明：审计视图不根据作者或提交内容推断来源。
- 分页（现象 2）：审计接口是游标分页且不返回总数，`nextCursor` 为空时旧实现不渲染任何控件。改为每页 20 条、上一页/下一页加已访问页码；只有当前页有 `nextCursor` 时下一页可用，刷新或回滚后游标链重置；页码只列已访问页，因为 API 不提供总数。
- 时间轴（现象 3）：圆点与竖线改到同一 `left-2` 列并用 `-translate-x-1/2` 居中对齐，首末卡片把连接线截到圆点中心以消除溢出线头；按本地日期分组并显示每组提交数；时间显示为本地 `YYYY-MM-DD HH:mm`、悬停给出原始 ISO；来源拆成 Agent/设备/会话三个标签；卡片内边距、行距与字号收紧。
- 验证（2026-10-19，darwin/arm64）：`go test ./internal/git ./internal/app ./internal/dream ./internal/cli ./internal/server` 与 `go test ./tests/integration -run 'TestT027|TestC2|TestB1|TestB3'`、`web` 的 `npm test`（94 项）全部通过；`go vet ./internal/...` 无输出。新增 `TestAppendTrailers`、`TestParseLogReadsAppendedTrailers`、`TestAuditResolvesRootAndWorkflowSource`、revert 与 dream apply 的 trailer 断言，以及 `web/src/audit.test.js` 的 8 项来源/分组/分页用例。
- 界面验证（ego-browser，真实二进制与临时记忆仓库，26 次提交）：第 1 页 20 条、下一页可用；第 2 页 6 条且「下一页」禁用、页码出现 1/2；提交展开后 Diff 与回滚面板正常；两条无 trailer 的合成提交显示「未记录来源」并带悬停说明；圆点与竖线同心、日期分组标题与计数正确。
- 未验证与遗留：来源补齐只对之后产生的提交生效，旧版本已写入且没有 trailer 的历史提交仍显示「未记录来源」，不提供历史改写；`dream --worker` 在 Actions 中的结果提交沿用其原有格式，不进入本地审计时间轴；窄屏（<640px）下的时间轴未单独复验。

## T091：拆分整理与客户端接入视图并展示 MCP 状态

- 目标与范围：落实待办中的前两项。原先唯一的「整理接入」视图同时承载 Dreaming 提案与 adapter 安装，职责混杂；MCP 通道状态只存在于 `install --status` 的文本输出，UI 无法查看。本轮把视图拆为「整理」（仅 Dreaming）与「客户端接入」（MCP 通道 + adapter 安装），并让 UI 展示 MCP 的注册动作与可用性。
- 契约：[Web UI](../04-ui-and-cli-specification.md#4-web-ui)、[REST 路由](../02-domain-models-and-contracts.md#42-路由)、[MCP 记忆工具的探测](../09-mcp-memory-tools.md#53-探测)。
- 后端：新增 `GET /api/v1/mcp`，返回 `{executable, available, version?, tools, reason?, registrations: [{agent, command?, instruction?}]}`。探测与注册表从 `internal/cli` 移到 `internal/mcp`（`StatusEnv`/`Status`/`Registrations`），`printMCPStatus` 与 HTTP 路由共用同一实现，避免 CLI 与 UI 的输出漂移。`server.Options` 新增可注入的 `MCPStatus`，集成测试不启动子进程。
- 前端：`app.jsx` 新增「整理」导航项（`dream`），原 `adapters` 项改名为「客户端接入」；新增 `DreamView.jsx`（只包 DreamPanel）与 `MCPPanel.jsx`（通道状态、工具列表、逐宿主注册动作与复制按钮、Codex/pi/Grok 的前置条件提示）；`AdapterView.jsx` 移除 DreamPanel，改为在顶部渲染 MCPPanel。`adapters.js` 新增 `mcpAvailable`/`mcpReason`/`mcpTools`/`mcpAction` 纯函数并补单元测试。
- 边界：MCP 面板只读。注册仍由宿主自己的 `mcp add` 完成，memctl 不运行宿主命令、不解析或合并宿主 MCP 配置；二进制未响应 `mcp` 时展示原因并保留注册动作供核对，不隐藏面板。
- 验证（2026-10-19，darwin/arm64）：`go test ./internal/... ./plugins/...` 全部通过；`go vet ./internal/...` 无输出；`cd web && npm test` 96 项通过；`make build` 成功。新增 `internal/mcp/status_test.go`（3 项）、`internal/server/mcp_test.go`（3 项）、`tests/integration/t091_mcp_status_test.go`（路由契约、只读、不写宿主配置）与 `web/src/adapters.test.js` 的 MCP 用例；`t037` 路由表与 `b1` 路由冒烟补入 `/api/v1/mcp`，分片清单登记三条新测试。
- 真实运行（隔离 `HOME` 与临时状态目录，`memctl ui --port 0`）：`GET /api/v1/mcp` 返回 `available: true`、版本 `v0.7.3-4-g335df66-dirty`、三个工具与 8 条注册动作（7 条命令 + dsh 手工步骤，均指向实际二进制绝对路径）；构建产物包含「记忆整理」「MCP 记忆工具通道」「客户端接入」文案。
- 未验证与遗留：未做浏览器内的视觉与交互验证（本轮未要求 UI/UX 验证）；「整理」页沿用原 DreamPanel 的轮询与禁用逻辑，未改动其行为。

## T092：发布 `v0.8.0`

- 目标与范围：发布含 `T090`（审计视图修正）与 `T091`（视图拆分与 MCP 状态）的 `v0.8.0`；插件源码未改动，保持 `0.5.1`，不发 npm。
- 契约：[发布入口](../04-ui-and-cli-specification.md#61-发布入口)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)。
- 发布结果：提交 `9a27293`，标签 `v0.8.0`，运行 [39245974657](https://github.com/gray0128/memctl/actions/runs/39245974657) 的 `verify`（4 个分片）、`build`、`publish` 全部成功，Release 见 [v0.8.0](https://github.com/gray0128/memctl/releases/tag/v0.8.0)。
- 附件与校验：下载到独立目录 `/tmp/memctl-v0.8.0` 后 `SHA256SUMS` 六项全部 OK；附件为四平台候选二进制、`memctl-dsh-0.5.1.tgz`、`memctl-pi-0.5.1.tgz`、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`；`memctl-darwin-arm64 version` 输出 `memctl v0.8.0`，`RELEASE-INFO.txt` 的 `commit` 为 `9a27293`、宿主运行时验证仅标注 darwin/arm64。
- 本机升级与冒烟：按记录用「下载到 `/tmp` → 校验 → `mv` 原子替换」升级 `~/.agent-memory/bin/memctl` 到 `v0.8.0`；`version` 输出 `memctl v0.8.0`，`install --status` 的 `mcp:` 块输出 `serving memory_read, memory_entries, memory_write (v0.8.0)` 与 8 条注册动作。用发布二进制在隔离 `HOME` 与临时状态目录启动 `ui --port 0`，`GET /api/v1/mcp` 返回 `executable=~/.agent-memory/bin/memctl`、`version=v0.8.0`、三个工具与 8 条注册；内嵌脚本 `/assets/index-BGLdd-0y.js` 含「记忆整理」「MCP 记忆工具通道」「客户端接入」。
- 未验证与遗留：macOS amd64、Linux amd64、Windows amd64 仍只交叉编译；浏览器内的视觉与交互未复验（本轮未安排 UI/UX 验证）；npm 插件包未随本版发布，仍为 `0.5.1`。
- pi.dev 目录（2026-10-19 核对）：`memctl-pi` 已发布到 npm 且带 `pi-package` 关键字与 `pi.extensions` 清单，但 `https://pi.dev/packages?name=memctl` 返回 0 条。目录只索引满足其收录条件的包（页面写明「published to npm」，同类未收录包亦可检索到），从外部无法确认是索引延迟、收录条件还有额外要求，还是需要人工提交；下一步需查 pi 的包文档或向维护者确认，不在本轮猜测提交路径。

## T093：定位 pi.dev 目录未收录的根因

- 目标与范围：待办要求「让插件出现在 pi.dev」。本轮只做只读排查与证据收集，不发布、不改包内容；结论用于决定下一步是重新发布、补元数据还是向上游报 issue。
- 契约：[pi 专用插件 §8.1](../08-pi-plugin.md#81-npm-主通道)。
- 观测（2026-10-19）：
  1. `https://pi.dev/packages/memctl-pi` 返回 200，正常渲染版本 `0.5.1`、README、`pi` manifest、License 与安装命令——详情页数据源正常。
  2. `https://pi.dev/packages?name=memctl-pi` 与 `?name=memctl` 均返回 `0 / 5327`；分类页不出现该包。
  3. npm 官方 search 索引里 `keywords:memctl` 能命中该包，但 `score.final = 0`、`downloads = {monthly: 0, weekly: 0}`，`name:memctl-pi`、`memctl-pi`、`keywords:pi-package` 的分页枚举（含尾部 `from=12000`–`from=20000`）都找不到它；npm 官方统计接口 `downloads/point/last-week` 却返回 21 次，说明包已发布且被下载。
  4. 对照已收录的 `pi-theme-aura`（184 次/月）、`pi-smart-ralph`、`pi-thought-bubbles`、`pi-cbmem`（未收录但属于 monorepo、有 `repository.directory`）与 `pi-researcher`（未收录）后，收录与下载量、readme 长度、`author`、`homepage`、`bugs`、`repository.directory` 均无一致相关性；已收录包在同名查询下的 `score.final` 为 46–84，未收录的为 0 或不在索引。
  5. `pi-theme-aura` 的 registry 文档 `readmeFilename` 为 `README.md`，`memctl-pi` 的 `readme` 为空、`readmeFilename` 为空；但 `pi-smart-ralph` 同样 `readme = 0` 却已收录，因此 README 缺失不是充分条件。
  6. 上游 [issue #11130](https://github.com/earendil-works/pi/issues/11130)（2026-10-19 自动关闭）记录同类现象：详情页 200、`?name=` 0 条、npm search 分数 0；报告者的排查同样排除了下载量与发布时间，并推测与索引文档构建有关。
- 结论：pi.dev 目录直接消费 npm search 索引，收录门槛不在 pi.dev 侧。本包的问题表现为 npm 索引文档的 `score.final = 0` 且不在可枚举窗口内，属于 npm 侧的索引状态，`pi` 仓库文档只声明 `pi-package` 关键字即可发现。因此「补关键字、补仓库字段」不是修复路径。
- 阻塞与解除条件：修复需要让 npm 重新索引该包，可行做法是发布一个新版本（例如 `0.5.2`，同时补齐 registry 文档缺失的 README 元数据）；本机 `~/.npmrc` 的 `registry.npmjs.org` token 已失效（`npm whoami` 返回 401，`GET /-/whoami` 返回 401），无法执行发布。解除条件：用户重新执行 npm 登录并确认可以发布 `memctl-pi` 新版本。
- 未验证：重新发布能否让索引恢复，需要一次真实发布后复测 `?name=memctl-pi` 与 `name:memctl-pi` 才能确认；向 npm 提交索引重建请求是否可行未验证。

## T094：记忆过期规则的读取侧实现

- 目标与范围：落实 [10. 记忆更新与过期](../10-memory-lifecycle.md) §3.3/§3.4。用户于 2026-10-19 确认三个未决问题：隐藏期 30 天、三个读取入口统一过滤、事件型条目继续留在 `MEMORY.md`/`notes/`。本轮实现 `until` 解析与读取期隐藏；隐藏期满后的删除未实现。
- 契约：[记忆生命周期](../10-memory-lifecycle.md#34-判定与隐藏的落地口径)、[REST 路由](../02-domain-models-and-contracts.md#42-路由)。
- 实现：
  1. `internal/vault/parse.go` 把合法 `until: YYYY-MM-DD` 解析为 `MemoryEntry.UntilDate`；缺失或非法值保持空，按永不过期处理，元数据原文与顺序不变。
  2. `internal/vault/lifecycle.go`、`hide.go` 提供唯一判定与投影：`EntryExpired` 以 `until` 次日本地零点为界，`HideExpiredEntries` 只替换到期条目的正文，保留 `until`/`source`/`added` 原文与其他所有字节。
  3. `App.ReadFile` 对 Markdown 应用投影并返回 `HiddenExpired`；`scanVault` 在快照与 `ListEntries` 中剔除到期条目，快照新增 `hiddenExpired` 总数；注入复用 `ReadFile`，因此自动一致。
  4. 三个入口的响应都带上计数：CLI `read --json`、MCP `memory_read` 与 `GET /api/v1/files/{path...}` 增加 `hiddenExpired`；`VaultSnapshot` 增加同名顶层字段。
  5. 写入路径不变：`planPut` 仍以磁盘原文做基线校验，隐藏条目在下次写入时按原样保留；测试 `TestWriteAfterHideKeepsExpiredEntry` 覆盖。
  6. 内嵌指引与 `skills/memctl` 约定补入 `until` 规则，`R01` 增加对应断言。
- 验证（2026-10-19，darwin/arm64）：`go test ./internal/...` 全部通过；`web/src` 未改动。新增 `internal/vault/lifecycle_test.go`（判定边界、解析、投影）、`internal/app/lifecycle_test.go`（读取投影、快照、注入、写入保留）、`tests/integration/t094_expiry_test.go`（HTTP 读取、快照与条目、注入），分片清单登记三条集成测试。
- 真实二进制手测（隔离 `HOME` 与临时仓库）：`read MEMORY.md --json` 返回 `hiddenExpired: 1`，正文里到期条目变成占位符且保留 `until`/`source`/`added`；`entries --json` 的 `total` 为 3，不含到期条目；`start --sid s1` 注入的同样是占位符；磁盘文件仍含原文；基于该读取结果做一次 `write` 后，被隐藏条目仍完整保留，再次读取仍报 `hiddenExpired: 1`。
- 未验证与遗留：隐藏期满 30 天后的删除入口未实现（不在读取路径自动删除）；`until` 只支持日期粒度，不含时刻；时区取本地时钟，跨时区设备对同一 `until` 的切换时刻可能相差数小时；UI 未展示 `hiddenExpired`（本轮未要求视觉验证）。

- 补充（同日）：`doctor` 新增 `EXPIRED_ENTRY`（info）诊断，报告已隐藏条目及其行号，不标记 `autoFixable`；`doctor.CheckAt` 允许测试固定时钟。新增 `TestCheckReportsExpiredEntryAsInfo` 与 `TestT094DoctorReportsExpiredEntry`，分片清单登记后者。

- 再补充（同日）：MCP `memory_read` 的返回体带上 `hiddenExpired`，`docs/09` 的 `M03` 与工具表同步；新增 `TestMemoryReadReportsHiddenExpired`。`make test` 全量通过（含 `tests/integration` 178 秒）。

- 删除闸门（同日）：`vault.DeletionDue`/`DeletionBlockedReason`/`HiddenPeriodDays` 给出 30 天隐藏期判定；`App.DeleteEntry` 在删除已到期条目时校验隐藏期，未满返回新错误码 `DELETION_DEFERRED`（HTTP 409，消息给出可删除日期）。新增 `TestDeletionWindowCoversHiddenPeriod`、`TestDeleteEntryRespectsHiddenPeriod`、`TestDeleteEntryWithoutUntilIsAlwaysAllowed`、`TestT094DeleteDeferredInsideHiddenPeriod` 与 `TestT094DeleteWithoutUntilStillWorks`；后两条登记到分片清单。批量清理由哪个入口发起仍未定。
