# memctl

`memctl` 计划以 Go 单文件二进制实现 [Agent Memory Repo](https://github.com/AgentMemoryRepo/agentmemoryrepo) 的记忆管理工作流：用 Markdown 保存知识，用 Git 保留历史和同步变更，向 Agent 提供上下文，并内嵌本地 Web UI。

**当前已完成阶段 A-1 的 Go 工程与命令入口、阶段 A-2 的本地仓库初始化、安全读取与格式解析、阶段 A-3 的本地受管写入、显式 capture 与事务恢复，以及阶段 A-4 的前台远端同步与冲突解决（`init`、`read`、`write`、`sync --capture`、`sync --wait`、`resolve`、`recover`）；尚未实现 Hook 与后台同步、Web UI、诊断、Dreaming 或正式安装流程。** 实施进度、验收证据和下一项任务统一记录在 [开发状态](./docs/STATUS.md)，不以方案中的功能描述代表整体实现完成。

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
  internal/cli/         # 命令分发、init/read/write/sync/resolve/recover、参数错误边界与终端检测
  internal/config/      # 仓库与状态路径解析、repoId
  internal/git/         # Git 调用、状态检查、环境清理、失败分类与同步原语
  internal/syncer/      # 前台同步编排、错误分类与冲突快照
  internal/lock/        # 仓库排他文件锁
  internal/state/       # 事务恢复记录、冲突/操作记录、远端审批与同步摘要
  internal/vault/       # 根相对路径边界、有界读取、格式解析与受管文件写入
  internal/app/         # 初始化、读取、受管写入、capture、远端同步与冲突解决
  internal/memerr/      # 契约错误码
  tests/integration/    # 跨模块与 CLI 二进制集成测试（R03、A-2、A-3、A-4）
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
```

当前 `version`、`help`、`init`、`read`、`write`、`sync --capture`、`sync --wait`、`resolve` 与 `recover` 可用。`init` 带 URL 时必须同时提供 `--confirm-private`，且不会自动改向已有 remote 的不同 URL；未接入远端时 `sync --wait` 返回 `remote: local_only`。`sync` 不带 `--capture`/`--wait` 以及 Hook 类子命令仍返回未实现错误（退出码 `1`）；未知命令或参数返回用法错误（退出码 `2`）。前端工程与完整打包目标将在对应阶段落地后再接入 `Makefile`。

## 上游与参考

本项目实现上游记忆格式并扩展受管写入、Hook、UI 和整理工作流，不宣称是上游官方二进制。固定上游版本、兼容范围与有意改变的行为见 [上游与参考边界](./docs/README.md#上游与参考边界)。

`脚本化实现参考/` 用于理解历史命令和适配样例；默认不修改，也不作为新实现已通过验收的证据。
