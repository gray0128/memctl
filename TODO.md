# TODO

## Todo

- **调研 `antigravity` 与 `grok` 接入能否改为 Plugin 模式**：参考 `https://github.com/michael-denyer/pstack-claude`，评估将当前 Hook 与配置合并接入升级为插件模式的可行性、宿主机制边界与改造方案。
- **`memctl-dsh@0.5.3` 真实桌面会话复验**：npm `latest` 已是 `0.5.3`（随 `v0.9.1` 进入 Release，本机 `desktop` profile 已升级），注入与同步目前仅在 `0.5.2` 会话中验证过，待在真实会话中复验一次并补入记录。验证记录见 [T096](docs/validation/t096-dsh-plugin-0-5-2-verification.md)。
- **CodeBuddy 与 WorkBuddy 交互式退出及桌面 GUI 验证**：待各跑一次交互式退出的 `SessionEnd` 与 WorkBuddy 桌面 GUI 会话，确认 `end` 触发且桌面应用不会重写 `~/.workbuddy/settings.json`（参见 [阶段 H 归档](docs/validation/stage-h.md#t115codebuddy-与-workbuddy-会话格式接入h-2c)）。
- **Release 产物的 Web UI 视图冒烟**：「整理」视图的逐文件 Diff 已在 `v0.10.1` 产物确认；「审计」「客户端接入」「条目」三个视图尚未在 Release 产物上逐一复看。

## Doing

- 暂无。

## Done

- **CodeBuddy 与 WorkBuddy 安装及 headless 会话验证**：已执行 `memctl install codebuddy workbuddy`，并在 `T115` 完成 headless 会话 `SessionStart`/`Stop` 触发与会话摘要落盘复验（见 [阶段 H 归档](docs/validation/stage-h.md#t115codebuddy-与-workbuddy-会话格式接入h-2c)）。
- **pi.dev 收录根因排查与 npm 索引修复**：定位 npm 索引评分为 `0` 的根因，发布 `memctl-pi@0.5.2` 后索引评分恢复正常；目录仅取前 5,250 个可枚举包名属外部装机量门槛而非代码问题，已收口（见 [T093](docs/validation/stage-g.md#t093定位-pidev-目录未收录的根因)）。

已完成事项与当前状态见 [docs/STATUS.md](docs/STATUS.md)。
