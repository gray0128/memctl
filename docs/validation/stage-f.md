# 阶段 F 归档：pi 专用插件

本文件归档阶段 F（pi 专用插件）及后续发布后轮次的完成记录，含 `T060`–`T075`。阶段步骤与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-f-的实施顺序)，当前进度见 [开发状态](../STATUS.md)。

## T060：实现 pi 专用插件

- 目标与范围：对应阶段 F 的 `F-1` 至 `F-4`。新增 `plugins/pi/` 原生扩展、Node 单元测试与 `go:embed`；实现 `memctl pi plugin-dir`；pi 适配器增加原生插件探测（npm、状态目录、settings 本地包）、旧 `agent-memory-*` 条目内容探测、宿主与插件版本门槛、安装提示与按接入路径报告 capabilities；增加 `make pi-plugin-test`/`pi-plugin-pack` 与 Release 审计 tarball。不发布 npm 包，不修改真实 pi 配置。
- 契约：[pi 专用插件](../08-pi-plugin.md)、[目录结构](../01-architecture-and-modules.md#5-目录结构与依赖)、[AdapterStatus](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，pi `1.3.0`；隔离 `HOME`/`PI_CODING_AGENT_DIR`、本地 mock OpenAI 兼容端点与假 memctl。
- 命令与结果：`node --test plugins/pi/test/*.test.js` 15 项通过；`go test ./internal/... ./plugins/... -count=1` 通过；`go test ./tests/integration -count=1` 通过（269.3 秒）；`go vet` 与 `gofmt` 无输出；`npm pack --dry-run` 只包含 `index.js`、`README.md`、`package.json`，无依赖与安装脚本；隔离 `pi install <plugins/pi>`、`pi list`、`pi remove` 可用，settings 中的相对路径仍被适配器识别；隔离 pi 会话观察到 `start` 后模型请求包含 `<memctl>` section，退出触发 `end`。证据见 [验证记录](./t060-pi-plugin.md)。
- 验收：`H12`–`H15` 的单元部分通过；`I07`、`I08`、`I09`、`I10` 通过。真实 pi 会话、真实模型、npm 发布与非 macOS 平台未验证。
- 未验证与遗留：`agent_settled` 的 `sync` 在 `pi -p` 立即退出时被 shutdown 取消，最终同步由 `end` 完成；交互式会话、恢复、分叉与子代理行为待 `F-5`；npm 包名与许可证、Windows/Linux 进程清理未验证。
- 下一步：`F-5` 在真实 pi 会话验证注入、同步提交、收尾、恢复、分叉与失败降级；npm 发布与版本提升留给后续发布轮次。

## T061：发布 v0.5.0

- 目标与范围：把 `T060` 的 `memctl-pi` 扩展、`memctl pi plugin-dir` 与 pi 适配器探测作为 `v0.5.0` 发布；`memctl-dsh` 与 `memctl-pi` 包版本同步到 `0.5.0`，pi 原生插件最低支持版本提升到 `0.5.0`；运行全量测试与 `make release`，校验四个候选二进制、两个插件 tarball、校验和与元数据，推送 tag 触发 GitHub Release。不改变 `T060` 之外的运行时行为。
- 契约：[pi 专用插件](../08-pi-plugin.md)、[dsh 专用插件](../07-dsh-plugin.md)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.5.0` 指向 `59a23ff`。
- 命令与结果：`VERSION=v0.5.0 make release` 通过（前端 16 文件 85 项；插件 Node 测试 dsh 14 项、pi 15 项；Go 测试全部通过，`tests/integration` 命中同一文件内容的缓存结果）；生成四个平台候选二进制、`memctl-dsh-0.5.0.tgz`、`memctl-pi-0.5.0.tgz` 与三个元数据文件；`dist/SHA256SUMS` 六个产物全部 `OK`，本机与下载的 `memctl-darwin-arm64` 均输出 `memctl v0.5.0`，`update --check` 输出 `current: v0.5.0`、`latest: v0.5.0`、`up to date`；两个插件 tarball 版本均为 `0.5.0`。tag 推送后 [GitHub Actions 运行 39050077059](https://github.com/gray0128/memctl/actions/runs/39050077059) 成功并创建 [Release v0.5.0](https://github.com/gray0128/memctl/releases/tag/v0.5.0)；下载 Release 附件后按发布 `SHA256SUMS` 校验，六个产物全部 `OK`，附件含四个平台二进制、两个插件 tarball、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`。本机 `~/.agent-memory/bin/memctl` 已升级到 `v0.5.0`。
- 验收：发布前测试与校验通过；`I09` 的包清单与本地目录场景包含在发布测试中；`v0.5.0` 的 CI Release 成功。
- 未验证与遗留：npm 发布未执行（后由 `T064` 完成）；macOS amd64、Linux 与 Windows 仅交叉编译；`memctl-pi` 真实 pi 会话、真实模型、恢复/分叉/子代理仍待 `F-5`。
- 下一步：执行 `F-5` 真实 pi 会话验证；npm 发布、包名与许可证确认留给后续发布轮次。

## T062：真实 pi 会话验证

- 目标与范围：对应阶段 F 的 `F-5`。把本机 pi 从旧 `pi-yaml-hooks` 条目迁移到 `memctl-pi@0.5.0`，用真实模型和真实记忆仓库验证注入、同步、收尾、恢复、分叉、子代理与失败降级；不改变插件代码。
- 契约：[pi 专用插件](../08-pi-plugin.md)、[验收 H12–H15](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-18，macOS arm64，pi `1.3.0`，memctl `v0.5.0`，真实仓库 `~/memory`。
- 命令与结果：旧 `hooks.yaml` 已备份到 `~/.agent-memory/backups/pi-hooks-20261018-004927.yaml`；`pi install /Users/libo/.agent-memory/pi` 成功；旧 `agent-memory-*` 条目移除，`install --status` 返回 `configured plugin@0.5.0`。`pi --model deepseek/deepseek-flash --no-tools -p` 退出码 `0`，会话 system sections 含 `<memctl>` 与 `[agent-memory]` 正文；退出后同步缓存更新为 `synced`，`lastSuccessAt=2026-10-17T16:49:47Z`。交互式会话 `01a14ac7` 中两次 `memctl write` 产生提交 `ff2d05d`、`4a0f82a`，同步缓存于 3 秒内更新为 `synced`，HEAD 为 `4a0f82a`；当前日志不能区分该次同步由 `agent_settled` 还是 `end` 触发。完整记录见 [T062 验证记录](./t062-pi-real-session.md)。
- 追加验证：`pi --mode rpc` 长会话中 `agent_settled` 后 `sync` 约 0.7 秒完成并返回 0；`new_session`、`clone`、`resume` 分别获得正确 SID，每个会话 `start`/`sync`/`end` 各一次。后台子代理获得独立 SID 与 `start`/`end`；前台 in-process 子代理不触发会话事件，不注入记忆。二进制缺失、非法 JSON、`start` 挂起 30 秒、仓库锁忙、远端不可达时会话均正常完成。
- 验收：`H12`–`H15` 的真实宿主部分通过；`memctl-pi` 在 pi `1.3.0`/darwin arm64 标记为 `verified`。前台子代理不注入是宿主生命周期限制，不按 `parentSession` 跳过。
- 未验证与遗留：前台子代理记忆注入；`sections` 缺失的低版本 pi 回退只由单元测试覆盖；npm 发布仍需确认包名与许可证。
- 下一步：npm 发布、包名与许可证确认；需要前台子代理记忆时另行评估宿主 API。

## T063：发布 memctl-pi 到 npm

- 目标与范围：把 `memctl-pi` 发布到 npm 官方仓库，采用 MIT 许可证；补齐 npm 元数据（`repository`、`homepage`、`bugs`、`publishConfig`），修正包内 README 的仓库内相对链接；不改变插件运行时行为。
- 契约：[pi 专用插件](../08-pi-plugin.md#81-npm-主通道)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)。
- 验证日期与环境：2026-10-18，macOS arm64，Node v26.10.0 / npm 11.19.1；npm 账号 `gray0128`，npmjs 官方 registry。
- 命令与结果：`0.5.0` 于 17:15:52 发布；首次 staged 发布创建的 `0.0.0-stage` 占位版本一度作为 latest 出现，后续由 `T065` 删除。包版本继续提升到 `0.5.1`，于 17:16:38 发布。`npm stage publish` 生成 stage id `5d2689f9-3c14-4e45-b402-24cffccc8267`，shasum `d83156061ca4de3f585e12954845d5379b84b22f`；维护者用 `npm stage approve` 完成 2FA 审批。`npm view memctl-pi@0.5.1 version` 输出 `0.5.1`，`dist-tags` 为 `{ latest: '0.5.1' }`；npm tarball 含 `LICENSE`、`index.js`、`package.json`、`README.md`。隔离 `HOME`/`PI_CODING_AGENT_DIR` 下 `pi install npm:memctl-pi@0.5.1` 成功，`pi list` 显示该 npm 包，`install --status` 返回 `configured plugin@0.5.1`。
- 验收：npm 官方包可安装、适配器可识别、版本与发布说明正确；`I09` 的 npm 通道场景通过。
- 未验证与遗留：`memctl-dsh` 当时仍未发布 npm 包；`0.5.0` 与 `0.5.1` 均已发布，npm `latest` 指向 `0.5.1`；npm 发布不改变二进制 Release。
- 下一步：按需发布 `memctl-dsh` 或创建 `v0.5.1` 发布轮次；继续维护 npm 包与 GitHub Release 的版本对应关系。

## T064：发布 memctl-dsh 到 npm 与 v0.5.1 Release

- 目标与范围：把 `memctl-dsh` 发布到 npm 官方仓库，并把 `memctl-dsh`、`memctl-pi` 版本提升到 `0.5.1`，重新生成候选二进制、两个插件 tarball、校验和与 CI Release；补齐 dsh 包的 npm 元数据和 README。不改变插件运行时行为。
- 契约：[dsh 专用插件](../07-dsh-plugin.md)、[pi 专用插件](../08-pi-plugin.md)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.5.1` 指向 `71ebf75`；npm 账号 `gray0128`。
- 命令与结果：`VERSION=v0.5.1 make release` 通过（前端 16 文件 85 项；插件 Node 测试 dsh 14 项、pi 15 项；Go 测试全部通过）；生成四个平台二进制、`memctl-dsh-0.5.1.tgz`、`memctl-pi-0.5.1.tgz` 与三个元数据文件，`SHA256SUMS` 六个产物全部 `OK`，二进制输出 `memctl v0.5.1`。tag 推送后 [GitHub Actions 运行 39053620948](https://github.com/gray0128/memctl/actions/runs/39053620948) 成功并创建 [Release v0.5.1](https://github.com/gray0128/memctl/releases/tag/v0.5.1)；下载附件重新校验，六个产物全部 `OK`。本机 `~/.agent-memory/bin/memctl` 已升级到 `v0.5.1`。维护者用 OTP 完成 `npm publish`；`npm view memctl-dsh@0.5.1` 输出 `0.5.1`，`dist-tags` 为 `{ latest: '0.5.1' }`，npm tarball 含 8 个运行时文件。隔离 `HOME` 下通过 npm 安装到 `~/.dsh/profiles/test/node_modules` 后，`install --status` 返回 `configured host=dsh@0.2.1-rc.2 plugin@0.5.1`。
- 验收：`v0.5.1` 二进制、两个插件 tarball、npm 包与适配器探测均通过；`T064` 完成。
- 未验证与遗留：macOS amd64、Linux 与 Windows 仅交叉编译；DSH 界面安装与 `desktop` profile 升级未复验（后续 `T067` 已复验 npm 包名界面安装与 `desktop` `0.5.1`）。
- 下一步：按需把本机 `desktop` profile 的 `memctl-dsh` 升级到 `0.5.1`，或在 DSH 界面验证 npm 包安装；两项均已由 `T067` 覆盖，绝对目录界面安装仍未验证。

## T065：删除 npm 占位版本

- 目标与范围：删除 npm 上由 staged publishing 创建的 `memctl-pi@0.0.0-stage` 占位版本，使 registry 只保留真实版本；不改变源码、二进制或插件行为。
- 契约：[pi 专用插件](../08-pi-plugin.md#81-npm-主通道)。
- 验证日期与环境：2026-10-18，npm 账号 `gray0128`，npm 官方 registry。
- 命令与结果：维护者执行 `npm unpublish memctl-pi@0.0.0-stage --registry=https://registry.npmjs.org` 并完成 2FA。`npm view memctl-pi versions` 返回 `['0.5.0', '0.5.1']`，`dist-tags` 为 `{ latest: '0.5.1' }`；`https://registry.npmjs.org/memctl-pi/0.0.0-stage` 返回 404。删除占位版本后，先前发布的 `0.5.0` 恢复可见。
- 验收：占位版本已删除，真实版本与 `latest` 正确；任务完成。
- 未验证与遗留：`0.5.0` 与 `0.5.1` 内容接近，`latest` 保持 `0.5.1`；无其他遗留。
- 下一步：无需额外操作。

## T066：文档一致性检查与整理

- 目标与范围：核对根 `README.md`、`AGENTS.md`、`docs/01`–`08`、`docs/README.md`、`docs/STATUS.md`、`docs/validation/` 索引与两个插件 README 的事实、版本、链接和结构；修正过期的发布状态、`F-5` 状态、Makefile 与发布产物描述、DSH/pi 状态段和历史章节措辞；归档 `T061`、`T065` 并补全验证记录索引。不改变产品行为。
- 契约：[方案索引](../README.md)、[开发循环](../06-development-loop.md#1-最小记录结构)、[验收与交付](../05-validation-and-delivery.md)。
- 验证日期与环境：2026-10-18，macOS arm64；仅文档检查。
- 命令与结果：对项目内全部 Markdown 文件检查相对链接与页内锚点，修正 `T058` 的失效锚点；核对 `git tag`、`dist/`、`plugins/*/package.json` 与文档中的 `v0.5.1`、插件版本和 Release 产物一致；`git diff --check` 通过。
- 验收：链接、锚点、版本引用与跨文档状态一致；文档检查项通过。
- 未验证与遗留：未运行二进制、平台或宿主测试（纯文档轮次）；`desktop` profile 的 `memctl-dsh` 当时仍为 `0.4.0`，DSH 界面 npm 安装未验证（后续由 `T067` 复验）。
- 下一步：把 `desktop` profile 的 `memctl-dsh` 升级到 `0.5.1` 并复验 DSH 界面 npm 包名安装（已由 `T067` 完成）。

## T067：复验 DSH 界面 npm 安装与 desktop 插件升级

- 目标与范围：记录用户通过 DSH 界面 npm 包名安装 `memctl-dsh` 的结果，核对本机 `desktop` profile 的插件版本、bundle 与适配器状态；更新「界面 npm 安装未验证」和「`desktop` 仍为 `0.4.0`」的记录；不改变产品行为。
- 契约：[dsh 专用插件](../07-dsh-plugin.md#13-实现状态)、[T052 验证记录](./t052-dsh-real-session.md)。
- 验证日期与环境：2026-10-18，macOS arm64，DSH 0.2.1-rc.2，`desktop` profile，`memctl v0.5.1`。
- 命令与结果：用户报告已在 DSH 界面用 npm 包名 `memctl-dsh` 完成安装。`~/.dsh/profiles/desktop/package.json` 的 `dsh.profile.bundles` 含 `memctl-dsh`，依赖为 `memctl-dsh: 0.5.1`；`node_modules/memctl-dsh/package.json` 版本为 `0.5.1`；`memctl install --status` 输出 `dsh: configured host=dsh@0.2.1-rc.2 plugin@0.5.1`。
- 验收：DSH 界面 npm 包名安装完成；`desktop` profile 已从 `0.4.0` 升级到 `0.5.1`，适配器识别为 `configured`。
- 未验证与遗留：绝对目录的界面安装；`agent/disposed` 真实事件路径；界面安装后的新会话注入未单独复验。
- 下一步：按需验证绝对目录界面安装；其余未验证项见 [STATUS](../STATUS.md#当前事实)。

## T068：中文帮助检查与优化

- 目标与范围：按 `tech-doc-style-chinese` 检查 `internal/cli/help_data.go` 的中文帮助（概览与 21 个命令页），优化术语、引号、留白与信息层级，并修正与 [帮助契约](../04-ui-and-cli-specification.md#13-面向-agent-的帮助与中英-i18n) 和实现不一致的表述；只改中文帮助文本，不改命令行为、英文帮助与 JSON 结构。
- 契约：[面向 Agent 的帮助与中英 i18n](../04-ui-and-cli-specification.md#13-面向-agent-的帮助与中英-i18n)、[环境变量与会话身份](../04-ui-and-cli-specification.md#2-环境变量与会话身份)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，`memctl v0.5.1` 工作树（发布前）。
- 命令与结果：`gofmt -l internal/cli`、`go vet ./internal/cli/` 无输出；`go test ./...` 全部通过（`tests/integration` 237.2 s）；`./bin/memctl help` 与 21 个 `help <command> --lang zh` 逐页人工复核，并以脚本检查中西文留白、直角引号与重复空格，0 处问题；`help --lang en` 与 `help --lang zh --json` 输出结构不变（`schemaVersion` 1、21 个命令条目）。
- 主要修改：把 `--sid`、`AGENT_MEMORY_*`、`~/memory`、`0`/`false` 等机器可读值统一改为行内代码；删除 `AGENT_MEMORY_SID`、`DSH_HOOK_SESSION_ID`、`MEMCTL_LANG`、`LC_ALL/LC_MESSAGES/LANG` 四项与实现不符的 `none`/`auto` 默认值（实现只打印非空默认值）；把分散在各选项里的互斥规则合并到「说明」，删去逐项重复；`update` 示例由「然后重启 memctl」改为「重启 memctl 后生效」，与实现一致；全角引号改为直角引号；「逃逸路径」「首推」「环回」等表述改为「越出仓库的路径」「首次推送」「回环」，并补全 `wrap`、`resolve`、`install` 等页面缺失的主语与动作。
- 验收：中文帮助术语、排版与事实一致；帮助渲染与 JSON 结构未变；Go 全量测试通过。
- 未验证与遗留：未验证终端宽度、颜色或字体渲染（帮助文本不使用 ANSI 颜色，也不探测终端宽度）；未改变英文帮助文本。
- 下一步：如需发布，按 [发布前检查](../05-validation-and-delivery.md#4-发布前检查) 走一次版本发布；其余未验证项见 [STATUS](../STATUS.md#当前事实)。

## T069：发布 v0.5.2

- 目标与范围：把 `T068` 的中文帮助优化作为 `v0.5.2` 发布；插件版本保持 `0.5.1`，不随本次二进制发布变化；运行全量测试与 `make release`，校验四个候选二进制、两个插件 tarball、校验和与元数据，推送 tag 触发 GitHub Release。
- 契约：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.5.2` 指向 `17fd083`。
- 命令与结果：`make release VERSION=v0.5.2` 通过（前端构建、插件 Node 测试、Go 全量测试）；生成四个平台二进制、`memctl-dsh-0.5.1.tgz`、`memctl-pi-0.5.1.tgz` 与三个元数据文件，`SHA256SUMS` 六个产物全部 `OK`，本机二进制输出 `memctl v0.5.2`。tag 推送后 [GitHub Actions 运行 39058657201](https://github.com/gray0128/memctl/actions/runs/39058657201) 成功并创建 [Release v0.5.2](https://github.com/gray0128/memctl/releases/tag/v0.5.2)；下载附件重新校验，`memctl-darwin-arm64`、两个插件 tarball 全部 `OK`，`RELEASE-INFO.txt` 的版本与 commit 与 tag 一致。本机 `~/.agent-memory/bin/memctl` 已替换为 Release 二进制，`version` 输出 `memctl v0.5.2`，`update --check` 返回 `up to date`。
- 验收：`v0.5.2` 二进制、两个插件 tarball、Release 产物与校验和均通过；`T069` 完成。
- 未验证与遗留：macOS amd64、Linux 与 Windows 仅交叉编译；插件版本未更新，npm 仍为 `0.5.1`；未发布 npm 包。
- 下一步：如需让插件跟随二进制版本，单独走一次插件版本与 npm 发布轮次；其余未验证项见 [STATUS](../STATUS.md#当前事实)。

## T070：受沙箱宿主的记忆写入通道方案

- 目标与范围：定位「DSH 中多个新会话没有记忆更新」的原因，比较可行通道，产出权威方案文档并登记实现任务；不写产品代码。
- 契约：[MCP 记忆工具](../09-mcp-memory-tools.md)。
- 验证日期与环境：2026-10-18，macOS arm64，DSH 0.2.1-rc.2，Go 1.27.1，`memctl v0.5.2`。
- 命令与结果：`memctl read MEMORY.md --json` 在 `workspace-write` 会话返回 `IO_FAILED: cannot open repository lock`；`touch ~/memory/x` 返回 `Operation not permitted`，同会话工作区内 `touch` 成功。检索 30 个历史 DSH 会话的 `tool/call`，无一次 `memctl write`。隔离 profile `probe` 插入 `@deepseek-ai/dsh-mcp-client` 的 stdio 条目后运行 `dsh --profile probe`，探针记录 `touch-memory=ok` 与 `memctl-read=ok`，证明 MCP 子进程不受 bash 沙箱限制。隔离 profile 内安装本地 bundle 后 `import('@deepseek-ai/dsh-tools')` 返回 `ERR_MODULE_NOT_FOUND`，排除「插件内注册工具」。核实 8 个宿主的 MCP 配置位置与 `mcp add` 参数。
- 验收：`M08` 的沙箱子集通过（宿主派生的 MCP 子进程可写 `~/memory`）；方案比较与选型完成；`T070` 完成。
- 未验证与遗留：除 DSH 外的宿主未做受限会话内的 MCP 写入实测；`memctl mcp` 尚未实现（`T071`）。
- 下一步：按 [09](../09-mcp-memory-tools.md) 实现并验证 `M01`–`M12`。

## T071：实现 memctl mcp 子命令

- 目标与范围：按 [09](../09-mcp-memory-tools.md) 实现 `memctl mcp`、`memory_read` 与 `memory_write`、协议循环与错误映射、帮助条目；不修改宿主 MCP 配置。
- 契约：[MCP 记忆工具](../09-mcp-memory-tools.md#4-系统如何工作)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，DSH 0.2.1-rc.2。
- 命令与结果：`go test ./internal/mcp ./internal/cli -count=1` 与 `go test ./tests/integration -run 'TestM0|TestM1' -count=1` 通过，覆盖 `M01`–`M07`、`M10`–`M12`；`gofmt -l` 无输出，`go vet` 通过。真实 DSH 会话中，模型调用 `mcp__memctl__memory_read` 取得 `fileRevision`，`mcp__memctl__memory_write` 走受管事务；同会话 bash 的 `memctl read` 返回 `IO_FAILED`、`touch` 返回 `touch-denied`，证明两条通道的权限确实不同。
- 验收：`M01`–`M08`、`M10`–`M12` 通过；`M09` 不在本轮范围，转入 `T072`。
- 未验证与遗留：除 DSH 外的宿主未做受限会话写入实测；`install --status` 的 MCP 探测输出未实现；`memory_write` 不创建缺失的父目录（既有契约）。
- 下一步：`T072` 补齐宿主侧探测与实测。

## T072：MCP 探测与宿主侧实测

- 目标与范围：补齐 `M09`，即 `install --status` 的 MCP 能力探测与各宿主注册命令输出；复核 `docs/09` 的宿主注册表并补充 Codex 的审批前置条件；在隔离环境完成宿主侧注册与一次受限会话写入。不修改宿主 MCP 配置，不扩大 adapter 契约。
- 契约：[MCP 记忆工具](../09-mcp-memory-tools.md#52-各宿主的注册命令)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，DSH 0.2.1-rc.2，Claude Code 2.1.220，Codex 0.160.0，pi 1.3.0，OpenCode 2.0.26，Grok 1.0.60，Command Code 1.84.0；探针使用隔离 `HOME`、`CODEX_HOME`、`AGENT_MEMORY_STATE` 与不接触真实记忆仓库的临时仓库。
- 命令与结果：新增 `internal/mcp/probe.go`，以 `mcp --agent status` 启动当前二进制、完成握手并读取 `tools/list`，3 秒预算；`internal/cli/mcp_status.go` 在 `install --status` 的适配器列表之后输出 `mcp: serving memory_read, memory_write (<version>)` 与 8 条注册动作，探测失败时输出 `mcp: unavailable: <原因>` 且不输出命令。`go test ./internal/mcp ./internal/cli ./tests/integration -run 'TestM|TestPrintMCPStatus' -count=1` 通过，覆盖 `M09` 的成功、失败与不写文件三个分支；`gofmt -l`、`go vet ./...` 无输出。宿主侧：Claude Code、Codex、pi、OpenCode、Grok、Command Code 各自执行 `mcp add` 后状态命令显示已连接（pi 列出 2 个工具），Antigravity 与 DSH 无等价子命令，注册表中二者改为手工编辑并标注实际文件。Codex `workspace-write` 会话中，`default_tools_approval_mode = "approve"` 时 `memory_read` 返回 `fileRevision`、`memory_write` 返回 `transaction`/`commit`/`revision`/`sync` 并产生提交，同会话 bash 的 `touch ~/memory/x` 返回 `Operation not permitted`、`memctl read` 返回 `IO_FAILED: cannot open repository lock`。
- 验收：`M09` 通过；`M08` 由 DSH 与 Codex 两个宿主覆盖；`M05` 在 Codex 通道复验。`docs/09` 的 5.2、5.3、6.2、7 与 `04`、`README` 已同步。
- 未验证与遗留：Claude Code、Grok、Command Code、OpenCode、Antigravity 与 pi 只确认注册与连接，未在受限会话中完成模型驱动的受管写入；`desktop` profile 未安装 `@deepseek-ai/dsh-mcp-client`，DSH 侧仍按手工插入条目接入；Codex 的 `approval_policy = "never"` 会让 MCP 调用失败，属宿主行为，需要用户侧配置。
- 更正（2026-10-19，`T079`）：上文「`desktop` profile 未安装 `@deepseek-ai/dsh-mcp-client`」的前提不成立。该包与 `dsh-base` 等内核包一起随 DSH 应用分发，位于 `app.asar` 的 `dsh/node_modules/@deepseek-ai/dsh-mcp-client`，空依赖 profile 也能加载；缺的是 profile 里的配置条目，不是包。原文保留，现行结论见 [09](../09-mcp-memory-tools.md#52-各宿主的注册命令)。
- 下一步：如需覆盖其余宿主的受限会话写入，按 `docs/09` 6.2 的相同步骤逐个实测；其余未验证项见 [STATUS](../STATUS.md#当前事实)。

## T073：其余宿主的受限会话实测

- 目标与范围：在隔离环境中完成 pi、OpenCode、Command Code、Grok、Claude Code、Antigravity 的受限会话写入实测，补齐 `M08` 的宿主覆盖；复核 `docs/09` 的注册命令与沙箱结论；不修改宿主配置以外的产品行为。
- 契约：[MCP 记忆工具](../09-mcp-memory-tools.md#62-真实验证边界)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)。
- 验证日期与环境：2026-10-18，macOS arm64，pi 1.3.0，OpenCode 2.0.26，Command Code 1.84.0，Grok 1.0.60，Claude Code 2.1.220，Antigravity 1.4.2；每个宿主使用独立隔离 `HOME`、独立 `AGENT_MEMORY_STATE` 与一次性记忆仓库，探针仓库与真实 `~/memory` 无关；Antigravity 例外，其凭据在系统钥匙串，改用真实登录态与临时配置条目，见下。
- 命令与结果：pi `--exposure direct` 后 `memory_read` 与 `memory_write` 可用，提交 `9bacc1b`；OpenCode 配置 `permission.bash = "deny"` 后 MCP 写入仍成功，提交 `18307b3`，模型明确报告无 bash 工具；Command Code print 模式默认拒绝 shell，MCP 写入成功，提交 `4bd9b7a`；Grok `--sandbox workspace` 下 MCP 子进程与 shell 均被拒绝，改用 `devbox` 或自定义 profile 的 `read_write` 后写入成功，提交 `7072167`、`3e2ecc`；Antigravity 在 `--sandbox` 下 shell 被拒绝（`Operation not permitted`）、MCP 写入成功（提交 `eb15947`），凭据在系统钥匙串，探针临时修改真实 `mcp_config.json` 并加 `env` 指向探针仓库，测完按 SHA-256 恢复。Claude Code 的 `ANTHROPIC_BASE_URL` 指向 `127.0.0.1:15721`，该代理未运行，未实测。
- 验收：`M08` 的宿主覆盖扩展到 Codex、pi、OpenCode、Command Code、Antigravity 与 Grok（Grok 在显式授权下）；`M05` 在六个宿主通道复验。`docs/09` 的 1、3、4.3、5.2、6.2 与 `install --status` 的注册命令已同步；`go test ./internal/cli ./tests/integration -run 'TestPrintMCPStatus|TestM09' -count=1` 通过。
- 未验证与遗留：Claude Code 的写入路径；Grok 的自定义 profile 只验证 `read_write`，未覆盖 `restrict_network`、`deny` 等字段；Antigravity 的探针依赖真实登录态与真实配置文件，未在完全隔离的环境复现。
- 下一步：如 `127.0.0.1:15721` 代理可用，按同一流程补 Claude Code。其余未验证项见 [STATUS](../STATUS.md#当前事实)。

## T074：构建链路提速

- 目标与范围：缩短 CI 与 `make release` 的耗时，不改产品行为与发布产物内容；覆盖集成测试的重复构建、流水线的作业划分与本地发布入口。附带修复 `T072` 引入的 MCP 探测超时缺陷。
- 契约：[单文件打包与 CI](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[MCP 探测](../09-mcp-memory-tools.md#53-探测)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，8 核。
- 命令与结果：先测基线，`go test ./tests/integration -count=1` 为 297 秒，CI 运行 [39093963934](https://github.com/gray0128/memctl/actions/runs/39093963934) 的 `make release` 步为 3:45，其中集成测试 198.7 秒。定位到 55 处 `buildMemctlBinary` 各自 `go build` 一次（缓存命中每次约 0.6 秒），改为进程级 `sync.Once` 共享构建后，集成测试降至 210 秒；`R03` 因需要指定版本号保留独立构建，`newC3Harness` 与 `T048` 复用共享二进制。`make release-dist` 实测 13 秒产出与 `make release` 相同的九个文件，`SHA256SUMS` 六项全部 `OK`。`gofmt -l`、`go vet ./...` 无输出；`go test ./... -count=1` 全量通过（3 分 57 秒）。
- 缺陷修复：全量并行跑测试时 `TestPrintMCPStatusServingBinary` 与 `TestProbeReadsToolsList` 稳定失败于 `signal: killed`。原因是探测预算 3 秒在并行编译争用 CPU 时不够，正常二进制会被误报为 `mcp: unavailable`。将 `ProbeTimeout` 提高到 10 秒并在 `docs/09` 5.3 写明理由；修复后在 `go test ./internal/... ./plugins/...` 三轮并行压力下均通过。
- 作业结构的第一版实测推翻：最初把 CI 拆成 `verify`（`make test`）与 `release`（`needs: verify`）两个串行作业，运行 [39099551113](https://github.com/gray0128/memctl/actions/runs/39099551113) 后总时长从 4:49 升到 5:13——两个作业各自支付约 38 秒环境准备，而原作业本就没有重复跑测试。据此改为分片矩阵：`verify` 四个分片并行、`build` 与测试并行、`publish` 同时依赖两者。
- 分片实现：`scripts/test-shards.txt` 提交「测试名 → 分片号」清单，`scripts/balance-test-shards.sh` 实测耗时后贪心分配，`scripts/test-shard.sh` 按清单过滤执行；清单缺项、分片号越界或分片为空时非零退出。分片按测试名而非按包切分，因为 Go 1.27 无 `-shard` 且集成测试集中在单包。本地并行跑四个分片，各分片集成部分 67–70 秒，全部退出 0；分片执行过的测试名并集与 `go test -list` 的 118 个完全一致，无遗漏或重复。前端与插件 Node 测试只在分片 1 运行。
- 验收：集成测试单进程只构建一次；分片并集覆盖全部 118 个集成测试；`make test` 保持全量语义，`make release` 的本地入口不变；发布产物集合、校验和与 `RELEASE-INFO.txt` 内容未变。
- 真实 CI 复测：运行 [39101067492](https://github.com/gray0128/memctl/actions/runs/39101067492) 总时长 **2:28**，较基线的 4:49 减少 49%。四个分片作业耗时 1:27–2:18，其中 `Run tests` 步 48–79 秒；`build` 作业与测试并行，49 秒完成。最长分片 1 为 2:18，是当前关键路径，其 `Run tests` 为 1:19，比预期高，因为该分片额外承担前端与插件 Node 测试。
- 未验证与遗留：清单在新增或重命名测试后需要人工重新生成，`scripts/test-shard.sh` 只在清单缺项时报错，不能发现清单里残留已删除的测试名；`t.Parallel()` 仍不可用，因为 `a4_credentials_test.go` 等使用 `t.Setenv` 改动进程级环境。
- 下一步：观察下一次 `main` 推送与标签发布的实际耗时，据此调整分片数；如需继续压缩，先消除进程级环境依赖以便包内并行。

## T075：发布 v0.6.0

- 目标与范围：把 `T070`–`T074` 的 MCP 记忆工具通道、`install --status` 的 MCP 探测、宿主侧实测与构建链路提速作为 `v0.6.0` 发布；插件源码自 `v0.5.1` 起未改动，本轮只发 GitHub Release，不发布 npm。
- 契约：[单文件打包与 CI](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.6.0` 指向 `184c521`。
- 命令与结果：`go test ./... -count=1` 全量通过，`gofmt -l` 与 `go vet ./...` 无输出；`scripts/test-shard.sh 1 4` 通过，分片清单与 `go test -list` 的 118 个测试名逐行一致。`make release VERSION=v0.6.0` 通过（前端构建、插件 Node 测试、Go 全量测试），生成四个平台候选二进制、`memctl-dsh-0.5.1.tgz`、`memctl-pi-0.5.1.tgz` 与三个元数据文件；`dist/SHA256SUMS` 六项全部 `OK`；`dist/memctl-darwin-arm64 version` 输出 `memctl v0.6.0`，`mcp --help` 退出 `0`，`install --status` 输出 `mcp: serving memory_read, memory_write (v0.6.0)`，`help --json` 为 `schemaVersion` 1、22 个命令。
- 版本决策：本轮新增 `memctl mcp` 子命令，按语义化版本升次版本号到 `v0.6.0`。插件包按 [dsh 专用插件](../07-dsh-plugin.md#13-实现状态) 的约定应与主次版本对齐，但插件源码自 `v0.5.1` 起未改动，且本轮不发布 npm；若把插件升到 `0.6.0`，`install --status` 的 `pi install npm:memctl-pi@<version>` 提示会指向不存在的 npm 版本。因此插件保持 `0.5.1`，这一偏离按契约「具体发布版本以发布轮次为准」处理并在此记录。
- CI 首次失败与修复：首次推送 tag 的运行 [39102651049](https://github.com/gray0128/memctl/actions/runs/39102651049) 中 `verify` 四个分片与 `build` 全部成功，仅 `publish` 失败于 `fatal: not a git repository`。原因是该作业只下载产物、没有检出仓库，而 `gh release create --generate-notes` 需要与上一个标签比较。补 `actions/checkout` 后删除并重建 tag，运行 [39102971477](https://github.com/gray0128/memctl/actions/runs/39102971477) 全部成功并创建 [Release v0.6.0](https://github.com/gray0128/memctl/releases/tag/v0.6.0)，总时长 4:54。
- 远端复验：用 `gh release download` 重新下载九个附件，`SHA256SUMS` 六项全部 `OK`；下载的 `memctl-darwin-arm64` 输出 `memctl v0.6.0`，`install --status` 输出 `mcp: serving memory_read, memory_write (v0.6.0)`，`RELEASE-INFO.txt` 的 commit 与 tag 指向的 `184c521` 一致。本机 `~/.agent-memory/bin/memctl` 已替换为 Release 二进制，`update --check` 返回 `up to date`。
- 验收：四个候选二进制、两个插件 tarball、`SHA256SUMS` 与 `RELEASE-INFO.txt` 通过；Release 由 CI 的 `verify` 分片与 `build` 作业共同把关，`publish` 在两者成功后创建。`T075` 完成。
- 未验证与遗留：macOS amd64、Linux 与 Windows 仅交叉编译；npm 未发布，`latest` 仍为 `0.5.1`；插件版本与二进制主次版本不一致，如需对齐需单独走插件版本与 npm 发布轮次。
- 下一步：如需让插件跟随二进制版本，单独走一次插件版本与 npm 发布轮次；其余未验证项见 [STATUS](../STATUS.md#当前事实)。

## T076：明确发布入口为标签推送

- 目标与范围：澄清发布入口的权威定义，消除「本地 `make release` 与流水线 `build` 都产出候选包、发布该用哪个」的歧义；不改变产品行为与流水线逻辑。
- 契约：[发布入口](../04-ui-and-cli-specification.md#61-发布入口)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)。
- 验证日期与环境：2026-10-18，macOS arm64；纯文档任务，不运行二进制测试。
- 起因：`T075` 发布时在本地执行了 `make release VERSION=v0.6.0`，该步骤并非契约要求。`04` 第 6 节原文把 `make release` 与 CI 的 `build` 作业并列描述为候选包产出方式，未说明发布时以哪条为准，导致发布轮次多跑一次全量测试并产出一份会被流水线覆盖的 `dist/`。
- 修改内容：`04` 新增 6.1 节「发布入口」，写明发布唯一入口是推送 `v*` 标签、Release 附件只由流水线产生，并给出两条理由（构建环境从干净检出开始；`RELEASE-INFO.txt` 记录的是实际产出该文件的环境）与发布轮次操作顺序；`04` 中「`make release` 仍是本地完整发布入口」改为「用于本地验证构建链路」；`05` 第 4 节新增一条判定依据。README 发布段同步改写：命令序列改为「推标签 → 等待 `publish` → 下载到独立目录校验」，并说明本地 `make release` 不构成发布步骤；同时修正该段仍写着 CI 调用 `make release` 的过期描述（`T074` 已改为 `release-dist`）。
- 验收：文档检查通过——新增锚点可解析，`04`、`05`、README 三处对发布入口的表述一致，本地构建产物在所有位置都不再被当作发布证据。链接与锚点检查脚本无输出。
- 未验证与遗留：无。该轮只澄清契约，`make release` 与流水线的实际行为均未改变。
- 下一步：后续发布轮次按 6.1 的操作顺序执行，本地只跑 `make test`。

## T077：文档一致性检查与更新

- 目标与范围：在 `T071`–`T076` 之后复核文档与实现、文档之间的一致性，修正事实过期、模块与目录清单缺失和重复定义；不改变产品行为。
- 契约：[模块职责](../01-architecture-and-modules.md#4-模块职责)、[目录结构与依赖](../01-architecture-and-modules.md#5-目录结构与依赖)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[发布入口](../04-ui-and-cli-specification.md#61-发布入口)、[MCP 记忆工具](../09-mcp-memory-tools.md#62-真实验证边界)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1；纯文档任务，不运行测试套件，仅编译当前提交的二进制核对帮助输出。
- 检查与结果：根 `README.md`、`AGENTS.md`、`待办.md` 与 `docs/` 下全部 Markdown 共 28 个文件逐条解析，失效链接与锚点 0；`help --json` 的 22 个命令与 `04` 第 1 节一致；`internal/server` 的 23 条 API 路由与 `02` 4.2 表一致。`01` 第 5 节补入 `docs/09`、`internal/mcp/`、`internal/update/`，第 4 节补入 MCP 协议适配与自更新两行，第 1 节流程图与受管写入定义补入 MCP 通道；`03` 第 2 节把 MCP 工具列为同一受管事务入口。README 与 `09` 的 MCP 宿主验证状态改为 DSH、Codex、pi、OpenCode、Command Code、Antigravity 已实测，Grok 需 `read_write`，Claude Code 未实测；`09` 2.3 删除与 6.2 冲突的旧表并改指 6.2；`09` 5.2 标明 Antigravity 使用真实登录态而非隔离 `HOME`。`04`、`05` 注明插件 tarball 的 `<version>` 取插件包自身版本；`07`、`08` 补记 `v0.6.0` Release 仍附带 `0.5.1` tarball；`04` 的 Makefile 片段补齐 `release-dist` 与 `test-shard`。`待办.md` 删除已完成的 README 条目并标注 release 提速进度。
- 验收：文档检查通过；链接与锚点脚本无失效项，CLI 命令与 REST 路由核对无差异，跨文档表述一致。`T076` 完成记录已归档到 [阶段 F](#t076明确发布入口为标签推送)。
- 未验证与遗留：文档检查不替代二进制、平台或宿主测试；本轮未改代码，未重跑测试套件。
- 下一步：可选事项为配套 Skill、Codex 插件调研、Dreaming 真实 runner 验证与 pi.dev 收录；未验证项见 [STATUS](../STATUS.md#当前事实)。

## T078：配套 Agent Skill

- 目标与范围：为使用 memctl 的 Agent 提供 `skills/memctl/`，覆盖读取与写入时机、按基线受管写入、同步与冲突恢复、沙箱宿主改用 MCP 工具；通过 `skills` CLI 从 GitHub 在线安装到 `~/.agents/skills`，不改变产品行为。
- 契约：[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[MCP 记忆工具](../09-mcp-memory-tools.md#4-系统如何工作)、[目录结构与依赖](../01-architecture-and-modules.md#5-目录结构与依赖)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1。
- 结构：`SKILL.md` 的 frontmatter 只有 `name` 与 `description`，name 为 `memctl`；正文按「确认环境 → 判断读写 → 受管写入 → 同步与冲突 → MCP 降级 → 边界」组织，每步给出完成标准；`references/memory-conventions.md` 与 `references/recovery.md` 由入口按需链接。
- 事实核对：Skill 中的 `read`、`write`、`sync`、`resolve`、`recover`、`mcp`、`install` 命令与参数逐一对照当前提交构建的 `help --json`；错误码取自 `internal/memerr` 并对照 `02` 4.3；冲突解决 JSON 字段对照 `app.ConflictView` 的 `baseRevision`、`conflictId` 与 `files`。
- 验收：三个 Skill 文件存在，frontmatter 可解析且无额外字段，入口链接可解析；`README` 与 `01` 第 5 节已登记 `skills/memctl/`；已用 `skills add gray0128/memctl -g -s memctl -a codex pi opencode claude-code -y` 在线安装到 `~/.agents/skills/memctl`，`~/.agents/.skill-lock.json` 记录来源 `gray0128/memctl`，`skills list -g --json` 可见，宿主技能目录识别出 `memctl` 并通过 `skill` 工具加载；全量 Markdown 链接与锚点检查无失效项；`git diff --check` 通过。
- 未验证与遗留：真实任务中的自动触发与执行未验证。
- 下一步：需要时分发到宿主 skills 目录，并用真实会话验证触发与执行。

## T079：DSH 的 MCP 通道生效

- 目标与范围：在本机 `desktop` profile 注册 `memctl` 的 MCP server，确认 DSH 的 MCP 通道可用；更正 `09` 5.2 与 `T072` 中把「profile 未安装 `@deepseek-ai/dsh-mcp-client`」当作阻塞原因的错误前提。不改变产品代码与协议契约。
- 契约：[MCP 记忆工具](../09-mcp-memory-tools.md#52-各宿主的注册命令)。
- 验证日期与环境：2026-10-19，macOS arm64，DSH 0.2.1-rc.2，`memctl v0.6.0`。
- 排查与结果：`install --status` 的 `mcp:` 块正常，`dsh:` 块只有手工编辑提示；`desktop` profile 的 `cordis.patch.yml` 无 mcp 条目，`package.json` 的 bundles 与 `node_modules` 也没有 mcp-client，这与「无需安装」并不矛盾——该包与其他内核包一起放在应用包的 `dsh/node_modules/@deepseek-ai/dsh-mcp-client`。用空依赖的隔离 profile 加一条 `insert:` patch 启动 `dsh`，探针记录到两次 `mcp --agent dsh` 派生（连接探测与正式连接），证明包可加载、缺的只是配置条目。补丁写法只有 `insert:` 有效：不带 `insert` 的顶层 `- id:` 记录分别报 `patch: entry "<id>" not found` 与 `id is required for non-insert patches`。
- 配置改动：`~/.dsh/profiles/desktop/cordis.patch.yml` 追加一条 `insert:`，`id` 为 `mcp-memctl`、`serverName` 为 `memctl`、`transport` 为 `stdio`、`command` 为 `/Users/libo/.agent-memory/bin/memctl`、`args` 为 `["mcp","--agent","dsh"]`；改动前已备份原文件。
- 验收：`dsh --profile <隔离 profile> --patch <真实补丁文件> --dump-config` 能合成出 `mcp-memctl` 条目，其余 4 条 id 定向补丁因目标不存在而被跳过，符合预期；保存后 13 秒内宿主派生 `memctl mcp --agent dsh`（PID 7710，父进程为 DSH 主进程 `61792`），无需重启应用；本轮已通过该通道调用 `memory_read`，返回的 `fileRevision` 与 CLI `read --json` 一致。
- 文档更正：`09` 5.2 的 DSH 行改为「该包随 DSH 应用分发，profile 无需安装」，并新增配置片段、`insert:` 必需性、免重启生效与工具名说明；`T072` 记录以「更正」条目保留原文并指向现行结论。
- 未验证与遗留：只在 macOS arm64 的 DSH 0.2.1-rc.2 验证；其他平台与其他 DSH 版本的路径与热加载行为未复验。
- 下一步：其他 DSH 用户按 `09` 5.2 手工插入同一条目；如需让 memctl 自动写入该条目，需要另行设计并扩大 `install` 的边界。

## T080：记忆更新与过期策略调研

- 目标与范围：调研其他 Agent 记忆项目如何处理记忆更新与过期，回答「过期记忆应删除还是保留完整时间线」，并把结论固化为规则文档 [10. 记忆更新与过期](../10-memory-lifecycle.md)。本轮只做调研与文档，不实现过滤、过期或删除能力。
- 契约：[10. 记忆更新与过期](../10-memory-lifecycle.md)、[DOMAIN 格式保真](../02-domain-models-and-contracts.md#12-格式保真)、[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[受管写入与条目删除](../02-domain-models-and-contracts.md#42-路由)。
- 验证日期与环境：2026-10-19，macOS arm64；调研通过官方文档与仓库源码直取，未依赖二手转述。
- 调研来源：Mem0 的 [how-it-works](https://docs.mem0.ai/core-concepts/how-it-works)、[memory expiration](https://docs.mem0.ai/platform/features/memory-expiration)、[history API](https://docs.mem0.ai/api-reference/memory/history-memory) 与 [v2→v3 迁移说明](https://docs.mem0.ai/migration/oss-v2-to-v3)；Zep 的 [facts](https://help.getzep.com/v4/facts)、[how graph creation works](https://help.getzep.com/v4/how-graph-creation-works)、[context retention](https://help.getzep.com/v4/context-retention)、[observations](https://help.getzep.com/v4/observations)；Letta 的 [memory blocks](https://docs.letta.com/guides/agents/memory-blocks) 与 [MemFS](https://docs.letta.com/concepts/memfs)；[LangMem 概念指南](https://langchain-ai.github.io/langmem/concepts/conceptual_guide/)；[Anthropic memory tool](https://platform.claude.com/docs/en/agents-and-tools/tool-use/memory-tool)；[Cognee forget](https://docs.cognee.ai/core-concepts/main-operations/forget)；arXiv 论文 [Mem0](https://arxiv.org/abs/2504.19413)、[Zep](https://arxiv.org/abs/2501.13956)、[MemGPT](https://arxiv.org/abs/2310.08560)、[MemoryBank](https://arxiv.org/abs/2305.10250)、[A-MEM](https://arxiv.org/abs/2502.12110)、[Generative Agents](https://arxiv.org/abs/2304.03442)。
- 结论：默认删除过期记忆，但删除只作用于注入与检索可见的当前内容，历史由 Git 提交保存；不默认保留完整时间线。记忆按状态型、事件型、有时效型三档处理，状态型就地改写、事件型追加保留、有时效型带 `until` 元数据到期隐藏。Zep 的失效而非删除与 Mem0 的 ADD-only 收敛是两条关键对照：前者说明矛盾事实不能并存，后者说明让模型自动判删不可靠。
- 代码核对：`internal/app/session.go` 的 `buildSessionBody` 注入 `MEMORY.md` 全文，无过滤点；`internal/vault/parse.go` 以 `MetadataField.Raw` 保留开放元数据，未读取 `until`；`internal/app/entries.go` 的 `DeleteEntry` 已提供条目级删除；`internal/dream` 的 `ChangeDelete` 已进入提案 Diff 与固定 Diff 展示。文档 §4 按此登记现状。
- 验收：新增 `docs/10-memory-lifecycle.md`，`docs/README.md` 第 10 行导航、`docs/01` 第 5 节目录树已登记；`docs/STATUS.md` 的能力段改为「01–10」并新增 `T080` 行。全量 Markdown 链接与锚点检查无失效项；`git diff --check` 通过。
- 未验证与遗留：文档中的过滤与过期规则尚未实现，`until` 过滤点（注入阶段或 `read` 阶段）、隐藏期长度与事件型记忆目录归属三项未决，记录在 §7。
- 下一步：确认 §7 的三个未决问题后再拆实现任务，并补充 §6 列出的验收场景编号。
## T081：注入条目失效的判定规则

- 目标与范围：把 [10. 记忆更新与过期](../10-memory-lifecycle.md) 第 3 节的分类规则落到每轮注入的内嵌默认指引，并同步 Skill 约定；不改变命令、协议与提案机制。
- 契约：[10. 记忆更新与过期](../10-memory-lifecycle.md#3-记忆分类与处理规则)、[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[Web UI 与内嵌指引注入](../04-ui-and-cli-specification.md#4-web-ui)。
- 验证日期与环境：2026-10-19，macOS arm64，Go 1.27.1。
- 改动：`internal/assets/INSTRUCTIONS.md` 的 Writing 段新增「After an entry stops being true」小节（状态型就地改写或删除、事件型追加并保留旧条目、不维护 changelog），并把原「Update or delete entries that became wrong」改为指向该小节的表述，避免两处规则冲突；`skills/memctl/SKILL.md` 第 2 节与 `skills/memctl/references/memory-conventions.md` 的条目格式段同步同一规则。
- 契约同步：`04` 第 6 节在用户指引导段之后补一段，说明该小节的规则来源与注入路径，类型划分改指 `10`；`05` 的 `R01` 预期结果补入新规则的断言点；`10` 第 4 节把「写入时的类型判定」登记为已有内嵌指引与 Skill 约定、仍依赖模型执行，第 6 节补一条验收场景，第 7 节补一条未决问题（是否需要用 `doctor` 做确定性检查）。
- 验证：`go test ./...` 全量通过；`go vet ./...` 无输出；`TestR01EmbeddedInstructionsFromEmptyHome` 增加对 `### After an entry stops being true`、`rewrite the entry in place` 与 `No change log in memory files` 的断言并单独重跑通过；`make build` 后执行 `./bin/memctl start --json`，实际注入正文含新小节；全量 Markdown 链接与锚点检查无失效项；`git diff --check` 通过。
- 未验证与遗留：新规则只约束模型行为，没有确定性检查；实际会话中是否遵守需要观察。指引篇幅从 2322 字节增加到 2913 字节，每轮注入的增量约 590 字节。
- 下一步：观察真实写入是否符合三档规则；若出现「把决策条目当错误改写」的实例，再考虑用 `doctor` 诊断补充确定性提示。

## T082：基于 GitHub Actions 的 Dreaming 远端整理方案与契约修订（`G-1`）

- 目标与范围：针对 [Dreaming 交接文档](./dreaming-handoff.md) 记录的本地沙箱 CLI runner 凭据阻塞，按用户确认的四项设计决策（删除本地 runner 与 Seatbelt 沙箱、代码控制的有界多步结构化调用、内置四家模型提供商并由用户配置 API Key、依托 GitHub Actions 自用且支持自部署），完成技术调研、撰写完整方案文档 [11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)，并修订权威契约 `01`–`05`。本轮属于阶段 G 的 `G-1`，只做方案设计与契约修订，不改动产品代码。
- 契约：[11. GitHub Actions Dreaming](../11-github-actions-dreaming.md)、[架构与边界](../01-architecture-and-modules.md#3-仓库路径与信任边界)、[模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Dreaming 提案](../03-workflows-and-dataflows.md#6-dreaming-提案)、[CLI 与 UI](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#阶段-g-的实施顺序)。
- 验证日期与环境：2026-10-19，macOS arm64。
- 调研结论：
  1. 通道协议：利用记忆仓库已有的已批准 GitHub 远端（`origin`）投递临时分支 `refs/heads/dream/req/<proposalId>` 触发 `.github/workflows/memctl-dream.yml`，Worker 写回 `refs/heads/dream/res/<proposalId>` 并在提交正文附带 `DreamWorkerMeta` JSON 块；失败时推送空提交携带脱敏错误摘要，使本地 `memctl` 在 5 秒轮询周期内立即收敛为 `failed`，无需本机额外配置 GitHub API Token。
  2. GitHub Actions 边界：默认 `GITHUB_TOKEN` 推送分支不会递归触发 `push` 工作流，且无 `workflows` 权限无法修改 `.github/workflows/`；本地必须在 `vault` 与 `dream.protectedPath` 中将 `.github` 纳入禁止编辑范围，防止 `doctor` 误报孤儿文件并阻断篡改后的远端分支借本地 `ApplyDream` 修改工作流。
  3. 有界多步结构化调用：`memctl dream --worker` 在 Actions 内按「阶段 1（静态扫描与分簇）→ 阶段 2（受控循环结构化调用 + Step Guard 护栏，最多 6 步）→ 阶段 3（跨文件一致性收敛）」执行，每步强约束 JSON Schema 并融合 [10. 记忆更新与过期](../10-memory-lifecycle.md) 的三档分类规则（状态型就地改写、事件型保护、死链修复）。
  4. 内置四个模型提供商：`deepseek`（`deepseek-chat` + `json_object`）、`minimax`（`MiniMax-M2.7` 非流式 JSON）、`openai`（`gpt-5.4-mini` + `json_schema` strict）、`anthropic`（`claude-sonnet-4-6` + `tools`/`tool_choice`），API Key 仅存于仓库 GitHub Actions Secrets。
- 改动与契约同步：新增 `docs/11-github-actions-dreaming.md`；更新 `docs/README.md`、`docs/01`–`docs/05`、`docs/validation/dreaming-handoff.md` 与 `docs/STATUS.md`，补入阶段 G（`G-1`–`G-4`）实施顺序与 `D01`–`D05` 验收场景。
- 验证：全量 Markdown 相对链接与锚点检查通过；`git diff --check` 无输出。
- 未验证与遗留：产品代码尚未迁移（当前代码仍为 `D-1`/`D-2` 本地沙箱实现），留待 `G-2`–`G-4` 实施与真实验证。
- 下一步：按 `G-2` 删除本地沙箱与 CLI runner，实现 `internal/dream/provider.go`、`internal/dream/worker.go` 与 `memctl dream --worker`。
