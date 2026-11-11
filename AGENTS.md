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

## 跨会话与轮次接续

本轮验证通过、台账已更新（`docs/STATUS.md` 保持在 `12,000` UTF-8 字符上限内）且变更已提交推送后，先用 `scripts/next-round.sh --check-only` 校验交接提示词四要素、热台账字符预算与远端同步状态。规则、四要素交接要求与失败处置见 [接续的执行机制](docs/06-development-loop.md#61-接续的执行机制)。

```bash
scripts/next-round.sh --check-only --prompt-file /tmp/next-round.md
```

- 脚本校验三项前置条件：交接提示词以 Markdown 标题或加粗行给出「有效规则与边界、本轮结果与证据、剩余事项与依赖、下一轮目标」四个小节；`docs/STATUS.md` 归一化 UTF-8 字符数 `<= 12,000`（超限时须先归档历史任务记录至 `docs/validation/stage-<x>.md`、清理已完成待办并精简当前事实）；工作树干净且 `HEAD` 与上游分支一致（追加 `--dry-run` 时校验提示词结构与 `STATUS.md` 字符上限，跳过 Git 状态检查）。任一不满足时返回退出码 `1`，全程不查找也不调用 `orca` 可执行文件。
- 前置检查通过后，按当前宿主环境选择以下任一宿主无关方式推进下一轮（严禁在同一上下文窗口内直接叠加两轮开发任务，避免并行修改）：
  1. 在支持子智能体上下文隔离的宿主中，由轻量主会话按轮次串行委派全新独立上下文的子智能体读取 `/tmp/next-round.md` 推进下一轮，本轮子智能体在任务完成后立即销毁；
  2. 在新终端直接启动独立新会话（如执行 `agy -i "$(cat /tmp/next-round.md)"`），当前会话立即停止开发，只向用户报告本轮结果；
  3. 若在 Orca 工作区中使用终端自动交接，可不带 `--check-only` 调用 `scripts/next-round.sh --prompt-file /tmp/next-round.md`（支持 `--model <id>`、`--effort <level>`、`--focus` 及失败后用 `--terminal <handle> --retry-request <id>` 重放同一次提示词投递）。
