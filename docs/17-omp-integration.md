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

> 状态：调研与方案制定完成（2026-11-06，基于本机 OMP `18.17.0` / darwin arm64 实测取证）。本文件确立 `memctl` 在 OMP（Oh My Pi）中的接入模型与最佳实践；相关实现计划与验证矩阵以 [05. 验收与交付](./05-validation-and-delivery.md) 和 [开发状态](./STATUS.md) 为准。

## 1. 结论

- OMP（全称 Oh My Pi，npm 包名 `@oh-my-pi/pi-coding-agent`，当前实测版本 `18.17.0`）是基于 Pi（`pi-coding-agent`）深度分叉与演进的 Agent 宿主，底层使用 Bun 打包为单文件独立二进制，主配置目录为 `~/.omp/agent`。
- **最佳使用方式**：推荐**「Extension 自动化生命周期注入/同步 + Skill / AGENTS.md 显式约束契约」的双轨组合**，必要时辅助全局 MCP 工具通道。
- **生命周期自动化轨（主干）**：OMP 原生完整兼容 Pi 的 Extension 扩展机制。通过在 `~/.omp/agent/extensions/` 部署轻量桥接扩展，可接管 `session_start`（启动预读与同步）、`before_agent_start`（将记忆正文结构化注入 `systemPromptOptions.sections["memctl"]`）、`agent_settled`（单轮结束触发后台有界同步）和 `session_shutdown`（会话结束有界收尾）。记忆正文作为 section 注入，不污染 user 历史对话，完全缓存友好。
- **显式契约轨（辅翼）**：OMP 原生开启了 `skills.enableAgentsUser: true`，且启动时会自动递归查找工作区及用户主目录的 `AGENTS.md`。本机通过 `skills` CLI 安装在 `~/.agents/skills/memctl` 的配套 Skill 和全局规则完全能够被 OMP 原生感知与执行，保障模型在执行写操作时严格遵循 Read-Before-Write 原子事务契约。
- **补充通道（MCP）**：OMP 原生支持全局 `~/.omp/agent/mcp.json`，且具备对 Claude Code、Gemini、Cursor、Codex 等各宿主 MCP 配置的兼容加载能力。如果需要提供原生工具调用（`memory_read`、`memory_write` 等），可直接配置 stdio MCP server。
- **会话身份传递**：OMP 在子进程（bash 工具）环境中注入 `PI_SESSION_ID` 与 `PI_SESSION_FILE`。建议后续将 `PI_SESSION_ID` 纳入 `memctl` CLI 的环境变量回退链，使终端中的 `memctl write` 自动关联会话。
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
- **memctl 回退缺口**：`memctl` 现有的会话身份回退链为 `--sid` → `AGENT_MEMORY_SID` → `DSH_HOOK_SESSION_ID` → `DSH_SESSION_ID` → `DELTA_CURRENT_THREAD_ID` → Hook JSON。尚未将 `PI_SESSION_ID` 纳入自动环境变量回退。

## 3. 方案比较与取舍

### 3.1 四种接入路线对比

| 维度 | 方案 A：原生 Extension（推荐主力） | 方案 B：Skill + AGENTS.md 静态契约 | 方案 C：全局 MCP stdio 服务 | 方案 D：`memctl wrap` 外部包装 |
| :--- | :--- | :--- | :--- | :--- |
| **接入层级** | 宿主运行时进程内扩展 | 纯提示词与 Skill 规则 | 外部进程间通信（stdio） | 进程生命周期外壳 |
| **会话启动自动注入** | **是**（`before_agent_start` 写入 prompt section） | 否（仅能静态阅读规则，无当前记忆动态快照） | 否（MCP 仅提供被动工具，不主动注入记忆） | 否（仅启动前在外部调 `start`，未注入 prompt） |
| **回合结束自动同步** | **是**（`agent_settled` 后台异步同步） | 否（依赖模型在 bash 中主动调用 `sync`） | 否（MCP 工具执行后不触发 Git 同步） | 否（仅会话退出时同步） |
| **会话结束自动收尾** | **是**（`session_shutdown` 有界收尾） | 否 | 否 | **是**（宿主退出后执行 `end`） |
| **写操作防冲突保障** | 强（通过最新快照基线校验） | 强（Skill 强制 Read-Before-Write 原子检查） | 强（工具强制要求提供 `baseRevision`） | 弱（若多进程并发容易产生基线冲突） |
| **Token 与缓存友好性** | 极佳（section 增量追加，不污染上下文） | 一般（若模型读取完整文件会占用 context） | 良好（按需调取工具返回） | 不影响 |
| **侵入与维护成本** | 极低（复用 `memctl-pi` 逻辑，单个轻量脚本） | 零侵入（当前系统已全自动就绪） | 极低（添加一个 JSON 配置段） | 低（需改动启动脚本或 Shell Alias） |

### 3.2 推荐的双轨接入组合

单一手段无法同时满足「隐式自动化」与「显式原子防错」的工程目标：

1. **为什么不能只用 Extension？**
   Extension 负责处理**生命周期的自动化桥接**（启动读取、section 注入、回合增量同步、退出收尾）。但在模型需要主动写入记忆、更新决策或解决冲突时，扩展不能擅自替模型猜测写入内容；模型必须有清晰的业务规范指导。
2. **为什么不能只用 Skill + AGENTS.md？**
   仅靠提示词规范，模型可能因上下文遗忘或节省 token 而漏执行回合后的 `memctl sync`，且模型每次需要手动调用 CLI 读取记忆，浪费往返回合。
3. **为什么不能只用 MCP？**
   MCP 工具是「被动触发」的，宿主在开启会话时不会通过 MCP 自动获知最新记忆，回合结束更无法通过 MCP 触发后台 Git 推送。

因此，**「Extension 自动化生命周期注入/同步 + Skill / AGENTS.md 显式约束契约」的双轨组合是最佳实践**。

## 4. 实施与落地方案

### 4.1 用户侧免编译即刻接入指南（现阶段）

无需重新编译 `memctl`，按以下步骤即可在 OMP 中获得完备的记忆管理体验：

#### 步骤 1：部署 OMP 生命周期桥接扩展
在 `~/.omp/agent/extensions/` 中创建 `memctl.js`，桥接 OMP 生命周期至 `memctl`：

```javascript
// ~/.omp/agent/extensions/memctl.js
import memctlPi from '/Users/libo/.agent-memory/pi/index.js'

export default function memctlOmp(omp) {
  // 注入环境变量，确保 memctl 记录正确的 agent 来源
  process.env.AGENT_MEMORY_AGENT = 'omp'
  return memctlPi(omp)
}
```

*说明：该扩展直接复用已验证的 `memctl-pi` 运行时逻辑。由于 OMP 与 Pi 的 Extension API 相同，启动注入、prompt section 挂载、回合后后台同步与退出收尾均自动生效。*

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

### 4.2 项目侧官方代码适配改动规划（后续交付）

为将 OMP 正式纳入 `memctl` 的受支持宿主矩阵，规划以下代码改造：

#### 1. 注册 OMP 适配器（`internal/adapters/registry.go`）
在适配器注册表中增加 `omp` 项，支持 `memctl install --status` 探测与 `memctl install omp` 安装提示：

```go
{
    Agent:        "omp",
    HostCommands: []string{"omp"},
    HostPaths:    []string{"$HOME/.omp/agent"},
    NativePlugin: &PluginSpec{
        Name:       "memctl-pi",
        MinVersion: "0.5.0",
        Manifests: []string{
            "$HOME/.omp/agent/npm/node_modules/memctl-pi/package.json",
        },
        DirTemplate:  "$STATE/pi/package.json",
        SettingsFile: "$HOME/.omp/agent/config.yml",
    },
    Artifacts: []ArtifactSpec{
        {
            PathTemplate: "$HOME/.omp/agent/extensions/memctl.js",
            Kind:         KindCopy,
            Capability:   CapabilityPlugin,
            Asset:        "omp/memctl.js",
        },
    },
}
```

#### 2. 扩展运行时多宿主自适应（`plugins/pi/index.js`）
调整 `plugins/pi/index.js` 中的 `syncArgs`，使其能够动态感知运行环境（通过 `process.env.OMP_PROFILE` 或运行时标记自动切换 `--agent omp` 或 `--agent pi`），避免硬编码 `--agent pi`。

#### 3. 会话身份环境变量回退支持（`internal/source/`）
在 `memctl` CLI 解析会话 ID 的逻辑中，将 `PI_SESSION_ID` 补充到回退列表中，使得在 OMP 内置终端中执行 `memctl write` 时，无需显式传 `--sid` 即可自动对齐当前会话。

## 5. 验证矩阵与完成条件

接入验证覆盖以下核心场景：

| 编号 | 场景 | 验证方法与预期结果 |
| :--- | :--- | :--- |
| **V-OMP-01** | 会话启动上下文注入 | 启动 `omp` 会话；检查发送给模型的初始请求包含 `memctl` prompt section，正文与 `~/memory/MEMORY.md` 投影一致。 |
| **V-OMP-02** | 会话缓存稳定性 | 在同一会话中进行连续多轮对话；未发生记忆变更时，`memctl` prompt section 保持不变，不产生多余的 transcript delta。 |
| **V-OMP-03** | 单轮回合增量同步 | 会话中产生一轮问答；`agent_settled` 触发后，在后台启动有界 `memctl sync --agent omp`，日志记录正常。 |
| **V-OMP-04** | 会话退出有界收尾 | 正常退出 `omp`（Ctrl+D 或 `/quit`）；`session_shutdown` 触发 `memctl end`，且在超时时限内平稳退出，不挂死进程。 |
| **V-OMP-05** | Skill 与规则驱动写入 | 在 OMP 对话中要求记录新事实；模型自动应用 `memctl` Skill，通过 `read` 获取 `fileRevision`，以 `--base` 原子提交 `write`，无直接手改文件行为。 |
| **V-OMP-06** | MCP 工具连通性（可选） | 配置 `~/.omp/agent/mcp.json` 后启动；OMP 成功列出 `memory_read`、`memory_write`、`memory_entries`、`memory_dream_*` 工具且调用成功。 |
