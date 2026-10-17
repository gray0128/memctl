# 阶段 E 归档：DSH 专用插件

本文件归档阶段 E 的完成记录。当前任务状态、未验证项与下一步见 [开发状态](../STATUS.md)；阶段 E 的步骤定义见 [验收与交付](../05-validation-and-delivery.md#阶段-e-的实施顺序)。

## T048：memctl-dsh 主机插件与发布产物

- 目标与范围：实现阶段 E 的 `E-1`–`E-4`：`plugins/dsh/` 包骨架与 Host 生命周期、`memctl dsh plugin-path`/`plugin-dir` 的 `go:embed` 运行时产物、dsh 原生插件与 legacy `dsh-hooks-plugin` 探测、安装提示，以及 Release 插件 tarball 与校验和。`D1` 取 A；npm 包、独立 GitHub 仓库与 Discover 索引不在本轮范围。
- 契约：[dsh 专用插件](../07-dsh-plugin.md)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#阶段-e-的实施顺序)、[模型与契约](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)。
- 验证日期与环境：2026-10-17，macOS 26.6.2 arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，DSH 0.2.1-rc.2；隔离 `HOME`、`DSH_HOME`、状态根与 `test`/`rel` profile。
- 命令与结果：`make test` 通过（前端 16 文件 85 项；插件 Node 13 项；Go 含 `tests/integration` 257.2 秒）。`TestT048DshPluginTarballInstall` 通过（5.94 秒）：`memctl dsh plugin-path` 输出存在的绝对 tarball，`dsh plugin --profile test add` 安装成功，`dsh --profile test --dump-config` 出现 `memctl-dsh`，删除 tarball 后仍可加载，`install --status` 返回 `dsh: configured host=dsh@0.2.1-rc.2 plugin@0.1.2` 且未生成 `hooks.json`；同流程用 `dist/memctl-dsh-0.1.2.tgz` 在 `rel` profile 复验通过。`VERSION=v0.1.2 make release` 通过并生成四个候选二进制、`memctl-dsh-0.1.2.tgz` 与三个元数据文件；`shasum -a 256 -c SHA256SUMS` 五个文件全部 `OK`；发布二进制内嵌的 `dsh plugin-path` 可输出运行时 tarball。构造 `engines.dsh: ">=9.0.0 <10.0.0"` 的 tarball 后，DSH 以退出码 `1` 拒绝安装并恢复 `package.json`，组合配置中 `memctl-dsh` 计数为 0。
- 验收：`I04` 通过，`I05` 通过，`I03`、`I06` 部分通过（隔离 profile 安装、插件状态、绝对 tarball 与目录路径；npm 包名、界面输入、升级与卸载未实测）。`H07`–`H10` 未在真实 DSH 会话执行；事件映射、单飞同步、子代理跳过与卸载清理由假宿主 Node 测试覆盖。
- 未验证与遗留：真实 DSH 会话的 `systemPrompt.context` 注入、首轮前快照、`turn/end` 提交、`agent/disposed` 收尾、恢复与子代理行为；DSH 0.2.1-rc.2 范围外版本的拒绝已在构造包上验证，未在每个 DSH 小版本复验；npm、独立 GitHub 仓库与 Discover 索引未发布；macOS amd64、Linux 与 Windows 未运行。
- 下一步：`E-1`–`E-4` 已完成；`E-5`（真实 DSH 会话与界面安装）登记为 `T052`，按 [停止条件](../06-development-loop.md#3-状态与推进) 暂停自动接续并等待用户确认。
