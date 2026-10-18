# 04. CLI、UI 与分发

## 1. CLI 命令

```text
memctl <subcommand> [flags] [-- command...]
```

本表是二进制的目标命令契约，参考脚本不是可执行规范。除 `wrap` 外，未知参数返回用法错误。文件与版本语义见 [模型与契约](./02-domain-models-and-contracts.md)。

| 命令 | 参数 | 行为 |
| --- | --- | --- |
| `init [remote-url]` | `--confirm-private` | 按初始化分支创建、克隆或复用；无 URL 默认仅本地；提供 URL 时要求显式私有性与所有权确认 |
| `start` | `--agent NAME --sid ID --stdin --json --agy` | 尝试同步并输出指引、入口与状态；不捕获外部编辑；`--agy` 输出 Antigravity 的 `injectSteps` envelope |
| `context` | 同 start，另有 `--once` | 按会话缓存同步成功与注入状态；默认每轮注入，`--once` 只控制正文重复输出 |
| `pull` | `--agent NAME --sid ID --stdin --once` | 与 start 使用同一同步流程，保持 stdout 为空；保留名字但实际可补推本地提交 |
| `sync` / `end` | `--agent NAME --sid ID --stdin` | 自动路径只尽力触发后台同步，stdout 为空；`sync --agy` 输出 `{}` 供 Antigravity Hook 使用 |
| `sync` | 另有 `--wait --capture` | `--wait` 等待同步结果；`--capture` 显式接收串行外部编辑；可组合，两者都不适用于低延迟 Hook |
| `tag` | `--agent NAME --sid ID` | 输出 `agent://<device>/<agent>/<sid>`；身份不能唯一确定时失败 |
| `wrap` | `--agent NAME -- <cmd...>` | 运行前同步，向子进程传入会话环境，结束后尽力同步，透传退出码和信号语义 |
| `read <path>` | `--json` | 默认输出完整原文；JSON 输出 `{filePath, fileRevision, rawContent}`，供受管编辑 |
| `write <path>` | `--base REV --stdin --agent NAME --sid ID` | stdin 为文件原文，不是 Hook JSON；基线为修订或 `absent`，原子受管写入并本地提交 |
| `resolve` | `--json`、`--apply FILE --conflict ID` 或 `--abort --conflict ID` | 默认查看快照；应用 JSON 文件中的 `baseRevision/resolvedFiles`，或中止当前解决过程 |
| `recover` | `--status` 或 `--rollback` | 查看或恢复本工具未完成事务；不接管外部 Git 操作 |
| `ui` | `--port 18420 --no-open --readonly` | 启动本地 UI；`--readonly` 在服务端禁止全部 POST/PUT/PATCH/DELETE，包括 sync、runner 与安装 |
| `doctor` | `--json --fix` | 默认只读诊断；`--fix` 在当前基线上执行支持的确定性修复，不自动消除矛盾事实 |
| `dream` | `--runner claude` 或 `--runner codex` | 选择 runner 即确认将导出的受支持文本发送给该 runner 的模型端点；生成并保存提案，输出 proposalId、patchHash、Diff 和警告；不写主库 |
| `dream` | `--show ID` 或 `--apply ID --patch-hash HASH` | 查看或确认已有提案；apply 不重新调用模型 |
| `install [agent...]` | `--all --dry-run --status` | 探测、预览或应用版本匹配的接入配置，备份并幂等合并 |
| `dsh` | `plugin-path` 或 `plugin-dir` | 把内嵌的 `memctl-dsh` 写成状态目录下的绝对 tarball 或目录路径，供 DSH「插件 → 添加插件」安装；只写状态目录，不修改 DSH profile |
| `pi` | `plugin-dir` | 把内嵌的 `memctl-pi` 写成状态目录下的绝对本地包目录，供 `pi install <目录>` 安装；只写状态目录，不修改 pi settings；不提供 tarball 形式 |
| `mcp` | `--agent NAME --sid ID` | 在标准输入输出上以单行 JSON-RPC 提供 `memory_read` 与 `memory_write`；工具使用宿主的文件权限，绕过 Agent 的 bash 沙箱；不写配置，不修改记忆仓库之外的路径 |
| `update` | `--check` | 查询 GitHub Releases 最新版本；按平台下载并校验 SHA-256 后原子替换当前二进制；`--check` 只报告，不写文件 |
| `help [command]` | `--lang en\|zh --json` | 输出面向 Agent 的概览或单命令帮助；`<command> --help` 等价；`--json` 输出 `schemaVersion` 1 的机器可读目录；不访问记忆库、Git 或 Node.js |
| `version` | 无 | 不访问记忆库、不初始化图谱 |

同一单元格中的 flags 是可选能力集合，不表示必须全部传入；`write` 的 `--base` 和 `--stdin`、`resolve --apply`/`--abort` 的 `--conflict`、应用提案的 ID 和 hash 为必填，`resolve --apply` 的 JSON 必须携带非空 `baseRevision`。`resolve` 的查看、应用、中止互斥；`dream` 的生成、查看、应用互斥；`install --status` 不写文件。

`install` 至少需要一个 `agent` 参数或 `--all`，两者都缺省时返回用法错误。`--status` 只探测并输出适配状态，末尾附带 MCP 工具探测与各宿主注册命令，`--dry-run` 输出安装计划，两者都不写文件也不执行中断恢复。`--all` 跳过未探测到宿主可执行文件的 adapter；显式指定的 agent 在宿主未探测到但目标配置已存在时仍生成计划，以便用户在看到警告后自主决定。`install` 不访问记忆仓库、不要求 Git，也不需要 Node.js。

dsh 的原生插件 `memctl-dsh` 由 DSH Plugin Manager 安装，`install` 只探测并报告，不写入 profile 或 `hooks.json`。只有旧插件 `dsh-hooks-plugin` 已存在时，`install dsh` 才沿用旧的 `hooks.json` 合并路径并将其标记为 deprecated；两者都不存在或同时存在时，`install --status` 返回 `needs_attention`/`not_installed` 和界面安装提示，不自动写旧配置。`memctl dsh plugin-path` 与 `memctl dsh plugin-dir` 只读内嵌源文件并写入 `$STATE/dsh/`，输出绝对路径，不执行 `pnpm`、不修改 DSH profile。

pi 的原生插件 `memctl-pi` 通过 `pi install npm:memctl-pi@<version>` 或 `pi install <memctl pi plugin-dir 输出的目录>` 安装，`install` 只探测并报告，不写 `hooks.yaml`。旧 `pi-yaml-hooks` + `agent-memory-*` 条目只在条目实际存在且旧插件已安装时继续幂等更新并标记 deprecated；只有旧插件包、没有条目时不构成旧接入。两者同时存在、有旧条目但缺少旧插件、两者都不存在或版本低于下界时，`install --status` 返回 `needs_attention`，并输出 npm、本地目录和 `pi list` 校验提示。`memctl pi plugin-dir` 只读内嵌源文件并写入 `$STATE/pi/`，输出稳定绝对路径；它不运行 `pi install`、不访问网络、不写 `settings.json`。pi 不接受 tarball 作为安装输入，帮助和文档不得把 `.tgz` 写成可用安装源。

`memctl mcp` 是宿主沙箱之外的记忆工具通道，供 Agent 主动读取基线与受管写入；工具契约、宿主注册命令与验收见 [MCP 记忆工具](./09-mcp-memory-tools.md)。`install --status` 只在适配器列表之后探测该命令能否响应并输出宿主注册命令，不读宿主 MCP 配置。它不读宿主的 MCP 配置文件，也不代替生命周期插件：注入与每轮同步仍由 `memctl-dsh` 与 `memctl-pi` 负责。

`init` 重复执行不修改已有 remote。已有 remote 与 URL 不同时拒绝；需要在暂停自动操作后显式修改 Git remote，再使用 `init <url> --confirm-private` 确认新目标。确认记录绑定有效 fetch/push URL，URL 不同即失效。

### 1.1 相对参考脚本的有意变化

- 保留常用 Hook 命令名称、`--agent/--sid/--stdin/--once/--json` 和纯文本/JSON 输出习惯，不承诺复制脚本所有退出与容错细节。
- 自动 Hook 不再提交未知工作树内容。适配指引要求受管 `read/write`；串行直接编辑通过显式 `sync --capture` 接收。
- 断网、锁忙不写「已同步」标记；注入和同步去重分开。
- `tag` 不读取全局 `last-sid-<agent>` 猜测当前会话，不自动生成看似稳定的来源。
- JSON 输出不回退成纯文本；Git 命令失败不无条件归为内容冲突。
- 整理应用必须引用已经展示的提案，不提供「重新生成并自动应用」的模糊 `--apply` 开关。

### 1.2 `update` 自更新

`update` 从 GitHub Releases 查询 `gray0128/memctl` 的最新发布，不访问记忆仓库，不要求 Git 或 Node.js。

- **平台与资产**：只支持 `darwin/arm64`、`darwin/amd64`、`linux/amd64` 与 `windows/amd64`，资产名沿用发布脚本 `scripts/release.sh` 的 `memctl-<os>-<arch>`（Windows 带 `.exe`）。其他组合返回 `UNSUPPORTED`，不尝试近似平台。
- **版本判断**：以 `releases/latest` 的 `tag_name` 为准。当前版本是发布版本且不早于最新版本时输出 `up to date`，不下载资产。非发布版本（如 `v2.0.0-dev`、`git describe` 后缀或 `-dirty`）视为开发构建，可更新到最新发布。
- **完整性与替换**：读取同一 Release 的 `SHA256SUMS`，下载时以流式写入目标目录的临时文件并同时计算 SHA-256。摘要不匹配、`SHA256SUMS` 缺失或平台资产缺失时删除临时文件并返回错误，当前二进制不变。校验通过后沿用原文件权限（缺少所有者执行位时补足）、flush 并以同目录 rename 原子替换；正在运行的旧进程继续使用旧映像，重启后生效。
- **安全边界**：只请求固定的 GitHub API 与 Release 资产地址，不接受用户提供的 URL，不读取凭据，不发送记忆内容或仓库信息；不执行下载的二进制做自检，也不自动重启。未认证 GitHub API 受速率限制：404 返回 `NOT_FOUND`，403 与 5xx 返回 `NETWORK_UNAVAILABLE`。
- **平台差异**：Windows 不能在运行中替换可执行文件，`update` 返回 `UNSUPPORTED` 并给出下载地址，`update --check` 仍可只读报告；其他已支持平台在目标目录不可写时返回 `IO_FAILED`，不请求提权。
- **输出与退出码**：`--check` 只输出 `current:`、`latest:` 与 `up to date`/`update available`；`update` 替换成功后输出 `updated:` 和重启提示。成功或已最新返回 `0`，网络、完整性、权限或不支持平台返回 `1`，未知参数返回 `2`。

### 1.3 面向 Agent 的帮助与中英 i18n

帮助是 CLI 的可用性契约，不是命令表的副本。目标是无仓库、无 Git、无 Node.js、无网络的干净环境里，Agent 仅凭 `memctl help`、`memctl help <command>` 或 `memctl <command> --help` 就能确定命令的前置条件、必填参数、输入格式、输出字段、退出码和恢复动作。

- **概览**：`memctl help` 输出用途、快速开始、使用规则、命令分组、环境变量、退出码、语言选择和机器可读入口。概览必须给出从初始化到受管读写的可执行步骤，并明确受管写入、串行 capture、前台与后台同步的边界。
- **单命令页**：每个可由 `Run` 分发的命令都有且只有一个帮助条目，包含用法、说明、参数、选项（标明必填或可选）、输入、输出、退出码、示例、注意事项和相关命令。`memctl help <command>` 与 `memctl <command> --help`/`-h` 输出同一页面；`--` 之后的 `--help` 属于被包装命令，不得拦截。
- **错误边界**：未知帮助主题、未知帮助参数或无效 `--lang` 返回 `2`，stdout 为空；已知命令页返回 `0`。帮助路径不得访问记忆仓库、状态目录、Git、Node.js 或网络，也不得读取 stdin。
- **参数错误提示**：命令参数错误返回 `2`；能定位到具体命令时，stderr 追加 `Run "memctl help <command>" for usage.`，让调用方无需额外查询即可定位帮助页。该提示保持英文，与稳定错误码一致。
- **语言选择**：优先级为 `--lang` → `MEMCTL_LANG` → 第一个非空的 `LC_ALL`、`LC_MESSAGES`、`LANG` → 英文。`--lang` 可在任意命令的 `--` 之前使用并从参数中移除；无效值返回 `2`。`MEMCTL_LANG` 接受带可选地区或编码的 `en`/`zh`，无效值回退到 locale。locale 以 `zh` 开头选择中文，其他非空 locale（包括 `C`、`POSIX` 和不支持的语言）选择英文。
- **本地化边界**：只本地化帮助文本。命令输出键、JSON 字段名、稳定错误码和退出码保持英文，避免同一脚本因语言变化而无法解析。帮助文本不使用 ANSI 颜色、不探测终端宽度，输出只由语言和内容决定。
- **机器可读目录**：`memctl help --json` 输出单个 JSON 对象：

  ```json
  {
    "schemaVersion": 1,
    "tool": "memctl",
    "language": "en",
    "title": "memctl - manage the agent memory repository",
    "summary": "...",
    "usage": ["..."],
    "quickStart": [{"command": "...", "note": "..."}],
    "rules": ["..."],
    "commands": [
      {
        "name": "write",
        "summary": "...",
        "usage": ["..."],
        "description": "...",
        "arguments": [{"name": "<path>", "required": true, "description": "..."}],
        "flags": [{"name": "--base", "value": "REV", "required": true, "description": "..."}],
        "input": ["..."],
        "output": ["..."],
        "exitCodes": [{"code": 0, "meaning": "..."}],
        "examples": [{"command": "...", "note": "..."}],
        "notes": ["..."],
        "related": ["read", "sync"]
      }
    ],
    "environment": [{"name": "AGENT_MEMORY_DIR", "default": "~/memory", "description": "..."}],
    "exitCodes": [{"code": 0, "meaning": "..."}],
    "languageHelp": "...",
    "moreHelp": "..."
  }
  ```

  `memctl help <command> --json` 使用相同 envelope，`commands` 只含一个条目，不重复输出概览专属字段；`memctl <command> --help --json` 等价。`schemaVersion` 只在字段语义发生不兼容变化时递增。JSON 只写入 stdout，诊断写入 stderr。

帮助目录与命令分发必须同步：新增、重命名或删除命令时，同时更新帮助目录、分组和回归测试。命令行为变化时先改本契约和帮助内容，再改实现；帮助文本与实现不一致时以本契约为准。

## 2. 环境变量与会话身份

| 变量 | 默认值与语义 |
| --- | --- |
| `AGENT_MEMORY_DIR` | `~/memory`；规范化为绝对路径后绑定仓库 |
| `AGENT_MEMORY_STATE` | `~/.agent-memory`；仅覆盖运行状态根，不改变仓库锁位置 |
| `AGENT_MEMORY_BACKGROUND` | 默认开启；设为 `0`、`false`、`off` 或 `no` 时受管写入与自动 Hook 不派生后台同步 |
| `AGENT_MEMORY_AGENT` | `unknown`；`--agent` 优先 |
| `AGENT_MEMORY_DEVICE` | OS 主机名；探测失败使用 `unknown` 并提示 |
| `AGENT_MEMORY_SID` | 无默认值；由宿主或 wrap 传递 |
| `DSH_HOOK_SESSION_ID` | dsh 的兼容身份来源 |
| `MEMCTL_LANG` | 无默认值；帮助语言 `en` 或 `zh`，接受带地区或编码的写法；优先于 `LC_ALL`/`LC_MESSAGES`/`LANG`，仅影响帮助文本 |

SID 优先级为 `--sid` → `AGENT_MEMORY_SID` → `DSH_HOOK_SESSION_ID` → 有 `--stdin` 时 JSON 的 `session_id/sessionId/sessionID/conversationId`。空值跳过，非字符串返回协议错误。其他宿主变量由 adapter 显式映射到 `--sid`，例如旧 `pi-yaml-hooks` 接入的 `PI_SESSION_ID`；原生 `memctl-pi` 直接传 `ctx.sessionManager.getSessionId()`，不读取该环境变量。

缺少 SID 时，start/context 仍可只读注入，但来源标签标为不可用，不做跨进程去重；`tag` 返回错误。受管写入允许人工或未知来源，不伪造 source。`wrap` 可以生成随机会话 ID，并向其子进程导出 `AGENT_MEMORY_SID`、`AGENT_MEMORY_AGENT` 和规范化仓库路径。

URI 每一段分别转义；会话缓存键对完整元组做摘要，不用有损字符替换导致不同 SID 碰撞。`wrap` 不经 shell 重拼 argv，保留 stdio、退出码和信号处理；同步失败不能覆盖被包装程序的退出状态。

## 3. Hook 输入输出与时限

只在显式 `--stdin` 时读取 stdin，避免手工调用等待输入。Hook JSON 的输入限额见接口文档；超限立即结束读取并关闭输入，不继续无界缓存。日志只记录错误类型，不记录 payload。

`start --json` 示例：

```json
{
  "hookSpecificOutput": {
    "hookEventName": "SessionStart",
    "additionalContext": "[agent-memory] repo=... sync=local\n..."
  }
}
```

`context --json` 使用 `UserPromptSubmit`。`start --agy` 输出 Antigravity 要求的另一种 envelope：

```json
{
  "injectSteps": [
    {
      "systemMessage": {
        "systemMessage": "[agent-memory] repo=... sync=local\n..."
      }
    }
  ]
}
```

各 adapter 决定是否能消费对应 envelope；不是所有宿主都支持相同结构。start/context 的 stdout 只包含上下文或单个 JSON 对象，Git 输出与诊断进入脱敏 stderr。

| 情况 | 退出行为 |
| --- | --- |
| 自动 Hook 遇到断网、忙碌、未初始化或仓库保护态 | 返回 0 以免阻断宿主；在允许的上下文或 stderr 中报告状态 |
| 参数错误、Hook JSON 格式错误 | 返回 2；不伪造成功注入 |
| 显式管理命令、read/write/resolve/recover、`sync --wait` 失败 | 返回 1，并给出稳定原因码 |
| doctor 发现 error 级诊断 | 返回 1；只有 warning/info 时为 0 |
| wrap | 返回被包装程序状态；包装器无法启动程序时返回自身错误 |

带 `--stdin` 的调用按宿主 Hook 处理，仓库、同步或保护态失败返回 0，只在 stderr 或上下文中报告；手工运行 `start`/`context`/`pull` 属显式调用，同类失败返回 1。`--once` 已注入时 stdout 可以为空；Hook 成功只说明调用按协议结束，不证明上下文被宿主采纳或远端同步完成。

凭据交互遵循 [同步流程](./03-workflows-and-dataflows.md#43-凭据环境继承与失败处理)：只有前台命令且标准输入与标准错误均为终端时才由 Git/SSH 原生提示，Hook 与后台派生路径强制非交互，`memctl` 不实现自己的凭据提示。`write` 已占用标准输入，不用于凭据输入。

### 3.1 延迟目标

CLI 不预加载 Web 路由、全库 AST 或 watcher。原方案的「冷启动 < 5ms」改为 `version/help` 的待测优化目标，「sync < 50ms」仅作为无竞争、无网络等待的派生路径目标，不是所有命令的时限保证。

本次设计初始业务预算：start/context/pull 的一次同步最多 8 秒；后台和 `sync --wait` 的一次远端任务最多 30 秒。每个 Git 子命令使用剩余预算，不重新获得完整预算；显式本地事务有独立有界预算，实施前按测试结果确定。清理子进程、恢复工作树的耗时单独测量，不在预算到期后无条件解锁。

已验证 adapter 的宿主 timeout 必须覆盖业务预算、输入输出和最坏清理时间；未测清理时间的组合不能标为 `verified`。宿主更短时降低本次业务预算或仅使用本地上下文，不依赖宿主强杀实现正常取消。

### 3.2 宿主生命周期事件映射

`sync` 与 `end` 使用同一后台同步实现，区别在触发边界：`sync` 对应回合结束后的提交或压缩前，`end` 对应会话运行时被宿主释放。`end` 不释放仓库锁、不写入「会话已关闭」状态，可以重复调用；自动 Hook 遇到断网、锁忙或保护态时仍按退出 0 处理，由下一次 `start` 或 `sync` 重试。

| 宿主 | 回合同步 | 会话结束触发 `end` | 备注 |
| --- | --- | --- | --- |
| Claude Code | `Stop`；`StopFailure` 与 `PreCompact` 为待补事件 | `SessionEnd`：`clear`、`resume`、`logout`、`prompt_input_exit`、`other` | 默认 1.5 秒；宿主不等待输出；`Stop` 不覆盖用户中断 |
| Codex | `Stop`；`PreCompact` | `SessionEnd`：归档或删除仍打开的会话、正常关闭、无客户端连接且空闲 30 分钟 | 默认 1 秒、上限 3 秒；`reason` 目前只有 `other`；切换走不结束 |
| Grok | `Stop`/`StopFailure`；`PreCompact` 为待补事件 | `SessionEnd` | 默认超时 5 秒 |
| Antigravity | `PostInvocation`；`Stop` | 待真实宿主确认 `Stop` 的语义；当前模板不写 `SessionEnd` | 未确认前不新增映射 |
| Command Code | `Stop` | 无 `SessionEnd` | 只保证回合级同步，不宣称收尾已验证 |
| OpenCode | `session.idle`（兼容）；`session.compacted` | `session.deleted` | `session.status` 迁移与 `session.idle` 弃用待后续处理；删除会话才结束 |
| pi | `session.idle`（旧 `pi-yaml-hooks`）；原生插件用 `agent_settled`，旧版回退 `agent_end` | `session.deleted`（旧）；原生插件用 `session_shutdown`，`reload` 不收尾 | 原生插件见 [pi 专用插件](./08-pi-plugin.md#63-生命周期映射) |
| DSH | `turn/end` | `agent/disposed`；插件卸载时对仍 live 的会话再触发一次兜底 `end` | 没有单会话关闭入口；归档不销毁运行时；退出应用依赖宿主释放 agent |

触发规则：

- `start` 绑定会话开始、恢复、清理和压缩后的重新注入；`context` 用于需要每轮注入的宿主。
- `sync` 绑定回合结束、错误或中断回合，以及压缩前；回合结束是耐久性主路径。
- `end` 绑定会话运行时释放：宿主的 `SessionEnd`、`session.deleted` 或 `agent/disposed`，以及插件卸载时的兜底。回合结束、空闲、压缩、仍保留运行时的归档和子代理结束不触发 `end`。
- 崩溃、SIGKILL 与宿主提前退出不保证 `end` 执行；本轮已提交内容由回合级 `sync` 保留，下一次 `start` 或 `sync` 继续重试。
- `end` 必须在宿主预算内返回。`memctl` 只派生后台工作进程并以 0 退出，不等待网络或 Git 完成。

## 4. Web UI

七个视图共享同一顶栏，统一显示仓库路径、本地状态、远端状态、最后同步时间和只读标识；最后同步时间按浏览器本地时区显示到分钟。ahead/behind 未知时显示「未知」，不显示零。外部写入的失效提示到达时写入方可能仍持有仓库锁，服务端会返回 `stale: true` 的缓存快照；客户端对此有界重试，取得新鲜快照后再渲染，重试仍失败才显示过期状态。读取接口返回 `BUSY` 时同样做有界重试，耗尽后显示仓库忙提示，不向界面暴露服务端诊断路径。

| 视图 | 展示与操作 | 失败或过期状态 |
| --- | --- | --- |
| 概览 | 主入口、修订版本、文件与条目计数、图谱节点与关系数、诊断计数；指标卡片跳转到条目、知识图谱与诊断 | 快照过期显示「快照已过期」并按有界重试刷新；诊断计数读取失败时显示占位符 |
| 知识图谱 | `MEMORY.md` 可达关系、死链、孤立文件；拆分条目为新文件。节点标签显示文件名，选中节点后显示完整路径 | 以同一仓库基线提交多文件计划；过期后重新预览 |
| 条目浏览 | 关键词、Agent、设备、日期筛选；文件原文编辑、条目原文编辑、条目移动和删除 | 保留未保存草稿；版本冲突显示当前内容与草稿差异，不自动覆盖 |
| 冲突解决 | 三方上下文、文本合并、删除选择、阶段进度与中止 | 「继续解决」可能进入下一提交；unsupported 类型提供人工入口，不称一键无损 |
| 审计回滚 | Git 历史、Diff、可解析来源、可疑内容提示；确认后 revert | 无法确定来源显示未知；回滚失败展示恢复状态 |
| 诊断 | 只读诊断按错误、告警、信息分组，展示问题位置与建议；对确定性修复执行 `doctor:fix` | 基线过期要求刷新后重试；无确定性修复规则时提示人工处理 |
| 整理接入 | 提案生成、固定 Diff 确认、状态查询；adapter 探测、预览、安装 | 沙箱不可用禁用生成；过期提案禁止应用；配置成功与宿主验证分开；安装基线过期后重新探测预览 |

图谱和条目编辑最终调用相同事务；「拆分」在同一事务内创建目标文件、从源文件移除该条目并保留其来源元数据，整体失败不能留下半套索引。拆分不自动修改入口文件：新文件在被条目链接指向之前按孤立文件显示。初版整理仅支持整份提案确认，符合流程文档中的应用边界。概览的指标卡片可键盘聚焦，按回车或空格触发跳转。

冲突解决的提交覆盖当前阶段全部冲突文件，应用与中止都复用 CLI `resolve` 的同一多阶段状态机；下一提交再次冲突时展示新的三方快照，不把一次确认当作全部完成。审计回滚只回滚当前可达历史中的单父提交，要求以当前仓库修订为基线，根提交与 merge commit 给出不支持说明，回滚冲突在恢复原始历史后展示恢复状态，不回显半应用结果。

来源链接只对明确允许的 `https` 等安全协议提供导航；`agent://` 默认提供复制，不任意唤起外部程序。来源 URI 在列表中截断显示，悬停时给出完整值。代码、Markdown 原始 HTML、Diff、文件名和来源字段按不可信文本渲染。可疑词扫描只显示线索，不把 `curl`、SQL 或 URL 本身当成恶意结论。

整理接入的 adapter 面板只调用 `GET /api/v1/adapters` 与 `POST /api/v1/adapters/{agent}/install`：先展示探测状态与安装计划，操作者确认后以预览中每个已存在目标文件的 `currentRevision` 作为 `expectedConfigRevisions` 提交安装；`REVISION_MISMATCH` 时保留预览并要求重新探测，未探测到宿主或需要人工补丁时不在界面上声称已支持。存在待恢复的中断安装时，预览标记 `pendingRecovery`，安装请求会先恢复现场再继续。只读模式下安装入口禁用，并依赖服务端 `READ_ONLY` 拦截。

UI 不自动生成健康分；按错误、警告、信息展示诊断数。只有实际需要检查视觉交互时才开展 UI/UX 验证，当前方案阶段以协议和业务测试为主。

## 5. HTTP 安全

1. 服务仅绑定 `127.0.0.1`，默认端口 18420；占用时明确报错，不复用未知服务或切换公网地址。Host 精确匹配实际监听端口的 `127.0.0.1` 或 `localhost`，不信任代理头。
2. 每次启动生成 32 字节随机 Session Token，嵌入当前首页 meta。所有 API 方法，包括 GET、PATCH 和 SSE，校验 `X-Memctl-Token`；Token 不放 URL、日志或 localStorage。
3. 浏览器 API 请求存在 Origin 时要求与当前页面允许源精确一致；拒绝 `null` 或其他来源，不开放跨域 CORS。没有 Origin 的本地 CLI 请求仍需 Token。
4. 首页和 API 返回 `Cache-Control: no-store`、`X-Content-Type-Options: nosniff`，首页设置 `Referrer-Policy: no-referrer` 和禁止被嵌入的 CSP `frame-ancestors 'none'`。脚本、样式与连接限制为所需本地资源，不加载外部 CDN。根路径 `GET /` 返回内嵌首页并在 meta 中注入 Session Token、只读标识与 Dreaming 隔离能力（`memctl-dream-sandbox` 取 `available`/`unavailable`）；UI 据此在没有已验证沙箱时禁用提案生成，服务端仍会独立重复校验。`GET /assets/` 返回内嵌脚本与样式且仅服务普通文件、不列出目录；两者属于静态资源，携带与首页一致的只允许本地 `self` 的 CSP，不要求 Token。
5. 只读模式由路由中间件执行，不仅隐藏按钮。文件路径校验仍使用架构定义的统一访问器，鉴权成功不能绕过路径边界。
6. API 限制请求体、并发生成任务、头部读取时间和空闲连接；SSE 采用适合长连接的单独写入策略、有限队列和心跳。不能用普通响应总写超时误杀所有 SSE。
7. Token 防御网页跨站请求，不隔离同一 OS 用户下有权读取首页的本机程序；不把本地 Web 控制台当成多用户认证系统。

## 6. 单文件打包

内嵌资产包括前端静态文件、默认指引、全部支持的 adapter 模板、`memctl-dsh` 与 `memctl-pi` 插件源文件。默认指引与插件随二进制发布，不依赖安装时额外复制文件；dsh 插件 tarball 和目录、pi 插件本地目录在运行时生成到状态目录。`make release` 附带 `memctl-pi-<version>.tgz` 仅用于 npm 发布和审计，不作为 pi 的安装输入。

```go
// internal/assets/embed.go
package assets

import "embed"

//go:embed INSTRUCTIONS.md
var Instructions string

//go:embed all:adapters
var Adapters embed.FS
```

```go
// web/embed.go
package web

import (
    "embed"
    "io/fs"
)

//go:embed all:dist
var files embed.FS

func DistFS() (fs.FS, error) {
    return fs.Sub(files, "dist")
}
```

```go
// plugins/dsh/embed.go
package dshplugin

import "embed"

//go:embed package.json cordis.patch.yml index.js icon.svg locale/*.json
var files embed.FS
```

`plugins/dsh/test/` 不匹配上述模式，因此不进入运行时 tarball；`npm pack` 的 `files` 字段独立限制发布文件清单。插件包不含 `dependencies`、`install`、`postinstall` 或 `prepare`。

兼容读取 `$STATE/INSTRUCTIONS.md` 作为用户显式创建的附加指引，不覆盖内嵌安全规则、不自动执行其中命令，输出时标明来源。升级不改写该文件；不存在时全部功能仍可运行。

以下 Makefile 是 POSIX 构建机上的实际构建入口。`make build` 先构建前端再编译本机二进制；`make release` 依赖 `make test`，先跑全量测试（含两个插件的 Node 测试）再由 `scripts/release.sh` 交叉编译四个候选二进制、打包两个插件 tarball 并生成发布元数据。`make release-dist` 只构建与打包、不跑测试，仅供「测试已由前置作业保证」的流水线使用，不用于本地发布。Windows 运行支持仍需对应平台测试。

```makefile
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v2.0.0-dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all ui build test install clean release dsh-plugin-test dsh-plugin-pack pi-plugin-test pi-plugin-pack
all: build

ui:
	cd web && npm ci && npm run build

test: ui
	cd web && npm test
	node --test plugins/dsh/test/*.test.js
	node --test plugins/pi/test/*.test.js
	$(GO) test ./...

dsh-plugin-test:
	node --test plugins/dsh/test/*.test.js

dsh-plugin-pack: dsh-plugin-test
	mkdir -p dist
	cd plugins/dsh && npm pack --pack-destination ../../dist

pi-plugin-test:
	node --test plugins/pi/test/*.test.js

pi-plugin-pack: pi-plugin-test
	mkdir -p dist
	cd plugins/pi && npm pack --pack-destination ../../dist

build: ui
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/memctl ./cmd/memctl

install: build
	mkdir -p "$(HOME)/.agent-memory/bin"
	install -m 755 bin/memctl "$(HOME)/.agent-memory/bin/memctl"

release: test
	VERSION="$(VERSION)" scripts/release.sh

release-dist: ui
	VERSION="$(VERSION)" scripts/release.sh

# Runs one quarter of the integration suite plus every unit package. CI only;
# `make test` stays the full local gate.
SHARD ?= 1
test-shard: ui
	scripts/test-shard.sh $(SHARD) 4

clean:
	rm -rf bin dist coverage web/dist
```

`make release` 在 `dist/` 产出 `memctl-darwin-arm64`、`memctl-darwin-amd64`、`memctl-linux-amd64`、`memctl-windows-amd64.exe` 四个候选二进制，以及 `memctl-dsh-<version>.tgz`、`memctl-pi-<version>.tgz` 两个插件包和三个元数据文件：`SHA256SUMS`（四个候选二进制与两个插件包的 SHA-256）、`THIRD_PARTY_LICENSES.txt`（Go 运行时模块与前端运行时依赖的许可证全文，构建期前端依赖只列 SPDX 标识）与 `RELEASE-INFO.txt`（版本、提交、Go/Node/npm/Git 版本与逐平台验证状态）。同一宿主上的重复构建在相同工具链下产出相同二进制。`bin/` 与 `dist/` 都不提交。

GitHub Actions 的 `build` 工作流（[.github/workflows/build.yml](../.github/workflows/build.yml)）分三个作业：

- `verify`：矩阵作业，四个分片并行。每个分片运行全部单元包与该分片分到的集成测试；前端与两个插件的 Node 测试只在分片 1 运行一次。
- `build`：与 `verify` 并行，不等待测试，运行 `make release-dist` 构建四个候选二进制与两个插件包并上传 `dist/`。
- `publish`：`needs: [verify, build]`，只在推送 `v*` 标签时运行，检出仓库历史、下载构建产物后创建 GitHub Release。检出是必需的：`--generate-notes` 要与上一个标签比较生成变更说明，缺少 `.git` 会以 `fatal: not a git repository` 失败。测试失败时该作业不会运行，因此不产出 Release。

`make release` 仍是本地完整发布入口，等于 `make test` 加 `scripts/release.sh`。`make test` 保持全量语义：它一次跑完所有单元包与全部集成测试，是本地发布的测试门槛。分片只是 CI 的调度手段，两者覆盖同一批测试，靠 `scripts/test-shards.txt` 保证。

集成测试分片由两个脚本承担：

- `scripts/test-shards.txt` 记录「测试名 → 分片号」，随代码提交，便于评审与复现。测试名有增减时必须重新生成。
- `scripts/balance-test-shards.sh [N]` 实测各测试耗时后按最长优先贪心分配到 N 个分片，重写清单。
- `scripts/test-shard.sh <分片> <总数>` 按清单过滤执行。清单缺项、分片号超出总数或某分片为空时以非零退出，避免静默漏测。

分片按**测试名**而不是按包切分，因为 Go 1.27 没有 `-shard` 标志，且集成测试集中在单个包。分片在独立进程中运行，因此 `t.Setenv` 等进程级状态不会跨分片互相污染。

2026-10-18 实测（[运行 39101067492](https://github.com/gray0128/memctl/actions/runs/39101067492)）：工作流总时长 2:28，基线为 4:49；四个分片作业 1:27–2:18，打包作业 0:49 并行完成。四个作业各自支付约 30 秒环境准备，因此分片数不宜继续增加，收益主要取决于最长分片。

`main` 推送与标签推送使用不同的并发策略：前者 `cancel-in-progress: true`，新提交取消排队中的旧运行；后者为 `false`，已开始的发布不被取消。

运行时核心依赖为 Git，以及所选 Git 传输需要的 SSH 或凭据环境；Dreaming 另需用户已安装的 runner 和经过验证的隔离能力。没有 runner 只影响整理生成，不影响 CLI、同步、诊断和 UI。发布包含校验和、依赖许可证、工具版本与平台测试结果，不将交叉编译通过当成运行验收。
