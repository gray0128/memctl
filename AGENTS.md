# 项目协作入口

- 撰写、修改、校对或审阅项目文档（包括 README、设计文档及 AGENTS.md）前，必须完整读取并应用 `tech-doc-style-chinese` Skill。若技能不可用，先说明原因并确认替代方式，再继续文档工作。
- 修改设计、代码、测试或构建前，读取 [开发循环规范](docs/06-development-loop.md)、[阶段计划](docs/05-validation-and-delivery.md#1-交付顺序) 和 [开发状态](docs/STATUS.md)，核对完整剩余事项及其依赖，明确本轮范围与验收标准；只回答问题时按需查阅，不创建开发任务。
- 涉及行为或接口时，通过 [方案索引](docs/README.md) 找到权威契约；新增文件时遵循 [目录规划](docs/01-architecture-and-modules.md#5-目录结构与依赖)。
- 跨会话接续或中断恢复时，先按 [开发循环规范](docs/06-development-loop.md) 核对工作区变更和台账，保留共享工作区中的他人改动；每个会话完成一轮任务，用户最新的停止、暂停或范围调整指令优先执行。
- `脚本化实现参考/` 仅作参考；新实现放入规划的源码目录，默认不修改参考内容。
- 每轮验证通过后，按 [开发循环规范](docs/06-development-loop.md) 同步更新开发进度与历史记录、提交推送并接续下一轮；文档检查不能代替二进制、平台或宿主测试。

## 代码检索工具

本项目已建立两份本地索引，供任意 Agent 在阅读或修改代码前检索上下文；索引不提交，可用 `codegraph init` 与 `zg index` 重建。定位代码时优先使用这两个工具，`rg` 仅用于精确标识符或正则匹配。

- `codegraph`：回答「某功能如何工作」「调用链」「改动影响范围」等结构性问题，入口为 `codegraph explore "<问题>"`，索引位于 `.codegraph/`，状态用 `codegraph status` 查看。
- `zg`（zvec-grep）：目标位置未知，或需要语义、模糊和跨文件检索时使用，入口为 `zg query "<问题>"`，索引位于 `.zvec-grep/`，状态用 `zg status` 查看。

支持 MCP 的 Agent 可通过 `codegraph serve --mcp` 与 `zg server --stdio` 接入各自的 MCP 配置，获得原生工具调用。
