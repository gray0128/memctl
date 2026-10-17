# 04. CLI、UI 与分发

## 1. CLI 命令

```text
memctl <subcommand> [flags] [-- command...]
```

本表是二进制的目标命令契约，参考脚本不是可执行规范。除 `wrap` 外，未知参数返回用法错误。文件与版本语义见 [模型与契约](./02-domain-models-and-contracts.md)。

| 命令 | 参数 | 行为 |
| --- | --- | --- |
| `init [remote-url]` | `--confirm-private` | 按初始化分支创建、克隆或复用；无 URL 默认仅本地；提供 URL 时要求显式私有性与所有权确认 |
| `start` | `--agent NAME --sid ID --stdin --json` | 尝试同步并输出指引、入口与状态；不捕获外部编辑 |
| `context` | 同 start，另有 `--once` | 按会话缓存同步成功与注入状态；默认每轮注入，`--once` 只控制正文重复输出 |
| `pull` | `--agent NAME --sid ID --stdin --once` | 与 start 使用同一同步流程，保持 stdout 为空；保留名字但实际可补推本地提交 |
| `sync` / `end` | `--agent NAME --sid ID --stdin` | 自动路径只尽力触发后台同步，stdout 为空 |
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
| `version` / `help` | 无 | 不访问记忆库、不初始化图谱 |

同一单元格中的 flags 是可选能力集合，不表示必须全部传入；`write` 的 `--base` 和 `--stdin`、`resolve --apply`/`--abort` 的 `--conflict`、应用提案的 ID 和 hash 为必填，`resolve --apply` 的 JSON 必须携带非空 `baseRevision`。`resolve` 的查看、应用、中止互斥；`dream` 的生成、查看、应用互斥；`install --status` 不写文件。

`install` 至少需要一个 `agent` 参数或 `--all`，两者都缺省时返回用法错误。`--status` 只探测并输出适配状态，`--dry-run` 输出安装计划，两者都不写文件也不执行中断恢复。`--all` 跳过未探测到宿主可执行文件的 adapter；显式指定的 agent 在宿主未探测到但目标配置已存在时仍生成计划，以便用户在看到警告后自主决定。`install` 不访问记忆仓库、不要求 Git，也不需要 Node.js。

`init` 重复执行不修改已有 remote。已有 remote 与 URL 不同时拒绝；需要在暂停自动操作后显式修改 Git remote，再使用 `init <url> --confirm-private` 确认新目标。确认记录绑定有效 fetch/push URL，URL 不同即失效。

### 1.1 相对参考脚本的有意变化

- 保留常用 Hook 命令名称、`--agent/--sid/--stdin/--once/--json` 和纯文本/JSON 输出习惯，不承诺复制脚本所有退出与容错细节。
- 自动 Hook 不再提交未知工作树内容。适配指引要求受管 `read/write`；串行直接编辑通过显式 `sync --capture` 接收。
- 断网、锁忙不写「已同步」标记；注入和同步去重分开。
- `tag` 不读取全局 `last-sid-<agent>` 猜测当前会话，不自动生成看似稳定的来源。
- JSON 输出不回退成纯文本；Git 命令失败不无条件归为内容冲突。
- 整理应用必须引用已经展示的提案，不提供「重新生成并自动应用」的模糊 `--apply` 开关。

## 2. 环境变量与会话身份

| 变量 | 默认值与语义 |
| --- | --- |
| `AGENT_MEMORY_DIR` | `~/memory`；规范化为绝对路径后绑定仓库 |
| `AGENT_MEMORY_STATE` | `~/.agent-memory`；仅覆盖运行状态根，不改变仓库锁位置 |
| `AGENT_MEMORY_BACKGROUND` | 默认开启；设为 `0`、`false` 或 `off` 时受管写入与自动 Hook 不派生后台同步 |
| `AGENT_MEMORY_AGENT` | `unknown`；`--agent` 优先 |
| `AGENT_MEMORY_DEVICE` | OS 主机名；探测失败使用 `unknown` 并提示 |
| `AGENT_MEMORY_SID` | 无默认值；由宿主或 wrap 传递 |
| `DSH_HOOK_SESSION_ID` | dsh 的兼容身份来源 |

SID 优先级为 `--sid` → `AGENT_MEMORY_SID` → `DSH_HOOK_SESSION_ID` → 有 `--stdin` 时 JSON 的 `session_id/sessionId/sessionID`。空值跳过，非字符串返回协议错误。其他宿主变量由 adapter 显式映射到 `--sid`，例如 pi 的 `PI_SESSION_ID`。

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

`context --json` 使用 `UserPromptSubmit`。各 adapter 决定是否能消费该 envelope；不是所有宿主都支持相同结构。start/context 的 stdout 只包含上下文或单个 JSON 对象，Git 输出与诊断进入脱敏 stderr。

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

## 4. Web UI

五个视图保持原功能范围，统一显示仓库路径、本地状态、远端状态、最后同步时间和只读标识。ahead/behind 未知时显示「未知」，不显示零。外部写入的失效提示到达时写入方可能仍持有仓库锁，服务端会返回 `stale: true` 的缓存快照；客户端对此有界重试，取得新鲜快照后再渲染，重试仍失败才显示过期状态。

| 视图 | 展示与操作 | 失败或过期状态 |
| --- | --- | --- |
| 知识图谱 | `MEMORY.md` 可达关系、死链、孤立文件；拆分条目为新文件。节点标签显示文件名，选中节点后显示完整路径 | 以同一仓库基线提交多文件计划；过期后重新预览 |
| 条目浏览 | 关键词、Agent、设备、日期筛选；文件原文编辑、条目原文编辑、条目移动和删除 | 保留未保存草稿；版本冲突显示当前内容与草稿差异，不自动覆盖 |
| 冲突解决 | 三方上下文、文本合并、删除选择、阶段进度与中止 | 「继续解决」可能进入下一提交；unsupported 类型提供人工入口，不称一键无损 |
| 审计回滚 | Git 历史、Diff、可解析来源、可疑内容提示；确认后 revert | 无法确定来源显示未知；回滚失败展示恢复状态 |
| 整理接入 | 提案生成、固定 Diff 确认、状态查询；adapter 探测、预览、安装 | 沙箱不可用禁用生成；过期提案禁止应用；配置成功与宿主验证分开；安装基线过期后重新探测预览 |

图谱和条目编辑最终调用相同事务；「拆分」在同一事务内创建目标文件、从源文件移除该条目并保留其来源元数据，整体失败不能留下半套索引。拆分不自动修改入口文件：新文件在被条目链接指向之前按孤立文件显示。初版整理仅支持整份提案确认，符合流程文档中的应用边界。

冲突解决的提交覆盖当前阶段全部冲突文件，应用与中止都复用 CLI `resolve` 的同一多阶段状态机；下一提交再次冲突时展示新的三方快照，不把一次确认当作全部完成。审计回滚只回滚当前可达历史中的单父提交，要求以当前仓库修订为基线，根提交与 merge commit 给出不支持说明，回滚冲突在恢复原始历史后展示恢复状态，不回显半应用结果。

来源链接只对明确允许的 `https` 等安全协议提供导航；`agent://` 默认提供复制，不任意唤起外部程序。代码、Markdown 原始 HTML、Diff、文件名和来源字段按不可信文本渲染。可疑词扫描只显示线索，不把 `curl`、SQL 或 URL 本身当成恶意结论。

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

内嵌资产包括前端静态文件、默认指引和全部支持的 adapter 模板。默认指引随二进制发布，不依赖安装时额外复制文件。

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

兼容读取 `$STATE/INSTRUCTIONS.md` 作为用户显式创建的附加指引，不覆盖内嵌安全规则、不自动执行其中命令，输出时标明来源。升级不改写该文件；不存在时全部功能仍可运行。

以下 Makefile 是 POSIX 构建机上的实际构建入口。`make build` 先构建前端再编译本机二进制；`make release` 依赖 `make test`，先跑全量测试再由 `scripts/release.sh` 交叉编译四个候选二进制并生成发布元数据。Windows 运行支持仍需对应平台测试。

```makefile
GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v2.0.0-dev")
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all ui build test install clean release
all: build

ui:
	cd web && npm ci && npm run build

test: ui
	cd web && npm test
	$(GO) test ./...

build: ui
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/memctl ./cmd/memctl

install: build
	mkdir -p "$(HOME)/.agent-memory/bin"
	install -m 755 bin/memctl "$(HOME)/.agent-memory/bin/memctl"

release: test
	VERSION="$(VERSION)" scripts/release.sh

clean:
	rm -rf bin dist coverage web/dist
```

`make release` 在 `dist/` 产出 `memctl-darwin-arm64`、`memctl-darwin-amd64`、`memctl-linux-amd64`、`memctl-windows-amd64.exe` 四个候选二进制，以及三个元数据文件：`SHA256SUMS`（四个二进制的 SHA-256）、`THIRD_PARTY_LICENSES.txt`（Go 运行时模块与前端运行时依赖的许可证全文，构建期前端依赖只列 SPDX 标识）与 `RELEASE-INFO.txt`（版本、提交、Go/Node/npm/Git 版本与逐平台验证状态）。同一宿主上的重复构建在相同工具链下产出相同二进制。`bin/` 与 `dist/` 都不提交。

运行时核心依赖为 Git，以及所选 Git 传输需要的 SSH 或凭据环境；Dreaming 另需用户已安装的 runner 和经过验证的隔离能力。没有 runner 只影响整理生成，不影响 CLI、同步、诊断和 UI。发布包含校验和、依赖许可证、工具版本与平台测试结果，不将交叉编译通过当成运行验收。
