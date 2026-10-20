# memctl

`memctl` 是管理 Agent 记忆仓库的单文件 Go 二进制。记忆以 Markdown 保存在独立的 Git 仓库，`memctl` 负责受管读写、本地提交与远端同步、Hook 上下文注入、内嵌 Web UI，以及需要人工确认的 Dreaming 整理（在记忆仓库的 GitHub Actions 中运行）。

当前版本 `v0.9.0`（2026-10-20 发布，新增记忆过期规则：`until` 到期条目在四个读取面统一隐藏、`EXPIRED_ENTRY` 诊断与 `DELETION_DEFERRED` 删除闸门；`v0.8.0` 于 2026-10-19 发布，新增「整理」与「客户端接入」两个独立视图、`GET /api/v1/mcp` 的 MCP 通道状态展示与审计视图修正；`v0.7.3` 新增工作流版本漂移拦截 `WORKFLOW_STALE`，`v0.7.0` 把 Dreaming 迁移到 GitHub Actions 远端整理并在本地删除 runner 与平台沙箱），阶段 A–D 的能力已全部交付；阶段 E 的 DSH 插件已发布，真实会话验证完成到 `v0.4.0`，npm 包名界面安装与 `desktop` profile `0.5.1` 已复验，`0.5.2` 完成真实会话复验，`0.5.3` 修复了非零退出的 `start` 丢弃上下文并已发布到 npm，`agent/disposed` 事件受宿主限制。阶段 F 的 pi 插件已随 `v0.5.0` 发布；两个插件自 2026-10-20 起统一为 `0.5.2` 并随 `v0.9.0` Release 附带（`memctl-dsh` 已单独发布 `0.5.3` 到 npm，未进 Release），pi `1.3.0`/darwin arm64 完成真实会话验证（注入、每轮同步、收尾、恢复、分叉、后台子代理与失败降级）；前台子代理不注入记忆。`version`、`help`、`install`、`dsh`、`pi`、`tag` 与 `update` 不访问记忆仓库，需要仓库的命令运行时依赖系统 Git；Node.js 仅用于前端构建，Dreaming 整理使用记忆仓库远端 GitHub Actions 中的模型提供商，本地不读取模型凭据。各平台与宿主组合的验证状态见 [平台与限制](#平台与限制)。

## 快速开始

从 [GitHub Releases](https://github.com/gray0128/memctl/releases) 下载对应平台的候选二进制（`memctl-darwin-arm64`、`memctl-darwin-amd64`、`memctl-linux-amd64`、`memctl-windows-amd64.exe`），或在本地执行 `make build` 生成 `bin/memctl`、执行 `make install` 安装到 `~/.agent-memory/bin/memctl`。

```bash
export AGENT_MEMORY_DIR=$HOME/memory   # 默认 ~/memory；仓库由 init 创建
./bin/memctl init                      # 无 URL 时只创建本地仓库与首笔提交
./bin/memctl read MEMORY.md --json     # 返回 {filePath, fileRevision, rawContent}

# 受管写入以 read --json 返回的 fileRevision 为基线；旧基线返回 REVISION_MISMATCH
printf '# Memory\n\n## Index\n- new entry\n' \
  | ./bin/memctl write MEMORY.md --base <fileRevision> --stdin

# 本地 Web UI：仅绑定 127.0.0.1，默认端口 18420，首页注入 Session Token
./bin/memctl ui --no-open

# 自更新：先只读检查；去掉 --check 才会下载、校验并替换当前二进制
./bin/memctl update --check
```

示例中的 `./bin/memctl` 指本地 `make build` 的产物，使用下载的二进制时替换为实际路径。接入远端（`init <url> --confirm-private`）、注册宿主 Hook 与使用整理能力都需要额外参数或前置条件，见下文「能做什么」与对应契约。

DSH 使用专用 Host 插件 `memctl-dsh`，可通过 npm 安装，也可使用二进制内嵌的 tarball：

```bash
dsh plugin --profile <profile> add memctl-dsh@0.5.3

# 离线备用
./bin/memctl dsh plugin-path   # 输出绝对 .tgz 路径
# 在 DSH：插件 → 添加插件 → 粘贴该路径
```

`memctl install --status` 只探测 `memctl-dsh` 与旧 `dsh-hooks-plugin`，不会修改 DSH profile，也不会在原生插件存在时写入 `hooks.json`。安装、升级与卸载由 DSH Plugin Manager 完成，详见 [dsh 专用插件](./docs/07-dsh-plugin.md)。

pi 的原生插件 `memctl-pi` 通过 npm 安装，也可使用离线本地目录：

```bash
pi install npm:memctl-pi@0.5.2

# 离线备用
./bin/memctl pi plugin-dir       # 输出稳定的绝对目录
pi install /absolute/path/printed/by/the/command
pi list
```

`memctl install --status` 探测 npm、状态目录与 pi settings 中的本地包；只有 `hooks.yaml` 仍有 `agent-memory-*` 条目时才把旧 `pi-yaml-hooks` 路径视为在用，且不会自动创建或删除这些条目。详见 [pi 专用插件](./docs/08-pi-plugin.md)。

受沙箱限制的宿主要让 Agent 主动写记忆，需要 `memctl mcp`：一个在标准输入输出上运行 MCP 的工具进程，由宿主启动，因此不受 Agent 的 bash 沙箱限制：

```bash
./bin/memctl install --status     # 末尾输出 mcp: 块与每个宿主的注册命令
# pi 为例；--exposure direct 不能省略，否则模型看不到工具
pi mcp add memctl --exposure direct -- ~/.agent-memory/bin/memctl mcp --agent pi
```

工具为 `memory_entries`（按关键词检索已存条目）、`memory_read` 与 `memory_write`，写入仍经过受管事务与基线校验。宿主侧还有以下前置条件：Codex 需要 `default_tools_approval_mode = "approve"`，否则审批策略为 `never` 时每次调用都会被拒绝；pi 需要 `--exposure direct`；Grok 的 `workspace` 沙箱会阻断 MCP 子进程，需要把记忆仓库加入 `read_write`，而该授权同时放开 shell；Antigravity 在 `--sandbox` 下可用，但记忆仓库不在工作区时需在 MCP 条目里用 `env` 指定 `AGENT_MEMORY_DIR`；DSH 没有 `mcp add` 子命令，需要在 profile 的 `cordis.patch.yml` 里用 `insert:` 追加一条 `@deepseek-ai/dsh-mcp-client` 条目（该包随应用分发，profile 不需要安装）。详见 [MCP 记忆工具](./docs/09-mcp-memory-tools.md#52-各宿主的注册命令)。

## 帮助

`memctl help` 输出面向 Agent 的概览：快速开始、使用规则、命令分组、环境变量、退出码和语言选择。单命令页包含用法、参数、输入、输出、退出码、示例和相关命令：

```bash
./bin/memctl help                 # 概览
./bin/memctl help write           # 单命令页
./bin/memctl write --help         # 等价写法
./bin/memctl help --json          # schemaVersion 1 的机器可读目录
MEMCTL_LANG=zh ./bin/memctl help  # 中文帮助；也可用 --lang zh
```

帮助语言按 `--lang` → `MEMCTL_LANG` → 第一个非空的 `LC_ALL`/`LC_MESSAGES`/`LANG` → 英文选择。只本地化帮助文本，JSON 字段、命令输出键和错误码保持英文。帮助不访问记忆仓库、Git、Node.js 或网络。完整契约见 [面向 Agent 的帮助与中英 i18n](./docs/04-ui-and-cli-specification.md#13-面向-agent-的帮助与中英-i18n)。

## 配套 Skill

[`skills/memctl/`](./skills/memctl/SKILL.md) 是面向 Agent 的使用 Skill，覆盖记忆读取、受管写入与基线冲突、同步与冲突恢复，以及沙箱宿主改用 MCP 工具。在线安装：

```bash
npx skills add gray0128/memctl -g -s memctl -a codex pi opencode claude-code -y
```

按需替换 `-a` 的宿主列表；读取 `~/.agents/skills` 的宿主（如 DSH）无需单独指定。也可把该目录复制或软链接到宿主扫描的 skills 目录，例如 `~/.agents/skills/memctl`；`SKILL.md` 是入口，其余文件按需加载。

## 能做什么

| 能力 | 命令 | 说明 |
| --- | --- | --- |
| 仓库初始化 | `init`、`init <url> --confirm-private` | 无 URL 只建本地仓库与首笔提交；带 URL 需确认私有性与所有权，空远端完成首推，已有历史则克隆并建立 upstream |
| 读取与受管写入 | `read`、`write` | 读写文件原文；写入校验修订基线并原子提交，不静默覆盖其他更新 |
| 串行编辑与恢复 | `sync --capture`、`recover --status`、`recover --rollback` | 显式接收串行外部编辑；查看或回滚本工具未完成的事务，不接管外部 Git 操作 |
| 同步与冲突 | `sync --wait`、`resolve` | 前台同步与多阶段冲突解决；断网不撤销已保存的本地内容 |
| 会话上下文 | `start`、`context`、`pull`、`tag`、`wrap` | Hook 注入、会话级去重与来源标签；`wrap` 透传被包装程序的退出码与信号 |
| 诊断与修复 | `doctor`、`doctor --fix` | 只读诊断死链、孤立文件、入口膨胀与运行状态；修复只重建缺失的入口文件 |
| Web UI | `ui` | 概览、知识图谱、条目、冲突解决、审计、诊断、整理、客户端接入 8 个视图；只读模式在服务端拒绝写请求 |
| Agent 接入 | `install [agent...]` | 探测宿主与插件版本，预览或应用配置合并；不自动安装第三方插件，不改全局 Git 配置 |
| DSH 插件 | `dsh plugin-path`、`dsh plugin-dir` | 输出内嵌 `memctl-dsh` 的绝对 tarball 或目录路径；npm 包为 `memctl-dsh@0.5.3`；只写状态目录，不修改 DSH profile |
| pi 插件 | `pi plugin-dir` | 输出内嵌 `memctl-pi` 的绝对本地包目录，供 `pi install` 使用；只写状态目录，不修改 pi settings |
| 自更新 | `update`、`update --check` | 查询 GitHub Releases 最新版本，按平台校验 SHA-256 后原子替换当前二进制；`--check` 只报告 |
| 整理提案 | `dream --init-workflow`、`dream --runner`、`dream --show`、`dream --apply` | 推送 `dream/req` 触发 GitHub Actions 结构化整理，拉取并校验结果树生成带固定 Diff 的提案；人工确认后按受管事务应用，应用不重新调用模型 |

命令、参数、退出码与输出格式的权威定义见 [CLI 命令](./docs/04-ui-and-cli-specification.md#1-cli-命令)，HTTP 路由与错误码见 [REST 契约](./docs/02-domain-models-and-contracts.md#4-rest-契约)。以下行为细节同样以契约为准：

- CLI 与 UI 的写入共用仓库锁和版本基线；HTTP 写路由要求 `X-Memctl-Token` 与 Host/Origin 校验，`--readonly` 下返回 `READ_ONLY`。
- Hook 子命令在 `--stdin` 下以退出码 `0` 结束，避免阻断宿主；缺少稳定 SID 时 `tag` 返回 `1`；未知命令或参数返回用法错误 `2`。
- 受管 `write` 与自动 `sync`/`end` 会尽力派生非交互后台同步，设置 `AGENT_MEMORY_BACKGROUND=0` 可关闭。
- `install` 不访问记忆仓库，不要求 Git 或 Node.js。

## 工作方式

1. 会话开始时读取独立记忆仓库，按需同步并注入上下文。
2. Agent 或 UI 读取文件修订，通过受管事务提交记忆，避免静默覆盖其他更新。
3. 本地 Git 提交与远端同步分别报告结果；断网不撤销已保存的本地内容。
4. 开发者通过 UI 检索、审阅历史、解决冲突，并确认 Dreaming 整理提案。

## 平台与限制

- darwin/arm64：全量测试、发布二进制冒烟与浏览器验证均在本机执行。
- macOS amd64、Linux amd64、Windows amd64：仅交叉编译通过，文件锁、路径语义、进程生命周期与 runner 隔离未验证。
- Dreaming：统一在记忆仓库所在 GitHub 远端的 Actions 运行器中执行，本地不再依赖平台沙箱；已在 `gray0128/agent-memory-repo` 用 DeepSeek 端点跑通一次，但两次真实运行模型都判定无需改动，「带实际变更的提案被 apply 提交」仍未验证。
- 真实宿主：macOS arm64 上 7 个宿主的 Hook 已完成配置，其中 5 个完成会话级验证；`install` 探测到的版本不代表 Hook 已被宿主采纳。
- pi 专用插件：`memctl-pi` 已在 npm 发布 `0.5.2`，`memctl pi plugin-dir` 与适配器探测随 `v0.5.0` 发布；pi `1.3.0`/darwin arm64 完成真实会话验证（注入、每轮同步、收尾、恢复、分叉、后台子代理与失败降级）；前台 in-process 子代理不注入记忆，非 macOS 平台未验证，见 [pi 专用插件](./docs/08-pi-plugin.md)。
- dsh 专用插件：已实现包清单、适配器状态与隔离 profile 安装验证；npm 包名界面安装与 `desktop` profile `0.5.1` 已复验，`0.5.2` 完成真实会话的注入、轮次同步与收尾复验；`0.5.3` 修复了非零退出的 `start` 丢弃上下文，已发布到 npm 并升级本机 `desktop`；绝对目录界面安装与 `agent/disposed` 事件仍受限制或未验证，见 [dsh 专用插件](./docs/07-dsh-plugin.md)。
- 记忆写入通道：生命周期插件在宿主进程内运行，Agent 在 bash 中执行 `memctl read/write` 时会受工作区沙箱限制，无法写 `~/memory`。`memctl mcp` 提供不受该沙箱限制的 MCP 工具；DSH、Codex、pi、OpenCode、Command Code 与 Antigravity 已完成 MCP 受管写入实测，Grok 需要把记忆仓库加入 `read_write`（同时放开 shell），Claude Code 因代理未运行未实测，见 [MCP 记忆工具](./docs/09-mcp-memory-tools.md#62-真实验证边界)。
- 自更新：Windows 不能在运行中替换可执行文件，`update` 返回 `UNSUPPORTED`，但 `update --check` 可用；其余平台的自更新仅在本机 darwin/arm64 验证，其他平台按交叉编译与单元测试记录。
- 其余未验证项与已知限制见 [开发状态](./docs/STATUS.md#当前事实)。

## 文档入口

| 目的                 | 入口                                                        |
| ------------------ | --------------------------------------------------------- |
| 了解设计、格式基线和功能边界     | [方案索引](./docs/README.md)                                  |
| 修改文档、实现功能或修复问题     | [开发循环规范](./docs/06-development-loop.md)                   |
| 查看当前事实、任务状态与最近一轮记录 | [开发状态](./docs/STATUS.md)                                  |
| 确定代码和测试文件放在哪里      | [目标目录结构](./docs/01-architecture-and-modules.md#5-目录结构与依赖) |
| 判断功能是否达到交付条件       | [验收与交付](./docs/05-validation-and-delivery.md)             |
| 让 Agent 在本仓库协作     | [Agent 入口](./AGENTS.md)                                   |

## 目录结构

```text
memctl/
  cmd/memctl/          # CLI 进程入口与版本注入
  internal/            # 应用服务、CLI、MCP 工具、自更新、仓库格式、Git、锁、状态、HTTP 服务、监听、适配器、诊断与 Dreaming
  web/                 # Preact 前端源码与内嵌入口；web/dist 由 make ui 生成，不提交
  plugins/dsh/         # memctl-dsh Host 插件源文件、Node 单元测试与 go:embed 入口
  plugins/pi/          # memctl-pi 扩展源文件、Node 单元测试与 go:embed 入口
  tests/integration/   # CLI 二进制与跨模块集成测试
  skills/memctl/       # 面向 Agent 的使用 Skill（SKILL.md 与按需参考）
  scripts/             # 跨会话接续、发布打包与集成测试分片
  docs/                # 设计契约、开发循环、任务状态与验收
  脚本化实现参考/        # 只读参考，不作为新实现或发布入口
```

完整代码、测试和构建布局只在 [架构文档](./docs/01-architecture-and-modules.md#5-目录结构与依赖) 定义。未落地的模块目录在首个实际文件实现时创建，不用空目录、占位模块或成功退出的空测试表示功能就绪。

## 构建与测试

Go 基线 `Go 1.27.1`，前端构建基线 Node v26.10.0 / npm 11.19.1。`make ui` 执行 `npm ci` 与 `vite build` 生成 `web/dist`，`internal/server` 通过 `web/embed.go` 内嵌该目录；`web/dist` 不提交，因此 `go build`、`go vet` 与 `go test` 前必须先执行 `make ui`，`make build` 与 `make test` 已包含该依赖。

```bash
make ui
make build
make test
./bin/memctl version
./bin/memctl help
./bin/memctl help write
```

`version` 与 `help` 不访问仓库，不要求 Git 或 Node.js。`make test` 同时运行前端测试、`plugins/dsh/test/` 与 `plugins/pi/test/` 的 Node 单元测试以及 Go 测试。

发布唯一入口是推送 `v*` 标签，Release 附件只由流水线产生，不从本地 `dist/` 上传。原因是构建环境与元数据语义：流水线从干净检出开始，`RELEASE-INFO.txt` 记录的构建宿主与验证状态描述的是实际产出该文件的环境。

```bash
git tag -a <version> -m "<说明>"
git push origin <version>
gh release view <version>            # 等待 publish 作业创建 Release

# 下载到独立目录校验，避免与本地 make release 的产物混在一起
mkdir -p /tmp/release-check && cd /tmp/release-check
gh release download <version>
shasum -a 256 -c SHA256SUMS
```

本地 `make release` 只用于离线或调试时验证构建链路，产物会被流水线覆盖，不构成发布步骤。发布前在本地应运行 `make test` 验证代码。发布入口与理由的权威定义见 [发布入口](./docs/04-ui-and-cli-specification.md#61-发布入口)。

GitHub Actions 的 `build` 工作流（`.github/workflows/build.yml`）分三个作业：`verify` 四个分片并行跑测试，`build` 与测试并行运行 `make release-dist` 产出候选包，`publish` 在两者成功后创建 Release。工作流在 `macos-latest` 上运行，`RELEASE-INFO.txt` 记录该次构建宿主的运行时验证状态；流水线通过不替代目标平台的运行时、生命周期与隔离验证。

发布产物内的静态资源、默认指引与 adapter 模板全部随二进制打包，不依赖外部 CDN；交叉编译通过只说明可构建，运行与生命周期以 [性能与适配记录](./docs/05-validation-and-delivery.md#3-性能与适配记录) 为准。`memctl-pi` 的 tarball 只用于 npm 发布和审计，不作为 `pi install` 的输入。`dist/` 与 `bin/` 不提交。

## 上游与参考

本项目实现上游记忆格式并扩展受管写入、Hook、UI 和整理工作流，不宣称是上游官方二进制。固定上游版本、兼容范围与有意改变的行为见 [上游与参考边界](./docs/README.md#上游与参考边界)。

`脚本化实现参考/` 用于理解历史命令和适配样例；默认不修改，也不作为新实现已通过验收的证据。

## 许可证

MIT，见 [LICENSE](./LICENSE)。
