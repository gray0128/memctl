# 13. Delta 宿主接入

> 状态：调研完成（2026-10-31，Delta 0.23.0 / darwin arm64）；本轮交付会话身份回退、适配器探测与状态提示（`T123`）。Delta 没有生命周期 Hook，也没有本地 stdio MCP 通道，因此本文件定义的接入以「Skill + Agent 主动调用 CLI」为主，不引入新的写入模型。文件格式、写入事务与接口仍以 [02](./02-domain-models-and-contracts.md) 与 [03](./03-workflows-and-dataflows.md) 为准。

## 1. 结论

- Delta 没有可供外部工具注册的生命周期 Hook：会话开始、每轮结束与会话结束都不会调用本工具。上下文注入与每轮同步在 Delta 上不是自动的。
- Delta 只接受远端 HTTPS MCP server，本地 stdio server 无法注册。`memctl mcp` 在 Delta 上不可用，不能用 [09](./09-mcp-memory-tools.md) 的方式接入。
- Delta 不对 Agent 施加文件沙箱。Agent 在终端里执行 `memctl read/write` 可以直接打开默认位于 `~/memory` 的记忆仓库，因此 [09](./09-mcp-memory-tools.md) 所解决的「沙箱阻断写入」问题在 Delta 上不存在，CLI 就是受支持的写入路径。
- Delta 在 Agent 运行的每条命令上注入 `DELTA_CURRENT_THREAD_ID`。Delta 没有 Hook，该变量是唯一可用的会话身份，因此把它加入 [SID 回退链](./04-ui-and-cli-specification.md#2-环境变量与会话身份)。
- Delta 从 `~/.agents/skills/` 读取个人 Skill，本项目的 Skill 已在生效。分发该 Skill 时 `npx skills add` 的宿主名是 `zed`，不是 `delta`。
- 不把记忆仓库移入 Delta 工作区。Delta 把线程附加仓库的内容上传到云端存储，独立私有仓库放入工作区会把记忆内容一并带出本机。
- 不实现 Delta 专用插件。宿主没有插件或 Hook 扩展点，也没有官方本地扩展 API；按 [09](./09-mcp-memory-tools.md#3-方案比较) 的同一取舍，不依赖宿主内部实现。

判据：在 Delta 里判断接入是否成立，看「Agent 能否执行 `memctl` 并拿到正确会话身份」，而不是看是否存在 Hook 配置。

## 2. Delta 提供的接入面

下表是 2026-10-31 在 Delta 0.23.0（CLI 契约版本 `min=8,max=8`）上的实测结果。

| 接入面 | Delta 是否支持 | 对本项目的含义 |
| --- | --- | --- |
| 个人与项目 Skill | 支持，`~/.agents/skills/`、`.agents/skills/`、`.delta/skills/` | 本项目的 Skill 被加载，指引与受管写入流程可用 |
| 仓库 `AGENTS.md` 与文件夹规则 | 支持，每轮重新读取 | 可作为 Skill 之外的项目约定入口 |
| 生命周期 Hook | 不支持 | 没有自动上下文注入，没有每轮 `sync` 与结束 `end` |
| MCP server | 仅远端 HTTPS，支持 OAuth 2.1 或无鉴权 | stdio 的 `memctl mcp` 无法注册 |
| Agent 文件沙箱 | 不提供 | CLI 可直接读写记忆仓库，不需要 MCP 写入通道 |
| 终端环境变量 | 注入 `DELTA_CURRENT_THREAD_ID`、`DELTA_THREAD_TITLE`、`DELTA_CLI_INTEGRATION`、`DELTA_SCRATCH_DIR` | `DELTA_CURRENT_THREAD_ID` 作为会话身份 |
| 会话记录 | DeltaDB 存储；`delta thread list`/`show` 可读 | 可作为会话蒸馏来源，但缺项目过滤，见 §7 |

证据要点：Delta 官方《Agentic Safety》写明不对 Agent 施加沙箱；《MCP Servers》写明不支持本地 MCP server；二进制中 `SessionStart`、`UserPromptSubmit`、`PreToolUse`、`hooks.json`、`mcpServers` 均无匹配；`delta integration register` 的唯一取值是 `claude-code`，方向是把外部 Agent 会话记入 Delta，不是让 Delta 调用本项目。Delta 的仓库级自动化只有 `.agents/prepare`（依赖安装）与 `.agents/linked`（本地文件链接），都不是上下文注入点。

## 3. 选定方案

三层，按依赖顺序：

1. **Skill 分发**：把 `skills/memctl/` 安装到 `~/.agents/skills/memctl`，由 Delta 自动加载。安装命令为 `npx skills add gray0128/memctl -g -s memctl -a zed -y`。宿主名是 `zed`：`skills` CLI 不接受 `delta`，而 Delta 读取的就是该命令写入的目录。
2. **Agent 主动调用 CLI**：读取、受管写入、同步与整理全部走 [04](./04-ui-and-cli-specification.md#1-cli-命令) 定义的命令，不新增命令、工具或存储路径。`memctl mcp` 在 Delta 上不使用。
3. **会话身份**：`memctl` 从 `DELTA_CURRENT_THREAD_ID` 取得会话 ID，写入来源标签与提交 trailer，因此审计视图能按线程追溯。

`install delta` 只做探测与提示，不写任何文件：Delta 没有受管配置。计划输出 `delta: configured host=delta@<version>` 与三条人工步骤；`capabilities` 为空，因为它只描述本工具在宿主里维护的对象。宿主版本低于 `0.23.0` 时报告 `needs_attention`，不静默接受。

不采用的路线：

| 路线 | 不采用的理由 |
| --- | --- |
| stdio MCP 桥接 | 宿主只接受远端 HTTPS MCP server，没有可用的注册入口 |
| Delta 生命周期插件 | 宿主没有插件或 Hook 扩展点，也没有官方本地扩展 API |
| `install delta` 代为安装 Skill | 与 `npx skills` 争夺同一目录的所有权，且现有合并引擎对 Markdown 只做整文件覆盖 |
| 把记忆仓库放进工作区 | Delta 把线程附加仓库的内容上传到云端，记忆内容会随之离开本机 |

## 4. 会话身份

SID 优先级为 `--sid` → `AGENT_MEMORY_SID` → `DSH_HOOK_SESSION_ID` → `DSH_SESSION_ID` → `DELTA_CURRENT_THREAD_ID` → 有 `--stdin` 时 Hook JSON 的 `session_id/sessionId/sessionID/conversationId`。空值跳过，非字符串返回协议错误。完整规则见 [04 §2](./04-ui-and-cli-specification.md#2-环境变量与会话身份)。

- `DELTA_CURRENT_THREAD_ID` 排在宿主环境变量之后、Hook JSON 之前。Delta 没有 Hook，`--stdin` 场景不存在，实际效果是它在 Delta 终端里总能命中。
- Delta 不注入 Agent 名称，`--agent` 或 `AGENT_MEMORY_AGENT` 仍决定来源标签的 Agent 段。缺省时沿用 `unknown`，不按宿主变量猜测。
- `DELTA_THREAD_TITLE` 只用于展示，不参与来源标签、去重键或提交 trailer。它是可改的显示名称，不是稳定标识。
- 提交只记录 `Agent`、`Session` 与 `Device` trailer，与 [02 §2.3](./02-domain-models-and-contracts.md#23-审计诊断整理与接入) 的来源规则一致。

## 5. 适配器契约

Delta 的适配器是一个只探测、不受管文件的条目，字段语义沿用 [03 §7](./03-workflows-and-dataflows.md#7-agent-配置安装)：

| 场景 | `status` | `applicable` | 输出 |
| --- | --- | --- | --- |
| 未探测到 `delta` 可执行文件 | `not_installed` | 否 | `host=not-detected` 与原因 |
| 版本低于 `0.23.0` | `needs_attention` | 否 | 原因写明最低版本 |
| 版本不低于 `0.23.0` | `configured` | 否 | 三条人工步骤，每条以 `step:` 开头 |

- `capabilities` 为空。该字段只描述本工具在宿主里维护的 Hook、功能开关或插件文件；Delta 三者都没有，报告一个占位能力会误导操作者。
- `install delta` 与 `install --all` 不写文件，也不执行宿主命令；`--status` 与 `--dry-run` 的输出相同。
- `mcp:` 块为 Delta 输出一条说明：本 server 无法注册，改用 CLI。不输出不可用的注册命令。
- `HostMinVersion` 只用于提示，不阻止使用：Agent 仍可在旧版本上运行 `memctl`，只是 `DELTA_CURRENT_THREAD_ID` 未必存在。

## 6. 验收

| ID | 场景 | 预期 |
| --- | --- | --- |
| `X01` | Delta 终端内只设置 `DELTA_CURRENT_THREAD_ID` 时执行 `tag`、`context --stdin` 与不带 `--sid` 的 `write` | 三者都取得该线程作为会话身份；提交说明含 `Session: <thread>`；`--sid` 与 `AGENT_MEMORY_SID` 仍优先 |
| `X02` | `install --status` 与 `install delta --dry-run`：宿主存在且版本达标、版本过低、宿主缺失三种情况 | 分别为 `configured` 附人工步骤、`needs_attention` 附最低版本、`not_installed`；三次都不在 `$HOME` 下写任何文件 |
| `X03` | `install --status` 的 `mcp:` 块 | Delta 条目说明本地 stdio server 不受支持，不输出注册命令；其余宿主的命令不变 |

## 7. 未验证与后续

- 真实 Delta 会话中的端到端验证：Skill 是否被自动加载并触发受管写入、提交在审计视图中是否可追溯。本轮验证的是命令行为与适配器输出，不是模型在真实会话中的执行。
- 平台：只在 darwin/arm64 验证。Windows 与 Linux 上的 Delta、以及 Delta Web 的终端行为未验证。
- 版本：只验证 Delta 0.23.0。更低版本可能没有 `DELTA_CURRENT_THREAD_ID`，更高版本可能改变 CLI 契约或环境变量。
- `internal/corpus` 的 Delta 会话蒸馏未实现。`delta thread list` 与 `thread show` 不暴露工作目录，无法满足 [12](./12-session-corpus-dreaming.md#22-本地会话采集与确定性蒸馏internalcorpus) 的「先按项目收窄」规则；当前线程也要绕开 `DELTA_CURRENT_THREAD_ID` 才能读取，而官方要求不直接读写 DeltaDB。要支持它，需要先定义按线程筛选项目的替代规则。
- Delta 会话结束不触发 `end`，因此会话摘要只在 Agent 主动执行 `sync` 时产生；不为此新增轮询或后台进程。
