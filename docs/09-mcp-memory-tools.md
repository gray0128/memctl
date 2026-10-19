# 09. 受沙箱宿主下的记忆写入通道：MCP 工具

> 状态：已实现并验收（2026-10-18，`T071`–`T073`）。本文档定义 `memctl mcp` 子命令、工具契约、各宿主注册方式与验收矩阵。分发与安装细节引用 [CLI、UI 与分发](./04-ui-and-cli-specification.md)，宿主生命周期映射引用 [07](./07-dsh-plugin.md) 与 [08](./08-pi-plugin.md)。

## 1. 结论

- 会话没有记忆更新的直接原因不是插件失效，而是写入动作落在宿主沙箱之外：生命周期插件在宿主进程内运行，能注入和同步；Agent 通过 bash 执行 `memctl read/write` 时，子进程受工作区沙箱约束，无法打开记忆仓库的锁文件。
- 生命周期插件只做桥接，不写入记忆。写入必须由 Agent 主动发起，因此需要一个不受 bash 沙箱约束、且跨宿主通用的调用通道。
- 选定方案：`memctl` 二进制新增 `memctl mcp` 子命令，以标准输入输出实现 MCP server，暴露 `memory_read`、`memory_entries` 与 `memory_write` 三个工具。各宿主通过自带 MCP 客户端配置注册该 server。
- 该通道已在 macOS arm64、DSH 0.2.1-rc.2 上验证：宿主派生的 MCP 子进程可以写入 `~/memory` 并成功执行 `memctl read`，不受 `workspace-write` 沙箱限制。
- 「子进程不受沙箱约束」是宿主相关的结论，不是通用性质：Grok 的 `workspace` profile 会对子进程施加同样的写限制，必须显式把记忆仓库加入 `read_write`，而该授权同时放开 shell。pi 没有文件沙箱，shell 本就可写。各宿主的实测矩阵见 6.2。
- 不采用的两条路线：把记忆仓库移入工作区会破坏跨项目共享语义；为每个宿主单独实现插件内工具会引入宿主内部依赖，且无法覆盖没有插件机制的宿主。

## 2. 写入为什么没有发生

### 2.1 两个权限域

| 执行位置 | 进程归属 | 文件权限 | 能做什么 |
| --- | --- | --- | --- |
| 生命周期插件 | 宿主进程内，沙箱外 | 完整用户权限 | 会话开始读取上下文、每轮触发同步、销毁时收尾 |
| bash 工具 | 工作区沙箱内 | 仅工作区与临时目录可写 | 运行构建、测试与项目内命令；不能写记忆仓库 |

记忆仓库默认位于 `~/memory`，不在任何项目工作区之内。因此 Agent 在 bash 中执行 `memctl write` 时，进程在打开 `~/memory/.git/memctl.lock` 阶段就被拒绝，返回 `IO_FAILED`，而不是内容或基线错误。

### 2.2 观测证据

| 观测 | 结果 | 说明 |
| --- | --- | --- |
| DSH `workspace-write` 会话内执行 `memctl read MEMORY.md --json` | `IO_FAILED: cannot open repository lock` | 锁文件在工作区外 |
| 同一会话内执行 `touch ~/memory/x` | `Operation not permitted` | 沙箱按路径拒绝写入，与命令无关 |
| 同一会话内执行 `touch <工作区>/x` | 成功 | 工作区可写 |
| 插件注入与 `sync` | 每轮成功，缓存为 `synced` | 插件在宿主进程内，不受该沙箱约束 |
| 近期 DSH 会话的 bash 调用 | 无一次 `memctl write` | 未尝试写入；尝试也会被上述限制拒绝 |

DSH 的 `workspace-write` 可写根在实现中由 `writableRoots` 固定为工作区根、`/tmp` 与系统临时目录，`--dump-config` 没有追加可写目录的配置项。因此不存在「只给记忆仓库加白名单」的官方开关。

### 2.3 其他宿主的同类条件

宿主是否阻断写入取决于其沙箱配置，而不是 memctl。已实测组合的结论如下，逐项证据见 [6.2](#62-真实验证边界)：

- DSH 与 Codex 的 `workspace-write` 会话中，同一会话的 bash 被拒绝，宿主派生的 MCP 子进程仍可写记忆仓库。
- pi 与不加 `--sandbox` 的 Antigravity 没有文件沙箱，shell 与 MCP 都可写；两条通道并存不构成隔离。
- Grok 的 `workspace` profile 对子进程同样施加写限制，必须把记忆仓库显式加入 `read_write`，而该授权同时放开 shell。
- Claude Code 只确认注册被宿主接受，写入路径未实测。

结论：不能依赖「用户把沙箱整体关掉」来保证记忆可写。需要一条与沙箱模式无关的通道。

## 3. 方案比较

| 方案 | 机制 | 跨宿主 | 与沙箱的关系 | 主要代价 |
| --- | --- | --- | --- | --- |
| A. 要求 `danger-full-access` | 用户放宽会话沙箱 | 是 | 取消保护 | 失去工作区隔离；审批策略为 `never` 时无法按次升级 |
| B. 插件内注册工具 | 宿主插件在自己的作用域注册工具，由宿主进程执行 memctl | 否，需逐宿主实现 | 天然绕过沙箱 | 依赖宿主内部 API；DSH 需要 `@deepseek-ai/dsh-tools` 的 `defineTool()` 产物，实测该包不在 profile 解析范围 |
| C. MCP server | memctl 以标准输入输出实现 MCP，宿主作为客户端启动它 | 是 | 子进程由宿主派生，通常不受 bash 沙箱约束；Grok 例外，见 6.2 | 需要各宿主支持 MCP 配置；工具调用有额外协议开销；部分宿主需要额外暴露或审批配置 |
| D. 常驻 HTTP 服务 | `memctl ui` 常驻，Agent 用 curl 调用写入接口 | 是 | 依赖网络可达 | 需要常驻进程与 Token 分发；等于绕过 CLI 语义 |

选定 C。理由：

1. MCP 是各宿主共同支持的外部工具协议，注册位置是宿主自身的配置，不需要 memctl 依赖宿主内部包。
2. MCP 子进程由宿主直接派生，权限等同于宿主进程；DSH 与 Codex 实测可写 `~/memory`，Grok 需放开 `read_write`。
3. 工具语义可以完全映射到既有 CLI：`memory_read` 对应 `read --json`，`memory_entries` 对应 `entries --json`，`memory_write` 对应 `write --base --stdin`，不新增写入模型、锁或事务。
4. B 仍需为每个宿主维护宿主专属实现；对 DSH 而言还需引入 peer 依赖，违背插件「零依赖、只执行固定 memctl 二进制」的既定边界。

方案 D 保留为不支持 MCP 的宿主的后备，不在本方案内实现。

## 4. 系统如何工作

### 4.1 组件与调用链

```text
Agent（模型）
  └─ 工具调用 memory_write(session tools)
       └─ 宿主 MCP 客户端（宿主进程内）
            └─ 派生子进程：memctl mcp --agent <宿主> --sid <会话>
                 └─ 复用 app 层：受管写入事务、仓库锁、Git 提交、后台同步
                      └─ ~/memory（Git 记忆仓库）
```

`memctl mcp` 只做协议适配：它把 MCP 请求翻译成与 CLI 相同的应用层调用，不实现新的存储、锁或同步逻辑。写入仍然经过受管事务与版本基线校验。

### 4.2 会话身份

- `--agent` 与 `--sid` 由宿主配置固定传入，或在配置中由宿主注入环境变量后由 `memctl mcp` 读取。
- 缺少稳定会话标识时，工具仍可工作，但写入提交的来源记录为 `unknown`，且不参与跨进程去重。不伪造来源。
- `source` 标签沿用 [CLI 与 Hook 契约](./04-ui-and-cli-specification.md) 的 `tag` 规则；上下文注入中已提供该标签，工具不需要重复输出。

### 4.3 协议与传输

- 传输为标准输入输出，消息为单行 JSON，以换行分隔。不使用长度前缀，不输出非 JSON 内容到标准输出。
- 实际内容还需要宿主侧配合：Codex 要 `default_tools_approval_mode = "approve"`，pi 要 `--exposure direct`，Grok 要可写的记忆仓库路径。缺少任一项时工具会「存在但调用失败」，见 5.2 与 6.2。
- 标准错误用于诊断，不写入记忆正文、凭据或完整工具参数。
- 实现 `initialize`、`notifications/initialized`、`tools/list`、`tools/call`、`ping`；未知方法返回 JSON-RPC 错误 `-32601`。
- 协议版本以客户端请求的版本为准，回退到实现支持的最高版本；不支持时返回可诊断错误，不静默降级。
- 单进程为单会话服务：进程生命周期由宿主管理，退出即结束，不保留后台状态。

### 4.4 工具集

只暴露三个工具，覆盖「定位事实 + 读基线 + 受管写入」，不复制 CLI 的全部能力。

| 工具 | 用途 | 参数 | 成功返回 |
| --- | --- | --- | --- |
| `memory_read` | 读取记忆文件并返回修订标识 | `path`（仓库相对路径，可选，默认 `MEMORY.md`） | `filePath`、`fileRevision`、`rawContent`，存在到期条目时附带 `hiddenExpired` |
| `memory_entries` | 检索已解析条目，定位事实所在文件 | `q`、`file`、`agent`、`device`、`from`、`to`、`limit`、`offset`（全部可选） | `revision`、`items`、`total` |
| `memory_write` | 以版本基线受管写入一个文件 | `path`、`base`、`content` | `transaction`、`commit`、`revision`、`sync` |

`memory_entries` 是只读查询：`q` 对条目正文做大小写不敏感的子串匹配，`file` 限定单文件，`agent`/`device`/`from`/`to` 按来源与 `added` 日期收窄，`limit` 上限 200（默认 50）。它不建立索引、不做语义检索，返回结构与 `GET /api/v1/entries` 一致；Agent 据此定位文件后再调用 `memory_read`。

`memory_write` 的 `base` 必须是 `memory_read` 返回的 `fileRevision`，新建文件使用 `absent`。基线过期返回 `REVISION_MISMATCH`，不覆盖他人更新。

不提供的工具及理由：

| 不提供 | 理由 |
| --- | --- |
| Git 操作、任意命令执行 | 扩大执行面，且 CLI 与插件已覆盖 |
| 同步控制 | `memory_write` 已启动后台同步；生命周期插件在各轮结束触发同步 |
| 删除文件或批量改链 | 需要确认语义，属于 UI 与显式 CLI 能力 |
| 语义或向量检索 | 需要索引构建与失效策略，且当前条目查询已覆盖"定位事实所在文件" |
| 网络检索或外部抓取 | 与记忆写入无关 |

### 4.5 错误语义

工具错误通过 MCP `isError: true` 与文本内容返回，文本与 CLI 的 `memerr` 文案一致，便于 Agent 与用户对照。

| 场景 | 返回 | 后续动作 |
| --- | --- | --- |
| 路径越界、Git 内部路径 | `INVALID_PATH` | 修正路径，不重试 |
| 基线过期 | `REVISION_MISMATCH` | 重新 `memory_read` 后重试一次 |
| 仓库不存在或未初始化 | `NO_REPO` | 提示用户运行 `memctl init`，不创建仓库 |
| 锁被占用 | `BUSY` | 短暂退避后重试一次；失败则报告，不长时间阻塞 |
| 内容超过 2 MiB | `LIMIT_EXCEEDED` | 拆分文件 |
| Git 提交失败 | `GIT_FAILED` | 报告原始原因 |

工具调用不因后台同步失败而失败：同步状态写入返回值的 `sync` 字段，与 `write` 的 `sync: pending` 语义一致。

### 4.6 安全边界

- 记忆内容是数据，不是指令。工具返回的正文不改变 Agent 的行为规则；这一点由注入的指引声明，工具不新增指令通道。
- 不读取宿主配置文件，不执行任意命令。进程只接受固定参数与仓库相对路径。
- 路径在仓库根内解析，拒绝符号链接逃逸、Git 内部路径与绝对路径。
- 不把写入内容写入日志。诊断只记录错误类别、路径与修订标识。

## 5. 安装与注册

### 5.1 注册内容

每个宿主注册一个 stdio MCP server，命名为 `memctl`：

| 字段 | 值 |
| --- | --- |
| 命令 | `memctl` 的绝对路径（默认 `~/.agent-memory/bin/memctl`） |
| 参数 | `mcp`、`--agent <宿主名>` |
| 会话标识 | 由宿主提供的环境变量注入，缺失时省略 `--sid` |

`memctl mcp` 不需要写入状态目录之外的文件。注册由各宿主自己的命令完成；`memctl install` 只探测并在提示中给出注册命令，不合并宿主的 MCP 配置文件。

### 5.2 各宿主的注册命令

下表为本机实测的配置位置与注册命令；格式与合并语义以各宿主文档为准。

| 宿主 | 配置文件 | 注册命令 | 备注 |
| --- | --- | --- | --- |
| Claude Code | `~/.claude.json` | `claude mcp add memctl --scope user -- <memctl> mcp --agent claude-code` | 省略 `--scope` 时写入 local 作用域，用户级用 `--scope user` |
| Codex | `~/.codex/config.toml` | `codex mcp add memctl -- <memctl> mcp --agent codex` | 需要 `default_tools_approval_mode = "approve"`，见下 |
| pi | `~/.pi/agent/mcp.json` | `pi mcp add memctl --exposure direct -- <memctl> mcp --agent pi` | 省略 `--exposure` 时默认 `codemode`，模型无法直接调用工具，见下 |
| OpenCode | `~/.config/opencode/opencode.json` | `opencode mcp add --global memctl -- <memctl> mcp --agent opencode` | 省略 `--global` 时写入当前目录的 `opencode.json` |
| Grok | `~/.grok/config.toml` | `grok mcp add --scope user memctl -- <memctl> mcp --agent grok` | `[mcp_servers.memctl]`，写入 `enabled = true` |
| Command Code | `~/.commandcode/mcp.json` | `commandcode mcp add --scope user memctl <memctl> mcp --agent commandcode` | 省略 `--scope user` 时写入项目级路径 |
| Antigravity | `~/.gemini/config/mcp_config.json` | `agy mcp add --type stdio memctl <memctl> mcp --agent antigravity` | 也可手工编辑 `mcpServers`，条目为 `{command, args, disabled}`；记忆仓库不在工作区时需同时设 `env` |
| DSH | profile 的 `cordis.patch.yml` | 用 `insert:` 追加 `@deepseek-ai/dsh-mcp-client` stdio 条目，`command` 为 `<memctl>`、`args` 为 `["mcp","--agent","dsh"]` | 该包随 DSH 应用分发，profile 无需安装或声明依赖；必须用 `insert:`，见下 |

把 `<memctl>` 替换为实际二进制路径，默认 `~/.agent-memory/bin/memctl`。需要会话级工具归属时由宿主注入 `--sid`；无法注入时省略该参数，来源记为 `unknown`。

有 `mcp add` 子命令的 7 个宿主已在 2026-10-18 执行上表命令，均被接受并在各自状态命令中显示已连接：`claude mcp list` 返回 `Connected`，`pi mcp list` 返回 `connected, 2 tools`（当时只有 `memory_read`、`memory_write`；`memory_entries` 是 `T089` 新增的第三个工具），`opencode mcp list` 返回 `✓ memctl connected`，`agy mcp list` 列出 `memctl`，Codex、Grok 与 Command Code 的 `mcp list` 显示 `enabled`。其中 6 个使用隔离 `HOME`；Antigravity 的凭据在系统钥匙串，改用真实登录态并在真实配置中临时加入指向探针仓库的条目。DSH 没有等价子命令，只能按上表手工编辑。

DSH 的注册条目写在 profile 的 `cordis.patch.yml` 里，追加一条 `insert:` 记录：

```yaml
- insert:
    - id: mcp-memctl
      name: "@deepseek-ai/dsh-mcp-client"
      config:
        serverName: memctl
        transport: stdio
        command: /Users/<user>/.agent-memory/bin/memctl
        args: ["mcp", "--agent", "dsh"]
```

`@deepseek-ai/dsh-mcp-client` 与 `@deepseek-ai/dsh-base` 等内核包一起随 DSH 应用分发，位于应用包内 `dsh/node_modules/` 下，profile 的 `package.json` 与 `node_modules` 都不需要它。因此该步骤只改 profile 配置，不装包。写成不带 `insert:` 的顶层 `- id:` 记录会被宿主拒绝：`patch: entry "<id>" not found`（目标条目必须已存在）或 `id is required for non-insert patches`，两种情况都不会加载该 server。

本机实测不需要重启应用：改动保存后 13 秒内，宿主重新合成条目树并派生了 `memctl mcp --agent dsh` 子进程。确认方式是看 `ps` 中该进程的父进程是否为 DSH 主进程；模型侧工具名固定为 `mcp__memctl__memory_read` 与 `mcp__memctl__memory_write`。

pi 的 MCP server 默认以 `codemode` 暴露：工具只对 `codemode` 脚本可见，既不向模型声明，也不出现在工具列表。注册时显式指定 `--exposure direct` 才能让模型直接调用 `memory_read`、`memory_entries` 与 `memory_write`。三种可行暴露方式中，`direct` 适合这几个常用工具；`deferred` 需要模型先经 `tool_search` 加载；`codemode` 需要同时启用 `codemode` 工具。

Codex 有一项与 memctl 无关但会让工具调用全部失败的前置条件：`approval_policy = "never"`（或 `-a never`）与 `default_tools_approval_mode` 缺省或 `auto` 组合时，宿主直接拒绝调用并返回 `MCP tool call requires approval, but approval policy is never`。实测只有 `default_tools_approval_mode = "approve"` 能放行，且该配置不覆盖沙箱：同一会话的 bash 仍受 `workspace-write` 限制。注册后未改这一项时，表现为「工具存在但每次都失败」，不要误判为 memctl 故障。

### 5.3 探测

- `memctl install --status` 在适配器列表之后输出 `mcp:` 块：把当前可执行文件按 `mcp --agent status` 启动一次，完成握手并读取 `tools/list`，成功时输出 `mcp: serving <工具列表> (<version>)`（当前为 `memory_entries, memory_read, memory_write`），随后按 5.2 输出每个宿主的注册命令。
- Web UI 的「客户端接入」视图通过 `GET /api/v1/mcp` 展示同一份结果：可执行文件、版本、工具列表、不可用原因，以及每个宿主的注册命令或手工步骤。探测逻辑与注册表实现在 `internal/mcp`，CLI 与 HTTP 共用，避免两处漂移。界面只提供复制命令，不运行 `mcp add`，也不写宿主配置。
- 探测只使用协议，不读状态目录，不写任何文件；超时上限 10 秒，子进程无论成功失败都会被回收。预算不能太小：探测与并行编译争用 CPU 时，正常的二进制也可能在 3 秒内没能完成握手，从而被误报为不可用。
- 二进制无法响应 `mcp`（旧版本、路径不可执行、未返回 `tools/list`）时输出 `mcp: unavailable: <原因>` 并省略全部注册命令：注册一个每次调用都失败的 server 比不注册更难排查。
- 不解析、不合并宿主的 MCP 配置文件。宿主间的字段名、数组与字符串拼写、项目级与用户级作用域均不同，手写解析器会随宿主升级失效；注册由各宿主自有的命令负责，失败时由宿主给出错误。
- 已注册同名 server 但命令不同：由宿主的 `mcp add` 覆盖或拒绝，memctl 不介入。
- 宿主没有 MCP 支持：不输出该宿主的注册提示，其余接入方式不变。

### 5.4 与生命周期插件的关系

MCP 与生命周期插件互补，不互相替代：

| 能力 | 生命周期插件 | MCP 工具 |
| --- | --- | --- |
| 会话开始注入记忆 | 是 | 否 |
| 每轮结束同步 | 是 | 否（写入时启动一次后台同步） |
| Agent 主动读取任意记忆文件 | 否 | 是 |
| Agent 主动受管写入 | 否 | 是 |

两者可同时启用。MCP 不改变注入内容，也不改变同步触发点。

## 6. 验收

### 6.1 矩阵

| ID | 场景 | 预期 |
| --- | --- | --- |
| M01 | `initialize` 正常握手、未知方法、非法 JSON 行 | 返回正确处理；非法行不终止进程；标准输出只有 JSON 行 |
| M02 | `tools/list` | 恰好返回 `memory_read`、`memory_entries` 与 `memory_write` 及其参数模式 |
| M03 | `memory_read` 读取 `MEMORY.md` 与指定路径 | 返回 `filePath`、`fileRevision`、`rawContent`，与 `read --json` 一致；`until` 已到期的条目以占位符出现并计入 `hiddenExpired` |
| M04 | 路径越界、绝对路径、Git 内部路径、符号链接逃逸 | 返回 `INVALID_PATH`，不读取仓库外内容 |
| M05 | `memory_write` 使用正确基线新建与覆盖 | 产生一次本地提交，返回 `transaction`、`commit`、`revision`、`sync` |
| M06 | 基线过期、并发写入 | 返回 `REVISION_MISMATCH`，不覆盖他人更新，不产生提交 |
| M07 | 锁被占用、仓库缺失、超大内容 | 分别返回 `BUSY`、`NO_REPO`、`LIMIT_EXCEEDED`；进程不崩溃 |
| M08 | 沙箱会话内通过 MCP 写入 `~/memory` | 写入成功；同期 bash 直接写入仍被拒绝，证明通道独立于沙箱 |
| M09 | `memctl install --status` 输出注册命令 | 不写文件；二进制不可执行或子命令缺失时给出可诊断结果 |
| M10 | 无 `--sid` 调用 | 正常读写，来源记为 `unknown`，不伪造会话标识 |
| M11 | 标准错误与诊断输出 | 不包含记忆正文、完整参数或凭据 |
| M12 | 宿主关闭连接 | 进程退出，不遗留子进程与锁 |
| M13 | `memory_entries` 按 `q`/`file`/`agent`/`device`/`from`/`to`/`limit`/`offset` 检索 | 参数原样下发到条目查询；返回 `{revision, items, total}` 与 `GET /api/v1/entries` 一致；负 `limit` 在调用前被拒绝；契约失败以 `isError` 携带错误码而非协议错误 |

### 6.2 真实验证边界

2026-10-18 在 macOS arm64、DSH 0.2.1-rc.2 上完成以下实测（`T070`、`T071`）：

| 观测 | 结果 |
| --- | --- |
| 宿主是否派生 MCP 子进程 | 是，探针记录 `mcp --agent dsh` 的两次启动（连接探测与正式连接） |
| 子进程能否写记忆仓库 | 是，`memory_write` 产生提交并返回 `transaction`、`commit`、`revision` |
| 同会话 bash 能否写同一仓库 | 否，`memctl read` 返回 `IO_FAILED`，`touch` 返回 `touch-denied` |
| 模型是否读到工具结果 | 是，会话回复给出 `memory_read` 的 `fileRevision` 与写失败的 `NOT_FOUND` 原文 |

以上覆盖 `M05`、`M08` 与 `M12` 的核心路径。`memory_write` 不创建缺失的父目录，返回 `NOT_FOUND`，与 CLI `write` 一致；这是既有契约，不是 MCP 的缺陷。

同日补充两组宿主侧验证（`T072`）：

| 观测 | 结果 |
| --- | --- |
| 6 个有 `mcp add` 的宿主在隔离 `HOME` 中执行 5.2 的命令 | 全部被接受，状态命令显示已连接；`pi mcp list` 列出 `memory_read`、`memory_write`（Antigravity 的 `agy mcp add` 在 `T073` 补测，见第三组） |
| Codex `approval_policy = never` + 缺省审批模式 | `memory_read` 被宿主拒绝：`MCP tool call requires approval, but approval policy is never` |
| Codex `default_tools_approval_mode = "approve"` | `memory_read` 返回 `fileRevision`，`memory_write` 返回 `transaction`、`commit`、`revision`、`sync`；提交落在探针仓库 |
| 同一 Codex `workspace-write` 会话的 bash | `touch ~/memory/x` 返回 `Operation not permitted`，`memctl read` 返回 `IO_FAILED: cannot open repository lock` |
| `install --status` 的 `mcp:` 块 | 输出 `serving memory_read, memory_write` 与 8 条注册动作；不写任何文件 |

第一组探测的隔离 `HOME` 曾放在系统临时目录下，Codex 的 `workspace-write` 允许写 `$TMPDIR`，因此该次 bash 写入成功，不能作为沙箱对照。改为真实用户目录下的临时 `HOME` 后重测，得到上表的拒绝结果；报告沙箱结论时必须区分这两种位置。

第三组在 2026-10-18 同日完成（`T073`）：每个宿主使用独立的隔离 `HOME`、独立状态目录与一次性记忆仓库，先由模型调用 `memory_read` 与 `memory_write`，再用同一会话的 shell 尝试写入记忆仓库。

| 宿主 | 会话限制 | MCP 受管写入 | 同会话 shell 写记忆仓库 |
| --- | --- | --- | --- |
| Codex 0.160.0 | `--sandbox workspace-write`、`approval_policy = never` | 成功（`T072` 实测） | 拒绝：`Operation not permitted`、`IO_FAILED` |
| pi 1.3.0 | 无文件沙箱；`--exposure direct` | 成功，提交 `9bacc1b` | 成功（宿主不提供文件沙箱） |
| OpenCode 2.0.26 | `permission.bash = "deny"` | 成功，提交 `18307b3` | 无 bash 工具，模型明确报告无法执行 |
| Command Code 1.84.0 | `-p`（print）默认拒绝 shell | 成功，提交 `4bd9b7a` | 拒绝：提示需要 `--yolo` |
| Grok 1.0.60 | `--sandbox workspace`；另测 `devbox` 与自定义 profile | `workspace` 下失败：`IO_FAILED: cannot open repository lock`；`devbox` 与加 `read_write` 的自定义 profile 下成功（提交 `7072167`、`3e2ecc`） | `workspace` 下拒绝；`devbox` 与自定义 profile 下成功 |
| Claude Code 2.1.220 | 未取得可用凭据 | 未实测 | 未实测 |
| Antigravity 1.4.2 | `--sandbox --dangerously-skip-permissions` | 成功，提交 `eb15947` | 拒绝：`Operation not permitted` |

Grok 与其他宿主不同：`workspace` profile 对**子进程**同样施加写限制，因此 MCP 服务进程也被拒绝访问记忆仓库。给 profile 增加 `read_write = ["<记忆仓库绝对路径>"]` 后，MCP 写入恢复为成功（提交 `3e2ecc`），同时 shell 在该路径上也变为可写；`devbox` profile 放开整个 home，MCP 与 shell 都能写。也就是说，Grok 需要用户显式把记忆仓库加入沙箱可写路径，而该授权同时放开 shell；这一点与「MCP 子进程不受 bash 沙箱限制」的假设不一致，必须在选择宿主时说明。

pi 与不带 `--sandbox` 的 Antigravity 没有文件沙箱：同一会话的 shell 可以直接改记忆仓库。它们的价值在于「多一条受管写入通道」，而不是隔离。pi 默认的 `codemode` 暴露还会让工具对模型不可见，注册时必须使用 `--exposure direct`。

Antigravity 的凭据存放在系统钥匙串，无法复制进隔离 `HOME`；探针改用真实登录态，在真实 `mcp_config.json` 中临时加入一个指向探针仓库的 `memctl-probe` 条目（通过 `env` 把 `AGENT_MEMORY_DIR` 与 `AGENT_MEMORY_STATE` 指向探针目录），测完按 SHA-256 校验恢复原文件，真实记忆仓库未被读写。不加 `--sandbox` 时 shell 可以直接写记忆仓库，与 pi 相同；加 `--sandbox` 后 shell 被拒绝而 MCP 写入仍成功，说明该宿主的 MCP 子进程同样在沙箱之外。

Claude Code 未实测：它的 `ANTHROPIC_BASE_URL` 指向本机代理 `127.0.0.1:15721`，该代理当轮未运行，隔离环境拿不到模型响应，钥匙串中也没有独立的 Claude 凭据。注册命令已确认被宿主接受，写入路径未验证。

未验证：Claude Code 的写入路径；Grok 的自定义 profile 只验证 `read_write`，未覆盖 `restrict_network`、`deny` 等字段。

## 7. 实现边界

已实现（`T071`、`T072`）：

- `memctl mcp` 子命令、三个工具、协议处理与错误映射。
- `internal/mcp` 包与对应单元、集成测试；`internal/mcp/probe.go` 提供 `M09` 的能力探测。
- `install --status` 的 `mcp:` 块：探测当前二进制并按 5.2 输出注册动作。
- 帮助目录的 `mcp` 条目与 `install` 的 MCP 说明，中英文一致。

不在实现范围：

- 修改宿主 MCP 配置文件；注册使用各宿主自有的 `mcp add` 命令。
- 多仓库、多记忆域、检索或语义搜索工具。
- 通过 MCP 触发 Dreaming 或 UI 操作。
- 读取或校验宿主已注册的 MCP 条目；`install --status` 只探测二进制自身。
