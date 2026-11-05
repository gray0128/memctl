# 16. MCP 工具注解与宿主审批

> 图位置：本文是未实现的行为方案，按决定不进入 spec 图（未加 frontmatter）。当前 `tools/list` 契约仍以 [09. MCP 记忆工具](./09-mcp-memory-tools.md) 为准；方案落地后把注解字段与验收并入 09 与 05。
>
> 状态：方案（未实现，2026-11-05 起草）。本文定义 `tools/list` 的工具注解契约、注解与宿主审批的关系，以及让只读工具免审的改动方案；不改变工具语义、写入模型与既有协议处理。

## 1. 结论

- 现象：在 ThinkRail 里，每个项目第一次调用 `memctl/memory_read` 都会弹出审批，提示「It may change data」，即使该工具是纯读。
- 两个独立原因：
  1. 宿主审批是项目级授权。ThinkRail 把「Always allow」写进 `~/.thinkrail/projects.json` 的项目条目，没有全局开关，也没有按服务器信任；新建项目、或改动服务器的 `command`/`args` 后都要重新授权。
  2. memctl 没有声明工具注解。`internal/mcp/protocol.go` 的 `Tool` 只有 `name`、`description`、`inputSchema`，`tools/list` 返回里没有 `annotations`；宿主的自动放行条件是 `annotations.readOnlyHint === true`，缺失时一律按「可能改数据」处理。
- 只改第 2 项即可让读操作在所有项目、所有会话中免审。写操作仍需宿主在每个项目首次使用时授权一次；这是宿主的有意设计，本方案不改。

## 2. 证据

### 2.1 当前 `tools/list` 没有注解

```bash
{ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; } | memctl mcp --agent pi \
  | jq -c 'select(.id==2) | .result.tools[] | {name, annotations}'
```

2026-11-05 在本机 `v0.15.0` 上执行，五个工具全部返回 `"annotations":null`：

```text
{"name":"memory_read","annotations":null}
{"name":"memory_entries","annotations":null}
{"name":"memory_dream_prepare","annotations":null}
{"name":"memory_dream_propose","annotations":null}
{"name":"memory_write","annotations":null}
```

### 2.2 宿主的判定顺序

以下摘自本机 ThinkRail 0.3.1 的会话守卫（`packages/server/src/agent/mcp/sessionHost.ts`），按执行顺序整理并省略与审批无关的分支：

```js
if (tool?.annotations?.readOnlyHint === true || allowedInChat.has(event.toolName)) return;
if (grantedMcpTools(policy(), server, config).includes(event.toolName)) return;
const question = `Allow ${server}/${tool.join("__")}? It may change data.`;
```

- 第一行是唯一的非交互放行途径，条件就是 `readOnlyHint === true`；「Allow in this chat」只记在当前会话内存里。
- 第二行读项目授权：`grantedMcpTools` 要求 `grant.target === mcpTargetOf(config)`，而 `mcpTargetOf` 是对 `{command,args}`（URL 型服务器取 `{url}`）求 SHA-256。指纹不含 `env`、`headers`，所以改环境变量不会要求重新授权，改命令或参数会让该项目已有授权全部失效。
- 项目条目结构：`mcpToolGrants: { <server>: { target, tools[] } }`，写入后立即落盘 `projects.json`。

### 2.3 引擎如何上报注解

pi 的 MCP 客户端把服务器在 `tools/list` 里声明的 `readOnlyHint`、`destructiveHint`、`idempotentHint`、`openWorldHint` 原样挂到工具上（`pi` 文档 `docs/mcp.md` 的工具注解一节），宿主据此判断。注解缺失时按 MCP 规范默认值处理：`readOnlyHint=false`、`destructiveHint=true`。

### 2.4 上游措辞

ThinkRail README 对同一机制的说法是：「asks in the chat before calls the server does not mark read-only (or always allows the tools you trust in a project)」。可见「没有项目级全局开关」与「只读靠服务器自述」都是上游既定设计，不是本机配置问题。

## 3. 契约变更

### 3.1 `Tool` 增加 `annotations`

`internal/mcp/protocol.go` 增加可选字段，序列化后即 MCP 标准的 `tools[].annotations`：

```go
// ToolAnnotations mirrors the MCP tool annotation object. Pointers keep
// explicit false values distinguishable from absent hints.
type ToolAnnotations struct {
	ReadOnlyHint    *bool `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool `json:"openWorldHint,omitempty"`
}

type Tool struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	InputSchema any              `json:"inputSchema"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}
```

`tools/list` 直接序列化 `Tool`（`internal/mcp/server.go`），无需改动处理器。

### 3.2 各工具的取值

| 工具 | 注解 | 依据 |
| --- | --- | --- |
| `memory_read` | `readOnlyHint: true`、`destructiveHint: false` | 只读 `vault`，不产生提交；读取时获取仓库锁，但不改仓库内容 |
| `memory_entries` | `readOnlyHint: true`、`destructiveHint: false` | 只读条目查询，不建立索引，不写文件 |
| `memory_write` | 不声明 | 写记忆仓库并提交 |
| `memory_dream_prepare` | 不声明 | 会刷新会话摘要缓存并写 `cache/dream/prepare.json` 快照 |
| `memory_dream_propose` | 不声明 | 持久化待审阅提案 |

只给两个只读工具加注解，其余工具保持无注解，JSON 里不出现 `annotations` 字段。这样宿主对写与提案的确认行为与现在完全一致。

### 3.3 只读的判定标准

判定标准是「不修改记忆仓库的文件内容、不产生 Git 提交、不改提案与消费记录」。读取路径会获取仓库锁，并可能创建锁文件，这不视为写：它不改变受管内容，也不改变 Git 历史。

## 4. 影响面

### 4.1 宿主

| 宿主 | 注解是否被采纳 | 说明 |
| --- | --- | --- |
| ThinkRail | 是（已核对判定逻辑） | 读工具不再弹窗；写工具仍按项目授权一次 |
| pi | 是（引擎上报注解） | pi 引擎本身不做 MCP 审批；是否放行由宿主的权限扩展决定，ThinkRail 即其一 |
| 其他宿主 | 未验证 | Codex、Claude Code、OpenCode、Command Code、Grok、Antigravity、CodeBuddy、WorkBuddy、DSH 是否读取 `readOnlyHint` 需逐宿主实测，不能从 ThinkRail 推断 |

### 4.2 已有的项目授权

改动不触碰注册条目，`command`/`args` 不变，`mcpTargetOf` 指纹不变，现存 `mcpToolGrants` 继续匹配。读工具对应的授权项变成冗余，可在宿主设置里撤销；不撤销也不影响行为。

### 4.3 协议兼容

`annotations` 是 MCP 标准的可选字段，不读取该字段的客户端按规范默认值处理（`readOnlyHint=false`）。因此旧版 memctl 与新版宿主、新版 memctl 与旧版宿主都能正常握手，行为差异只在「是否免审」。

### 4.4 安全

- 误标只读会让宿主跳过一次人工确认，因此只标注两个纯读工具，且以 §3.3 的标准判定。
- 注解不改变权限：`memory_write` 仍需要 `base` 基线与受管事务，路径边界仍由 `vault` 校验。
- 注解不改变宿主沙箱：Codex、Grok、Antigravity 的写入限制与注册前置条件不受影响。

## 5. 变更清单

| 文件 | 改动 |
| --- | --- |
| `internal/mcp/protocol.go` | 新增 `ToolAnnotations` 与 `Tool.Annotations` |
| `internal/mcp/tools.go` | 给 `memory_read`、`memory_entries` 设置注解 |
| `internal/mcp/tools_test.go` | `TestM02ToolsList` 增加注解断言：两个读工具有 `readOnlyHint: true`，其余三个工具 `Annotations == nil` |
| `tests/integration/t071_mcp_test.go` | 按 §6 的 `M15` 断言真实 `tools/list` 输出 |
| `docs/09-mcp-memory-tools.md` | §4.4 工具契约表增加「注解」列；§6.1 增加 `M15` |
| `docs/05-validation-and-delivery.md` | 验收矩阵行由 `M01–M12` 更新为覆盖 `M15` |
| `docs/STATUS.md` | 登记本轮任务与完成记录 |

## 6. 验收

新增稳定场景 `M15`：`tools/list` 中 `memory_read` 与 `memory_entries` 返回 `annotations: {readOnlyHint: true, destructiveHint: false}`；`memory_write`、`memory_dream_prepare`、`memory_dream_propose` 不返回 `annotations`；`M02` 的工具名与参数模式不变。

| 检查 | 判定方式 |
| --- | --- |
| 单元 | `TestM02ToolsList` 覆盖五个工具的注解取值 |
| 协议 | 用 §2.1 的命令对真实二进制取 `tools/list`，逐项比对 |
| 宿主 | 撤销某项目的 `memory_read` 授权后新开会话：读操作不弹窗，`memory_write` 仍弹窗；记录宿主版本、项目路径与截图 |
| 回归 | `make test` 全绿；`make install` 后重复协议检查 |

## 7. 发布与升级

- 版本：补丁版（新增可选字段，向后兼容）。
- 本机安装：`make install` 覆盖 `~/.agent-memory/bin/memctl`；macOS 上运行中的二进制要用 `mv` 原子替换，直接 `cp` 覆盖会被系统终止。
- 生效条件：宿主重启或在下一个会话重连 MCP 子进程后，`tools/list` 才带上注解。已打开的会话仍按旧工具表判定。
- 宿主文档：Codex 的 `default_tools_approval_mode = "approve"` 等前置条件在逐宿主实测前保留原样，不因本方案删除。

## 8. 待确认

- 除 ThinkRail 与 pi 外，哪些宿主读取 `readOnlyHint`。需要每个宿主各跑一次「只读工具是否仍弹审批」。
- `memory_dream_prepare` 是否也应标为只读。它写本地缓存快照，不写记忆仓库；当前按保守口径不标注。
- 读工具的冗余授权是否统一清理。清理只影响设置面板的列表，不影响安全边界。
- 上游是否计划提供「按服务器全局信任」。若后续版本提供该开关，应优先使用官方能力，而不是继续依赖注解或手工改配置。

## 9. 不采用的做法

| 做法 | 不采用的原因 |
| --- | --- |
| 直接编辑 `~/.thinkrail/projects.json` 批量写入 grants | 进程内有项目缓存，任何一次保存都会覆盖外部改动；且 `target` 指纹与条目结构属宿主实现细节 |
| 给写或提案工具也标只读 | 绕过宿主的人工确认，等于把「可改数据」的工具当成纯读 |
| 改服务器名、拆成多个服务器或降低工具可见性 | 工具名、注册命令与 `exposure` 是既有契约，改动会破坏宿主侧配置与文档 |
| 在 memctl 内拦截或模拟宿主确认 | 确认属于宿主职责，memctl 无法判断用户是否已审阅，与「不提供 `memory_dream_apply`」的理由一致 |
