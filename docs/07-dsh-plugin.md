# 07. dsh 专用插件：分发、安装与实现方案

> 状态：已实现（`T048`，2026-10-17）。插件版本自 2026-10-20 起为 `0.5.2`，与 `memctl-pi` 对齐；`0.5.1` 是此前长期使用的版本，`v0.5.1` 起各次 Release 曾共用同一 tarball。`D1` 取 A：npm 主通道 + 内嵌 tarball 备用；两条通道均可用，npm 包名界面安装与 `desktop` profile `0.5.1` 已复验，`0.5.2` 的界面安装尚未复验；绝对目录与独立 GitHub 仓库、Discover 索引尚未验证或发布。实现与验证边界见 [§13](#13-实现状态)。

## 1. 结论

- 不再依赖 `dsh-hooks-plugin` 作为 dsh 的唯一接入方式，改为自建一个只服务 memctl 的 DSH Host-only bundle：`memctl-dsh`。
- 插件只负责三件事：会话开始时读取 memctl 上下文、每轮结束时触发同步、会话销毁时触发收尾；记忆仓库、同步、锁、冲突处理仍由 memctl 二进制负责。
- 分发以 DSH「插件 → 添加插件」对话框实际接受的输入为准：npm 包名、GitHub 仓库地址、本地绝对目录路径，以及当前解析器还接受的绝对 tarball 路径。D1 已确认取 A：npm 包名为主通道，memctl 内嵌 tarball 释放到绝对路径为备用通道；两者都在同一个对话框安装。这样不发布 npm 也能在界面安装。
- `memctl install dsh` 不自动安装插件，也不写入 DSH profile。DSH 的 Plugin Manager 拥有安装、批准、升级和卸载；memctl 只负责探测、提示和验证。
- 第一版不做客户端 UI，只做 Host half。状态面板、每轮检索注入等能力留到后续版本。
- 运行时使用当前 DSH 0.2.1-rc.1 实际存在的事件：`agent/created`、`session/event` 中的 `turn/end`、`agent/disposed`；不使用已经不存在于当前安装的 `agent/session-start`。

## 2. 范围

本方案覆盖：

- `memctl-dsh` 的包结构、生命周期接线、上下文注入、同步触发和错误隔离。
- npm 包、内嵌或随 Release 发布的 tarball、可选的独立 GitHub 仓库，以及 Discover 页面使用的市场索引。
- `internal/adapters` 对 dsh 原生插件与旧 hooks 插件的探测、状态和安装提示。
- 单元、包安装、真实宿主三层验收。

本方案不覆盖：

- DSH 客户端 UI 面板、插件市场客户端实现。
- MCP server、新的记忆检索服务或记忆仓库格式变更。
- 由 memctl 直接修改 DSH profile、`package.json`、`cordis.patch.yml` 或执行 `pnpm`。
- dsh 之外的其他宿主适配器迁移。
- 自动升级插件；升级仍由 DSH Plugin Manager 或用户显式命令完成。

## 3. 为什么需要自建插件

### 3.1 自建插件之前的链路事实

下表是 `T047` 调研时旧 `dsh-hooks-plugin` 链路的事实，不代表 `memctl-dsh` 的当前状态；当前实现与验证见 [§13](#13-实现状态)。

| 事实 | 证据 | 影响 |
| --- | --- | --- |
| 当前 dsh 安装没有 `agent/session-start` 事件 | DSH 0.2.1-rc.1 的 `app.asar` 中该字符串为 0 次匹配；`dsh-agent` 的公开事件清单也没有它；`agent/created` 才携带 `SessionStartSource` | `dsh-hooks-plugin` 订阅的 SessionStart 不会触发，`memctl start --json` 不执行 |
| 全局 hooks 文件路径与插件实际读取路径不一致 | 插件从 `DSH_HOME`/`USERPROFILE` 推导路径；日志实际落在 `~/.dsh/profiles/desktop/.dsh/logs/`；memctl 写的是 `~/.dsh/hooks.json` | 即使项目层 hooks 被读取，全局层也不会按预期生效 |
| 版本错配 | `dsh-hooks-plugin@0.2.15` 的 peer 要求 dsh-shell `^0.1.0-rc.6`，当前 DSH 为 `0.2.1-rc.1`；[开发状态](./STATUS.md) 已记录精确版本豁免 | 事件名与服务语义会继续漂移 |
| 注入形态是 user message | 插件用 `agent.inject(createTextUserMessage(...))` | 不是 DSH 为动态上下文提供的快照通道，也没有 `form`/`source` 语义 |
| 未在真实会话验证注入 | [开发状态](./STATUS.md) 记录的是「以样例载荷验证」 | 当前 dsh 接入最多是 `configured`，不是 `verified` |

这些事实说明，继续修第三方 hooks 配置只能解决部分问题；要可靠拿到会话开始事件和正确的上下文通道，需要插件直接使用当前 DSH API。

### 3.2 当前 DSH 实际可用的接入点

| 接入点 | 模式 | 用途 |
| --- | --- | --- |
| `agent/created` | serial，创建完成前等待监听器 | 会话开始；读取 memctl 上下文并注册到该 agent |
| `session/event` + `event.type === 'turn/end'` | emit，提交后通知 | 每轮结束触发同步 |
| `agent/disposed` | emit，离开注册表后通知 | 会话销毁触发收尾 |
| `agent.ctx.systemPrompt.context()` | 作用域注册 | 把动态上下文作为耐久 user-role 快照注入 |
| `agent/turn-stopping` | serial，轮次关闭前等待 | 本方案不使用：会阻塞轮次关闭，且语义是关闭前而不是提交后 |

`turn/end` 是会话事件类型，不是同名 Cordis 事件；必须通过 `session/event` 过滤。当前 DSH 文档明确区分了这两类事件。[事件系统](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/user/develop/framework/events.zh.md)

## 4. 分发与安装

### 4.1 约束

- DSH 的 profile 位于 `$DSH_HOME/profiles/<name>`，由有序 bundle 列表和 profile 层 patch 组成；bundle 是声明 `dsh.bundle.patch` 的 npm 包。[打包与安装插件](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/user/develop/basic/publish.zh.md)
- DSH 0.2.1-rc.1 的「插件 → 添加插件」对话框把输入作为安装 spec 交给 Plugin Manager。当前 Host 解析器接受以下形式；这决定了「能在界面安装」的具体入口。[install-spec.ts](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/packages/boot/plugin-manager/src/install-spec.ts)

| 输入 | 示例 | 约束 |
| --- | --- | --- |
| npm 包名 | `memctl-dsh`、`memctl-dsh@0.5.2` | 可带版本或范围；走 registry，对话框可选镜像源 |
| GitHub 仓库根地址 | `https://github.com/<owner>/<repo>`、`github:<owner>/<repo>` | Host 只接受 host/owner/repo 两段路径；仓库根必须有 `package.json` 和 bundle patch；不支持仓库子目录 |
| 本地目录 | `/Users/me/.agent-memory/dsh/0.5.2` | 必须是绝对路径，`~` 和相对路径被拒绝；以 link 依赖安装，目录必须长期存在 |
| 本地或远程 tarball | `/Users/me/.agent-memory/dsh/memctl-dsh-0.5.2.tgz`、`https://example.com/memctl-dsh-0.5.2.tgz` | 必须是绝对路径或 http(s) URL；对话框文案没有列出 tarball，但当前解析器和 Host 接受；当前安装可用，重装或升级需重新提供 tarball |

界面输入的是安装 spec，不是市场索引 URL。Discover 页面才读取市场索引；两者不能混用。

- `dsh plugin --profile <name> <args>` 把参数转发给 profile 目录内的 pnpm。npm 包、tarball、本地目录和 git 源都可以安装；git 源默认拉源码，不自动运行构建，除非作者提供 `prepare` 且用户授权。
- DSH 在安装和启动时检查 `engines.dsh`，没有声明时回退检查 `@deepseek-ai/dsh*` peer 范围；不兼容的组合会被跳过或拒绝。版本豁免记录在 profile 的 `compatibility.json`，不会随升级继承。[app-boot](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/packages/boot/app-boot/README.zh.md)
- Plugin Manager 的安装、升级、卸载需要危险权限或逐次批准；已安装的 Host 插件在进程内执行，位于工作区沙箱之外。[Plugin Manager](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/packages/boot/plugin-manager/README.zh.md)
- Discover 页面使用的插件市场是用户配置的 HTTPS 索引，不是默认商店，也不是「添加插件」对话框的输入；索引格式 1 要求每个版本声明 `engines.dsh`，npm 源固定到精确版本，git 源固定到 commit。[市场索引格式](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/.agents/notes/implemented/feature/2026-10-02-plugin-market-index-format.md)
- memctl 的安装器当前只写配置文件，不自动安装第三方插件，也不依赖 Node.js；这条边界见 [流程与恢复](./03-workflows-and-dataflows.md#7-agent-配置安装)。

### 4.2 方案比较

| 方案 | 界面输入 | 安装体验 | 更新 | 离线 | 额外设施 | 判断 |
| --- | --- | --- | --- | --- | --- | --- |
| npm 包名 | `memctl-dsh` 或 `memctl-dsh@x` | 最好：输入包名，选择镜像，点击安装 | 第三方插件通常不显示自动更新；需显式版本或卸载重装 | 首次需网络 | npm 发布 | 推荐主通道 |
| memctl 内嵌 tarball | memctl 输出的绝对 `.tgz` 路径 | 好：复制路径到同一对话框 | 换 memctl 版本后重新安装新 tarball | 最好 | Go `go:embed` 源文件、运行时生成 tarball | 推荐备用通道；npm 不可用时升为主通道 |
| 随 Release 发布的 tarball | 下载后的绝对 `.tgz` 路径 | 中：先下载，再粘贴路径 | 手动下载新 tarball | 好 | GitHub Release 已有 | 离线备选；与内嵌 tarball 同一份源码 |
| 独立 GitHub 仓库 | 仓库根 URL | 中：输入 URL；不经过 registry | pnpm 记录 git 源，更新需重装 | 需 Git 网络 | 独立仓库、预构建入口 | 可选；要维护第二个仓库 |
| 本地目录 link | 绝对目录路径 | `memctl dsh plugin-dir` 或开发目录 | 改代码后重载 | 好 | 无 | tarball 被拒绝时的回退；目录不能删除 |
| 直接改 profile 文件 | 无 | 看似一键 | 不可控 | 好 | 无 | 不采用 |
| Discover 市场索引 | 不是本对话框输入 | 取决于 Discover 页面 | 取决于索引 | 需索引托管 | 静态索引、图标、版本元数据 | 第二阶段；不是本对话框的前置条件 |

### 4.3 推荐

推荐组合：

1. **主通道：npm 包名 `memctl-dsh`**。用户在「插件 → 添加插件」中输入包名，点击安装；必要时在对话框选择中国大陆镜像源。npm 包不声明 install/postinstall 脚本，不引入运行时依赖，只包含已构建的 ESM 文件、patch、locale 和图标。官方发布文档也把 npm 预构建包列为不需要构建授权的分发方式。
2. **备用通道：memctl 内嵌 tarball**。把插件源文件嵌入 memctl 二进制，由 `memctl dsh plugin-path` 在运行时生成 `memctl-dsh-<version>.tgz` 到状态目录；用户运行该命令得到稳定的绝对路径，把它粘贴到同一个「添加插件」对话框。它不发布 npm、不需要 Git 仓库、不需要市场索引；重装或升级时需重新提供 tarball。这是「下载 memctl 一个文件即可在 DSH 界面安装插件」的路径。
3. **可选通道：随 GitHub Release 发布的 tarball**。与内嵌 tarball 同一份源文件，适合不想让 memctl 二进制变大、或希望手工下载审计的用户。
4. **可选通道：独立 GitHub 仓库根**。如果希望用户粘贴仓库地址安装，需要一个根目录就是 bundle 的独立仓库；不能直接使用 memctl 的 Go 仓库根，也不能依赖仓库子目录（当前 Host 只接受 owner/repo 两段路径）。该仓库必须提交预构建入口，不能依赖 `prepare` 构建。
5. **不作为第一版主通道：Discover 市场索引**。市场索引是 Discover 页面入口，不是「添加插件」对话框的输入；它适合后续做发现和版本提示，不应阻塞本对话框的 npm 或 tarball 安装。

`D1` 已确认取 A：npm 主通道 + 内嵌 tarball 备用。若 npm 发布受阻，内嵌 tarball 可直接升为主通道，仍然满足「在插件安装界面安装」；`D2` 独立 GitHub 仓库和 `D3` Discover 索引仍为可选，不阻塞 A。

### 4.4 安装流程

前置条件：

- DSH 0.2.1-rc.1 或声明的兼容版本。
- memctl 已安装，默认路径 `~/.agent-memory/bin/memctl`，或通过 `MEMCTL_DSH_BIN` 指定。
- 用户已在 DSH 中打开要使用的 profile；桌面端常见为 `desktop`，CLI 常见为 `web`。

在「插件 → 添加插件」对话框安装：

1. **npm 包名（推荐）**：输入 `memctl-dsh`，需要固定版本时输入 `memctl-dsh@<version>`；网络受限时在「安装源」选择镜像。点击安装并确认信任提示。
2. **内嵌 tarball（离线）**：先运行只读命令 `memctl dsh plugin-path` 取得绝对路径，例如 `/Users/me/.agent-memory/dsh/memctl-dsh-0.5.2.tgz`；把该路径粘贴到同一个输入框，点击安装。安装完成后当前会话可用；重装或升级时需重新提供 tarball，建议保留到确认稳定。
3. **随 Release 下载的 tarball**：下载 `memctl-dsh-<version>.tgz`，粘贴其绝对路径；效果与内嵌 tarball 相同。
4. **独立 GitHub 仓库**：粘贴仓库根 URL，例如 `https://github.com/<owner>/memctl-dsh`；仓库根必须有 `package.json`、bundle patch 和预构建入口，不能依赖 `prepare` 构建。
5. **本地目录（开发或 tarball 被拒绝时的回退）**：粘贴绝对目录路径；DSH 会以 link 依赖安装，目录不能移动或删除。

如果某个 DSH 小版本拒绝 tarball 输入，回退为本地目录路径：`memctl dsh plugin-dir` 把同一 bundle 写到 `$STATE/dsh/<version>/` 并输出绝对目录，用户粘贴该目录；DSH 以 link 安装，目录必须长期保留。这仍然不需要 npm，也不要求 GitHub 仓库。tarball 输入在 DSH 0.2.1-rc.1 的 Host 解析器中可用，但对话框文案没有把它列为正式选项。2026-10-17 已在 DSH 0.2.1-rc.2 的隔离 profile 中通过「插件 → 添加插件」用绝对 tarball 完成安装并启用，见 [T052 验证记录](./validation/t052-dsh-real-session.md)；npm 包名已通过同一界面完成安装并升级本机 `desktop` profile，绝对目录仍未做界面实测。

#### 4.4.1 安装失败与恢复

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 对话框提示插件已安装 | 同名包已在 profile 中 | 升级时先卸载旧版本再装新版本；不要直接覆盖未验证的构建 |
| 对话框提示不兼容 | `engines.dsh` 或 peer 范围不匹配 | 升级 DSH 或安装兼容版本；版本豁免只用于已确认风险的组合，不修改 `engines.dsh` 规避 |
| tarball 路径被拒绝 | 路径是相对路径、含 `~`，或该 DSH 小版本不接受 tarball | 改用绝对路径；仍失败则用 npm 包名或解包后的绝对目录 |
| 安装后插件不加载 | profile 未选中 bundle、DSH 需重启、或包缺少 `dsh.bundle.patch` | 在 DSH 插件列表确认已启用；检查 `package.json` 的 `dsh.bundle.patch`；重启 DSH |
| 会话没有记忆上下文 | memctl 不存在、仓库未初始化、`start` 超时 | 先运行 `memctl version` 和 `memctl install --status`；看 DSH 日志中的 `memctl-dsh:` 错误；插件不阻塞会话 |
| 每轮没有同步 | 插件未收到 `turn/end`、同步失败或锁忙 | 看插件日志和 `memctl` 同步日志；`sync` 失败只影响该轮，不阻塞下一轮 |
| 卸载后仍有旧 hooks 行为 | 旧 `dsh-hooks-plugin` + `hooks.json` 仍在 | `memctl install --status` 检查 legacy 配置，手工移除；两套不要同时启用 |

命令行等价入口：

```sh
dsh plugin --profile <profile> add memctl-dsh@<version>
dsh plugin --profile <profile> add /absolute/path/to/memctl-dsh-<version>.tgz
```

安装后验证：

```sh
dsh --profile <profile> --dump-config
```

输出中应出现 `memctl-dsh` 的插件行；新会话开始后，插件日志应出现会话 id 和 `memctl start` 结果。DSH 插件列表应显示 `memctl-dsh`，并显示 locale 中的标题、描述和图标。

升级：

- npm：重新输入 `memctl-dsh@<new-version>`。对话框提示官方按需插件才提供版本更新，其他插件通常需要先卸载再安装新版本；按提示操作。
- tarball：取得新 tarball 的绝对路径后重新安装；若 DSH 拒绝覆盖，先卸载再安装。
- 插件升级不改变记忆仓库；memctl 二进制可以独立升级。

卸载：

```sh
dsh plugin --profile <profile> remove memctl-dsh
```

卸载只移除 DSH 插件；记忆仓库、状态目录和 memctl 二进制保留。若之前装过旧 hooks 方案，`memctl install --status` 应同时报告旧 hooks 配置，用户手工决定是否清理。

### 4.5 版本与信任

- 插件包版本与 memctl 发布版本保持同一主次版本，避免「插件 0.3 调 memctl 0.1 CLI」的隐藏矩阵；补丁版本可以独立。
- `engines.dsh` 使用精确下界和上界，例如 `>=0.2.1-rc.1 <0.3.0`；DSH 升级到范围外时由 DSH 拒绝加载，而不是静默不工作。
- 插件不声明 `@deepseek-ai/dsh-*` peer 依赖：它只使用宿主传入的 `ctx` 和 Node 内置模块，不需要解析 DSH 包。这样避免 `dsh-hooks-plugin` 式的 peer 版本错配。
- 不声明 `dependencies`、`install`、`postinstall`、`prepare`。安装不执行第三方构建脚本，也不需要 pnpm 构建授权。
- npm 发布使用 provenance（可用时）和精确版本；Release tarball 写入 `SHA256SUMS`；运行时 tarball 与 Release tarball 使用同一份源文件，文件清单和版本一致，不要求字节一致；如启用 Discover 市场索引，只列已发布版本。
- 安装 Host 插件等于授权其在 DSH 进程内以用户权限执行。插件只执行固定路径的 memctl 二进制和固定参数，不读取项目 hooks 文件，不执行任意命令；但 memctl 本身会写 `~/memory`，这一点必须在安装文档中写明。

## 5. 插件设计

### 5.1 包结构

```text
plugins/dsh/
  package.json          # dsh.bundle.patch、engines、files、exports
  cordis.patch.yml      # insert: memctl-dsh
  index.js              # Host half，纯 ESM，无第三方依赖
  locale/en.json        # meta.title / meta.description
  locale/zh.json
  icon.svg
  test/                 # Node 单元测试，不进入 npm 包
```

`package.json` 的 `files` 只包含 `index.js`、`cordis.patch.yml`、`locale/*.json` 和 `icon.svg`。不提交 `lib/` 之外的多余产物，不允许 `src/` 与预构建入口混用。

### 5.2 生命周期映射

| DSH 事件 | 监听位置 | 动作 |
| --- | --- | --- |
| `agent/created` | 根 `ctx` | 过滤子代理；读取 memctl 上下文；在该 agent 的作用域注册动态上下文 |
| `session/event`，`event.type === 'turn/end'` | `agent.ctx` | 每轮提交后后台触发 `memctl sync` |
| `agent/disposed` | 根 `ctx` | 尽力触发 `memctl end`；清理内存状态和子进程 |

不使用的接入点：

- `agent/session-start`：当前 DSH 不存在。
- `agent/turn-stopping`：serial 且会等待，适合需要阻止轮次关闭的插件；memctl 只需要提交后的同步。
- `agent/pre-step`：可以按用户输入做相关性检索，但第一版不做；它需要每步调用额外逻辑，成本和复杂度更高。
- `tools/pre-execute`、`UserPromptSubmit`：第一版不介入工具和用户消息，避免与宿主或其他插件竞争。

### 5.3 会话开始与上下文

`agent/created` 监听器执行：

1. 读取 `agent.id` 作为会话 id；读取 `agent.session.header.cwd` 作为工作目录（memctl 不依赖它绑定仓库，但可作为子进程 cwd）。
2. 若 `agent.session.header.parentSession` 非空，判定为子代理，跳过后面的读取、注入和同步。子代理共享父会话的 memory，不应重复启动和重复同步。
3. 以固定参数运行：

```text
memctl start --agent dsh --sid <agent.id> --json
```

不使用 `--stdin`，不经过 shell；参数数组直接传给 `spawn`。`start` 的 stdout 只允许一个 JSON 对象，解析 `hookSpecificOutput.additionalContext`。

4. 成功后在 `agent.ctx` 注册动态上下文：

```js
agent.ctx.systemPrompt.context({
  name: 'memctl:memory',
  order: 9000,
  text: () => contextCache.get(agent.id) ?? '',
  interpolate: false,
  required: true,
})
```

使用 `systemPrompt.context` 而不是 `agent.inject` 的原因：

- 它是 DSH 为动态上下文定义的持久 user-role 快照，只在完整快照变化或压缩移除时重新记录，避免每轮重复 token。[system-prompt](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/subsystems/system-prompt.zh.md)
- 它带作用域，注册在 `agent.ctx` 上只影响该会话。
- 它不伪造 inbox 消息，不依赖注入时序，也不会和用户输入竞争。

不把整段记忆放进 `systemPrompt.section()`：section 是系统提示文本，适合稳定规则；记忆是动态数据，放 context 更符合 DSH 的语义，也避免每请求重复大段文本。`required: true` 让记忆上下文在用户关闭可选运行时上下文时仍然保留。

5. `text` 提供器不能抛异常；缓存值必须是字符串。`interpolate: false` 防止记忆正文里的 `{{...}}` 被当成提示词变量。
6. 对 `agent/created` 的 `source` 处理：

| source | 行为 |
| --- | --- |
| `startup`、`resume` | 正常执行 start |
| `clear` | 重新执行 start，刷新上下文 |
| `compact` | 不重复 start；已注册的 context 会在下一次组装时按当前快照恢复 |

`agent/created` 是 serial 事件，DSH 在创建完成前等待监听器，并在此期间暂存输入。插件可以在监听器内等待 `start`，但要设置超时；如果 `start` 失败，记录错误并继续创建会话，不能让 DSH 启动失败。

### 5.4 同步与收尾

每轮同步：

- 在 `agent.ctx` 监听 `session/event`，只处理 `event.type === 'turn/end'`。
- `turn/end` 是提交后的耐久事件；此时触发同步不会读到未提交的半成品。
- 使用后台 `spawn`，不等待结果，不阻塞轮次关闭。参数固定为：

```text
memctl sync --agent dsh --sid <agent.id>
```

- 同一会话同时只允许一个同步进程；如果有新的 `turn/end` 到来，只记录一个 pending 标记，当前同步结束后再补一次，避免队列无限增长。
- 同步失败只写日志，不改变会话状态。memctl 的自动路径本身以退出 0 处理断网、锁忙等失败，插件不应额外制造宿主错误。

会话销毁与插件卸载：

- `agent/disposed` 上触发 `memctl end --agent dsh --sid <agent.id>`，使用 detached + unref，尽力在 DSH 退出前完成。
- 插件卸载时，对仍处于 live 状态且尚未收尾的会话再触发一次 detached `end`；已由 `agent/disposed` 清理的会话不重复触发。`end` 是幂等的后台同步，不写入会话关闭状态，因此插件重载时多一次同步不会改变会话语义。
- 子代理没有注册监听器，不会触发。
- 插件卸载时清理所有子进程和定时器；不要等待 `agent.whenIdle()` 或自己的 disposal，避免创建/销毁死锁。

### 5.5 进程与超时

| 动作 | 调用 | 超时 | 输出 |
| --- | --- | --- | --- |
| 会话开始 | `start --json` | 20 s | 有界读取 stdout，上限 1 MiB |
| 每轮同步 | `sync` | 15 s | 忽略 stdout/stderr，仅记录退出状态 |
| 会话收尾 | `end` | 15 s | detached + unref，尽力执行 |

- 使用 `spawn(bin, args, { shell: false, windowsHide: true })`，不拼接命令字符串。
- POSIX 下超时先发 `SIGTERM`，宽限后发 `SIGKILL`；需要时以进程组方式终止。Windows 下先 `kill`，必要时使用 `taskkill /T /F`，并记录无法清理的残留。
- 所有子进程登记到插件状态；插件卸载或 agent 销毁时取消未完成的 start，并允许 sync/end 尽力完成。
- stdout 超过上限时终止进程并记录错误，不截断后当成完整 JSON 解析。

### 5.6 身份与配置

- 会话 id 使用 `agent.id`，通过 `--sid` 显式传入。不依赖 `DSH_HOOK_SESSION_ID`，也不读取宿主 hooks JSON。
- agent 名固定为 `dsh`；设备名、仓库路径、Git 凭据由 memctl 现有配置和环境决定。
- memctl 路径按以下顺序解析：
  1. `MEMCTL_DSH_BIN` 环境变量。
  2. `~/.agent-memory/bin/memctl`。
  3. `/opt/homebrew/bin/memctl`、`/usr/local/bin/memctl`。
  4. PATH 中的 `memctl`。
- 不读取项目 `.dsh/hooks.json`，不提供任意命令配置；这消除第三方 hooks 插件「配置文件即执行内容」的通用执行面。
- 第一版不增加 DSH `Config` schema。需要调整路径或超时时用环境变量；如果后续需要设置面板，再引入受校验的 Config，避免第一版依赖 Schemastery 或宿主内部配置 API。

### 5.7 错误处理

| 场景 | 行为 |
| --- | --- |
| memctl 不存在或不可执行 | 记录一次错误，跳过注入；会话继续；不重试到每个事件 |
| 记忆仓库未初始化 | `start` 返回可诊断错误；插件记录，不注入伪造上下文 |
| `start` 超时或输出不是合法 JSON | 终止进程，记录错误；不注册半截上下文 |
| `sync` 失败或超时 | 记录错误；不影响下一轮；不重试风暴 |
| `end` 失败 | 记录错误；不能保证崩溃/强杀场景下一定执行 |
| `systemPrompt` 服务缺失 | 插件声明 `inject = ['systemPrompt']`，让加载器等待或在缺失时保持非活动；不在 apply 里抛异常 |
| 子代理 | 跳过注入和同步；不创建独立记忆上下文 |
| 插件重复安装/旧 hooks 共存 | 记录警告；不自动删除用户配置 |
| DSH 版本不匹配 | 由 DSH 的 `engines.dsh` 检查拒绝加载；Plugin Manager 显示不兼容原因 |
| 插件卸载时有子进程 | 取消 start 与未完成的 sync；对仍 live 的会话触发一次 best-effort `end`，已 disposed 的不重复；清理状态与连接 |

所有事件监听器必须内部捕获异常。`agent/created` 监听器抛错会让 agent 创建失败，这是最需要避免的宿主破坏。

## 6. memctl 适配器改动

### 6.1 探测

dsh 适配器同时识别两种接入：

- 原生插件：`$HOME/.dsh/profiles/*/node_modules/memctl-dsh/package.json`，以及 `$HOME/.dsh/node_modules/memctl-dsh/package.json`、`$HOME/.dsh/plugins/memctl-dsh/package.json`。
- 旧 hooks 插件：保留现有 `dsh-hooks-plugin` 探测，作为兼容回退。

两种都未探测到时状态为 `not_installed` 或 `needs_attention`，并给出安装命令；不能只因为 dsh 可执行文件存在就返回成功。

### 6.2 状态

| 探测结果 | 状态 | 说明 |
| --- | --- | --- |
| dsh 存在，原生插件存在 | `configured` | 不需要 hooks.json；首次真实验证后才是 `verified` |
| dsh 存在，只有旧 hooks 插件和 hooks 配置 | `configured` | 兼容路径；标记为 legacy，提示迁移 |
| dsh 存在，原生插件和旧 hooks 同时存在 | `needs_attention` | 重复触发风险；提示用户移除其中一套，不自动删 |
| dsh 存在，没有任何插件 | `needs_attention` | 输出「添加插件」界面的 npm 包名与内嵌 tarball 绝对路径 |
| dsh 不存在 | `not_installed` | 不写入任何配置 |
| 插件版本低于最低支持版本 | `needs_attention` | 提示升级 |
| DSH 版本不在 `engines.dsh` 范围 | `unsupported` | 不进入安装流程 |

### 6.3 安装计划

- 原生插件已存在：`install dsh` 返回零配置写入的计划，输出「无需 hooks.json」。
- 原生插件缺失：`install dsh --dry-run` 输出 npm 包名和绝对 tarball 路径两种界面输入；不执行 pnpm，不写 profile。
- 旧 hooks 回退：保留现有模板和合并逻辑，但标记 deprecated；只有用户显式选择旧路径时才写入。
- 不把 `memctl-dsh` 作为 `PluginSpec` 的必需插件来写 hooks；需要扩展 `AdapterSpec`，增加「原生插件」字段或候选列表，并让 `Plan` 在原生插件存在时返回空 artifact 集。具体改动在实现前更新 [架构与模块](./01-architecture-and-modules.md#5-目录结构与依赖) 和 [模型与契约](./02-domain-models-and-contracts.md#23-审计诊断整理与接入)。

### 6.4 安装提示

`memctl install dsh` 的提示必须包含：

```text
DSH 界面: 插件 → 添加插件 → memctl-dsh@<version>
DSH 离线: 运行 memctl dsh plugin-path，把输出的绝对 tarball 路径粘贴到同一对话框
CLI: dsh plugin --profile <profile> add memctl-dsh@<version>
```

独立 GitHub 仓库发布后，可在同一提示中增加仓库根地址；当前实现不打印未发布的入口。

不猜测 profile 名称；让用户使用 Plugin Manager 或填入自己的 profile。若当前进程带有 `DSH_PROFILE` 环境变量，可以把它作为默认值显示，但不假定它一定存在。

## 7. 发布工程

### 7.1 产物

每个 memctl 版本产出：

- `memctl-dsh-<version>.tgz`：预构建 npm 包，无依赖、无安装脚本；作为 GitHub Release 附件，同时由 memctl 在运行时从内嵌源文件生成到状态目录。
- 可选的 npm 发布：包名 `memctl-dsh`，精确版本，带 `engines.dsh`。
- 可选的独立 GitHub 仓库：根目录就是 bundle，提交预构建入口。
- 可选的 `docs/dsh-market/index.json`：格式 1，`source.type = npm`，精确版本，`engines.dsh`，图标和本地化元数据；只服务 Discover 页面。

tarball、npm 包和 GitHub 仓库来自同一份源码和同一份 `package.json`；不允许其中一个是另一套构建结果。运行时 tarball 由 memctl 用 `go:embed` 的内嵌源文件生成，Release tarball 由同一份源文件经 `npm pack` 生成；两者文件清单和版本一致，不要求字节一致（tar/gzip 元数据可能不同）。

### 7.2 构建入口

```sh
make dsh-plugin-test     # Node 单元测试
make dsh-plugin-pack     # npm pack，产出 tgz 并校验文件清单
make dsh-plugin-index    # 生成 Discover 市场索引（可选）
```

memctl 的 Go 包直接 `go:embed` 插件源文件，`go build`/`go test` 不依赖先生成 tarball；`memctl dsh plugin-path` 在运行时把内嵌源文件打包成状态目录下的 `.tgz`。`make release` 先执行插件测试和打包，再构建四个平台二进制；Release 附件加入 `memctl-dsh-<version>.tgz` 和对应 SHA-256。插件本身不参与 Go 交叉编译；它是平台无关的 ESM 包。

### 7.3 CI 与发布

- 在现有 GitHub Actions `v*` 标签流水线中增加：
  1. 运行插件单元测试和 `npm pack --dry-run` 文件清单检查。
  2. 产出 tarball，加入 Release 附件与 `SHA256SUMS`；二进制构建不依赖该 tarball。
  3. 如启用 npm，使用 `NPM_TOKEN` 发布 `memctl-dsh@<version>`；优先使用 npm provenance。
  4. 如启用独立 GitHub 仓库，验证仓库根 `package.json` 的 bundle 声明和预构建入口；不从主仓库子目录发布。
  5. 如启用市场索引，生成并校验 `index.json`，发布到静态托管地址。
- npm 发布失败不应导致二进制 Release 失败；反之，二进制测试失败不能发布插件。两类产物分别报告状态。
- 市场索引必须校验：`format = 1`、`name`/`version` 与包清单一致、`engines.dsh` 非空、`publishTimestamp` 正确、图标路径可解析、无重定向。
- 不在 CI 中运行 `dsh plugin add` 修改真实用户 profile；安装验证使用隔离的 `DSH_HOME` 和临时 profile。
- 安装验证至少覆盖界面使用的三种 spec：npm 包名、绝对 tarball 路径、绝对目录路径；GitHub URL 在独立仓库可用后单独验证。

### 7.4 版本矩阵

| 维度 | 策略 |
| --- | --- |
| 插件版本 | 与 memctl 主次版本对齐，补丁可独立 |
| DSH 兼容 | `engines.dsh` 精确声明；每次 DSH 升级跑真实宿主验证 |
| memctl CLI 兼容 | 只依赖 `start/sync/end` 的稳定参数和 JSON envelope；插件加载时可选执行 `memctl version` 记录版本，不因版本不同拒绝工作 |
| Node | `engines.node >= 20`；使用 DSH 自带 Node 运行时 |
| 平台 | 插件平台无关；进程清理和路径解析需在 macOS、Linux、Windows 验证 |

## 8. 验证与验收

### 8.1 单元测试

使用假 `ctx`、假 `agent` 和假 `spawn` 覆盖：

- `agent/created` 只处理根 agent，子代理跳过。
- `start` 参数、超时、JSON 解析、上下文缓存、`systemPrompt.context` 注册。
- `session/event` 只对 `turn/end` 触发；并发时只保留一个 pending。
- `agent/disposed` 触发 `end`，重复 disposed 不重复触发。
- 插件卸载清理监听器、子进程和定时器。
- 二进制缺失、非零退出、超时、非法 JSON、输出超限都不抛出宿主异常。
- `source` 为 `startup/resume/clear/compact` 时的行为。

### 8.2 包与安装测试

- `npm pack` 后检查 `files`：不包含测试、源码映射、缓存和凭据。
- `package.json` 无 `dependencies`、`scripts.install/postinstall/prepare`；`engines.dsh` 有效。
- 内嵌 tarball 与 Release tarball 的文件清单、`package.json` 版本和 `engines.dsh` 一致（不要求字节一致）；`memctl dsh plugin-path` 输出绝对路径，且路径存在。
- 在隔离 `DSH_HOME` 和临时 profile 中执行 `dsh plugin --profile test add /absolute/path/memctl-dsh-<version>.tgz`，再执行 `dsh --profile test --dump-config` 检查插件行；安装后删除 tarball，当前插件仍可加载；这不验证后续 `pnpm install`。
- 验证界面 spec 解析：npm 包名通过；绝对 tarball 路径通过；绝对目录路径通过；`~` 路径、相对路径、带子目录的 GitHub URL 被拒绝。
- 安装不兼容的 `engines.dsh` 时 DSH 拒绝，profile 不出现半安装状态。
- 卸载后插件行消失，profile 依赖清理，记忆仓库不变。
- 旧 hooks 与原生插件同时存在时，适配器报告 `needs_attention`，不自动删除。
- 用隔离 profile 手工走一遍「添加插件」对话框：npm 包名、绝对 tarball 路径、绝对目录路径三种输入；记录截图或命令输出，不修改真实用户 profile。

### 8.3 真实宿主测试

环境：DSH 0.2.1-rc.1，macOS arm64；隔离 `HOME` 或隔离 `AGENT_MEMORY_DIR`；临时记忆仓库和可控远端。

安装路径至少一次通过「插件 → 添加插件」对话框完成，而不是只用 CLI。必须验证：

1. 新会话首个模型请求前，`systemPrompt.context` 快照包含 `[agent-memory]` 内容；会话日志可复核。
2. 一轮对话结束后，`git -C <repo> log -1` 出现 memctl 提交，且没有重复同步。
3. 关闭会话后 `end` 被调用；强杀/崩溃场景记录为未保证。
4. 恢复旧会话时上下文重新注册，不重复创建记忆提交。
5. 子代理不触发独立 start/sync。
6. memctl 不存在、仓库未初始化、断网、锁忙时，会话仍能正常使用，日志有明确错误。
7. 旧 hooks 插件卸载后不再重复触发。
8. DSH 版本升级到范围外时，Plugin Manager 显示不兼容，DSH 不加载插件且不崩溃。

### 8.4 建议的验收 ID

在 [验收与交付](./05-validation-and-delivery.md#2-核心测试矩阵) 中新增或映射：

| ID | 场景 | 预期 |
| --- | --- | --- |
| `H07` | 原生 dsh 插件事件映射 | `agent/created` 注入、`turn/end` 同步、`agent/disposed` 收尾各一次 |
| `H08` | 上下文注入 | 首个请求前存在 `[agent-memory]` 快照，不伪造 inbox 用户消息 |
| `H09` | 并发与失败 | 多轮快速结束只保留一个有界同步；断网/锁忙不阻塞会话 |
| `H10` | 子代理与生命周期 | 子代理不重复注入/同步；插件卸载清理子进程 |
| `I03` | 插件安装/升级/卸载 | 隔离 profile 中幂等；无 hooks.json；无安装脚本 |
| `I04` | tarball 离线安装 | 无 npm 网络也能安装；文件清单和版本正确 |
| `I05` | DSH 兼容拒绝 | 范围外版本被拒绝，不出现半安装或静默失效 |
| `I06` | 添加插件界面输入 | npm 包名、绝对 tarball、绝对目录均可解析并安装；`~`、相对路径、带子目录的 GitHub URL 被拒绝 |

这些 ID 在实现前写入 05，避免实现后再补验收定义。

## 9. 风险与待确认事项

| ID | 事项 | 影响 | 建议 |
| --- | --- | --- | --- |
| `D1` | 已确认：A（npm 包名主通道 + 内嵌 tarball 备用） | 影响发布工程、用户入口、离线能力 | 两条通道都在「添加插件」界面安装；npm 发布受阻时内嵌 tarball 直接升为主通道 |
| `D2` | 是否维护独立 GitHub 仓库作为界面入口 | 影响第二个仓库的发布与审计成本 | 第一版可不做；npm 或内嵌 tarball 已覆盖界面安装 |
| `D3` | 是否发布 Discover 市场索引及托管位置 | 影响发现性、更新提示 | 第一版可不做；插件页安装不依赖市场索引 |
| `D4` | 插件版本是否与 memctl 完全同步 | 影响兼容矩阵和发布节奏 | 主次版本同步，补丁独立 |
| `D5` | 记忆上下文过大时的截断策略 | 影响 token 成本和信息完整性 | 第一版不静默截断；超限记录错误并保留 memctl 原始输出策略 |
| `D6` | 子代理是否注入简短指针 | 影响子代理能力和 token | 第一版跳过；需要时再做短指针 |
| `D7` | 是否在 v1 提供客户端状态面板 | 影响插件复杂度和验收 | 不做；Host-only 先跑通 |
| `D8` | 旧 hooks 配置是否自动迁移 | 影响用户配置所有权 | 不自动删除；报告并让用户选择 |

其他风险：

- DSH 仍是 developer preview，事件和服务可能在下个版本改名。`engines.dsh` 只能拒绝不兼容版本，不能消除维护成本；每次 DSH 升级都要跑真实宿主验证。
- Host 插件在进程内、沙箱外执行；安装即信任。插件本身很小，但仍应发布源码、校验和、无依赖清单。
- `agent/disposed` 在进程被强杀或崩溃时不能保证执行；`end` 只能尽力而为，不能对外宣称「一定同步」。
- `systemPrompt.context` 会把记忆作为 user-role 快照写入会话历史；大段 `MEMORY.md` 会增加 token 和压缩成本。建议 memctl 输出保持「规则 + 入口」，详细内容按需读取。
- npm 供应链和包名占用风险；使用精确版本、provenance、无 install 脚本和发布前文件清单检查。

## 10. 实施顺序

| 步骤 | 内容 | 完成条件 |
| --- | --- | --- |
| `E-1` | 包骨架、`package.json`、patch、locale、icon、单元测试 | `npm pack` 文件清单正确；假 ctx 测试通过 |
| `E-2` | 生命周期、上下文注入、同步/收尾、进程管理 | 假宿主集成测试覆盖 H07–H10 |
| `E-3` | 适配器探测、状态、安装提示、旧 hooks 共存 | `install --status` 区分原生/旧/缺失；无自动写 profile |
| `E-4` | tarball 产物、运行时 tarball/目录生成、CI、Release 附件与可选 GitHub 仓库和市场索引；npm 发布由后续发布轮次完成 | Release 附件和校验和；`go:embed` 源文件随二进制构建；隔离 profile 安装测试 |
| `E-5` | 真实 DSH 会话验证、文档、发布 | H07–H10、I03–I06 有真实宿主证据；至少一次从「添加插件」界面安装；未验证项如实记录 |

实现前需要：

1. 更新 [目录规划](./01-architecture-and-modules.md#5-目录结构与依赖)，增加 `plugins/dsh/`；插件源文件由该目录的 Go 文件 `go:embed`，运行时生成 tarball 或目录，不提交生成物。
2. 更新 [模型与契约](./02-domain-models-and-contracts.md#23-审计诊断整理与接入) 的适配器结构，支持原生插件与旧 hooks 两种模式。
3. 更新 [验收与交付](./05-validation-and-delivery.md#2-核心测试矩阵) 的验收 ID 和阶段计划。
4. `D1` 已确认取 A；`D2`、`D3` 可先按可选处理，后续再确认。

上述 `E-1` 至 `E-4` 已由 `T048` 实现，`E-5` 已由 `T052` 完成：本机 `desktop` 与隔离 web 会话验证注入、轮次同步、受管写入提交、恢复、锁忙/断网与 tarball 界面安装；`agent/disposed` 事件受宿主限制。npm 包由 `T064` 发布。实现状态见 [§13](#13-实现状态)。

## 11. 类似项目调研

| 项目 | 分发/安装 | 上下文接入 | 可借鉴与差异 |
| --- | --- | --- | --- |
| [`dsh-context-memory`](https://github.com/hj01857655/dsh-plugins/tree/main/packages/dsh-context-memory) | npm 包，`dsh plugin --profile web add dsh-context-memory`；无自定义安装器 | `agent/pre-step` 注入带 `plugin` source 的 recall 消息，按用户输入检索 | 最接近的 DSH 记忆插件；证明「npm + Host 插件」足够，不必先做安装器。它把记忆存在项目 `.memory`，memctl 用 Git 仓库，职责不同 |
| [`dsh-hooks-plugin`](https://github.com/KYinCode/dsh-hooks-plugin) | npm 包，`dsh plugin add` + `hooks.json`；memctl 当前依赖 | 订阅旧事件，`agent.inject` 注入 | 说明通用 hooks 的维护面；其 SessionStart 事件和全局路径在 0.2.1-rc.1 已失效 |
| [`dsh-hooks`](https://github.com/PeterBon/dsh-hooks) | npm 包，配置写在 `cordis.patch.yml` | 事件执行 shell 命令，无 stdin | 更轻，但同样受 DSH 事件版本影响；不适合需要结构化上下文的 memctl |
| [`claude-mem`](https://github.com/thedotmack/claude-mem/blob/main/docs/public/installation.mdx) | npm 包 + `npx claude-mem install` + 插件市场；安装器写 hooks、启动 worker、配置 launcher | 各宿主 hooks + 本地 worker | 展示「一键安装器」的复杂度与失败面；memctl 不需要 worker 和 launcher，应避免复制 |
| [`basic-memory`](https://docs.basicmemory.com/local/local-install) | Python CLI（uv/Homebrew）+ 用户手工配置 MCP client | MCP tools / 资源 | 包管理器 + 手动配置可以工作，但 DSH 用户更习惯 Plugin Manager；不作为主通道 |
| [Claude Code 插件市场](https://code.claude.com/docs/en/plugin-marketplaces) | 市场仓库 `.claude-plugin/marketplace.json`；`/plugin marketplace add` + `/plugin install` | 插件自带 hooks、skills、agents | 与 DSH 市场索引思路一致：目录与包分离，安装由宿主完成；memctl 可复用这种「目录 + 预构建包」模式 |
| [DSH Plugin Hub](https://github.com/dshplugin/dsh-plugin-hub) | 第三方社区市场，npm 安装，自带浏览器端目录 | 不涉及 memctl | 可作为发现渠道，但不是 memctl 控制的发布通道；不能把可用性建立在第三方审核上 |

调研结论：

- DSH 的「插件 → 添加插件」是 spec 驱动：npm 包名、GitHub 仓库根、本地绝对目录和绝对 tarball 路径都能进入安装流程。因此 memctl 的 UI 分发不需要先做市场索引，npm 名或内嵌 tarball 就足够。
- DSH 生态的主流安装方式是 npm bundle + `dsh plugin add`，不是每个项目自带安装器。
- 只有需要写宿主配置或启动常驻服务时，才值得做 `npx ... install`（claude-mem 的路径）；memctl 的插件不需要写 hooks.json，也不需要 worker，因此不应引入安装器。
- 记忆类插件通常把「检索/注入」和「存储」分开；memctl 已有 Git 仓库和同步，插件应只做桥接。
- 市场索引适合 Discover 发现和更新，但它是另一个入口；本对话框的 npm 或 tarball 安装不应依赖它。

## 12. 参考

- [DSH 打包与安装插件](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/user/develop/basic/publish.zh.md)
- [DSH 插件与生命周期](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/user/develop/framework/index.zh.md)
- [DSH 事件系统](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/user/develop/framework/events.zh.md)
- [DSH 服务与依赖](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/user/develop/framework/service.zh.md)
- [DSH 会话事件](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/subsystems/session.zh.md)
- [DSH 系统提示](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/subsystems/system-prompt.zh.md)
- [DSH app-boot](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/packages/boot/app-boot/README.zh.md)
- [DSH Plugin Manager](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/packages/boot/plugin-manager/README.zh.md)
- [DSH 市场索引格式](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/.agents/notes/implemented/feature/2026-10-02-plugin-market-index-format.md)
- [DSH 核心子系统 Cordis 接口](https://github.com/deepseek-ai/DeepSeek-Harness/blob/master/docs/subsystems/core.zh.md)

## 13. 实现状态

已落地：

- `plugins/dsh/` 保存 `package.json`、`cordis.patch.yml`、`index.js`、`locale/en.json`、`locale/zh.json`、`icon.svg`、`README.md`、`LICENSE`、Node 单元测试与 `embed.go`。包无 `dependencies`、`install`、`postinstall` 或 `prepare`；`files` 只包含运行时文件。
- `memctl dsh plugin-path` 把 `go:embed` 的源文件写成 `$STATE/dsh/memctl-dsh-<version>.tgz`；`memctl dsh plugin-dir` 写成 `$STATE/dsh/<version>/`。两者只输出绝对路径，不修改 DSH profile，也不执行 `pnpm`。
- dsh 适配器同时探测原生 `memctl-dsh` 与旧 `dsh-hooks-plugin`。原生插件存在时 `artifacts` 为空、状态 `configured`，不写 `hooks.json`；两者共存或都不存在时返回 `needs_attention`；只存在旧插件时保留 deprecated 的 hooks 合并路径。
- `install --status` 在缺少任何 dsh 插件时打印「插件 → 添加插件」的 npm 名、内嵌 tarball 命令和 CLI 等价入口。
- `make test` 运行插件 Node 单元测试；`make release` 在 `dist/` 生成 `memctl-dsh-<version>.tgz`，并纳入 `SHA256SUMS` 与 `RELEASE-INFO.txt`。`v0.4.0` Release 已附带 `memctl-dsh-0.4.0.tgz`；`v0.5.0` Release 附带 `memctl-dsh-0.5.0.tgz` 与 `memctl-pi-0.5.0.tgz`；`v0.5.1` Release 附带 `memctl-dsh-0.5.1.tgz` 与 `memctl-pi-0.5.1.tgz`；`v0.6.0` 与 `v0.7.0` Release 附带同名 `memctl-dsh-0.5.1.tgz`（插件源码未改动，版本保持 `0.5.1`）。2026-10-20 起两个插件统一为 `0.5.2`，该版本随 `v0.9.0` 发布；本机 `desktop` profile 仍使用 `0.5.1`，因此 `install --status` 仍返回 `configured host=dsh@0.2.1-rc.2 plugin@0.5.1`。`0.5.2` 的 npm 与界面安装待发布后复验。
- 插件卸载时，对仍 live 且尚未收尾的会话触发一次 detached `end`；`agent/disposed` 已清理的会话不重复触发。`end` 是幂等的后台同步，不写入会话关闭状态。
- 验证证据：插件 Node 单元测试 14 项；Go 包清单、tarball 内容与路径测试；隔离 `DSH_HOME` 的 `test` profile 通过 `dsh plugin add <tgz>` 安装，`dsh --profile test --dump-config` 出现 `memctl-dsh`，删除 tarball 后仍可加载；该隔离 HOME 中 `memctl install --status` 返回 `configured`，且未生成 `hooks.json`。`v0.2.0` 发布后，本机 `desktop` 真实会话已出现 `[agent-memory]` 上下文快照；`v0.4.0` 已发布，`desktop` profile 的 `memctl-dsh` 已更新到 `0.4.0`；`v0.5.0` 已发布，`desktop` profile 的插件仍为 `0.4.0`；`v0.5.1` 已发布，两个插件均可在 npm 安装；本机 `desktop` profile 已通过 DSH 界面 npm 包名安装升级到 `0.5.1`，profile 的 bundle 列表与依赖一致，`install --status` 返回 `configured host=dsh@0.2.1-rc.2 plugin@0.5.1`；当前真实会话的 `runtime-context` 快照包含 `memctl:memory`，同步缓存于 23:20:24 更新为 `synced`。`0.3.0` 在隔离 headless 与 web host 正常退出时都通过插件卸载兜底恰好触发一次 `end`；隔离 web 会话完成两次成功模型轮次，两次 `runtime-context` 快照都包含 `memctl:memory` section，`turn/end` 后触发 `sync`；重启 web host 并恢复同一会话后再次注入并同步，仓库 HEAD 不重复提交；绝对 tarball 已通过「插件 → 添加插件」界面安装并启用。`0.4.0` 隔离 web 会话中模型实际执行 `memctl write`，产生提交 `61fd68e`，`turn/end` 后触发 `sync`；人工持锁时 `sync --wait` 在 3.1 秒内返回 `BUSY`，会话继续且本地提交保留；远端离线时缓存记录 `NETWORK_UNAVAILABLE`，会话继续且本地提交保留。`agent/disposed` 事件本身未在真实宿主观察到；DSH 0.2.1-rc.2 没有单会话销毁入口。

未验证：

- 真实 DSH 会话中的 `agent/disposed` 事件路径（DSH 0.2.1-rc.2 无单会话销毁入口，仅单元测试覆盖），以及绝对目录的界面安装。`systemPrompt.context` 快照、`turn/end` 后同步、受管写入提交、恢复、锁忙/断网降级、tarball 与 npm 包名界面安装已有真实验证，详见 [T052 验证记录](./validation/t052-dsh-real-session.md)。
- DSH 0.2.1-rc.2 的 `engines.dsh` 安装已通过；构造 `>=9.0.0 <10.0.0` 的包后，DSH 以退出码 `1` 拒绝安装并恢复 `package.json`；未在每个 DSH 小版本复验。
- 独立 GitHub 仓库与 Discover 市场索引未发布；`D2`、`D3` 仍为可选。
