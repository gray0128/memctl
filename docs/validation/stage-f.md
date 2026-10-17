# 阶段 F 归档：pi 专用插件

本文件归档阶段 F（pi 专用插件）各任务的完成记录，含 `T060`、`T062`、`T063`。阶段步骤与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-f-的实施顺序)，当前进度见 [开发状态](../STATUS.md)。

## T060：实现 pi 专用插件

- 目标与范围：对应阶段 F 的 `F-1` 至 `F-4`。新增 `plugins/pi/` 原生扩展、Node 单元测试与 `go:embed`；实现 `memctl pi plugin-dir`；pi 适配器增加原生插件探测（npm、状态目录、settings 本地包）、旧 `agent-memory-*` 条目内容探测、宿主与插件版本门槛、安装提示与按接入路径报告 capabilities；增加 `make pi-plugin-test`/`pi-plugin-pack` 与 Release 审计 tarball。不发布 npm 包，不修改真实 pi 配置。
- 契约：[pi 专用插件](../08-pi-plugin.md)、[目录结构](../01-architecture-and-modules.md#5-目录结构与依赖)、[AdapterStatus](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，pi `1.3.0`；隔离 `HOME`/`PI_CODING_AGENT_DIR`、本地 mock OpenAI 兼容端点与假 memctl。
- 命令与结果：`node --test plugins/pi/test/*.test.js` 15 项通过；`go test ./internal/... ./plugins/... -count=1` 通过；`go test ./tests/integration -count=1` 通过（269.3 秒）；`go vet` 与 `gofmt` 无输出；`npm pack --dry-run` 只包含 `index.js`、`README.md`、`package.json`，无依赖与安装脚本；隔离 `pi install <plugins/pi>`、`pi list`、`pi remove` 可用，settings 中的相对路径仍被适配器识别；隔离 pi 会话观察到 `start` 后模型请求包含 `<memctl>` section，退出触发 `end`。证据见 [验证记录](./t060-pi-plugin.md)。
- 验收：`H12`–`H15` 的单元部分通过；`I07`、`I08`、`I09`、`I10` 通过。真实 pi 会话、真实模型、npm 发布与非 macOS 平台未验证。
- 未验证与遗留：`agent_settled` 的 `sync` 在 `pi -p` 立即退出时被 shutdown 取消，最终同步由 `end` 完成；交互式会话、恢复、分叉与子代理行为待 `F-5`；npm 包名与许可证、Windows/Linux 进程清理未验证。
- 下一步：`F-5` 在真实 pi 会话验证注入、同步提交、收尾、恢复、分叉与失败降级；npm 发布与版本提升留给后续发布轮次。

## T062：真实 pi 会话验证

- 目标与范围：对应阶段 F 的 `F-5`。把本机 pi 从旧 `pi-yaml-hooks` 条目迁移到 `memctl-pi@0.5.0`，用真实模型和真实记忆仓库验证注入、同步、收尾、恢复、分叉、子代理与失败降级；不改变插件代码。
- 契约：[pi 专用插件](../08-pi-plugin.md)、[验收 H12–H15](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-18，macOS arm64，pi `1.3.0`，memctl `v0.5.0`，真实仓库 `~/memory`。
- 命令与结果：旧 `hooks.yaml` 已备份到 `~/.agent-memory/backups/pi-hooks-20261018-004927.yaml`；`pi install /Users/libo/.agent-memory/pi` 成功；旧 `agent-memory-*` 条目移除，`install --status` 返回 `configured plugin@0.5.0`。`pi --model deepseek/deepseek-flash --no-tools -p` 退出码 `0`，会话 system sections 含 `<memctl>` 与 `[agent-memory]` 正文；退出后同步缓存更新为 `synced`，`lastSuccessAt=2026-10-17T16:49:47Z`。交互式会话 `01a14ac7` 中两次 `memctl write` 产生提交 `ff2d05d`、`4a0f82a`，同步缓存于 3 秒内更新为 `synced`，HEAD 为 `4a0f82a`；当前日志不能区分该次同步由 `agent_settled` 还是 `end` 触发。
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
- 未验证与遗留：macOS amd64、Linux 与 Windows 仅交叉编译；DSH 界面安装与 `desktop` profile 升级未复验。
- 下一步：按需把本机 `desktop` profile 的 `memctl-dsh` 升级到 `0.5.1`，或在 DSH 界面验证 npm 包安装。
