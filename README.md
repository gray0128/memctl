# memctl

`memctl` 计划以 Go 单文件二进制实现 [Agent Memory Repo](https://github.com/AgentMemoryRepo/agentmemoryrepo) 的记忆管理工作流：用 Markdown 保存知识，用 Git 保留历史和同步变更，向 Agent 提供上下文，并内嵌本地 Web UI。

**当前已完成阶段 A-1 的 Go 工程与命令入口、阶段 A-2 的本地仓库初始化、安全读取与格式解析、阶段 A-3 的本地受管写入、显式 capture 与事务恢复、阶段 A-4 的前台远端同步与冲突解决、阶段 A-5 的 Hook 与后台能力、阶段 A-6 的只读诊断与内嵌默认指引（`init`、`read`、`write`、`sync --capture`、`sync --wait`、`sync`/`end` 后台派生、`start`/`context`/`pull`/`tag`/`wrap`、`resolve`、`recover`、`doctor`），以及阶段 B-1 的本地只读 HTTP 安全框架与 `snapshot`/`files`/`entries`/`audit`/`doctor`/`conflict` 只读 API（`internal/server`）；启动入口 `memctl ui`、内嵌前端与 SSE 属后续阶段，尚未实现。** 实施进度、验收证据和下一项任务统一记录在 [开发状态](./docs/STATUS.md)，不以方案中的功能描述代表整体实现完成。

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
  Makefile              # 阶段 A 的 CLI 构建、测试与清理入口
  go.mod                # Go 模块定义（Go 1.27.1）
  cmd/memctl/           # CLI 进程入口与版本注入
  internal/cli/         # 命令分发、init/read/write/sync/end/start/context/pull/tag/wrap/resolve/recover/doctor、参数错误边界与终端检测
  internal/hookio/      # Hook JSON 有界读取、SID 优先级与 stdout envelope 编码
  internal/assets/      # 内嵌默认指引（后续含 adapter 模板）
  internal/config/      # 仓库与状态路径解析、repoId
  internal/git/         # Git 调用、状态检查、环境清理、失败分类与同步原语
  internal/syncer/      # 前台同步编排、错误分类、冲突快照与后台派生
  internal/lock/        # 仓库排他锁；锁描述符可被 Git 子进程继承
  internal/state/       # 事务恢复记录、冲突/操作记录、远端审批、同步摘要与会话缓存
  internal/vault/       # 根相对路径边界、有界读取、格式解析、仓库修订与受管文件写入
  internal/doctor/      # 只读诊断：死链、孤立文件、入口膨胀、元数据与 Git/状态一致性
  internal/server/      # 本地只读 HTTP 路由、Session Token 鉴权、Host/Origin 校验、安全响应头与只读拦截
  internal/app/         # 初始化、读取、受管写入、capture、远端同步、会话上下文、冲突解决、只读诊断与只读查询
  internal/memerr/      # 契约错误码
  tests/integration/    # 跨模块与 CLI 二进制集成测试（R01–R04、A-2、A-3、A-4、A-5、A-6）
  docs/                 # 设计契约、开发循环、任务状态与验收
  脚本化实现参考/         # 只读参考，不作为新实现或发布入口
```

完整代码、测试和构建布局只在 [架构文档](./docs/01-architecture-and-modules.md#5-目录结构与依赖) 定义。未落地的模块目录在首个实际文件实现时创建，不用空目录、占位模块或成功退出的空测试表示功能就绪。

## 开发方式

每次围绕一个可验证目标执行「文档契约 → 最小实现 → 验收证据 → 回写状态」。先从状态台账确认范围，再按方案索引读取相关契约；测试失败回到当前任务修复，影响结果的决策缺失则记录阻塞并确认。流程、状态转换和完成条件见 [开发循环规范](./docs/06-development-loop.md)。

阶段 A 已验证的构建与测试命令（工具链基线 `Go 1.27.1`，不依赖本机记忆库、Git 远端或 Node.js）：

```bash
make build
make test
./bin/memctl version
./bin/memctl help
```

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
```

当前 `version`、`help`、`init`、`read`、`write`、`sync --capture`、`sync --wait`、`sync`/`end` 后台派生、`start`、`context`、`pull`、`tag`、`wrap`、`resolve`、`recover` 与 `doctor` 可用。`doctor` 默认只读，不改变记忆内容、Git 索引或 HEAD；确定性修复 `doctor --fix` 与 `POST /api/v1/doctor:fix` 在阶段 C-1 落地，当前调用 `--fix` 返回 `UNSUPPORTED`。`init` 带 URL 时必须同时提供 `--confirm-private`，且不会自动改向已有 remote 的不同 URL；未接入远端时 `sync --wait` 返回 `remote: local_only`。受管 `write` 与自动 `sync`/`end` 会尽力派生一个非交互后台同步工作进程；设置 `AGENT_MEMORY_BACKGROUND=0` 可关闭该行为。Hook 子命令在 `--stdin` 下以退出码 `0` 结束，避免阻断宿主；`tag` 缺少稳定 SID 时返回退出码 `1`，`wrap` 返回被包装程序的退出状态。未知命令或参数返回用法错误（退出码 `2`）。前端工程与完整打包目标将在对应阶段落地后再接入 `Makefile`。

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
