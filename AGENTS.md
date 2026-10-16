# 项目协作入口

- 撰写、修改、校对或审阅项目文档（包括 README、设计文档及 AGENTS.md）前，必须完整读取并应用 `tech-doc-style-chinese` Skill。若技能不可用，先说明原因并确认替代方式，再继续文档工作。
- 修改设计、代码、测试或构建前，读取 [开发循环规范](docs/06-development-loop.md) 和 [开发状态](docs/STATUS.md)，确认本次授权范围、当前任务和完成条件；只回答问题时按需查阅，不创建开发任务。
- 涉及行为或接口时，通过 [方案索引](docs/README.md) 找到权威契约；新增文件时遵循 [目录规划](docs/01-architecture-and-modules.md#5-目录结构与依赖)。
- 中断后接续工作，先按开发循环规范核对磁盘变更和台账，再继续当前任务；台账中的后续建议不代表执行授权。
- `脚本化实现参考/` 仅作参考；新实现放入规划的源码目录，默认不修改参考内容。
- 完成前按循环规范回写验收证据与未完成项；文档检查不能代替二进制、平台或宿主测试。

## 代码检索工具

本项目已建立两份本地索引，供任意 Agent 在阅读或修改代码前检索上下文；索引不提交，可用 `codegraph init` 与 `zg index` 重建。定位代码时优先使用这两个工具，`rg` 仅用于精确标识符或正则匹配。

- `codegraph`：回答「某功能如何工作」「调用链」「改动影响范围」等结构性问题，入口为 `codegraph explore "<问题>"`，索引位于 `.codegraph/`，状态用 `codegraph status` 查看。
- `zg`（zvec-grep）：目标位置未知，或需要语义、模糊和跨文件检索时使用，入口为 `zg query "<问题>"`，索引位于 `.zvec-grep/`，状态用 `zg status` 查看。

支持 MCP 的 Agent 可通过 `codegraph serve --mcp` 与 `zg server --stdio` 接入各自的 MCP 配置，获得原生工具调用。
