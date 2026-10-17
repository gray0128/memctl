# 08. pi 专用插件：调研、分发与实现方案

> 状态：`F-1` 至 `F-4` 已随 `v0.5.0` 发布（2026-10-18），隔离宿主验证通过，真实 pi 会话与真实模型、npm 发布、非 macOS 平台仍未验证。结论是新增 `memctl-pi` 原生扩展作为 pi 的默认接入；`pi-yaml-hooks` 保留为 legacy 兼容路径。实现证据见 §11，验收编号已登记到 [验收与交付](./05-validation-and-delivery.md#2-核心测试矩阵)。

## 1. 结论

- 适合为 pi 单独创建插件，不建议继续把 `pi-yaml-hooks` 作为唯一接入。
- dsh 专用插件已经实现并完成真实会话验证，验证了「宿主插件只做桥接、memctl 负责存储与同步」的模式；pi 的主要差异在宿主 API 和安装入口，见 [dsh 专用插件](./07-dsh-plugin.md)。
- `memctl-pi` 只负责三件事：会话开始时读取记忆并同步、每次 agent run 开始时注入上下文、run 结束同步与会话结束时收尾。记忆仓库、锁、冲突处理、Git 提交和远端同步仍由 memctl 二进制负责。
- 与 dsh 专用插件相比，pi 插件更小：不需要 bundle patch、locale、icon，也不需要自定义 tarball 安装通道；pi 包就是普通 npm 包或本地目录，安装入口是 `pi install`。
- 推荐分发组合：npm 包 `memctl-pi` 为主通道，`memctl pi plugin-dir` 输出的本地包目录为离线备用通道。pi 不把本地 `.tgz` 当作 npm tarball 解包，不能照搬 dsh 的 tarball 通道。
- 推荐生命周期映射：`session_start` 后台执行 `memctl start --json`；`before_agent_start` 把缓存的正文写入 `systemPromptOptions.sections["memctl"]`；`agent_settled` 触发 `memctl sync`；`session_shutdown` 触发 `memctl end`。
- 不采用两套接入同时启用；不自动修改 pi 的 `settings.json`；不自动删除旧 `hooks.yaml`；不在插件内实现记忆存储、检索、向量库或后台模型整理。

## 2. 范围

### 2.1 本方案覆盖

- `memctl-pi` 的包结构、生命周期接线、上下文注入、同步与收尾。
- npm 包、本地包目录、版本矩阵和发布产物。
- `internal/adapters` 对 pi 原生插件与旧 `pi-yaml-hooks` 配置的探测、状态和安装提示。
- 单元测试、包安装测试、隔离宿主测试与真实 pi 会话验收。
- 与 [dsh 专用插件](./07-dsh-plugin.md) 的差异和可复用部分。

### 2.2 本方案不覆盖

- OMP 原生插件。OMP 是另一个宿主，包名和扩展 API 不同；v1 不为 OMP 提供 `memctl-pi`。
- 由 memctl 自动安装、升级或卸载 pi 包；安装仍由 `pi install` / `pi remove` 完成。
- pi 客户端面板、`/memctl` 命令、模型可调用的 memctl 工具或 MCP server。
- 记忆格式、检索策略、Dreaming、锁和同步协议的变化。
- 自动删除或自动迁移旧 `pi-yaml-hooks` 配置。
- 真实模型质量、延迟和 token 成本的基准结论；这些需要后续实测。

## 3. 为什么需要独立插件

### 3.1 当前 pi 接入的事实

| 事实 | 证据 | 影响 |
| --- | --- | --- |
| 当前 pi 为 `1.3.0` | 本机 `pi --version`；pi 包 `@earendil-works/pi-coding-agent` | 原生扩展 API 以该版本为验收基线 |
| 当前依赖 `pi-yaml-hooks@2026.9.26` | 本机 `~/.pi/agent/npm/node_modules/pi-yaml-hooks/package.json` | memctl 的 pi 接入受第三方通用桥的维护节奏影响 |
| `pi-yaml-hooks` 的兼容矩阵止于 pi `0.86.1` | 其 README 的 Compatibility 段 | 本机 `1.3.0` 属于未验证组合；未验证不等于已损坏 |
| `pi-yaml-hooks` 需要 `bash`，且 README 明确不支持 Windows | 其 README 的 Requirements 段 | pi 自身可用于 Windows，但当前 memctl 的 pi 接入不能跟随 |
| 当前 `memctl install --status` 对 pi 报告 `configured plugin@2026.9.26` | 本机只读探测 | `configured` 只说明配置存在，不说明注入已真实发生 |
| 当前 `install pi` 会合并 `~/.pi/agent/hook/hooks.yaml` | [Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装) | 新用户仍会得到 YAML + 第三方插件的组合 |
| `pi-yaml-hooks` 的 `user.prompt.submit` 是同步 bash，输出上限 64 KiB；`session.deleted` 是尽力而为 | 其 hooks reference | memctl 的注入大小、退出时机和错误可见性受通用桥限制 |

这些事实说明，`pi-yaml-hooks` 是一个可用的通用桥，但它不是 memctl 专用接入。继续使用它可以工作，代价是接受第三方插件的平台限制、事件映射和版本矩阵；自建插件可以把这些边界收回到 memctl 自己的契约和测试中。

### 3.2 原生扩展提供的接入点

pi 扩展是运行在 pi 进程内的 TypeScript/JavaScript 模块，通过默认导出的工厂函数接收 `ExtensionAPI`。扩展可以监听生命周期事件、修改提示词段落、注册工具和命令。[pi Extensions](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/extensions.md)

| 接入点 | 语义 | memctl 用途 |
| --- | --- | --- |
| `session_start` | 会话启动、加载、恢复、分叉或扩展重载；`reason` 为 `startup \| reload \| new \| resume \| fork` | 读取会话 id，后台执行 `memctl start --json` |
| `before_agent_start` | 每次由用户消息或自定义消息启动一次 agent run 前触发；可修改 `systemPromptOptions` | 把缓存的记忆正文写入 `sections["memctl"]` |
| `agent_settled` | 一次 agent run 完全结束，且没有自动重试、压缩或排队续跑 | 触发一次有界后台 `memctl sync` |
| `session_shutdown` | 扩展运行时因退出、重载或会话替换被拆除前触发；`reason` 为 `quit \| reload \| new \| resume \| fork` | 触发 `memctl end`，或清理子进程 |
| `ctx.sessionManager.getSessionId()` | 当前会话的稳定 id；同一会话文件保持不变，分叉或新会话得到新 id | 作为 `--sid`，不依赖 `PI_SESSION_ID` 环境变量 |
| `header.parentSession` | 会话头记录父会话文件；`/fork`、`/clone` 和子代理都可能带有该字段 | 不能单独作为子代理标记 |
| `PI_SUBAGENT_CHILD=1` | `pi-subagents` 子进程使用的约定标记，不是 pi 核心 API | 可作为未来跳过子会话的可选开关，不作为 v1 默认行为 |

`before_agent_start` 的官方文档明确建议优先修改提示词 section、工具或 guideline，让 pi 通过 transcript delta 追加变化；返回 `systemPrompt` 或设置 `forceSystemPrompt` 会替换整个提示词，可能破坏缓存。`systemPromptOptions.sections` 是结构化注入的首选通道。

`session_shutdown` 的 handler 会被 pi 等待。成熟 pi 记忆插件指出 pi 核心等待 shutdown handler 时没有超时，因此插件必须自己设置超时，不能让一次网络同步无限阻塞退出。

### 3.3 与继续使用 pi-yaml-hooks 的比较

| 维度 | `pi-yaml-hooks` | 原生 `memctl-pi` |
| --- | --- | --- |
| 依赖面 | 第三方通用桥、bash、YAML、trust 配置 | pi 扩展 API、memctl 二进制 |
| 会话 id | 由 hook 注入的 `PI_SESSION_ID` 环境变量 | 直接读 `ctx.sessionManager.getSessionId()` |
| 上下文注入 | `user.prompt.submit` 的 bash stdout；pi 0.86 起写入 section，旧版拼接字符串 | 直接写 `systemPromptOptions.sections`，必要时回退到 `systemPrompt` |
| 同步时机 | `session.idle` 映射；依赖通用桥对 `agent_end` / `agent_settled` 的处理 | `agent_settled` 直接触发，必要时兼容 `agent_end` |
| 收尾 | `session.deleted` 尽力而为 | `session_shutdown` 明确触发并被 pi 等待 |
| 平台 | README 声明 macOS/Linux，不支持 Windows | 跟随 pi 支持的平台；不额外依赖 bash |
| 版本兼容 | 精确测试矩阵，当前止于 0.86.1 | 需要自建矩阵；pi 没有 `engines.dsh` 式强制拒绝 |
| 分发 | npm + YAML 配置 | npm 包或本地包目录；不需要 YAML 合并 |
| 通用性 | 同时支持 pi 和 OMP，可配置任意 hook | 只服务 memctl，不支持 OMP |
| 维护 | 第三方维护，memctl 只维护 YAML 模板 | memctl 维护插件与适配器，控制力更强 |

结论：如果目标是「最低维护成本 + 继续支持 OMP」，保留 `pi-yaml-hooks` 合理；如果目标是「pi 的默认接入可控、可测试、可在 Windows 使用，并去掉通用 bash/YAML 执行面」，原生插件更合适。两者不应同时启用。

## 4. 成熟项目调研

### 4.1 调研对象与方法

调研日期为 2026-10-17。除阅读 pi 官方文档和本机 `1.3.0` 的实现外，还检查了以下项目的 README、包清单和关键源码：

- pi 官方扩展与内置 MCP 扩展：生命周期、section 注入、session 级清理的参考实现。
- `pi-subagents`：子会话和 `PI_SUBAGENT_CHILD` 标记的来源。
- `pi-yaml-hooks`：当前 memctl 依赖的通用桥、兼容矩阵和已知限制。
- `pi-memory`、`@askjo/pi-mem`、`@shog-lab/pi-memory`、`pi-hermes-memory`：不同规模的 pi 记忆插件。
- `dsh-context-memory`：DSH 生态中最接近的记忆插件，用于对照 dsh 专用插件的边界。
- `claude-mem`、`basic-memory`：安装器/worker 与 MCP 两条不同路线。
- OMP：pi 的分叉宿主，判断原生插件不能覆盖的范围。

本机另做了一次隔离探针：使用临时 `PI_CODING_AGENT_DIR` 和本地 mock OpenAI 兼容端点，加载测试扩展运行 `pi -p`。实际观察到 `session_start` → `before_agent_start` → `agent_settled` → `session_shutdown`，写入 `systemPromptOptions.sections` 的文本出现在模型请求的 system prompt 中；`pi install /absolute/package-dir` 和 `pi install /absolute/file.ts` 都能被 `pi` 加载。该探针只证明 API 行为，不替代真实 memctl 会话和真实模型验证。

表中的版本号和测试数量来自调研时的 README、`package.json` 或包清单；没有对第三方项目做完整代码审计，也不把这些项目的外部结论当作 memctl 的验收证据。第三方项目后续可能改名、拆分或改变兼容范围，实施前需要重新核对。

### 4.2 官方与邻近实现

| 项目 | 形态 | 关键做法 | 对 memctl 的启示 |
| --- | --- | --- | --- |
| pi 内置 MCP 扩展 | pi 第一方扩展 | `session_start` 建立 session 级 AbortController；首次 `before_agent_start` 有界等待启动连接；把服务器列表写入 `sections`；`session_shutdown` 取消连接并等待清理 | 使用 session 级状态、section 注入和有界启动等待；不要在扩展工厂里启动长生命周期资源 |
| `pi-subagents` | pi npm 包 | 子代理是独立 pi session；子进程设置 `PI_SUBAGENT_CHILD=1`；父会话文件通过 `parentSession` 记录 | 子会话不能只靠 `parentSession` 判断；需要跳过时应使用显式标记或配置 |
| `pi-yaml-hooks` | pi/OMP npm 包 | YAML 事件映射；`session.deleted` 尽力而为；pi 0.86 起走 prompt sections；精确版本矩阵 | 通用桥能覆盖两类宿主，但平台、版本和注入通道受其约束 |

pi 内置 MCP 扩展是目前最接近「只做桥接」的第一方示例：它不把服务器数据写入记忆文件，只把当前状态渲染成 section，并在 shutdown 时释放连接。`memctl-pi` 应沿用这种「session 级资源 + section + 可取消后台工作」的结构。

### 4.3 记忆类插件

| 项目 | 版本（调研时） | 安装与形态 | 记忆模型 | 注入与检索 | 生命周期 | 可借鉴与代价 |
| --- | --- | --- | --- | --- | --- | --- |
| [`pi-memory`](https://github.com/jayzeng/pi-memory) | `0.4.2` | npm 包，`pi install npm:pi-memory`；`index.ts` 入口 | `MEMORY.md`、`SCRATCHPAD.md`、daily 日志；可选 qmd 语义检索 | `before_agent_start` 拼接 `systemPrompt`；快照模式；2K/3K/4K 分级截断 | `session_start`、`session_shutdown`；shutdown 自设超时 | 快照与 token 预算值得借鉴；字符串替换不如下面的 section 方案缓存友好 |
| [`@askjo/pi-mem`](https://github.com/jo-inc/pi-mem) | `1.2.0` | npm/git 包；`index.ts` 入口 | 纯 Markdown；长期记忆索引、daily、scratchpad、notes | `context` 事件追加一条尾部 user 消息；side model 选择最多 5 条相关记忆 | `session_start`、`session_shutdown` | 不依赖向量库；每轮注入新鲜度更高；尾部 user 消息会进入对话历史并占 token |
| [`@shog-lab/pi-memory`](https://github.com/shog-lab/pi-mind) | `0.15.1` | 项目级 `pi install -l npm:@shog-lab/pi-memory` | `.pi-mind/knowledge/*.md` + FTS5 + 向量 + 知识图谱 | 显式 `remember_this` / `observe` 工具；按需检索 | 宿主扩展 | 「记忆被动、写入显式、行为变更先征求同意」值得借鉴；SQLite/向量依赖增加安装复杂度 |
| [`pi-hermes-memory`](https://github.com/chandra447/pi-hermes-memory) | `0.9.10` | npm 包 | `MEMORY.md`、`USER.md`、SQLite FTS5；两层记忆 | 后台学习、会话搜索、secret scanning、自动合并 | 宿主扩展 | secret scanning、容量与老化策略值得借鉴；`better-sqlite3` 原生依赖和后台学习不适合 memctl v1 |
| [`dsh-context-memory`](https://github.com/hj01857655/dsh-plugins/tree/main/packages/dsh-context-memory) | 未固定 | DSH npm 插件 + CLI + 面板 | append-only 事件日志；provenance、冲突、可验证声明 | `agent/pre-step` 注入 top-5 recall；解释性排序 | DSH 事件 | 存储与宿主桥分离、来源可追溯、冲突不擅自裁决；v1 不需要面板和完整记忆存储 |
| [`claude-mem`](https://github.com/thedotmack/claude-mem) | README 标注 `13.41.0` | npm + `npx claude-mem install` + worker | 观察压缩、语义摘要 | 各宿主 hooks + 本地 worker | 安装器与 worker 生命周期 | 展示了安装器和常驻 worker 的复杂度；memctl 不应复制 |
| [`basic-memory`](https://github.com/basicmachines-co/basic-memory) | 未固定 | Python CLI + MCP server | Markdown 知识图谱 | MCP tools / resources | 由 MCP 客户端管理 | MCP 可跨宿主提供工具，但不提供会话生命周期注入和同步；可作为未来工具通道 |

逐项结论：

- `pi-memory` 证明「Markdown + 显式工具 + 上下文快照」可以在 pi 上工作。它把上下文按 2K/3K/4K 截断，说明注入必须考虑 token 成本；它在 `session_shutdown` 内自设超时，说明 pi 等待 handler 时插件必须自己限时。
- `@askjo/pi-mem` 使用 `context` 事件追加尾部 user 消息，避免修改 system prompt。这条路线能保持 system prompt 稳定，但注入内容会进入对话历史，可能和用户输入竞争，也会在长会话中累积。对于 memctl，system prompt section 更符合「长期规则 + 入口」的定位。
- `@shog-lab/pi-memory` 和 `pi-hermes-memory` 都把「记忆写入」做成显式工具或显式行为，并强调用户同意、审计和容量控制。memctl 已有受管 `write` 和 Dreaming 确认流程，插件不应再引入自动写入。
- `dsh-context-memory` 的来源、冲突和验证模型比 memctl 当前范围更完整，但它的存储、CLI、面板和 DSH 服务耦合也更多。memctl 的插件只应做桥接，不复制这套系统。
- `claude-mem` 的安装器和 worker 是重资产；memctl 的单文件二进制和 Git 仓库不需要再引入常驻服务。
- `basic-memory` 证明 MCP 是跨宿主工具通道，但 MCP 本身不解决「会话开始注入、run 结束同步、会话结束收尾」。MCP 只能作为未来的工具补充，不能替代生命周期插件。

### 4.4 共性结论

1. **宿主插件应薄，存储和检索应在插件之外。** `pi-memory`、`pi-mind`、`dsh-context-memory` 都不同程度地把存储放在插件外或独立层；memctl 已有 Git 仓库和 CLI，插件只做生命周期桥。
2. **显式写入优先于自动写入。** 成熟项目普遍提供 `memory_write`、`remember_this` 等显式工具；memctl 的 `write` 和 Dreaming 确认流程已经覆盖这一点，插件不应在 run 结束时自动改写记忆。
3. **上下文注入是 token 和缓存问题，不只是拼接字符串。** 需要控制长度、避免每轮重复、优先使用 section，并让未变化的正文不产生新的 transcript delta。
4. **shutdown handler 必须自限时。** pi 会等待 handler；网络同步、子进程和日志都必须在有界时间内结束，不能阻塞退出。
5. **子会话需要显式标记。** `parentSession` 同时出现在分叉和子代理中，不能作为唯一判断；`PI_SUBAGENT_CHILD=1` 是 `pi-subagents` 的约定，不是核心 API。
6. **版本兼容要显式测试。** `pi-yaml-hooks` 使用精确矩阵；pi 没有宿主引擎门槛，插件和适配器必须自己报告未验证版本。
7. **分发优先 npm，本地路径用于开发和离线。** 成熟 pi 包都支持 `pi install npm:...`；本地路径不复制文件，目录必须长期存在。
8. **避免原生依赖和安装脚本。** `pi-hermes-memory` 的 `better-sqlite3`、`pi-memory` 的 `postinstall` 都增加了安装失败面；`memctl-pi` 应保持零依赖、零安装脚本。
9. **测试规模与生命周期覆盖相关。** `pi-mind` 标注 345 项测试，`pi-hermes-memory` 标注 732 项；memctl 插件不需要同等规模，但至少要有假宿主单元测试、隔离宿主集成测试和真实会话验收。
10. **OMP 是独立宿主。** `pi-yaml-hooks` 能同时覆盖 pi 和 OMP；原生 `memctl-pi` 不能。若 OMP 在支持范围内，应保留 YAML 路径或另做 OMP 插件。

## 5. 方案选择

| 方案 | 生命周期 | 上下文注入 | 依赖与分发 | 判断 |
| --- | --- | --- | --- | --- |
| 原生 `memctl-pi` | 直接使用 `session_start`、`before_agent_start`、`agent_settled`、`session_shutdown` | `systemPromptOptions.sections`，字符串回退 | pi 扩展 API + memctl；npm/本地目录 | 推荐 |
| 继续使用 `pi-yaml-hooks` | 通用桥映射 `session.created/idle/deleted` | `user.prompt.submit` stdout；pi 0.86 起 section | 第三方包 + bash + YAML | 保留为 legacy；不作为新默认 |
| Fork `pi-yaml-hooks` | 同通用桥 | 同通用桥 | 需要维护 fork 和上游兼容 | 不推荐；维护面没有减少 |
| 只做 MCP server | 无会话生命周期 | MCP tools / resources | memctl + MCP 配置 | 不能替代插件；可作为未来工具通道 |
| 做完整记忆插件 | 完整 | 工具 + 自动注入 + 存储 | 大量依赖和状态 | 与 memctl 职责重复；不采用 |

推荐方案是「原生插件优先 + 旧 YAML 兼容」：

- 新用户默认使用 `memctl-pi`；`memctl install pi --dry-run` 输出 npm 包名和本地目录两种入口。
- 已有 `pi-yaml-hooks` + `agent-memory-*` 条目的用户继续可用；适配器标记 legacy，并提示迁移。
- 两套同时存在时报告 `needs_attention`，不自动删除任何一方。
- OMP 不在 v1 范围内；需要 OMP 时继续使用 YAML 路径或另立方案。

## 6. 插件设计

### 6.1 职责边界

`memctl-pi` 只做桥接，不成为第二个记忆系统：

- 读取：调用 memctl，取得当前会话的上下文正文。
- 注入：把正文交给 pi 的 system prompt section，不做内容改写。
- 同步：在 agent run 结束后触发 memctl 同步，不解释同步结果。
- 收尾：在会话结束时触发一次 memctl 收尾，不写入会话关闭状态。
- 不直接读写记忆文件，不实现锁、Git、冲突、检索、Dreaming 或向量库。
- 不执行项目配置中的任意命令；只执行固定路径的 memctl 二进制和固定参数。
- v1 不注册 `memctl` 工具。模型已经可以通过 pi 的 bash 工具调用 memctl CLI，重复注册工具会再造一套参数、权限和输出协议；工具通道留到有明确需求时再评估。

### 6.2 包结构

```text
plugins/pi/
  package.json          # pi.extensions、版本、files、peerDependencies
  index.js              # 单文件 ESM 入口，无构建步骤
  README.md             # 安装、验证、限制
  test/                 # Node 单元测试，不进入 npm 包
  embed.go              # go:embed 运行时文件并写入 $STATE/pi/
```

选择 JavaScript 而不是 TypeScript：pi 直接加载 JavaScript，测试使用 `node --test`，不需要 `tsc`、`tsx` 或 jiti；这与 `memctl-dsh` 的零构建方式一致。代价是失去编译期类型检查，用 JSDoc、假宿主测试和隔离集成测试补偿。若后续团队更重视类型，可改为 TypeScript，但仍应保持无运行时依赖。

`plugins/pi/` 只保存运行时源文件和 `embed.go`；`test/` 不进入 npm 包；`$STATE/pi/` 下的生成目录不提交。

入口只注册事件，不在工厂中启动进程或定时器：

```js
export default function memctlPi(pi) {
  pi.on('session_start', onSessionStart)
  pi.on('before_agent_start', onBeforeAgentStart)
  pi.on('agent_settled', onAgentSettled)
  pi.on('agent_end', onAgentEnd)
  pi.on('session_shutdown', onSessionShutdown)
}
```

`onSessionStart` 创建该 SID 的状态并启动后台 `start`；`onBeforeAgentStart` 只读取缓存并写 section；`onAgentSettled` 和 `onAgentEnd` 共用同一套去重同步；`onSessionShutdown` 清理状态并按 reason 决定是否执行 `end`。

### 6.3 生命周期映射

| pi 事件 | 触发条件 | 插件动作 | 关键约束 |
| --- | --- | --- | --- |
| `session_start` | `reason` 为 `startup`、`reload`、`new`、`resume`、`fork` | 读取 `getSessionId()`；启动后台 `memctl start --agent pi --sid <id> --json` | 不阻塞 TUI 启动；在 `before_agent_start` 中按需等待 |
| `before_agent_start` | 每次用户消息或自定义消息启动 run 前 | 等待缓存（有界）；写 `systemPromptOptions.sections["memctl"]` | 正文不变时不产生新的 section delta；section 不可用时回退 |
| `agent_settled` | 一次 run 完全结束 | 触发一次后台 `memctl sync --agent pi --sid <id>` | 同时只允许一个同步；新事件只置 pending |
| `session_shutdown` | `reason` 为 `quit`、`new`、`resume`、`fork` | 分离并 unref `memctl end`；清理子进程和定时器 | 不阻塞退出；有界等待启动和早期退出 |
| `session_shutdown` | `reason` 为 `reload` | 只清理子进程，不执行 `end` | 同一会话继续，不应把重载当作会话结束 |

`session_start` 的 `start` 在后台执行，不直接在 handler 内等待完整同步。第一次 `before_agent_start` 最多等待 `START_WAIT_MS`；超时则本轮不注入，等 `start` 完成后的下一次 prompt 再注入。这样既不让网络同步阻塞 pi 启动，也能保证首个请求在预算内获得上下文。

### 6.4 会话开始与上下文

1. 从 `ctx.sessionManager.getSessionId()` 取 SID；空值或非字符串时记录一次错误，不注入上下文。
2. 以固定参数启动：

   ```text
   memctl start --agent pi --sid <sessionId> --json
   ```

   不使用 `--stdin`，不经过 shell，参数数组直接传给 `spawn`。`start` 的 stdout 只允许一个 JSON 对象，读取 `hookSpecificOutput.additionalContext`。非零退出时，如果 stdout 仍能解析为合法 envelope，保留其中的上下文并记录同步错误；只有 stdout 为空或不是合法 JSON 时才放弃注入。
3. 缓存正文；缓存键为 SID。内部用 `null` 表示尚未取得正文，用空字符串表示明确没有正文；`null` 时保留已有 section，空字符串时删除 section。`session_start` 的 `reload` 不立即清空已有 section，等新正文到达后再替换；这样扩展重载不会让模型短暂失去记忆。
4. 在 `before_agent_start` 中写入 section：

   ```js
   event.systemPromptOptions.sections['memctl'] = contextBody
   ```

   section 名称固定为 `memctl`，避免使用冒号或其他可能被 XML 标签解析影响的字符。正文为空时删除该 section，避免残留旧值。
5. 优先使用 section。只有在 `systemPromptOptions.sections` 不存在，或另一个扩展已经设置 `forceSystemPrompt` 时，才回退到返回 `{ systemPrompt: event.systemPrompt + "\n\n" + contextBody }`。回退路径应在日志中记录一次，便于诊断。
6. 不在正文中加入时间戳、同步原因或每次变化的随机值。正文应保持字节稳定，让 pi 的 section diff 只在内容真正变化时追加 transcript delta。正文是会话开始时的快照；后续 `sync` 的结果不会改写已注入的 section，需要最新同步状态时使用 memctl CLI 或 UI。
7. 不把记忆作为额外的 user message 注入。user message 会进入对话历史、占用 token，并可能与用户输入竞争；section 是更稳定的长期上下文通道。
8. 注入失败、`start` 超时、JSON 非法或二进制缺失时，记录错误并继续本轮；不伪造空上下文，也不把错误文本当作记忆正文。

注入正文的大小由 memctl 的 `MEMORY.md` 读取上限控制（见 [会话开始与上下文注入](./03-workflows-and-dataflows.md#3-会话开始与上下文注入)）。插件侧仍设置 `MAX_START_BYTES = 1 MiB` 的输出上限；超过上限时终止进程并记录错误，不截断后当成完整 JSON 解析。

### 6.5 每次 run 结束后的同步

- 主触发点是 `agent_settled`。它表示一次 run 已经结束，没有自动重试、压缩或排队续跑；此时触发同步不会读到未提交的半成品。
- 兼容旧版 pi：同时注册 `agent_end`，在 `ctx.isIdle()` 为真且没有 pending messages 时触发；用每个 run 的 settled 标记去重，避免 `agent_end` 和 `agent_settled` 重复同步。
- 同一会话同时只允许一个同步进程。新的 settled 事件只设置 `syncPending = true`；当前同步结束后再补一次，不建立无界队列。
- 同步参数固定为：

  ```text
  memctl sync --agent pi --sid <sessionId>
  ```

  同步进程不捕获 stdout；stderr 只保留尾部用于日志。失败、超时、锁忙或断网只记录，不改变 pi 会话状态。
- 同步进程属于当前会话；在 `session_shutdown` 时终止未完成的同步，由 `end` 执行最后一次收尾同步。

### 6.6 会话结束与重载

- `session_shutdown` 的 `quit`、`new`、`resume`、`fork` 代表旧会话运行时被释放，执行一次 `memctl end --agent pi --sid <sessionId>`。
- `end` 采用 `detached: true`、`unref()` 和忽略 stdio 的方式启动，最多等待 `END_JOIN_MS` 观察早期退出，然后返回；不等待完整网络同步。原因是 pi 会等待 shutdown handler，完整同步可能阻塞退出。
- `reload` 只清理定时器和未完成的子进程，不执行 `end`。同一会话在重载后继续运行，把重载当作会话结束会产生多余的同步。
- `end` 是幂等的后台同步，不释放仓库锁、不写入「会话已关闭」状态；重复调用不会改变会话语义。
- 崩溃、SIGKILL 和宿主提前退出不保证 `end` 执行；最后一次成功 `agent_settled` 的同步已经保留已提交内容，下一次 `start` 或 `sync` 会继续重试。

### 6.7 子代理与分叉

`header.parentSession` 不能作为子代理判断：`/fork`、`/clone` 和某些子代理都会设置该字段。按父会话一刀切跳过，会让分叉会话失去记忆。

`pi-subagents` 子进程使用 `PI_SUBAGENT_CHILD=1`，这是第三方约定，不是 pi 核心 API。v1 的默认策略是：

- 不跳过任何由 pi 正常启动的会话；分叉、子代理和恢复会话都获得自己的 SID 和生命周期。
- 子代理与父会话可能各自同步；memctl 的仓库锁会串行化写入，重复同步在无变化时是空操作。
- 如果实测发现子代理产生明显的重复提交或 token 浪费，再增加显式环境变量 `MEMCTL_PI_SKIP_SUBAGENTS=1`，只跳过 `PI_SUBAGENT_CHILD=1` 的会话；不使用 `parentSession` 自动推断。

这与 dsh 插件跳过子代理的策略不同。dsh 的子代理共享同一 agent 和记忆上下文；pi-subagents 的子代理是独立 pi session，需要独立判断，不能直接复制 dsh 的规则。

### 6.8 身份、二进制与配置

- agent 名固定为 `pi`；SID 使用 `ctx.sessionManager.getSessionId()`，通过 `--sid` 显式传递。
- 子进程的 `cwd` 使用 `ctx.sessionManager.getCwd()`，不依赖 pi 进程的启动目录；memctl 的仓库仍由 `AGENT_MEMORY_DIR` 决定。
- 不使用 `PI_SESSION_ID` 环境变量，不读取 `hooks.yaml`，不解析宿主注入的 JSON。
- memctl 路径按以下顺序解析：
  1. `MEMCTL_PI_BIN` 环境变量。
  2. `~/.agent-memory/bin/memctl`。
  3. `/opt/homebrew/bin/memctl`、`/usr/local/bin/memctl`。
  4. `PATH` 中的 `memctl`；Windows 下尝试 `memctl.exe`、`memctl.cmd`。
- 不提供任意命令配置，不读取项目 `.pi` 配置或 `hooks.yaml`。这消除了「配置文件即执行内容」的通用执行面。
- v1 不增加 pi Config schema。需要调整路径时使用 `MEMCTL_PI_BIN`；需要关闭接入时卸载包或使用显式的环境开关。
- 设备名、仓库路径、Git 凭据由 memctl 现有配置和环境决定，插件不重复解析。

### 6.9 进程、超时与输出

| 动作 | 调用 | 超时 | 输出 | 生命周期 |
| --- | --- | --- | --- | --- |
| 会话开始 | `start --agent pi --sid <id> --json` | `START_TIMEOUT_MS = 20 s` | stdout 上限 1 MiB；stderr 尾部 8 KiB | 后台 promise 等待；shutdown 时终止 |
| 首次注入等待 | 等待 `start` 完成 | `START_WAIT_MS = 8 s` | 无 | 超时后本轮跳过，下一次 prompt 再试 |
| 每次 run 同步 | `sync --agent pi --sid <id>` | `SYNC_TIMEOUT_MS = 15 s` | 忽略 stdout；stderr 保留尾部 | 后台；shutdown 时终止 |
| 会话收尾 | `end --agent pi --sid <id>` | `END_JOIN_MS = 1.5 s` 观察早期退出 | 忽略 | detached + unref；超时后由子进程继续 |

- 使用 `spawn(bin, args, { shell: false, windowsHide: true })`，不拼接命令字符串。
- POSIX 下超时先发 `SIGTERM`，宽限 `KILL_GRACE_MS = 1 s` 后发 `SIGKILL`；Windows 下先 `kill`，必要时使用 `taskkill /T /F`，并记录无法清理的残留。
- 每个会话使用一个 `AbortController` 管理 `start` 和 `sync`；`session_shutdown` 或扩展卸载时 abort，取消未完成的子进程，再启动 `end`。
- 插件卸载时对仍然运行的会话执行一次尽力 `end`，已完成的会话不重复；清理状态、监听器和定时器。
- 不在扩展工厂中启动进程、定时器或 watcher；长生命周期资源只从 `session_start` 或命令/工具中启动。

### 6.10 错误处理

| 场景 | 行为 |
| --- | --- |
| memctl 不存在或不可执行 | 记录一次错误，跳过注入和同步；会话继续；不每个事件重复尝试 |
| 记忆仓库未初始化 | `start` 返回可诊断错误；记录，不注入伪造上下文 |
| `start` 超时或 JSON 非法 | 终止进程，记录错误；不注册半截上下文；下一次 prompt 不重复等待已失败的启动 |
| `sync` 失败、超时或锁忙 | 记录；不阻塞下一轮；不建立重试风暴 |
| `end` 失败或进程提前退出 | 记录；不能保证崩溃或强杀场景一定执行 |
| 同一会话重复 `session_start` | 复用或替换状态；不重复注册监听器 |
| 旧 `pi-yaml-hooks` 条目仍存在 | 插件本身不删除；由适配器报告 `needs_attention`，由用户决定移除 |
| pi 版本不支持 `sections` | 回退到 `systemPrompt` 字符串；记录一次兼容性警告 |
| 子会话重复同步 | 由 memctl 锁串行化；实测后再决定是否跳过 |

所有事件监听器必须在内部捕获异常。`session_start` 或 `before_agent_start` 的异常不能让 pi 启动失败或本轮失败；`tool_call` 类失败安全规则不适用于本插件，因为本插件不拦截工具。

### 6.11 包清单

```json
{
  "name": "memctl-pi",
  "version": "0.5.0",
  "description": "Loads memctl memory at pi session start and synchronizes it after each turn.",
  "type": "module",
  "main": "./index.js",
  "exports": {
    ".": "./index.js"
  },
  "pi": {
    "extensions": ["./index.js"]
  },
  "files": [
    "index.js",
    "README.md"
  ],
  "engines": {
    "node": ">=22.19.0"
  },
  "peerDependencies": {
    "@earendil-works/pi-coding-agent": "*"
  },
  "keywords": [
    "pi-package",
    "pi-extension",
    "memctl",
    "memory",
    "agent-memory"
  ],
  "scripts": {}
}
```

包规则：

- 不声明 `dependencies`、`install`、`postinstall` 或 `prepare`。
- 不把 `@earendil-works/pi-coding-agent` 放进 `dependencies`；按 pi 包文档放在 `peerDependencies`，范围为 `*`。
- 不在包内放置测试、源码映射、缓存或凭据；`files` 只包含运行时文件。
- 不声明 `engines.pi`：当前 1.3.0 未发现对应的宿主引擎检查。宿主版本由适配器和测试矩阵记录，插件在运行时做能力探测。
- 版本与 memctl 主次版本保持一致；补丁版本可以独立。具体发布版本以发布轮次为准，示例中的 `0.5.0` 不是已发布版本。
- 许可证在发布前按仓库现有许可策略确认；当前仓库根没有 `LICENSE` 文件，不能擅自写成与仓库不一致的许可证。

### 6.12 与 dsh 插件的差异

| 维度 | `memctl-dsh` | `memctl-pi` |
| --- | --- | --- |
| 宿主包结构 | Bundle patch + locale + icon + ESM | 普通 npm 包 + `pi.extensions`，无 patch/locale/icon |
| 安装入口 | DSH Plugin Manager / `dsh plugin add` | `pi install npm:...` 或 `pi install <本地目录>` |
| 离线分发 | 内嵌 tarball 或目录 | 本地包目录；不做 tarball |
| 宿主版本门槛 | `engines.dsh` 明确拒绝 | 没有 `engines.pi`；适配器报告并测试 |
| 会话开始事件 | `agent/created` | `session_start` |
| 上下文通道 | `agent.ctx.systemPrompt.context()` | `systemPromptOptions.sections` |
| run 同步事件 | `session/event` 中的 `turn/end` | `agent_settled`（旧版回退 `agent_end`） |
| 收尾事件 | `agent/disposed` + 插件卸载时的收尾调用 | `session_shutdown`，reload 不执行 end |
| 子代理策略 | 跳过 `parentSession` 子代理 | v1 不跳过；需要时使用 `PI_SUBAGENT_CHILD=1` 显式开关 |
| 收尾等待 | detached + unref | 同样 detached + unref，但 `session_shutdown` 被 pi 等待，需有界观察 |

## 7. memctl 适配器改动

### 7.1 探测

pi 适配器当前把 `pi-yaml-hooks` 作为必需插件，并管理 `$HOME/.pi/agent/hook/hooks.yaml`。新增原生插件后，探测需要同时覆盖：

- npm 安装：`$HOME/.pi/agent/npm/node_modules/memctl-pi/package.json`。
- 本地目录安装：`memctl pi plugin-dir` 默认写到 `$STATE/pi/`；适配器在默认状态根下探测 `$HOME/.agent-memory/pi/package.json`。
- 自定义本地路径：读取 `$HOME/.pi/agent/settings.json` 的 `packages` 数组，兼容字符串和 `{source}` 对象两种形式；相对路径按 settings 文件所在目录解析，再读取目标的 `package.json`；只读全局设置，不读取未信任的项目 `.pi/settings.json`。
- 旧路径：`$HOME/.pi/agent/hook/hooks.yaml`，并解析其中 `id` 以 `agent-memory-` 开头的条目。

不能只因为 `pi-yaml-hooks` 包存在就认定旧接入仍在使用；用户可能只是用它配置了其他 hook。旧路径的判定必须基于 `agent-memory-*` 条目的实际存在。

适配器默认按 `$HOME/.pi/agent` 探测。`PI_CODING_AGENT_DIR` 适用于隔离测试或自定义安装；v1 不自动跟随该环境变量，避免把测试环境误当成用户环境。旧插件版本仍按现有 npm manifest 探测；本地安装的 `pi-yaml-hooks` 可能没有可读 manifest，版本显示未知，但 `hooks.yaml` 中的条目仍可判定。

`PluginSpec` 当前只支持 manifest 路径和最低版本。dsh 适配器已经通过 `NativePlugin` 字段支持原生插件，pi 可以复用这一结构；仍需补充 settings 解析、旧配置内容探测和宿主版本判断。pi 适配器需要扩展为：

- 一个原生插件候选列表，至少包含 npm manifest 和默认状态根 manifest；
- 可选的 settings 解析，用于本地目录安装；
- 可选的旧配置内容探测，用于区分「插件存在但没有 memctl 条目」和「旧接入仍在使用」；
- 可选的宿主最低版本，用于 pi 这类没有 `engines.pi` 的宿主。

`internal/adapters/install.go` 的原生插件分支当前包含 dsh 专用文案和「dsh hooks.json」判断。实现 pi 适配器时需要按 adapter 提供提示文本和旧路径名称，不能复用 dsh 的字符串，也不能把 `hooks.yaml` 写成所有宿主的通用文件名。

### 7.2 状态

| 探测结果 | 状态 | 说明 |
| --- | --- | --- |
| pi 可用，原生 `memctl-pi` 存在，旧条目不存在 | `configured` | 不需要 `hooks.yaml`；首次真实验证后才是 `verified` |
| pi 可用，只有旧 `agent-memory-*` 条目，且 `pi-yaml-hooks` 已安装 | `configured` | 标记 legacy，提示迁移；不自动删除 |
| pi 可用，旧条目存在但 `pi-yaml-hooks` 未安装 | `needs_attention` | 旧配置不会生效；提示安装旧插件或迁移到 `memctl-pi` |
| pi 可用，原生插件和旧条目同时存在 | `needs_attention` | 重复注入/同步风险；提示用户移除一套 |
| pi 可用，两者都不存在 | `needs_attention` | 输出 npm 包名和本地目录安装提示 |
| pi 不存在 | `not_installed` | 不写任何配置 |
| 原生插件版本低于最低支持版本 | `needs_attention` | 提示升级 |
| pi 版本低于测试下界或无法解析 | `needs_attention` / `unsupported` | 不把未验证组合报告为 `configured` |
| `hooks.yaml` 无法解析 | `needs_attention` | 不写入，输出人工处理提示 |

`AdapterStatus.capabilities` 对原生插件报告 `plugin`；旧路径报告 `hooks`。原生插件存在时 `artifacts` 为空，不生成 `hooks.yaml` 计划。

### 7.3 安装计划与提示

- 原生插件已存在：`install pi` 返回空计划，说明「memctl-pi 已安装，不需要 hooks.yaml」。
- 原生插件缺失、旧条目存在且 `pi-yaml-hooks` 已安装：保留旧条目并标记 deprecated；可以继续幂等更新旧条目，但提示迁移到 `memctl-pi`。
- 原生插件缺失、旧条目存在但 `pi-yaml-hooks` 未安装：报告 `needs_attention`，提示安装旧插件或迁移到 `memctl-pi`；不默认写 `hooks.yaml`。
- 原生插件缺失、旧条目不存在：返回不可应用的空计划，输出安装提示，不创建 `hooks.yaml`。这是与当前行为的关键变化。
- 安装提示至少包含：

  ```text
  npm:   pi install npm:memctl-pi@<version>
  离线:  memctl pi plugin-dir
         pi install <输出的绝对目录>
  校验:  pi list
  ```

- 不猜测 pi 的 profile 或 settings 路径；不自动执行 `pi install`，不写 `settings.json`，不修改 `hooks.yaml` 之外的宿主配置。
- `install --dry-run` 与 `install --status` 只报告计划和探测结果，不写文件。

### 7.4 旧路径共存

- 旧 `hooks.yaml` 中的 `agent-memory-context`、`agent-memory-sync`、`agent-memory-end` 由现有合并逻辑管理，保持幂等。
- 原生插件安装后，旧条目仍然存在时报告 `needs_attention`；不自动删除、不自动注释、不自动迁移。
- 用户移除旧条目的方式由用户决定；可以手工编辑，也可以在后续版本提供显式的 `--remove-legacy`，但 v1 不增加自动删除。
- `pi-yaml-hooks` 包本身可以继续保留用于其他 hook；memctl 只关心 `agent-memory-*` 条目，不把整个包当作冲突。

### 7.5 契约改动清单

实现时已同步以下契约：

- [目录结构与依赖](./01-architecture-and-modules.md#5-目录结构与依赖)：新增 `plugins/pi/`。
- [模型与契约](./02-domain-models-and-contracts.md#23-审计诊断整理与接入)：`AdapterStatus` 的 pi 原生插件语义；必要时增加宿主版本字段。
- [流程与恢复](./03-workflows-and-dataflows.md#7-agent-配置安装)：pi 原生插件优先、旧路径 legacy、安装提示。
- [CLI 与 UI](./04-ui-and-cli-specification.md#1-cli-命令)：新增 `memctl pi plugin-dir`；明确 `install pi` 不再默认创建 `hooks.yaml`，只有旧条目已存在时才继续幂等更新。
- [验收与交付](./05-validation-and-delivery.md)：把 §9.5 的候选 ID 写入正式矩阵。
- [单文件打包](./04-ui-and-cli-specification.md#6-单文件打包)：内嵌 pi 插件源文件；发布产物增加 npm 包或本地目录源。

## 8. 分发与发布

### 8.1 npm 主通道

```sh
pi install npm:memctl-pi@<version>
```

- npm 包名 `memctl-pi` 在调研时通过 `npm view` 检查为未占用；发布前需要复查名称和权限。
- 使用精确版本；需要镜像时由用户在 pi 的安装源中选择。
- 包不声明安装脚本和运行时依赖；安装时不需要编译或授权构建脚本。
- 包版本与 memctl 主次版本保持一致；`peerDependencies` 只声明 pi 宿主包 `*`，不打包宿主包。
- 发布使用 npm provenance（可用时）和精确版本；Release 附带 `npm pack` 产物用于审计。

### 8.2 本地目录备用

```sh
memctl pi plugin-dir
pi install /absolute/path/printed/by/the/command
```

- `memctl pi plugin-dir` 把 `plugins/pi/` 的运行时文件写入 `$STATE/pi/`，输出绝对目录；目录包含 `package.json`、`index.js` 和 `README.md`。实现不复用 `plugins/dsh/embed.go` 的 tar/gzip 逻辑，只做原子文件写入和目录版本检查。
- `pi install` 对本地目录按包发现规则加载，不复制文件；目录必须长期存在。
- 目录使用稳定路径，不带版本号；memctl 升级时覆盖 `package.json` 和 `index.js`，pi 下次启动读取新内容，不需要重新安装。
- 目录必须可读；不要把 `$STATE` 放在临时目录或会被清理的位置。
- 单文件 `pi install /absolute/file.ts` 或 `.js` 可以用于开发探针，但不作为正式离线通道；目录形式与 npm 包共享同一份 `package.json`，便于适配器探测。

### 8.3 不采用 tarball

pi 的包安装文档列出的来源是 npm、git 和本地路径；当前 1.3.0 的安装实现把本地路径按文件或目录加载，不按 npm tarball 解包。把 dsh 的 `.tgz` 路径直接粘贴到 `pi install` 不会得到可加载的包，因此：

- `memctl pi` 不提供 `plugin-path` 的 tarball 形式。
- Release 可以附带 `memctl-pi-<version>.tgz` 作为 npm 审计产物，但安装说明必须使用 npm 包名或已解包的本地目录。
- 文档和帮助信息不得把 `.tgz` 写成 pi 的可用安装输入。

### 8.4 版本、CI 与发布物

- 插件版本与 memctl 主次版本保持一致；补丁独立。版本矩阵至少记录 `pi@1.3.0`，并逐步加入更低和更高的测试版本。
- 兼容下界暂定 `0.86.1`：该版本同时具备 `systemPromptOptions.sections` 与 `agent_settled`，是暂定的最低验证版本；更低版本需要字符串回退和 `agent_end` 回退，未验证前不宣称支持。
- 适配器记录 pi 宿主版本和插件版本；低于下界时报告 `needs_attention`，不报告 `configured`。
- CI 增加：
  1. `node --test plugins/pi/test/*.test.js`。
  2. `npm pack --dry-run` 文件清单检查：只包含运行时文件，无测试、无依赖、无安装脚本。
  3. 隔离 `PI_CODING_AGENT_DIR` 下 `pi install <本地目录>`、`pi list` 和 `pi --help` 加载检查。
  4. 适配器状态矩阵测试：原生、旧路径、共存、缺失、版本过低。
- `make pi-plugin-test`、`make pi-plugin-pack` 与现有 dsh 入口并列；`make pi-plugin-pack` 生成 `dist/memctl-pi-<version>.tgz`，用于 npm 发布和审计；本地目录源由 `memctl pi plugin-dir` 在运行时生成，不提交到仓库。
- npm 发布失败不应导致二进制 Release 失败；两类产物分别报告状态。

### 8.5 安装、升级与卸载

| 通道 | 安装 | 升级 | 卸载 |
| --- | --- | --- | --- |
| npm | `pi install npm:memctl-pi@<version>` | 重新安装新的精确版本，或使用 `pi update --extensions` | `pi remove npm:memctl-pi` |
| 本地目录 | `memctl pi plugin-dir`，再 `pi install <绝对目录>` | 升级 memctl 后覆盖目录；pi 下次启动读取新内容 | `pi remove <绝对目录>` |

- 推荐全局安装。项目级 `pi install -l` 需要项目信任，并会把包声明写入 `.pi/settings.json`；v1 不作为默认通道。
- 卸载只移除包和 settings 声明；记忆仓库、状态目录和 memctl 二进制保留。
- 本地目录被删除或移动后，pi 无法加载；卸载或迁移前先确认目录仍存在。
- 旧 `pi-yaml-hooks` 条目不会因卸载 `memctl-pi` 自动恢复或删除；两套接入的清理仍由用户决定。

## 9. 验证与验收

### 9.1 单元测试

使用假 `pi` API、假 `ctx`、假 `spawn` 和假 `memctl` 覆盖：

- `session_start` 取 SID、启动 `start`、缓存正文、首次 prompt 等待有界。
- `before_agent_start` 写入 `sections["memctl"]`；正文不变不产生新 delta；`sections` 缺失时回退到 `systemPrompt`。
- `agent_settled` 触发一次同步；并发 settled 只保留一个 pending；`agent_end` 回退不重复。
- `session_shutdown` 的 `quit/new/resume/fork` 触发 `end`，`reload` 不触发。
- `end` detached + unref，不阻塞 handler；启动和同步子进程在 shutdown 时被取消。
- 二进制缺失、非零退出、超时、非法 JSON、输出超限、锁忙、断网都不抛出宿主异常。
- `PI_SUBAGENT_CHILD=1` 的显式开关行为（若实现）。
- 扩展卸载清理监听器、子进程、定时器和 session 状态。

### 9.2 包与安装测试

- `npm pack` 后检查 `files`：只包含 `index.js`、`README.md` 等运行时文件。
- `package.json` 无 `dependencies`、`scripts.install`、`scripts.postinstall`、`scripts.prepare`；`pi.extensions` 指向存在的入口。
- 在隔离 `PI_CODING_AGENT_DIR` 中执行 `pi install /absolute/package-dir`，再执行 `pi list` 和 `pi --help` 检查扩展已加载；删除源目录后不宣称仍可加载。
- 在隔离环境中执行 `pi remove`，确认 settings 条目移除，记忆仓库不变。
- 检查 `memctl pi plugin-dir` 输出的目录存在、包含 `package.json`、版本与 memctl 一致。
- 检查 npm 包名语法、版本和 `pi-package` 关键字；发布前确认名称可用。

### 9.3 隔离宿主测试

使用隔离 `PI_CODING_AGENT_DIR`、临时记忆仓库、假 memctl 二进制和本地 mock OpenAI 兼容端点：

1. 启动 pi，确认 `session_start` 触发 `start`，参数为 `--agent pi --sid <id> --json`。
2. 确认首个模型请求的 system prompt 包含 `[agent-memory]` 或 `<memctl>` section。
3. 确认 `agent_settled` 触发一次 `sync`，重复 settled 不产生无界进程。
4. 确认 `session_shutdown` 触发 `end`；`reload` 不触发。
5. 确认 `start` 超时或二进制缺失时，模型请求仍能完成，日志有明确错误。
6. 确认恢复会话、`/new`、`/fork` 的 SID 和 section 行为符合预期。

本机调研探针已经验证了事件顺序、section 注入和本地包安装；实现时应把探针固化为可重复测试，而不是依赖一次性命令。

### 9.4 真实宿主测试

环境：macOS arm64，pi `1.3.0` 或声明支持的版本；隔离 `HOME` 或 `PI_CODING_AGENT_DIR`；临时记忆仓库；可控远端；真实模型或可复现的本地模型端点。

至少验证：

1. 新会话首个模型请求前，system prompt 包含 `[agent-memory]` 内容；会话日志可复核。
2. 一轮对话结束后，`git -C <repo> log -1` 出现 memctl 提交（有记忆变更时），或同步缓存显示已同步（无变更时）；没有重复同步。
3. 关闭 pi 后 `end` 被调用；强杀/崩溃场景记录为未保证。
4. 恢复旧会话时重新注入或更新 section，不重复创建记忆提交。
5. 子代理、分叉和新会话的 SID、注入与同步行为符合 §6.7。
6. memctl 不存在、仓库未初始化、断网、锁忙时，pi 仍能正常使用，日志有明确错误。
7. 旧 `hooks.yaml` 与原生插件共存时，适配器报告 `needs_attention`，不自动删除。
8. 移除旧条目后，不再重复注入/同步。
9. 原生插件版本过低或 pi 版本低于下界时，适配器给出可诊断状态，pi 不崩溃。

真实模型回复质量、注入对任务成功率的影响和 token 成本不在本方案的验收范围内；需要单独的性能与效果测量。

### 9.5 验收 ID

以下 ID 已写入 [验收与交付](./05-validation-and-delivery.md#2-核心测试矩阵)，是正式验收契约。

| ID | 场景 | 预期 |
| --- | --- | --- |
| `H12` | pi 原生插件生命周期 | `session_start` 注入、`agent_settled` 同步、`session_shutdown` 收尾各一次；`reload` 不执行 `end` |
| `H13` | pi 上下文注入与缓存稳定 | 首个请求存在 `[agent-memory]` section；正文不变不产生新 delta；不伪造 user message；`forceSystemPrompt` 时有回退 |
| `H14` | pi 并发与失败 | 多轮快速结束只保留一个有界同步；二进制缺失、超时、非法 JSON、锁忙、断网不阻塞会话 |
| `H15` | pi 会话边界与子代理 | `new/resume/fork` 使用正确 SID；子代理不因 `parentSession` 被误跳过；显式开关行为可验证 |
| `I07` | pi 原生插件探测 | 原生存在时 `configured` 且无 `hooks.yaml` 计划；原生与旧条目共存时 `needs_attention` |
| `I08` | pi 安装计划与提示 | 原生缺失且无旧条目时不写 `hooks.yaml`，输出 npm 与本地目录提示；旧条目保留并标记 legacy；旧条目存在但插件缺失时 `needs_attention` |
| `I09` | pi 包安装 | `npm pack` 文件清单正确；隔离 `PI_CODING_AGENT_DIR` 下本地目录安装、`pi list`、卸载可用；无依赖和安装脚本 |
| `I10` | pi 版本与降级 | 低于下界或无法解析的 pi 版本不被报告为 `configured`；`sections` 缺失时回退，不静默失效 |

### 9.6 建议的实施顺序

已作为阶段 F 登记到 [验收与交付](./05-validation-and-delivery.md#阶段-f-的实施顺序)：

| 步骤 | 内容 | 完成条件 |
| --- | --- | --- |
| `F-1` | 契约与骨架 | 更新 01、02、03、04、05；`plugins/pi/` 包清单、入口、单元测试骨架；`npm pack` 文件清单正确 |
| `F-2` | 生命周期与注入 | `session_start`、`before_agent_start`、`agent_settled`、`session_shutdown` 接线；假宿主测试覆盖 H12 至 H15 |
| `F-3` | 适配器探测与状态 | 原生插件、旧条目、settings 本地路径、版本门槛；`install pi` 计划与提示；覆盖 I07 至 I08、I10 |
| `F-4` | 分发与发布 | `memctl pi plugin-dir`、`go:embed`、`make pi-plugin-*`、npm/Release 产物；覆盖 I09 |
| `F-5` | 真实宿主验证 | 真实 pi 会话的注入、同步、收尾、恢复、失败降级；覆盖 H12 至 H15 的真实宿主部分；未验证项如实记录 |

实施前需要确认 `§10.2` 的决策；未确认的决策不能由实现代码静默选择。

## 10. 风险与待确认

### 10.1 风险

| 风险 | 影响 | 应对 |
| --- | --- | --- |
| pi 扩展 API 快速演进 | 事件、section 或 shutdown 语义变化 | 固定测试版本；`sections` 缺失时回退；适配器报告宿主版本；每次升级跑真实宿主验证 |
| 没有 `engines.pi` | 旧版 pi 可能加载插件但不触发事件 | 适配器报告版本；插件做能力探测；真实矩阵记录已验证版本 |
| `session_shutdown` 被 pi 等待 | 网络同步可能阻塞退出 | `end` 使用 detached + unref；启动和同步有超时；shutdown 只做有界清理 |
| 旧 `hooks.yaml` 与原生插件共存 | 重复注入、重复同步 | 内容级探测 `agent-memory-*`；`needs_attention`；不自动删除 |
| `parentSession` 不能区分 fork 和子代理 | 误跳过分叉会话 | v1 不按 `parentSession` 跳过；需要时使用 `PI_SUBAGENT_CHILD=1` 显式开关 |
| 上下文过大 | token 成本和压缩成本上升 | 复用 memctl 的 `MEMORY.md` 上限；section 只注入一次；不做每轮重复拼接 |
| 正文变化导致 section delta | 缓存失效 | 快照式正文；避免时间戳和易变字段；只在会话开始或显式刷新时更新 |
| 原生插件在进程内执行 | 安装即信任，扩展可访问会话和凭据 | 零依赖、固定二进制和参数、无任意命令配置、发布源码和校验和 |
| OMP 不在范围内 | OMP 用户仍需要 YAML 路径 | 保留 legacy；另立 OMP 方案，不把 `memctl-pi` 宣称为跨宿主 |
| npm 名称或发布权限变化 | 主通道不可用 | 发布前复查；本地目录备用可独立工作 |
| Windows 进程清理差异 | 子进程残留或退出变慢 | 分平台测试 `kill`/`taskkill`；未验证平台不报告 `verified` |
| 真实宿主未验证 | 只能算设计可行，不能算已支持 | 完成 §9.4 后再标记 `verified` |

### 10.2 决策

以下决策按建议确认（2026-10-18，用户指令「完成开发」），实现以本节为准。

| ID | 事项 | 建议 | 影响 |
| --- | --- | --- | --- |
| `D1` | 是否新增原生插件 | 是，作为 pi 默认接入 | 决定是否进入 F 阶段 |
| `D2` | 是否保留 `pi-yaml-hooks` | 保留为 legacy，不自动删除 | 影响迁移和 OMP |
| `D3` | 分发组合 | npm 主通道 + 本地目录备用 | 影响发布工程和离线能力 |
| `D4` | 注入频率 | 会话开始快照，section 注入；每轮新鲜度作为后续选项 | 影响 token、缓存和进程延迟 |
| `D5` | 子代理策略 | v1 不跳过；需要时用 `PI_SUBAGENT_CHILD=1` 显式开关 | 影响子代理记忆和重复同步 |
| `D6` | 旧路径迁移 | 不自动删除；`needs_attention` + 手工移除 | 影响用户配置所有权 |
| `D7` | 插件版本 | 与 memctl 主次版本保持一致，补丁独立 | 影响兼容矩阵和发布节奏 |
| `D8` | 许可证 | 发布前按仓库策略确认 | 影响 npm 发布和再分发 |

## 11. 实现状态

已实现（`F-1` 至 `F-4`，2026-10-18）：

- `plugins/pi/`：`package.json`、单文件 ESM `index.js`、`README.md`、Node 单元测试和 `embed.go`。入口只注册 `session_start`、`before_agent_start`、`agent_settled`、`agent_end` 与 `session_shutdown`，进程和定时器只从 `session_start` 启动。
- 生命周期：`start` 后台执行并有 20 秒上限；首个 `before_agent_start` 最多等待 8 秒；正文写入 `sections["memctl"]`，`sections` 缺失或 `forceSystemPrompt` 时回退到替换 `systemPrompt`；`agent_settled` 与 `agent_end` 共用一个 pending 去重；`session_shutdown` 的 `reload` 只清理子进程，其余 reason 分离执行 `end` 并最多等待 1.5 秒。
- 适配器：`memctl-pi` 从 npm 清单、`$STATE/pi/package.json` 和 `~/.pi/agent/settings.json` 的字符串或 `{source}` 本地包声明探测；旧路径按 `hooks.yaml` 中实际的 `agent-memory-*` 条目判定；`pi` 宿主下界 `0.86.1`、原生插件最低版本 `0.5.0`；原生存在时 `artifacts` 为空，`capabilities` 为 `[plugin]`。
- 分发：`memctl pi plugin-dir` 从 `go:embed` 写到 `$STATE/pi/`，输出稳定绝对路径；`make pi-plugin-test`、`make pi-plugin-pack`；`scripts/release.sh` 附带 `memctl-pi-<version>.tgz` 供 npm 发布和审计，不把它写成 pi 安装输入。

已验证：

- `node --test plugins/pi/test/*.test.js` 15 项通过，覆盖注入、缓存稳定、回退、等待上限、同步去重、`reload` 不收尾、子进程取消、缺失二进制、非法 JSON、超时、输出超限与 `dispose`。
- `go test ./internal/... ./plugins/... -count=1` 与 `go test ./tests/integration -count=1` 通过，覆盖 `I07`、`I08`、`I10` 的合成状态矩阵。
- 隔离 `PI_CODING_AGENT_DIR` 下 `pi install <plugins/pi>`、`pi list`、`pi remove` 可用；settings 写入相对路径后适配器仍能探测到原生插件。
- 隔离 pi `1.3.0` 会话使用本地 mock OpenAI 兼容端点和假 memctl：观察到 `start` → 模型请求包含 `<memctl>` section → 进程退出触发 `end`；`agent_settled` 的 `sync` 在 `-p` 立即退出时被 shutdown 取消，最终同步由 `end` 承担，假 memctl 只验证了调用参数。
- 本机真实 `install --status`：存在 `pi-yaml-hooks` 和旧 `agent-memory-*` 条目时报告 `configured`、`plugin@2026.9.26`，并提示迁移。

未验证：

- 真实 pi 会话与真实模型端点下的注入、同步提交、恢复、分叉和子代理行为；`-p` 模式没有给 `sync` 留出完成时间。
- npm 发布、包名最终可用性、许可证选择和真实模型效果。
- Windows、Linux、其他 pi 版本及其进程清理。
- `pi` 版本无法解析与 `sections` 缺失的宿主级回退只由单元测试覆盖。

因此当前状态是「已实现、隔离宿主验证通过」，不是「已支持 pi」。完成 §9.4 的真实会话验证后，才能在 [开发状态](./STATUS.md) 中标记 `verified`。

## 12. 参考

### 12.1 pi 官方

- [pi Extensions](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/extensions.md)
- [pi Packages](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/packages.md)
- [pi Sessions and Context](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/sessions.md)
- [pi Session File Format](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/session-format.md)
- [pi Environment Variables](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/docs/environment-variables.md)
- [pi Extension Types](https://github.com/earendil-works/pi/blob/main/packages/coding-agent/src/core/extensions/types.ts)
- [pi 内置 MCP 扩展](https://github.com/earendil-works/pi/tree/main/packages/coding-agent/src/extensions/mcp)
- [pi Package Gallery](https://pi.dev/packages)

### 12.2 邻近项目

- [pi-yaml-hooks](https://github.com/KristjanPikhof/pi-yaml-hooks)
- [pi-subagents](https://github.com/nicobailon/pi-subagents)
- [Oh My Pi (OMP)](https://github.com/can1357/oh-my-pi)

### 12.3 记忆类项目

- [pi-memory (jayzeng)](https://github.com/jayzeng/pi-memory)
- [@askjo/pi-mem](https://github.com/jo-inc/pi-mem)
- [@shog-lab/pi-memory / pi-mind](https://github.com/shog-lab/pi-mind)
- [pi-hermes-memory](https://github.com/chandra447/pi-hermes-memory)
- [dsh-context-memory](https://github.com/hj01857655/dsh-plugins/tree/main/packages/dsh-context-memory)
- [claude-mem](https://github.com/thedotmack/claude-mem)
- [basic-memory](https://github.com/basicmachines-co/basic-memory)

### 12.4 本项目

- [dsh 专用插件](./07-dsh-plugin.md)
- [Agent 配置安装](./03-workflows-and-dataflows.md#7-agent-配置安装)
- [宿主生命周期事件映射](./04-ui-and-cli-specification.md#32-宿主生命周期事件映射)
- [验收与交付](./05-validation-and-delivery.md)
- [目录结构与依赖](./01-architecture-and-modules.md#5-目录结构与依赖)
- [开发状态](./STATUS.md)
