# 阶段 F 归档：pi 专用插件

本文件归档阶段 F（pi 专用插件）及后续发布后轮次的完成记录，含 `T060`–`T071`。阶段步骤与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-f-的实施顺序)，当前进度见 [开发状态](../STATUS.md)。

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
