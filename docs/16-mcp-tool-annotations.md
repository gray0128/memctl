# 16. MCP 工具注解与宿主审批

> 图位置：本文是行为方案，按决定不进入 spec 图（未加 frontmatter）。注解字段的权威契约已并入 [09. MCP 记忆工具 §4.4](./09-mcp-memory-tools.md#44-工具集)，验收场景为 09 的 `M15` 与 [05 核心测试矩阵](./05-validation-and-delivery.md#2-核心测试矩阵) 的 `M01–M15` 行；本文保留调研证据、宿主对照与取舍理由。
>
> 状态：已实现（2026-11-05，`M15`）。`tools/list` 为 `memory_read` 与 `memory_entries` 返回 `annotations`，其余三个工具不返回该字段；不改工具语义、写入模型与既有协议处理。

## 1. 结论

- 现象：在 ThinkRail 里，每个项目第一次调用 `memctl/memory_read` 都会弹出审批，提示「It may change data」，即使该工具是纯读。
- 两个独立原因：
  1. 宿主审批是项目级授权。ThinkRail 把「Always allow」写进 `~/.thinkrail/projects.json` 的项目条目，没有全局开关，也没有按服务器信任；新建项目、或改动服务器的 `command`/`args` 后都要重新授权。
  2. memctl 没有声明工具注解。`internal/mcp/protocol.go` 的 `Tool` 只有 `name`、`description`、`inputSchema`，`tools/list` 返回里没有 `annotations`；宿主的自动放行条件是 `annotations.readOnlyHint === true`，缺失时一律按「可能改数据」处理。
- 只改第 2 项即可让读操作在所有项目、所有会话中免审。写操作仍需宿主在每个项目首次使用时授权一次；这是宿主的有意设计，本方案不改。
- 已按本方案实现：两个只读工具的注解已随 `tools/list` 返回（`M15`），对 pi、Codex、OMP 三类直接读取注解的实现生效，对其他宿主不引入回归。

## 2. 证据

### 2.1 当前 `tools/list` 没有注解

```bash
{ printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'; } | memctl mcp --agent pi \
  | jq -c 'select(.id==2) | .result.tools[] | {name, hasAnnotations: has("annotations"), annotations}'
```

2026-11-05 在本机 `v0.15.0` 上执行，五个工具全部返回 `"hasAnnotations":false`。后续验收必须用 `has("annotations")` 而不是直接展开 `annotations`：字段被 `omitempty` 省略时展开结果同样是 `null`，两种状态无法区分。

```text
{"name":"memory_read","hasAnnotations":false,"annotations":null}
{"name":"memory_entries","hasAnnotations":false,"annotations":null}
{"name":"memory_dream_prepare","hasAnnotations":false,"annotations":null}
{"name":"memory_dream_propose","hasAnnotations":false,"annotations":null}
{"name":"memory_write","hasAnnotations":false,"annotations":null}
```

### 2.2 宿主的判定顺序

以下摘自本机 ThinkRail 0.3.1 的会话守卫（`packages/server/src/agent/mcp/sessionHost.ts`），按执行顺序整理并省略与审批无关的分支：

```js
if (tool?.annotations?.readOnlyHint === true || allowedInChat.has(event.toolName)) return;
if (grantedMcpTools(policy(), server, config).includes(event.toolName)) return;
const question = `Allow ${server}/${rest.join("__")}? It may change data.`;
```

`tool` 是查到的工具定义对象（读 `annotations`），`rest` 是从 `event.toolName` 拆出的工具名片段（拼提示文案）。两者不是同一个变量，摘录时保持原名。

- 第一行是唯一的非交互放行途径，条件就是 `readOnlyHint === true`；「Allow in this chat」只记在当前会话内存里。
- 第二行读项目授权：`grantedMcpTools` 要求 `grant.target === mcpTargetOf(config)`，而 `mcpTargetOf` 是对 `{command,args}`（URL 型服务器取 `{url}`）求 SHA-256。指纹不含 `env`、`headers`，所以改环境变量不会要求重新授权，改命令或参数会让该项目已有授权全部失效。
- 项目条目结构：`mcpToolGrants: { <server>: { target, tools[] } }`，写入后立即落盘 `projects.json`。

### 2.3 引擎如何上报注解

pi 的 MCP 客户端把服务器在 `tools/list` 里声明的 `readOnlyHint`、`destructiveHint`、`idempotentHint`、`openWorldHint` 原样挂到工具上（`pi` 文档 `docs/mcp.md` 的工具注解一节），宿主据此判断。注解缺失时按 MCP 规范默认值处理：`readOnlyHint=false`、`destructiveHint=true`。

### 2.4 上游措辞

ThinkRail README 对同一机制的说法是：「asks in the chat before calls the server does not mark read-only (or always allows the tools you trust in a project)」。可见「没有项目级全局开关」与「只读靠服务器自述」都是上游既定设计，不是本机配置问题。

### 2.5 其他 Agent 是否读取注解

2026-11-05 在本机逐个核对二进制与文档。取证方式分三类：可直接读到源码或类型定义时读源码，只发布二进制的宿主读内嵌文档与符号，能反汇编时核对判定分支。

| Agent | 版本 | 读取 `annotations` | 证据 |
| --- | --- | --- | --- |
| pi | 1.5.0 | 是 | `dist/extensions/mcp/tools.js` 的 `toToolAnnotations` 取四个布尔 hint 挂到工具定义；`pi.getAllTools()` 把它们上报给权限扩展或宿主。pi 自身不弹工具审批 |
| Codex | 0.162.1 | 是 | 反汇编 `codex_core::mcp_tool_call::requires_mcp_tool_approval_for_mode(annotations: Option<&ToolAnnotations>, mode: AppToolApproval)` 证实：审批判定直接接受工具注解并按四个 hint 字节分支；`annotations` 为 `None` 时无条件返回「需要审批」。`AppToolApproval` 的四个取值见下 |
| OMP | 18.16.0 | 是 | `packages/coding-agent/src/mcp/tool-bridge.ts` 把 `p.annotations?.readOnlyHint` 存成 `mcpReadOnlyHint`，由 `getAllTools()` 暴露；`session/prewalk.ts` 用它避免把只读 MCP 调用当成写操作触发模型切换。但工具包装类的 `approval` 仍固定为 `"write"`，未按 hint 降级 |
| OpenCode | 2.0.28 | 解析但不用于放行 | `@effect/ai/McpSchema/Tool` 完整解析 `annotations`；`@opencode/McpTool` 的 `execute` 仍调用 `Permission.assert`，不读 hint。主 Agent（`Build`）默认 `{action:"*",effect:"allow"}`，因此主会话本来就不弹窗；`Explore` 子代理以 `deny` 开头，只白名单内置只读工具，任何 MCP 工具都会被拒 |
| Grok | 1.0.70 | 解析但不用于放行 | 协议层 `ToolAnnotations` 反序列化四个 hint；`~/.grok/docs/user-guide/22-permissions-and-safety.md` 的授权顺序第 4 步「Built-in auto-approvals」只列内置只读工具与只读 shell 命令，MCP 工具需命中 `[permission]` 的 `allow` 规则，或整会话处于 `always-approve` |
| Antigravity | 1.5.0 | 解析但不用于放行 | `ToolDefinition` 的 protobuf `ToolAnnotations` 含四个 hint；审批走 `cortex/permissions` 的 `PermissionGrantStore`，未发现按 `readOnlyHint` 跳过检查的分支。另有独立的按需加载开关：`1.3.3` 起支持 `tools/list` 返回 `_meta: {"antigravity/alwaysLoad": true}` 让工具直接进入模型工具集 |

结论：注解对 pi、Codex、OMP 三个宿主直接生效，对 ThinkRail（pi 引擎 + 宿主权限扩展）同样生效；对 OpenCode、Grok、Antigravity 不改变审批行为，但也不引入任何回归，因为三者都按 MCP 规范把缺失的注解当默认值处理。

Codex 的 `AppToolApproval` 取值集合为 `auto`、`prompt`、`writes`、`approve`（见二进制里的枚举字面量），可按服务器级 `default_tools_approval_mode` 或单工具级 `[mcp_servers.<name>.tools.<tool>] approval_mode` 配置。反汇编能确定「注解参与判定、缺失即需审批」两点，但逐 hint 到模式的对应关系依赖结构体内存布局推断，不作事实陈述：`auto` 与 `writes` 下究竞哪个 hint 放行，需用一条真实会话复验一次（验收项列入 §8）。本轮结论不依赖该细节：只要声明的 hint 是 `readOnlyHint: true` 与 `destructiveHint: false`，两者在四种模式里都不会被当成写操作。

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
| Codex | 是（注解参与审批判定） | 审批函数直接接受 `ToolAnnotations`，注解缺失时一律要求审批；声明只读后有免审可能。具体放行哪些模式待一次真实会话复验 |
| OMP | 是（影响扩展与 prewalk） | 注解经 `getAllTools()` 对上暴露，且避免只读 MCP 调用触发 prewalk 切模型；审批层级仍按 `approval = "write"`，默认 `approvalMode = "yolo"` 下不弹窗 |
| OpenCode | 否（不影响行为） | 主 Agent 默认放行全部工具，本就不弹窗；受限于 `Explore` 子代理的 `deny` 白名单，属于用户侧权限配置 |
| Grok | 否（不改变审批） | 只放行内置只读工具与只读 shell 命令；需要读工具免审时应写 `[permission]` 的 `MCPTool(memctl__*)` 规则或使用 `always-approve` |
| Antigravity | 否（不改变审批） | 审批走会话与项目的授权存储；如需让工具直接进入模型工具集，可另行评估 `_meta` 的 `antigravity/alwaysLoad` |
| 其他宿主 | 未验证 | Claude Code、Command Code、CodeBuddy、WorkBuddy、DSH 是否读取 `readOnlyHint` 需逐宿主实测，不能从上述结论推断 |

证据与取证方式见 §2.5。

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
| `internal/cli/help_data.go` | `mcp` 帮助与示例从「三个工具」更正为五个（阶段 H 后一直是过期文案） |
| `tests/integration/h3_http_dream_test.go`、`h3_local_dream_test.go`、`x10_project_root_test.go`、`internal/dream/local_test.go` | 写死的摘要日期改为相对今天（新增 `h3Day` / `testDay`，`writeH3Digest` 与 `seedDigest` 改收「相对天数」）；固定日期会随 `corpus.CorpusStateTTL`（14 天）过期而被 `PrepareLocal` 清理 |
| `scripts/test-shards.txt` | 重新生成，覆盖新增的 `M15` |
| `docs/09-mcp-memory-tools.md` | §4.4 工具契约表增加「注解」列；§5.2 补 Codex 与 Grok 的审批说明；§6.1 增加 `M15`，并把 `M02` 与 §5.3 的探测输出从三个工具更正为当前的五个 |
| `docs/05-validation-and-delivery.md` | 验收矩阵行由 `M01–M12` 更新为覆盖 `M15`（同时补上已实现的 `M13`、`M14`） |
| `docs/16-mcp-tool-annotations.md`、`docs/09-mcp-memory-tools.md`、`docs/05-validation-and-delivery.md`、`docs/STATUS.md` | 文档同步，见下方补充说明 |

变更清单里后四行是范围外发现、但会阻塞本轮验证的项：帮助文案与 `M02` 描述的是同一份接口契约，摘要日期测试则直接让 `make test` 变红（`internal/dream` 与 `tests/integration` 各一组）。它们不属于注解契约，因此在本轮一并修正并在此列出，不埋进其他改动里。

## 6. 验收

新增稳定场景 `M15`：`tools/list` 中 `memory_read` 与 `memory_entries` 返回 `annotations: {readOnlyHint: true, destructiveHint: false}`；`memory_write`、`memory_dream_prepare`、`memory_dream_propose` 的响应 JSON 里没有 `annotations` 键；`M02` 的工具名与参数模式不变。

| 检查 | 判定方式 | 2026-11-05 执行结果 |
| --- | --- | --- |
| 单元 | `TestM02ToolsList` 覆盖五个工具的注解取值，并断言后三个工具的序列化结果不含 `annotations` 键 | 通过；两个方向的反向验证（去掉 hint、给写工具加注解）均按预期失败 |
| 协议 | 用 §2.1 的命令对真实二进制取 `tools/list`，逐项比对 `hasAnnotations` 与 `annotations` | 通过；`TestM15MCPToolAnnotations` 用真实二进制断言，并且反向验证（把 hint 清空）按预期失败 |
| 宿主 | 撤销某项目的 `memory_read` 授权后新开会话：读操作不弹窗，`memory_write` 仍弹窗；记录宿主版本、项目路径与截图 | 本轮未执行：需要 ThinkRail 交互会话，列为遗留项 |
| 回归 | `make test` 全绿；`make install` 后重复协议检查 | 见 [开发状态](./STATUS.md) 的 `T132` 记录；本轮未执行 `make install`，协议检查改用临时构建的二进制 |

## 7. 发布与升级

- 版本：补丁版（新增可选字段，向后兼容）。
- 本机安装：`make install` 覆盖 `~/.agent-memory/bin/memctl`；macOS 上运行中的二进制要用 `mv` 原子替换，直接 `cp` 覆盖会被系统终止。
- 生效条件：宿主重启或在下一个会话重连 MCP 子进程后，`tools/list` 才带上注解。已打开的会话仍按旧工具表判定。
- 宿主文档：`docs/09` §5.2 现有的 Codex `default_tools_approval_mode = "approve"` 前置条件保留。它原本是对全部工具的门槛；加上注解后只读工具有机会在更宽松的模式下免审，但具体哪些模式生效需真实会话复验，在复验前不删该前置条件。

## 8. 待确认

- 除 §2.5 已核对的六个宿主外，Claude Code、Command Code、CodeBuddy、WorkBuddy、DSH 是否读取 `readOnlyHint`。需要每个宿主各跑一次「只读工具是否仍弹审批」。
- Codex 的逐 hint 到 `AppToolApproval` 模式对应关系。反汇编能确定注解参与判定且缺失即需审批，但结构体字段到内存偏移的映射是推断；需用真实 Codex 会话确认 `auto` 与 `writes` 下的具体行为。
- 读工具的冗余授权是否统一清理。清理只影响设置面板的列表，不影响安全边界。
- 上游是否计划提供「按服务器全局信任」。若后续版本提供该开关，应优先使用官方能力，而不是继续依赖注解或手工改配置。

已定案、不再待确认的两项：

- `memory_dream_prepare` 不标只读。它会刷新会话摘要缓存并改写 `$STATE/cache/dream/prepare.json` 快照，而该快照正是 `memory_dream_propose` 解析 `sessionIndices` 的基线来源；改写它属于可观察的状态变更，不满足 §3.3 的判定标准。
- Antigravity 的 `_meta: {"antigravity/alwaysLoad": true}` 不纳入本方案。它解决的是工具按需发现，与审批是两件事；是否需要单独评估取决于真实会话中模型是否主动调用 `memctl` 工具，本轮没有该证据。

## 9. 不采用的做法

| 做法 | 不采用的原因 |
| --- | --- |
| 直接编辑 `~/.thinkrail/projects.json` 批量写入 grants | 进程内有项目缓存，任何一次保存都会覆盖外部改动；且 `target` 指纹与条目结构属宿主实现细节 |
| 给写或提案工具也标只读 | 绕过宿主的人工确认，等于把「可改数据」的工具当成纯读 |
| 改服务器名、拆成多个服务器或降低工具可见性 | 工具名、注册命令与 `exposure` 是既有契约，改动会破坏宿主侧配置与文档 |
| 在 memctl 内拦截或模拟宿主确认 | 确认属于宿主职责，memctl 无法判断用户是否已审阅，与「不提供 `memory_dream_apply`」的理由一致 |
