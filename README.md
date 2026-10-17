# memctl

`memctl` 计划以 Go 单文件二进制实现 [Agent Memory Repo](https://github.com/AgentMemoryRepo/agentmemoryrepo) 的记忆管理工作流：用 Markdown 保存知识，用 Git 保留历史和同步变更，向 Agent 提供上下文，并内嵌本地 Web UI。

**当前已完成阶段 A-1 的 Go 工程与命令入口、阶段 A-2 的本地仓库初始化、安全读取与格式解析、阶段 A-3 的本地受管写入、显式 capture 与事务恢复、阶段 A-4 的前台远端同步与冲突解决、阶段 A-5 的 Hook 与后台能力、阶段 A-6 的只读诊断与内嵌默认指引（`init`、`read`、`write`、`sync --capture`、`sync --wait`、`sync`/`end` 后台派生、`start`/`context`/`pull`/`tag`/`wrap`、`resolve`、`recover`、`doctor`），阶段 B-1 的本地只读 HTTP 安全框架与 `snapshot`/`files`/`entries`/`audit`/`doctor`/`conflict` 只读 API，阶段 B-2 的 `internal/watcher` 递归文件监听与 `GET /api/v1/events` SSE 状态流（`vault.updated` 与 `resync.required` 提示、Token 鉴权与评论心跳），以 `fetch` 流解析 SSE 并在每次连接与重连后重新读取快照，只读模式下服务端拒绝全部写请求。阶段 B 的全部能力已完成：新增只读知识图谱（Cytoscape.js）、条目检索与过滤浏览、审计 Diff 预览（Diff2Html）与诊断计数展示（按 error/warning/info 分组，不生成健康分），图谱与 Diff 库按需分块并随二进制内嵌。阶段 C 已启动：`C-1` 子任务 1 实现了与 CLI 共用锁和版本基线的受管写入、移动改链、条目增删改与跨文件移动、确定性修复与手动同步后端与 API（`PUT /api/v1/files/{path...}`、`POST /api/v1/files:move`、`POST/PATCH/DELETE /api/v1/entries`、`POST /api/v1/doctor:fix`、`POST /api/v1/sync`，以及 CLI `doctor --fix`）；UI 编辑与图谱拆分交互属 `C-1` 子任务 2。`C-1` 与 `C-2` 已全部完成：UI 编辑、条目移动改链、图谱拆分、手动同步与确定性修复交互已接入；冲突解决与审计回滚实现了后端与 API（`POST /api/v1/conflict:resolve`、`POST /api/v1/conflict:abort`、`app.Revert` 与 `POST /api/v1/audit/{hash}/revert`）并接入 UI「冲突解决」与「审计回滚」视图。`C-3` 子任务 1 实现了内嵌 adapter 模板（`internal/assets/adapters/`）、`internal/adapters` 的宿主与插件版本探测、按配置路径独立锁、基线校验、JSON/JSONC/YAML/TOML 保留语义更新、同目录原子替换与中断恢复，以及 CLI `install`（`--all`、`--dry-run`、`--status`）。`C-3` 子任务 2 已实现 `GET /api/v1/adapters` 与 `POST /api/v1/adapters/{agent}/install` 并接入 UI「整理接入」面板，阶段 C 实现审查与收口（`T034`）已完成。阶段 D 的 `D-1` 已实现隔离的 Dreaming 提案生成与持久化预览：`internal/dream`（锁内干净基线、受支持文件导出、平台沙箱受限执行、范围校验与 `$STATE/repos/<repoId>/proposals/` 持久化）、CLI `dream --runner`/`dream --show` 与 `POST /api/v1/dream:preview`、`GET /api/v1/dream/{proposalId}`；`D-2` 子任务 1 已接入显式应用与 UI 整理视图：`app.ApplyDream`（基线校验、过期拒绝、受管事务提交、中断恢复与重复 apply 幂等）、CLI `dream --apply ID --patch-hash HASH`、`POST /api/v1/dream:apply`，以及 UI「整理接入」的提案生成、固定 Diff 预览、状态查询与确认应用。阶段 D 的 `D-2` 子任务 2 已完成跨平台发布工程与全量交付验收：`make release` 交叉编译 darwin arm64/amd64、linux amd64、windows amd64 四个候选二进制，生成 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`，并完成降级与恢复演练、阶段 A–D 全量回归与发布前检查。阶段 A、B、C、D 全部完成。** 实施进度、验收证据和下一项任务统一记录在 [开发状态](./docs/STATUS.md)，不以方案中的功能描述代表整体实现完成。

## 从哪里开始

| 目的 | 入口 |
| --- | --- |
| 了解设计、格式基线和功能边界 | [方案索引](./docs/README.md) |
| 修改文档、实现功能或修复问题 | [开发循环规范](./docs/06-development-loop.md) |
| 查看进度、接续任务或确认阻塞 | [开发状态](./docs/STATUS.md) |
| 确定代码和测试文件放在哪里 | [目标目录结构](./docs/01-architecture-and-modules.md#5-目录结构与依赖) |
| 判断功能是否达到交付条件 | [验收与交付](./docs/05-validation-and-delivery.md) |
| 让 Agent 在本仓库协作 | [Agent 入口](./AGENTS.md) |

## 计划如何工作

1. 会话开始时读取独立记忆仓库，按需同步并注入上下文。
2. Agent 或 UI 读取文件修订，通过受管事务提交记忆，避免静默覆盖其他更新。
3. 本地 Git 提交与远端同步分别报告结果；断网不撤销已保存的本地内容。
4. 开发者通过 UI 检索、审阅历史、解决冲突，并确认 Dreaming 整理提案。

核心运行依赖为系统 Git。Node.js 仅用于前端构建；AI runner 仅用于整理生成。目标平台与接入能力以对应验收结果为准，详见 [架构与模块](./docs/01-architecture-and-modules.md)。

## 目录入口

```text
memctl/
  README.md             # 项目介绍与导航
  AGENTS.md             # Agent 按需加载规则的入口
  .gitignore            # 本机构建、依赖与临时文件排除
  Makefile              # CLI 与前端构建、测试与清理入口（ui/build/test/clean）
  go.mod                # Go 模块定义（Go 1.27.1）
  cmd/memctl/           # CLI 进程入口与版本注入
  internal/cli/         # 命令分发、init/read/write/sync/end/start/context/pull/tag/wrap/resolve/recover/doctor/ui、参数错误边界与终端检测
  internal/hookio/      # Hook JSON 有界读取、SID 优先级与 stdout envelope 编码
  internal/assets/      # 内嵌默认指引与全部支持的 adapter 模板（JSON/JSONC/YAML/TOML/插件文件）
  internal/adapters/    # 宿主与插件版本探测、配置格式定点合并、独立配置锁、基线校验、备份与中断恢复
  internal/config/      # 仓库与状态路径解析、repoId
  internal/git/         # Git 调用、状态检查、环境清理、失败分类、提交日志与提交 Diff（禁用外部 diff/textconv、有界读取）
  internal/syncer/      # 前台同步编排、错误分类、冲突快照与后台派生
  internal/lock/        # 仓库排他锁；锁描述符可被 Git 子进程继承
  internal/state/       # 事务恢复记录、冲突/操作记录、远端审批、同步摘要与会话缓存
  internal/vault/       # 根相对路径边界、有界读取、格式解析、仓库修订与受管文件写入
  internal/doctor/      # 只读诊断：死链、孤立文件、入口膨胀、元数据与 Git/状态一致性
  internal/server/      # 本地 HTTP 路由（snapshot/files/entries/audit/audit diff/doctor/conflict 只读，files 写入、files:move、entries 增删改、doctor:fix、sync 写入）、内嵌首页与静态资源、Session Token 鉴权、Host/Origin 校验、安全响应头、只读拦截与 SSE 状态流（vault.updated、resync.required、心跳）
  internal/watcher/     # 递归文件监听、新建目录登记、rename/remove/原子替换处理与 150ms 防抖扇出
  web/                  # 前端源码（Preact、Vite、Tailwind CSS、Cytoscape.js、Diff2Html）与 web/embed.go 内嵌入口；含概览、知识图谱、条目、审计 Diff 与诊断视图；web/scripts/ 生成 npm 依赖许可证清单；dist 由 make ui 构建，不提交
  internal/app/         # 初始化、读取、受管写入与文件移动、条目增删改与跨文件移动、capture、远端同步、会话上下文、冲突解决、只读诊断、确定性修复与只读查询
  internal/memerr/      # 契约错误码
  tests/integration/    # 跨模块与 CLI 二进制集成测试（R01–R04、A-2、A-3、A-4、A-5、A-6、B-1、B-2、B-3）
  scripts/              # 按需自动化：跨会话接续（next-round.sh）与发布打包（release.sh）
  docs/                 # 设计契约、开发循环、任务状态与验收
  脚本化实现参考/         # 只读参考，不作为新实现或发布入口
```

完整代码、测试和构建布局只在 [架构文档](./docs/01-architecture-and-modules.md#5-目录结构与依赖) 定义。未落地的模块目录在首个实际文件实现时创建，不用空目录、占位模块或成功退出的空测试表示功能就绪。

## 开发方式

每次围绕一个可验证目标执行「文档契约 → 最小实现 → 验收证据 → 回写状态」。先从状态台账确认范围，再按方案索引读取相关契约；测试失败回到当前任务修复，影响结果的决策缺失则记录阻塞并确认。流程、状态转换和完成条件见 [开发循环规范](./docs/06-development-loop.md)。

构建与测试命令（Go 基线 `Go 1.27.1`，前端构建基线 Node v26.10.0 / npm 11.19.1）。`make ui` 执行 `npm ci` 与 `vite build` 生成 `web/dist`，`internal/server` 通过 `web/embed.go` 内嵌该目录；`web/dist` 不提交，因此 Go 构建、`go vet` 与 `go test` 前必须先执行 `make ui`，`make build` 与 `make test` 已包含该依赖。`version` 与 `help` 在运行时不访问仓库，也不要求 Git 或 Node.js：

```bash
make ui
make build
make test
./bin/memctl version
./bin/memctl help
```

发布候选包用 `make release` 生成（先跑全量测试与前端构建，再交叉编译）：

```bash
make release
ls dist/                 # 四个候选二进制、SHA256SUMS、THIRD_PARTY_LICENSES.txt、RELEASE-INFO.txt
(cd dist && shasum -a 256 -c SHA256SUMS)
```

`dist/` 与 `bin/` 不提交。发布产物内的静态资源、默认指引与 adapter 模板全部随二进制打包，不依赖外部 CDN；交叉编译通过只说明可构建，运行与生命周期验证以 [验收与交付](./docs/05-validation-and-delivery.md#3-性能与适配记录) 的记录为准。

GitHub Actions 的 `build` 工作流（`.github/workflows/build.yml`）在 `main` 推送、拉取请求、`v*` 标签推送与手动触发时调用同一 `make release` 入口，构建四个候选二进制并保留 `dist/` 构建产物；推送 `v*` 标签时创建 GitHub Release，并附带四个候选二进制与 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`：

```bash
git tag v0.1.0
git push origin v0.1.0
```

工作流在 `macos-latest` 上运行，`RELEASE-INFO.txt` 记录该次构建宿主的运行时验证状态；流水线通过不替代目标平台的运行时、生命周期与隔离验证。

本地初始化与读取使用隔离路径验证；`init` 不带 URL 时只创建本地仓库与首笔提交，不配置远端：

```bash
export AGENT_MEMORY_DIR=/tmp/memory-demo
./bin/memctl init
./bin/memctl read MEMORY.md
./bin/memctl read MEMORY.md --json

# 受管写入：以 read --json 返回的 fileRevision 为基线；旧基线返回 REVISION_MISMATCH
printf '# Memory\n\n## Index\n- new entry\n' | ./bin/memctl write MEMORY.md --base <fileRevision> --stdin

# 显式接收串行外部编辑，查看或回滚未完成事务
./bin/memctl sync --capture
./bin/memctl recover --status

# 连接已确认私有性和所有权的远端；空远端完成首推，已有历史则克隆并建立 upstream
./bin/memctl init git@github.com:<you>/memory.git --confirm-private

# 前台完整同步；冲突时进入多阶段解决（--json 查看、--apply 提交本阶段结果、--abort 中止）
./bin/memctl sync --wait
./bin/memctl resolve --json
./bin/memctl resolve --apply ./resolution.json --conflict <conflictId>
./bin/memctl resolve --abort --conflict <conflictId>

# 只读诊断：默认检查死链、孤立文件、入口膨胀与运行状态；发现 error 返回退出码 1，仅 warning/info 返回 0
./bin/memctl doctor
./bin/memctl doctor --json

# 确定性修复：当前只重建缺失的入口文件；没有确定性修复的诊断返回 UNSUPPORTED，不猜测修复
./bin/memctl doctor --fix
./bin/memctl doctor --json --fix

# 探测、预览或安装 agent 接入配置；安装不访问记忆仓库，常驻状态在 $AGENT_MEMORY_STATE/adapters/
./bin/memctl install --status
./bin/memctl install --all --dry-run
./bin/memctl install claude-code codex pi

# 本地只读 Web UI：仅绑定 127.0.0.1，首页注入 Session Token；--readonly 在服务端拒绝全部写请求
./bin/memctl ui --no-open
```

当前 `version`、`help`、`init`、`read`、`write`、`sync --capture`、`sync --wait`、`sync`/`end` 后台派生、`start`、`context`、`pull`、`tag`、`wrap`、`resolve`、`recover`、`doctor`、`doctor --fix`、`install` 与 `ui` 可用。`install [agent...]`（`--all`、`--dry-run`、`--status`）按宿主与插件版本探测结果生成安装计划，用格式专用定点合并保留用户已有配置与注释，写入前校验配置基线并备份，安装中断后下一次运行先恢复现场；不支持的结构、未内嵌模板的宿主或缺少必需插件时只给出人工补丁与说明，不强制全文件重序列化。`install` 不访问记忆仓库、不要求 Git 或 Node.js，且不会自动安装第三方插件或修改全局 Git 配置。`doctor` 默认只读，不改变记忆内容、Git 索引或 HEAD；`doctor --fix` 与 `POST /api/v1/doctor:fix` 经受管事务执行确定性修复，首版只重建缺失的入口文件，其余 `issueCodes` 返回 `UNSUPPORTED`。UI 与 CLI 的写入共用同一仓库锁与版本基线：HTTP 写路由为 `PUT /api/v1/files/{path...}`、`POST /api/v1/files:move`、`POST/PATCH/DELETE /api/v1/entries`、`POST /api/v1/doctor:fix` 与 `POST /api/v1/sync`，均要求 `X-Memctl-Token`、Host/Origin 校验，并在 `--readonly` 下返回 `READ_ONLY`。`init` 带 URL 时必须同时提供 `--confirm-private`，且不会自动改向已有 remote 的不同 URL；未接入远端时 `sync --wait` 返回 `remote: local_only`。受管 `write` 与自动 `sync`/`end` 会尽力派生一个非交互后台同步工作进程；设置 `AGENT_MEMORY_BACKGROUND=0` 可关闭该行为。Hook 子命令在 `--stdin` 下以退出码 `0` 结束，避免阻断宿主；`tag` 缺少稳定 SID 时返回退出码 `1`，`wrap` 返回被包装程序的退出状态。未知命令或参数返回用法错误（退出码 `2`）。前端工程已接入 `Makefile`：`make ui` 构建 `web/dist`，Go 构建与测试依赖该产物；跨平台发布打包由 `make release` 完成，候选二进制与校验和、许可证清单位于 `dist/`（不提交），交叉编译通过不代表运行验收。

Hook 场景使用隔离路径验证（`--stdin` 读取宿主 JSON，`--json` 输出单个 envelope）：

```bash
# 会话开始注入：stdout 为上下文或单个 SessionStart envelope；不写工作树
printf '{"session_id":"s1"}' | ./bin/memctl start --json --stdin --agent claude

# 每轮注入：--once 在已成功注入后不重复输出正文，但仍可重试失败同步
printf '{"session_id":"s1"}' | ./bin/memctl context --once --stdin --agent claude

# 纯文本来源标签；缺少稳定 SID 时返回 NO_SESSION_ID
./bin/memctl tag --agent claude --sid s1

# 包装没有 Hook 的命令：运行前同步，向子进程导出会话环境，结束后尽力同步
./bin/memctl wrap --agent claude -- claude
```

## 上游与参考

本项目实现上游记忆格式并扩展受管写入、Hook、UI 和整理工作流，不宣称是上游官方二进制。固定上游版本、兼容范围与有意改变的行为见 [上游与参考边界](./docs/README.md#上游与参考边界)。

`脚本化实现参考/` 用于理解历史命令和适配样例；默认不修改，也不作为新实现已通过验收的证据。
