# 阶段 F 归档：pi 专用插件

本文件归档阶段 F（pi 专用插件）各任务的完成记录。阶段步骤与验收映射见 [验收与交付](../05-validation-and-delivery.md#阶段-f-的实施顺序)，当前进度见 [开发状态](../STATUS.md)。

## T060：实现 pi 专用插件

- 目标与范围：对应阶段 F 的 `F-1` 至 `F-4`。新增 `plugins/pi/` 原生扩展、Node 单元测试与 `go:embed`；实现 `memctl pi plugin-dir`；pi 适配器增加原生插件探测（npm、状态目录、settings 本地包）、旧 `agent-memory-*` 条目内容探测、宿主与插件版本门槛、安装提示与按接入路径报告 capabilities；增加 `make pi-plugin-test`/`pi-plugin-pack` 与 Release 审计 tarball。不发布 npm 包，不修改真实 pi 配置。
- 契约：[pi 专用插件](../08-pi-plugin.md)、[目录结构](../01-architecture-and-modules.md#5-目录结构与依赖)、[AdapterStatus](../02-domain-models-and-contracts.md#23-审计诊断整理与接入)、[Agent 配置安装](../03-workflows-and-dataflows.md#7-agent-配置安装)、[CLI 命令](../04-ui-and-cli-specification.md#1-cli-命令)、[验收与交付](../05-validation-and-delivery.md#2-核心测试矩阵)。
- 验证日期与环境：2026-10-18，macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，pi `1.3.0`；隔离 `HOME`/`PI_CODING_AGENT_DIR`、本地 mock OpenAI 兼容端点与假 memctl。
- 命令与结果：`node --test plugins/pi/test/*.test.js` 15 项通过；`go test ./internal/... ./plugins/... -count=1` 通过；`go test ./tests/integration -count=1` 通过（269.3 秒）；`go vet` 与 `gofmt` 无输出；`npm pack --dry-run` 只包含 `index.js`、`README.md`、`package.json`，无依赖与安装脚本；隔离 `pi install <plugins/pi>`、`pi list`、`pi remove` 可用，settings 中的相对路径仍被适配器识别；隔离 pi 会话观察到 `start` 后模型请求包含 `<memctl>` section，退出触发 `end`。证据见 [验证记录](./t060-pi-plugin.md)。
- 验收：`H12`–`H15` 的单元部分通过；`I07`、`I08`、`I09`、`I10` 通过。真实 pi 会话、真实模型、npm 发布与非 macOS 平台未验证。
- 未验证与遗留：`agent_settled` 的 `sync` 在 `pi -p` 立即退出时被 shutdown 取消，最终同步由 `end` 完成；交互式会话、恢复、分叉与子代理行为待 `F-5`；npm 包名与许可证、Windows/Linux 进程清理未验证。
- 下一步：`F-5` 在真实 pi 会话验证注入、同步提交、收尾、恢复、分叉与失败降级；npm 发布与版本提升留给后续发布轮次。
