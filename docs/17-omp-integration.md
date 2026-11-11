---
id: memctl-host-omp
type: module-design
title: OMP 宿主接入
status: active
parent: memctl-architecture
depends-on: [memctl-cli-ui, memctl-plugin-pi]
references: [memctl-mcp]
---

# 17. OMP 宿主接入：调研、适配分析与最佳使用方案

> 状态：调研、方案与代码适配完成（`T133`、`T134`，2026-11-06，基于本机 OMP `18.17.0` / darwin arm64 实测取证）。`memctl install` 已注册 `omp` 适配器，`memctl-pi 0.6.0` 在运行时识别 OMP 并以 `--agent omp` 调用 CLI，`PI_SESSION_ID` 已进入 SID 回退链，OMP 会话可蒸馏。注册、探测与生命周期命令真机验证见 §6；未验证项在 §6 逐条列出。

## 1. 结论

- OMP（全称 Oh My Pi，npm 包名 `@oh-my-pi/pi-coding-agent`，当前实测版本 `18.17.0`）是基于 Pi（`pi-coding-agent`）深度分叉与演进的 Agent 宿主，底层使用 Bun 打包为单文件独立二进制，主配置目录为 `~/.omp/agent`。
- **最佳使用方式**：推荐**「Extension 自动化生命周期注入/同步 + Skill / AGENTS.md 显式约束契约」的双轨组合**，必要时辅助全局 MCP 工具通道。
- **生命周期自动化轨（主干）**：OMP 原生完整兼容 Pi 的 Extension 扩展机制，用 OMP 插件管理器链接 `memctl-pi` 即可接管 `session_start`（启动预读与同步）、`before_agent_start`（注入记忆正文）、`agent_settled`（单轮结束触发后台有界同步）和 `session_shutdown`（会话结束有界收尾）。OMP `18.17.0` 没有 `systemPromptOptions.sections`，实际走替换 `systemPrompt` 的回退路径；该回退会替换整个提示词，缓存收益不如 pi 的 section。
- **显式契约轨（辅翼）**：OMP 原生开启了 `skills.enableAgentsUser: true`，且启动时会自动递归查找工作区及用户主目录的 `AGENTS.md`。本机通过 `skills` CLI 安装在 `~/.agents/skills/memctl` 的配套 Skill 和全局规则由 OMP 原生感知；机制已就绪，Skill 在真实 OMP 会话中触发受管写入尚未验证（`V-OMP-05`）。
- **补充通道（MCP）**：OMP 原生支持全局 `~/.omp/agent/mcp.json`，且具备对 Claude Code、Gemini、Cursor、Codex 等各宿主 MCP 配置的兼容加载能力。需要原生工具调用（`memory_read`、`memory_write` 等）时配置 stdio MCP server；OMP 没有 `mcp add` 子命令，`install --status` 给出文件写法提示。
- **会话身份传递**：OMP 在子进程（bash 工具）环境中注入 `PI_SESSION_ID` 与 `PI_SESSION_FILE`；`PI_SESSION_ID` 已纳入 `memctl` CLI 的 SID 回退链，终端中的 `memctl write` 自动关联当前会话。`ctx.sessionManager.getSessionId()` 与转录文件头 `id` 一致，插件优先使用该值。
- **不采用方案**：
  - 不采用「纯 MCP 唯一接入」：MCP 是被动工具通道，无法实现会话启动自动注入和回合结束自动同步。
  - 不采用「仅靠模型自主调用 bash」：缺少生命周期钩子，首尾同步容易漏执行。
  - 不在插件内部重复实现存储、Git 提交、锁管理或向量数据库；这些职责统一由 `memctl` 核心负责。

## 2. OMP 提供的接入面与实测证据

调研基于本机已安装的 OMP `18.17.0`（位于 `/Users/libo/Library/Application Support/Tern/omp/versions/18.17.0`）以及 `~/.omp/` 运行时目录进行取证。

### 2.1 宿主身份与运行环境

- **运行架构**：OMP 二进制大小约为 220 MB，使用 Bun Standalone 打包，内嵌了完整的 JavaScript 运行时与原生扩展。
- **状态与配置目录**：OMP 默认将配置、缓存、会话历史与扩展存放在 `~/.omp/agent/`（可通过环境变量 `PI_CODING_AGENT_DIR` 重定向）。主要文件包括 `config.yml`、`agent.db`、`extensions/` 等。

### 2.2 Extension 扩展机制

OMP 保持了与 Pi 高度同源的扩展运行时。本机已有的 `~/.omp/agent/extensions/orca-agent-status.ts` 代码明确记录：

```typescript
// @orca-managed-pi-extension
// Why: no package-specific type import here. Pi and OMP expose the same
// extension API, but publish their types under different package names.
```

- **扩展发现路径**：
  - 全局用户扩展：`~/.omp/agent/extensions/*.{ts,js}`（启动时自动按目录加载）。
  - 项目局部扩展：`./.omp/extensions/*.{ts,js}`。
  - 命令行显式指定：`-e, --extension <path>` 或 `--hook <path>`。
  - 插件包关联：`omp plugin link <path>` 或 `omp plugin install npm:<pkg>`（写入 `~/.omp/agent/installed_plugins.json`）。
- **生命周期事件**：
  - `session_start`：会话创建、恢复、分叉或重载时触发。
  - `before_agent_start`：模型请求发送前触发；允许通过 `systemPromptOptions.sections["<name>"]` 注入自定义系统提示词段落。
  - `agent_settled`：单次 agent 轮次完成、没有未完成重试或压缩时触发；适合触发后台增量同步。
  - `agent_end`：回合结束；在 OMP 中包含 `event.willContinue` 标记，用于区分中间状态与最终停机。
  - `session_shutdown`：会话进程退出或销毁时触发；宿主会等待 handler 执行（需自设超时，避免阻塞退出）。
- **会话上下文句柄**：
  - `ctx.sessionManager.getSessionId()`：返回当前会话的唯一 UUID。
  - `ctx.sessionManager.getSessionFile()`：返回当前会话的 JSONL 转录文件绝对路径。

### 2.3 规则与上下文加载机制（AGENTS.md）

OMP 内置了多宿主兼容的上下文探测层：

- **自动加载 `AGENTS.md`**：OMP 启动时会自工作区根目录向上逐级（project walk-up）查找 `AGENTS.md`，同时读取用户主目录中的 `~/.agents/AGENTS.md` 和 `~/.codex/AGENTS.md`。
- **扩展配置**：配置项 `contextFiles.extra` 允许增加额外的说明文件名；支持 `--no-context-files` 启动参数按需关闭。
- **事实结论**：当前仓库中的 `AGENTS.md` 与本机 `~/.agents/AGENTS.md` 中的 `memctl` 全局操作规范，在 OMP 启动时会被自动加载为上下文。

### 2.4 Skills 发现机制

检查 `omp config list --json`，OMP 的 Skill 发现默认配置为：

```json
{
  "skills.enabled": true,
  "skills.enableAgentsUser": true,
  "skills.enablePiUser": true,
  "skills.enableClaudeUser": true,
  "skills.enableCodexUser": true
}
```

- **发现范围**：OMP 原生支持从 `~/.agents/skills/`、`~/.pi/agent/skills/`、`~/.claude/skills/`、`~/.codex/skills/` 加载 Skill。
- **事实结论**：本机使用 `skills` CLI 安装在 `~/.agents/skills/memctl` 的配套 Skill，OMP 启动即直接可用，无需对 OMP 做额外安装。

### 2.5 MCP 支持机制

OMP 原生具备完整的 MCP 客户端支持：

- **配置文件定位**：
  - 项目级：`./.omp/mcp.json`、`./.omp/.mcp.json`、`./mcp.json`、`./.mcp.json`。
  - 用户全局级：`~/.omp/agent/mcp.json` 或 `~/.omp/agent/.mcp.json`。
- **外部宿主配置继承**：OMP 还会主动探测并加载 `.claude/mcp.json`、`~/.gemini/settings.json`、`~/.cursor/mcp.json` 和 Codex `config.toml` 的 `[mcp_servers.*]`。
- **配置示例**：在 `~/.omp/agent/mcp.json` 中配置以下内容即可启用原生工具支持：
  ```json
  {
    "mcpServers": {
      "memctl": {
        "command": "memctl",
        "args": ["mcp", "--agent", "omp"]
      }
    }
  }
  ```

### 2.6 会话身份与环境变量传递

- **子进程导出**：OMP 在 bash 工具和子进程中注入了 `PI_SESSION_ID`、`PI_SESSION_FILE`（沿用自 Pi 核心）以及 `OMP_PROFILE` 等环境变量。
- **回退链**：`--sid` → `AGENT_MEMORY_SID` → `DSH_HOOK_SESSION_ID` → `DSH_SESSION_ID` → `PI_SESSION_ID` → `DELTA_CURRENT_THREAD_ID` → Hook JSON。`PI_SESSION_ID` 由 `T134` 加入，真机 `getSessionId()` 与转录文件头 `id` 一致。

## 3. 方案比较与取舍

### 3.1 四种接入路线对比

| 维度 | 方案 A：原生 Extension（推荐主力） | 方案 B：Skill + AGENTS.md 静态契约 | 方案 C：全局 MCP stdio 服务 | 方案 D：`memctl wrap` 外部包装 |
| :--- | :--- | :--- | :--- | :--- |
| **接入层级** | 宿主运行时进程内扩展 | 纯提示词与 Skill 规则 | 外部进程间通信（stdio） | 进程生命周期外壳 |
| **会话启动自动注入** | **是**（`before_agent_start` 写入 prompt section；OMP 18.17.0 无 `sections`，回退替换 `systemPrompt`） | 否（仅能静态阅读规则，无当前记忆动态快照） | 否（MCP 仅提供被动工具，不主动注入记忆） | 否（仅启动前在外部调 `start`，未注入 prompt） |
| **回合结束自动同步** | **是**（`agent_settled` 后台异步同步） | 否（依赖模型在 bash 中主动调用 `sync`） | 否（MCP 工具执行后不触发 Git 同步） | 否（仅会话退出时同步） |
| **会话结束自动收尾** | **是**（`session_shutdown` 有界收尾） | 否 | 否 | **是**（宿主退出后执行 `end`） |
| **写操作防冲突保障** | 强（通过最新快照基线校验） | 强（Skill 强制 Read-Before-Write 原子检查） | 强（工具强制要求提供 `baseRevision`） | 弱（若多进程并发容易产生基线冲突） |
| **Token 与缓存友好性** | pi 极佳（section 增量追加）；OMP 一般（无 `sections`，每轮替换整个系统提示词） | 一般（若模型读取完整文件会占用 context） | 良好（按需调取工具返回） | 不影响 |
| **侵入与维护成本** | 极低（复用 `memctl-pi`，交 OMP 插件管理器链接） | 零侵入（当前系统已全自动就绪） | 极低（添加一个 JSON 配置段） | 低（需改动启动脚本或 Shell Alias） |

### 3.2 推荐的双轨接入组合

单一手段无法同时满足「隐式自动化」与「显式原子防错」的工程目标：

1. **为什么不能只用 Extension？**
   Extension 负责处理**生命周期的自动化桥接**（启动读取、提示词注入、回合增量同步、退出收尾）。但在模型需要主动写入记忆、更新决策或解决冲突时，扩展不能擅自替模型猜测写入内容；模型必须有清晰的业务规范指导。
2. **为什么不能只用 Skill + AGENTS.md？**
   仅靠提示词规范，模型可能因上下文遗忘或节省 token 而漏执行回合后的 `memctl sync`，且模型每次需要手动调用 CLI 读取记忆，浪费往返回合。
3. **为什么不能只用 MCP？**
   MCP 工具是「被动触发」的，宿主在开启会话时不会通过 MCP 自动获知最新记忆，回合结束更无法通过 MCP 触发后台 Git 推送。

因此，**「Extension 自动化生命周期注入/同步 + Skill / AGENTS.md 显式约束契约」的双轨组合是最佳实践**。

## 4. 实施与落地方案

### 4.1 用户侧接入指南

使用包含 `memctl-pi 0.6.0` 的 `memctl` 构建（源码构建或后续 Release），按以下步骤在 OMP 中接入：

#### 步骤 1：用 OMP 插件管理器链接 `memctl-pi`
OMP 能直接加载 pi 格式的扩展包，推荐用插件管理器安装，而不是在
`~/.omp/agent/extensions/` 写包装文件：

```bash
memctl pi plugin-dir                  # 输出 ~/.agent-memory/pi
omp plugin install /absolute/path/printed/by/the/command
omp plugin list                       # 确认 memctl-pi 已启用
```

`memctl-pi 0.6.0` 起在加载时识别 OMP，并以 `--agent omp` 调用 CLI。更早的版本会把命令
标成 `--agent pi`，不要使用。`0.6.0` 已发布到 npm，但 OMP 的
`omp plugin install npm:memctl-pi@0.6.0` 通道尚未实测；上面的本地目录通道是已验证路径。
包装扩展会让同一份插件被加载两次，`start`/`sync` 重复触发，因此不作为受支持路径。

#### 步骤 2：验证 Skill 与规则自动生效
- 检查 `~/.agents/skills/memctl` 是否存在。OMP 会自动索引该目录。
- 启动 `omp`，输入有关跨会话偏好或项目记忆的问题，模型能直接读取规则并在需要时执行 `memctl` CLI。

#### 步骤 3：（可选）配置全局 MCP 工具通道
若需要在受控或无终端权限场景下使用记忆工具，在 `~/.omp/agent/mcp.json` 中配置：

```json
{
  "mcpServers": {
    "memctl": {
      "command": "memctl",
      "args": ["mcp", "--agent", "omp"]
    }
  }
}
```

---

### 4.2 项目侧代码适配（`T134` 已交付）

落地时按实测调整了原规划：OMP 的 `omp plugin install` 会把 pi 格式的包目录链接到
`~/.omp/plugins/node_modules/<name>`，与 `pi install` 读取同一份 `memctl-pi` 包。因此
OMP 不需要额外的包装扩展文件，也不写 `~/.omp/agent/extensions/`；`memctl-pi` 自身在
加载时识别宿主并以 `--agent omp` 调用 CLI。这样只有一条加载路径，不会出现「插件 +
包装扩展」双份监听，也不需要把 `$STATE/pi` 的绝对路径写进宿主文件。

已交付的改动：

#### 1. 注册 OMP 适配器（`internal/adapters/registry.go`）
`omp` 项只探测、不写文件：宿主可执行文件为 `omp`，状态目录为 `~/.omp/agent`，最低验证版本
`18.17.0`；原生插件 `memctl-pi` 的最低版本 `0.6.0`，清单固定在
`~/.omp/plugins/node_modules/memctl-pi/package.json`（`omp plugin install` 的链接位置）。
适配器没有 `Artifacts`，因为插件安装由 OMP 自己的插件管理器完成。`install --status` 在
插件缺失或版本过低时报告 `needs_attention`，并打印离线安装步骤：

```text
OMP:   memctl pi plugin-dir
       omp plugin install <输出的绝对目录>
校验:  omp plugin list
```

#### 2. 扩展运行时多宿主自适应（`plugins/pi/index.js`）
`detectAgent` 按 `MEMCTL_PI_AGENT` → 进程可执行文件（`process.title`、`process.env._`、
`argv[0]`、`argv[1]`，含 `omp-*` 打包名）→ `PI_CODING_AGENT_DIR` 含 `/.omp/` → `pi`
的顺序解析宿主；`session_start` 把它写入会话状态，`start`/`sync`/`end` 全部使用该值，
不再硬编码 `--agent pi`。真机 `process.title` 为 `omp`，`argv[1]` 为
`/$bunfs/root/omp-darwin-arm64`，两条都能识别。真机没有设置 `OMP_PROFILE` 与
`PI_CODING_AGENT_DIR`，因此不以它们作为主判据；`PI_CODING_AGENT_DIR` 只作为 OMP 重定向
配置目录时的补充。

#### 3. 会话身份环境变量回退支持（`internal/hookio`）
`ResolveSID` 在 `DSH_SESSION_ID` 之后、`DELTA_CURRENT_THREAD_ID` 之前加入
`PI_SESSION_ID`，用于 OMP/pi 的 bash 工具里手工执行的 `memctl write`；插件自身仍显式传
`ctx.sessionManager.getSessionId()`，该值优先。OMP 真机确认
`getSessionId()` 与转录文件头的 `id` 一致。

#### 4. 会话蒸馏与 MCP 状态
- `internal/corpus` 把 `omp` 注册到 `parsePi`；解析器改为按记录类型查找 `session` 记录，
  因为 OMP 在它之前写一行 `title`。补扫增加 `~/.omp/agent/sessions`，目录命名和转录
  文件名与 pi 相同。
- `internal/mcp` 的注册表增加 `omp` 条目：OMP 没有 `mcp add` 子命令，提示写入
  `~/.omp/agent/mcp.json`（项目级为 `.omp/mcp.json`）。

## 5. 验证矩阵与完成条件

接入验证覆盖以下核心场景：

| 编号 | 场景 | 验证方法与预期结果 |
| :--- | :--- | :--- |
| **V-OMP-01** | 会话启动上下文注入 | 启动 `omp` 会话；检查发送给模型的初始请求包含 `memctl` prompt section，正文与 `~/memory/MEMORY.md` 投影一致。 |
| **V-OMP-02** | 会话缓存稳定性 | 在同一会话中进行连续多轮对话；未发生记忆变更时，`memctl` prompt section 保持不变，不产生多余的 transcript delta。 |
| **V-OMP-03** | 单轮回合增量同步 | 会话中产生一轮问答；`agent_settled` 触发后，在后台启动有界 `memctl sync --agent omp`，日志记录正常。 |
| **V-OMP-04** | 会话退出有界收尾 | 正常退出 `omp`（Ctrl+D 或 `/quit`）；`session_shutdown` 触发 `memctl end`，且在超时时限内平稳退出，不挂死进程。 |
| **V-OMP-05** | Skill 与规则驱动写入 | 在 OMP 对话中要求记录新事实；模型自动应用 `memctl` Skill，通过 `read` 获取 `fileRevision`，以 `--base` 原子提交 `write`，无直接手改文件行为。 |
| **V-OMP-06** | MCP 工具连通性（可选） | 配置 `~/.omp/agent/mcp.json` 后启动；OMP 成功列出 `memory_read`、`memory_append`、`memory_write`、`memory_entries`、`memory_dream_*` 工具且调用成功。 |

## 6. 实现状态与验证记录（`T134`）

- 环境：2026-11-06，darwin/arm64，OMP `18.17.0`，`memctl-pi 0.6.0`，`memctl` 源码构建（`v0.15.0-10-gd216a1e-dirty`）。
- 插件安装：`omp plugin uninstall memctl-pi` 清掉临时链接后，执行
  `memctl pi plugin-dir` → `omp plugin install ~/.agent-memory/pi`，得到
  `~/.omp/plugins/node_modules/memctl-pi` 符号链接；`omp plugin list --json` 报告
  `version 0.6.0` 与 `extensions: ["./index.js"]`。
- 适配器探测：`memctl install omp --dry-run` 输出
  `omp: configured host=omp@18.17.0 plugin@0.6.0`，无待写文件；`install --status` 的
  `mcp:` 块列出 OMP 的 `~/.omp/agent/mcp.json` 提示。
- 生命周期（`V-OMP-04`）：在干净环境用假 `memctl` 运行
  `omp -p --session-dir /tmp/... "say hi"`，插件发出
  `start --agent omp --sid <id> --json` 与
  `end --agent omp --sid <id> --transcript <真实转录路径>`；`end` 的转录文件存在。
- 会话蒸馏：对上述转录执行
  `memctl sync --agent omp --sid <id> --transcript <path>`（临时仓库与状态目录），
  `$STATE/repos/<repoId>/cache/corpus/` 生成 `agent: "omp"` 的 `SessionDigest`，
  `sourceTag` 为 `agent://<device>/omp/<id>`。
- 端到端（真实二进制）：把 `MEMCTL_PI_BIN` 指向源码构建的 `memctl`，用临时
  `AGENT_MEMORY_DIR`/`AGENT_MEMORY_STATE` 与 `--session-dir` 运行一次
  `omp -p "Reply with exactly: ok"`。`end` 收尾后缓存中出现
  `agent: "omp"`、1 个回合的摘要，证明「OMP 插件 → `--agent omp` → `end` →
  `--transcript` → 蒸馏」整条链路可用。
- npm 发布：`memctl-pi@0.6.0` 于 2026-11-06 发布到 npm，`latest` 为 `0.6.0`，shasum
  `14e7ae9189093bca109a768a40669ba5ed8c4508`；OMP 的 npm 安装通道未实测。
- MCP（`V-OMP-06`）：本机在 `~/.omp/agent/mcp.json` 配置 stdio 条目 `memctl`
  （`command` 为 `~/.agent-memory/bin/memctl`，`args` 为 `["mcp","--agent","omp"]`）；新启动的 OMP
  会话列出 `memory_read`、`memory_entries`、`memory_write`、`memory_dream_prepare`、
  `memory_dream_propose` 五个工具，`memory_read` 调用成功返回真实 `MEMORY.md` 首行；写入工具未调用。
- 会话身份：`PI_SESSION_ID=... memctl tag --agent omp` 输出
  `agent://<device>/omp/<PI_SESSION_ID>`。
- `V-OMP-01`（部分）：扩展在 OMP 中加载并调用 `start`，但 OMP `18.17.0` 不提供
  `systemPromptOptions.sections`，插件记录一次回退日志后改用替换 `systemPrompt`；
  本次没有检查模型请求的最终 system prompt 文本。
- 未验证：`V-OMP-02`（多轮缓存稳定）、`V-OMP-03`（`agent_settled` 每轮同步；`omp -p`
  立即退出，只观察到 `end`）、`V-OMP-05`（Skill 驱动的受管写入）；OMP 的
  `session_shutdown` 在交互式 `/quit` 下的行为未单独复验。
