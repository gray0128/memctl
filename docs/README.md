# `memctl` 二进制实现方案

`memctl` 用 Go 二进制管理独立的 Git 记忆仓库：向 Agent 提供上下文，接收记忆变更，协调本地提交与远端同步，并内嵌 Web UI、诊断、冲突处理、记忆整理和 Agent 接入功能。Markdown 与 Git 是事实来源，派生索引和缓存可重建；恢复记录与待审阅提案按架构要求持久保存。

本目录是修订后的设计，不表示功能已经实现或验收通过。先阅读架构边界，再按契约和流程实现；验收矩阵用于判断功能是否可以发布。

项目介绍见 [根 README](../README.md)。开始修改时先读 [开发循环](./06-development-loop.md) 和 [当前状态](./STATUS.md)，再按下表读取与本轮目标相关的契约。

## 文档导航

| 文档 | 权威定义 |
| --- | --- |
| [01. 架构与模块](./01-architecture-and-modules.md) | 写入模型、仓库及路径边界、模块依赖、运行依赖、平台边界 |
| [02. 模型与契约](./02-domain-models-and-contracts.md) | 修订标识、状态模型、模块接口、REST、错误、SSE |
| [03. 流程与恢复](./03-workflows-and-dataflows.md) | 初始化、写入事务、同步、冲突恢复、Dreaming、安装事务 |
| [04. CLI 与 UI](./04-ui-and-cli-specification.md) | 命令、环境变量、Hook 协议、交互、HTTP 安全、打包 |
| [05. 验收与交付](./05-validation-and-delivery.md) | 测试场景、性能测量、发布门槛、审查问题追踪 |
| [06. 开发循环](./06-development-loop.md) | 执行步骤、任务状态、完成条件、交接方式 |
| [07. dsh 专用插件](./07-dsh-plugin.md) | 分发、安装、生命周期、上下文注入、实现状态与验收 |
| [开发状态](./STATUS.md) | 当前事实、任务总表与最近一轮完成记录 |

同一规则只在表中对应文档定义，其他文档通过链接引用。流程变更需同步调整契约和验收项。

中文正文的引号基线为直角引号「」，嵌套引用使用『』；代码、路径、命令、字段名、JSON 与配置示例等机器可读内容保持原样。

## 上游与参考边界

- 格式基线固定为上游提交 [`8798cb26c817d451a5ed6cba0ad8d9eca8d5ec3e`](https://github.com/AgentMemoryRepo/agentmemoryrepo/tree/8798cb26c817d451a5ed6cba0ad8d9eca8d5ec3e) 的 [`SPEC.md`](https://github.com/AgentMemoryRepo/agentmemoryrepo/blob/8798cb26c817d451a5ed6cba0ad8d9eca8d5ec3e/SPEC.md)：入口为 `MEMORY.md`，单行条目、可选开放元数据、根相对 `[[path]]` 链接，以及非 Markdown 资产。
- 上游 [`README.md`](https://github.com/AgentMemoryRepo/agentmemoryrepo/blob/8798cb26c817d451a5ed6cba0ad8d9eca8d5ec3e/README.md) 描述记忆循环、Dreaming 和多仓库组合。首版每次命令绑定一个仓库，不自动把多个所有者的记忆合并。
- 上游随附 Skill 的本地优先、私有远端确认和谨慎写入是本项目安全策略的参考，不将它与格式标准混为一谈。自动 Hook、Web UI、后台同步和人工确认的 Dreaming 属于本项目扩展。
- `source`、`added` 是推荐元数据，不是格式必填项。`MEMORY.md` 超过 100 行是本项目的提示阈值，不是上游规范错误。
- `脚本化实现参考/` 仅提供命令习惯和适配样例；不修改该目录，不承诺逐行为复刻。兼容范围以 [CLI 与 Hook 契约](./04-ui-and-cli-specification.md) 为准。

## 本次修订的设计取舍

1. 保留既定功能范围，以分阶段验收控制发布，不将未验证能力写成已具备的保证。
2. 并发写入采用受管事务：Agent 与 UI 先读取修订标识，再通过 `memctl` 写入。为此新增最小的 `read`、`write` 命令；它们不依赖运行 Web 服务。
3. 原有直接编辑文件的习惯仅在串行场景支持，由显式 `sync --capture` 接收。自动 Hook 不再无条件提交工作树里的全部变化。这是有意的行为调整。
4. 同步只在干净工作树上运行，不用 autostash 隐藏未提交变化。人工解冲突期间暂停自动写入和同步。
5. 保留 Go、系统 Git 和内嵌前端方案；「单文件分发」不等于不依赖 Git，也不等于内置 AI 模型。
6. macOS、Linux、Windows 均为目标平台，但后台派生和 Dreaming runner 按平台验收。未验证组合标为不支持，不通过放宽安全边界补齐功能。

这些取舍改变的是实现约束和适配指引，不是记忆文件格式。若后续要求多个 Agent 无改造地同时直接写同一目录，需要重新选择独立工作树等隔离模型，不能仅依靠本方案中的文件锁。
