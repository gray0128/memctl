# memctl

`memctl` 计划以 Go 单文件二进制实现 [Agent Memory Repo](https://github.com/AgentMemoryRepo/agentmemoryrepo) 的记忆管理工作流：用 Markdown 保存知识，用 Git 保留历史和同步变更，向 Agent 提供上下文，并内嵌本地 Web UI。

**当前仅完成阶段 A-1 的最小 Go 工程与 `version/help` 命令入口，尚未实现记忆库读写、Git 同步、Web UI 或正式安装流程。** 实施进度、验收证据和下一项任务统一记录在 [开发状态](./docs/STATUS.md)，不以方案中的功能描述代表整体实现完成。

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
  Makefile              # 阶段 A-1 的 CLI 构建、测试与清理入口
  go.mod                # Go 模块定义（Go 1.27.1）
  cmd/memctl/           # CLI 进程入口与版本注入
  internal/cli/         # 命令分发、version/help 与参数错误边界
  tests/integration/    # 跨模块与 CLI 二进制集成测试（R03）
  docs/                 # 设计契约、开发循环、任务状态与验收
  脚本化实现参考/         # 只读参考，不作为新实现或发布入口
```

完整代码、测试和构建布局只在 [架构文档](./docs/01-architecture-and-modules.md#5-目录结构与依赖) 定义。未落地的模块目录在首个实际文件实现时创建，不用空目录、占位模块或成功退出的空测试表示功能就绪。

## 开发方式

每次围绕一个可验证目标执行「文档契约 → 最小实现 → 验收证据 → 回写状态」。先从状态台账确认范围，再按方案索引读取相关契约；测试失败回到当前任务修复，影响结果的决策缺失则记录阻塞并确认。流程、状态转换和完成条件见 [开发循环规范](./docs/06-development-loop.md)。

阶段 A-1 已验证的构建与测试命令（工具链基线 `Go 1.27.1`，不依赖本机记忆库、Git 远端或 Node.js）：

```bash
make build
make test
./bin/memctl version
./bin/memctl help
```

当前除 `version` 与 `help` 外，其余规划子命令均明确返回未实现错误（退出码 `1`），未知命令或参数返回用法错误（退出码 `2`）。前端工程与完整打包目标将在对应阶段落地后再接入 `Makefile`。

## 上游与参考

本项目实现上游记忆格式并扩展受管写入、Hook、UI 和整理工作流，不宣称是上游官方二进制。固定上游版本、兼容范围与有意改变的行为见 [上游与参考边界](./docs/README.md#上游与参考边界)。

`脚本化实现参考/` 用于理解历史命令和适配样例；默认不修改，也不作为新实现已通过验收的证据。
