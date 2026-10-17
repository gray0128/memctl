# memctl

`memctl` 是管理 Agent 记忆仓库的单文件 Go 二进制。记忆以 Markdown 保存在独立的 Git 仓库，`memctl` 负责受管读写、本地提交与远端同步、Hook 上下文注入、内嵌 Web UI，以及需要人工确认的 Dreaming 整理。

当前版本 `v0.1.2`（2026-10-17 发布），阶段 A–D 的能力已全部交付。除 `version`、`help` 与 `install` 外，运行时依赖系统 Git；Node.js 仅用于前端构建，AI runner 仅用于整理生成。各平台与宿主组合的验证状态见 [平台与限制](#平台与限制)。

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
```

示例中的 `./bin/memctl` 指本地 `make build` 的产物，使用下载的二进制时替换为实际路径。接入远端（`init <url> --confirm-private`）、注册宿主 Hook 与使用整理能力都需要额外参数或前置条件，见下文「能做什么」与对应契约。

## 能做什么

| 能力 | 命令 | 说明 |
| --- | --- | --- |
| 仓库初始化 | `init`、`init <url> --confirm-private` | 无 URL 只建本地仓库与首笔提交；带 URL 需确认私有性与所有权，空远端完成首推，已有历史则克隆并建立 upstream |
| 读取与受管写入 | `read`、`write` | 读写文件原文；写入校验修订基线并原子提交，不静默覆盖其他更新 |
| 串行编辑与恢复 | `sync --capture`、`recover --status`、`recover --rollback` | 显式接收串行外部编辑；查看或回滚本工具未完成的事务，不接管外部 Git 操作 |
| 同步与冲突 | `sync --wait`、`resolve` | 前台同步与多阶段冲突解决；断网不撤销已保存的本地内容 |
| 会话上下文 | `start`、`context`、`pull`、`tag`、`wrap` | Hook 注入、会话级去重与来源标签；`wrap` 透传被包装程序的退出码与信号 |
| 诊断与修复 | `doctor`、`doctor --fix` | 只读诊断死链、孤立文件、入口膨胀与运行状态；修复只重建缺失的入口文件 |
| Web UI | `ui` | 概览、知识图谱、条目、冲突解决、审计、诊断、整理接入 7 个视图；只读模式在服务端拒绝写请求 |
| Agent 接入 | `install [agent...]` | 探测宿主与插件版本，预览或应用配置合并；不自动安装第三方插件，不改全局 Git 配置 |
| 整理提案 | `dream --runner`、`dream --show`、`dream --apply` | 隔离生成带固定 Diff 的提案，人工确认后按受管事务应用；应用不重新调用模型 |

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
- Dreaming：仅 macOS Seatbelt 为已验证隔离后端，Linux 与 Windows 返回 `UNSUPPORTED`；真实 runner 与模型端点未验证。
- 真实宿主：macOS arm64 上 7 个宿主的 Hook 已完成配置，其中 5 个完成会话级验证；`install` 探测到的版本不代表 Hook 已被宿主采纳。
- dsh 专用插件：当前只有[设计提案](./docs/07-dsh-plugin.md)，实现任务待确认。
- 其余未验证项与已知限制见 [开发状态](./docs/STATUS.md#当前事实)。

## 文档入口

| 目的 | 入口 |
| --- | --- |
| 了解设计、格式基线和功能边界 | [方案索引](./docs/README.md) |
| 修改文档、实现功能或修复问题 | [开发循环规范](./docs/06-development-loop.md) |
| 查看当前事实、任务状态与最近一轮记录 | [开发状态](./docs/STATUS.md) |
| 确定代码和测试文件放在哪里 | [目标目录结构](./docs/01-architecture-and-modules.md#5-目录结构与依赖) |
| 判断功能是否达到交付条件 | [验收与交付](./docs/05-validation-and-delivery.md) |
| 让 Agent 在本仓库协作 | [Agent 入口](./AGENTS.md) |

## 目录结构

```text
memctl/
  cmd/memctl/          # CLI 进程入口与版本注入
  internal/            # 应用服务、CLI、仓库格式、Git、锁、状态、HTTP 服务、监听、适配器、诊断与 Dreaming
  web/                 # Preact 前端源码与内嵌入口；web/dist 由 make ui 生成，不提交
  tests/integration/   # CLI 二进制与跨模块集成测试
  scripts/             # 跨会话接续（next-round.sh）与发布打包（release.sh）
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
```

`version` 与 `help` 不访问仓库，不要求 Git 或 Node.js。

发布候选包用 `make release` 生成：先跑全量测试与前端构建，再交叉编译四个平台。

```bash
make release
ls dist/                 # 四个候选二进制、SHA256SUMS、THIRD_PARTY_LICENSES.txt、RELEASE-INFO.txt
(cd dist && shasum -a 256 -c SHA256SUMS)
```

`dist/` 与 `bin/` 不提交。发布产物内的静态资源、默认指引与 adapter 模板全部随二进制打包，不依赖外部 CDN；交叉编译通过只说明可构建，运行与生命周期以 [性能与适配记录](./docs/05-validation-and-delivery.md#3-性能与适配记录) 为准。

GitHub Actions 的 `build` 工作流（`.github/workflows/build.yml`）在 `main` 推送、拉取请求、`v*` 标签推送与手动触发时调用同一 `make release` 入口，构建四个候选二进制并保留 `dist/` 构建产物；推送 `v*` 标签时创建 GitHub Release，并附带四个候选二进制与 `SHA256SUMS`、`THIRD_PARTY_LICENSES.txt`、`RELEASE-INFO.txt`。

```bash
git tag <version>
git push origin <version>
```

工作流在 `macos-latest` 上运行，`RELEASE-INFO.txt` 记录该次构建宿主的运行时验证状态；流水线通过不替代目标平台的运行时、生命周期与隔离验证。

## 上游与参考

本项目实现上游记忆格式并扩展受管写入、Hook、UI 和整理工作流，不宣称是上游官方二进制。固定上游版本、兼容范围与有意改变的行为见 [上游与参考边界](./docs/README.md#上游与参考边界)。

`脚本化实现参考/` 用于理解历史命令和适配样例；默认不修改，也不作为新实现已通过验收的证据。
