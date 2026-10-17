# 阶段 E 与发布后归档

本文件归档阶段 E 与发布后轮次的完成记录。当前任务状态、未验证项与下一步见 [开发状态](../STATUS.md)；阶段 E 的步骤定义见 [验收与交付](../05-validation-and-delivery.md#阶段-e-的实施顺序)。

## T048：memctl-dsh 主机插件与发布产物

- 目标与范围：实现阶段 E 的 `E-1`–`E-4`：`plugins/dsh/` 包骨架与 Host 生命周期、`memctl dsh plugin-path`/`plugin-dir` 的 `go:embed` 运行时产物、dsh 原生插件与 legacy `dsh-hooks-plugin` 探测、安装提示，以及 Release 插件 tarball 与校验和。`D1` 取 A；npm 包、独立 GitHub 仓库与 Discover 索引不在本轮范围。
- 契约：[dsh 专用插件](../07-dsh-plugin.md)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#阶段-e-的实施顺序)、[模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，DSH 0.2.1-rc.2；隔离 `HOME`、`DSH_HOME`、状态根与 `test`/`rel` profile。
- 命令与结果：`make test` 通过（前端 16 文件 85 项；插件 Node 13 项；Go 含 `tests/integration` 257.2 秒）。`TestT048DshPluginTarballInstall` 通过（5.94 秒）：`memctl dsh plugin-path` 输出存在的绝对 tarball，`dsh plugin --profile test add` 安装成功，`dsh --profile test --dump-config` 出现 `memctl-dsh`，删除 tarball 后仍可加载，`install --status` 返回 `dsh: configured host=dsh@0.2.1-rc.2 plugin@0.1.2` 且未生成 `hooks.json`；同流程用 `dist/memctl-dsh-0.1.2.tgz` 在 `rel` profile 复验通过。`VERSION=v0.1.2 make release` 通过并生成四个候选二进制、`memctl-dsh-0.1.2.tgz` 与三个元数据文件；`shasum -a 256 -c SHA256SUMS` 五个文件全部 `OK`；发布二进制内嵌的 `dsh plugin-path` 可输出运行时 tarball。构造 `engines.dsh: ">=9.0.0 <10.0.0"` 的 tarball 后，DSH 以退出码 `1` 拒绝安装并恢复 `package.json`，组合配置中 `memctl-dsh` 计数为 0。
- 验收：`I04` 通过，`I05` 通过，`I03`、`I06` 部分通过（隔离 profile 安装、插件状态、绝对 tarball 与目录路径；npm 包名、界面输入、升级与卸载未实测）。`H07`–`H10` 未在真实 DSH 会话执行；事件映射、单飞同步、子代理跳过与卸载清理由假宿主 Node 测试覆盖。
- 未验证与遗留：真实 DSH 会话的 `systemPrompt.context` 注入、首轮前快照、`turn/end` 提交、`agent/disposed` 收尾、恢复与子代理行为；DSH 0.2.1-rc.2 范围外版本的拒绝已在构造包上验证，未在每个 DSH 小版本复验；npm、独立 GitHub 仓库与 Discover 索引未发布；macOS amd64、Linux 与 Windows 未运行。
- 下一步：`E-1`–`E-4` 已完成；`E-5`（真实 DSH 会话与界面安装）登记为 `T052`，按 [停止条件](../06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

## T053：发布 v0.2.0

- 目标与范围：将 `T048` 的 `memctl-dsh` Host 插件、`memctl dsh` 子命令、dsh 原生插件探测与插件 tarball 发布为 `v0.2.0`；按设计约定把插件包版本从 `0.1.2` 同步到 `0.2.0`。不改变插件生命周期、CLI 参数或适配器状态语义。
- 契约：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[dsh 专用插件](../07-dsh-plugin.md)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.2.0` 指向 `3e965d4`。
- 命令与结果：`make test` 全量通过（前端 16 文件 85 项；插件 Node 13 项；Go 含 `tests/integration` 243.2 秒）。tag 推送后 [GitHub Actions 运行 39030270763](https://github.com/gray0128/memctl/actions/runs/39030270763) 在 2 分 56 秒内通过并创建 [Release v0.2.0](https://github.com/gray0128/memctl/releases/tag/v0.2.0)；附件含四个平台二进制、`memctl-dsh-0.2.0.tgz`、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`。下载 `memctl-darwin-arm64` 与插件 tarball 后，按发布 `SHA256SUMS` 校验两者 `OK`；`memctl-darwin-arm64 version` 输出 `memctl v0.2.0`；安装到 `~/.agent-memory/bin/memctl` 后 SHA-256 为 `49c061c3…a012dac9`，与发布校验和一致；`memctl dsh plugin-path` 输出 `~/.agent-memory/dsh/memctl-dsh-0.2.0.tgz`，其常规文件清单与发布 tarball 一致，运行时打包的 SHA-256 为 `972a1f46…c1d06d2c`，与 npm pack 产物的 `8d71b7ab…afc9089d` 不同，符合设计不要求字节一致。旧二进制备份为 `~/.agent-memory/bin/memctl.v0.1.2`，不进入仓库。
- 未验证与遗留：真实 DSH 会话中的注入已在本机 `desktop` 确认，每轮同步、收尾、恢复与子代理由 `T052` 跟踪；macOS amd64、Linux 与 Windows 仍只交叉编译；npm、独立 GitHub 仓库与 Discover 索引未发布。
- 下一步：`T052` 进行中，注入、每轮同步与子代理跳过已确认，剩余 `agent/disposed`、恢复、失败路径与界面安装；按 [停止条件](../06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。

## T054：宿主会话结束映射与 DSH 收尾兜底

- 目标与范围：把回合结束、压缩与会话结束的边界写入 Hook 契约；补齐 Codex 的 `SessionEnd`/`PreCompact`、OpenCode 的 `session.deleted`/`session.compacted` 与 DSH 插件卸载时对仍 live 会话的 `end` 兜底；补对应模板与回归测试。不改变 `end` 的 CLI 语义，不新增 adapter capability 字段。
- 契约：[Hook 生命周期事件映射](../04-ui-and-cli-specification.md#32-宿主生命周期事件映射)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[dsh 同步与收尾](../07-dsh-plugin.md#54-同步与收尾)。
- 验证日期与环境：2026-10-17，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；Codex 0.160.0、OpenCode 2.0.26、DSH 0.2.1-rc.2 的模板与假宿主测试环境；全量测试在具备 macOS `sandbox-exec` 权限的受控环境运行。
- 命令与结果：`make test` 全量通过（前端 16 文件 85 项；插件 Node 14 项；Go 含 `tests/integration` 248.0 秒）。其中 `TestC3EmbeddedTemplatesCoverR01` 覆盖 Codex `SessionEnd`/`PreCompact` 与 OpenCode `session.deleted`/`session.compacted` 的安装结果，`TestT048DshPluginTarballInstall` 通过。`TestRegistryHostProtocols` 固定 Codex 与 OpenCode 命令形状；DSH 插件测试 14 项覆盖 `agent/disposed` 一次 `end`、卸载兜底一次 `end`，以及已 disposed 后不重复。
- 验收：`H11` 在模板与假宿主层面通过；`H07`、`H10`、`I01` 回归通过。真实 Codex 与 OpenCode 会话结束事件、真实 DSH 退出兜底未执行。
- 未验证与遗留：真实会话结束路径；Claude Code `StopFailure`/`PreCompact` 与 Grok `StopFailure`/`PreCompact` 未补；Antigravity `Stop` 语义待确认；Command Code 无 `SessionEnd`；插件版本未提升，未重新构建发布 tarball。
- 下一步：`T052` 重新安装插件并正常退出 DSH，确认恰好一次 `end`；再评估补齐 Claude Code 与 Grok 的缺失事件与发布补丁版本。

## T055：发布 v0.2.1

- 目标与范围：把 `T054` 的宿主会话结束映射作为 `v0.2.1` 发布；插件包版本提升到 `0.2.1`，运行全量测试与 `make release`，校验四个候选二进制和插件 tarball，推送 tag 触发 GitHub Release。不改变 `end` 的 CLI 语义或新增 adapter 能力。
- 契约：[单文件打包](../04-ui-and-cli-specification.md#6-单文件打包)、[发布前检查](../05-validation-and-delivery.md#4-发布前检查)、[dsh 专用插件](../07-dsh-plugin.md)。
- 验证日期与环境：2026-10-17，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；tag `v0.2.1` 指向 `9c136e1`；全量测试在具备 macOS `sandbox-exec` 权限的受控环境运行。
- 命令与结果：`VERSION=v0.2.1 make release` 全量测试通过（前端 16 文件 85 项；插件 Node 14 项；Go 含 `tests/integration` 246.5 秒），生成四个平台候选二进制、`memctl-dsh-0.2.1.tgz` 与三个元数据文件；`dist/SHA256SUMS` 五个产物全部 `OK`，`dist/memctl-darwin-arm64` 与 `memctl-darwin-amd64 version` 均输出 `memctl v0.2.1`，插件 tarball 版本为 `0.2.1` 且包含卸载兜底代码。tag 推送后 [GitHub Actions 运行 39034737007](https://github.com/gray0128/memctl/actions/runs/39034737007) 在 3 分 1 秒内通过并创建 [Release v0.2.1](https://github.com/gray0128/memctl/releases/tag/v0.2.1)；Release 附件含四个平台二进制、`memctl-dsh-0.2.1.tgz`、`SHA256SUMS`、`THIRD_PARTY_LICENSES.txt` 与 `RELEASE-INFO.txt`。下载 Release 附件后按发布 `SHA256SUMS` 校验，五个产物全部 `OK`；发布二进制输出 `memctl v0.2.1`。
- 验收：`make test` 全量通过；本地 `dist/SHA256SUMS` 与下载的 Release 附件校验全部 `OK`；tag `v0.2.1` 的 CI Release 成功；`T054` 的 `H11`、`H07`、`H10`、`I01` 回归包含在发布前测试中。
- 未验证与遗留：真实 DSH 退出兜底、真实 Codex `SessionEnd` 与 OpenCode `session.deleted` 仍未执行；macOS amd64、Linux 与 Windows 仅交叉编译；本机 `~/.agent-memory` 中的 memctl 与 `memctl-dsh` 尚未升级到 `v0.2.1`。
- 下一步：`T052` 安装 `v0.2.1` 的 `memctl-dsh` 后正常退出 DSH，确认恰好一次 `end`；随后处理恢复、失败路径与界面安装。

## T056：新增 update 命令

- 目标与范围：为单文件 CLI 增加 `update` 与 `update --check`：查询 `gray0128/memctl` 的最新 GitHub Release，按 `runtime.GOOS/GOARCH` 选择已发布资产，读取同一 Release 的 `SHA256SUMS`，在目标目录写入临时文件并同步计算 SHA-256；校验通过后沿用权限、flush 并以同目录 rename 原子替换当前二进制。Windows 运行中不可替换，返回 `UNSUPPORTED`；`--check` 只查询不写文件。不修改记忆仓库、不读取凭据、不自检下载的二进制，也不自动重启。
- 契约：[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[`update` 自更新](../04-ui-and-cli-specification.md#12-update-自更新)、[验收矩阵](../05-validation-and-delivery.md#2-核心测试矩阵) 的 `R05`。
- 验证日期与环境：2026-10-17，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1；真实 Release `v0.2.1` 与临时目录中的候选二进制；全量测试在具备 macOS `sandbox-exec` 权限的受控环境运行。
- 命令与结果：`make test` 全量通过（前端 16 文件 85 项；插件 Node 14 项；Go 含 `tests/integration` 261.3 秒）。`go test ./internal/update ./internal/cli` 通过（更新包 11 项，CLI 含新增用法错误）；`go vet ./internal/update ./internal/cli` 通过；`GOOS=linux GOARCH=amd64` 与 `GOOS=windows GOARCH=amd64` 交叉编译通过。以 `-X main.version=v0.2.0` 构建临时二进制后，`update --check` 输出 `current: v0.2.0`、`latest: v0.2.1`、`update available`；`update` 从真实 Release 下载并校验 `memctl-darwin-arm64`，输出 `updated: /private/tmp/memctl-update-smoke`，替换后 `version` 输出 `memctl v0.2.1`，退出码 `0`，目标目录无临时文件；以 `v0.2.1` 重新构建后执行 `update`，输出 `up to date`，文件 mtime 未变化。合成服务器覆盖校验和不匹配、`SHA256SUMS` 缺失、资产缺失与不支持平台，均保持原文件不变。
- 验收：`R05` 通过（已最新、有新版本、校验和不匹配、资产缺失与不支持平台；真实 Release 下载、校验与原子替换在 darwin/arm64 执行）。既有 `F01`–`R04` 场景由全量 `make test` 回归通过。
- 未验证与遗留：macOS amd64、Linux amd64 与 Windows amd64 的运行时替换；Windows 的 `UNSUPPORTED` 分支只做单元测试与交叉编译；断网、GitHub 403 速率限制与目标目录无写权限未做真实故障注入；`update` 只依赖发布校验和，不额外校验新二进制的可执行性。
- 下一步：下次发布后用安装位置的实际二进制验证一次 `update`；`T052` 继续真实 DSH 会话收尾验证。
