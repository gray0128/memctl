# memctl-pi

pi 与 OMP（Oh My Pi）共用的扩展：会话开始时读取 memctl 记忆并注入上下文，agent run 结束后同步，会话结束时收尾。插件只做宿主生命周期桥接；记忆存储、锁、Git 提交、冲突处理与远端同步仍由 `memctl` 二进制负责。pi 与 OMP 的事件 API 同源，扩展在加载时按宿主可执行文件识别运行环境，命令分别携带 `--agent pi` 或 `--agent omp`。设计与验收见 [docs/08-pi-plugin.md](https://github.com/gray0128/memctl/blob/main/docs/08-pi-plugin.md) 与 [docs/17-omp-integration.md](https://github.com/gray0128/memctl/blob/main/docs/17-omp-integration.md)。

## 安装

npm 主通道：

```sh
pi install npm:memctl-pi@<version>
```

离线或本地开发通道：

```sh
memctl pi plugin-dir
pi install /absolute/path/printed/by/the/command
```

`pi install` 对本地目录只记录路径，不复制文件；目录必须长期存在。确认已加载：

```sh
pi list
```

OMP 使用同一份包，由 OMP 的插件管理器链接：

```sh
memctl pi plugin-dir              # 同一目录同时供 pi 与 OMP 使用
omp plugin install /absolute/path/printed/by/the/command
omp plugin list
```

`memctl-pi 0.6.0` 起识别运行环境；更早的包会把命令错误地标成 `--agent pi`。卸载：

```sh
pi remove npm:memctl-pi
# 或
pi remove /absolute/path/printed/by/the/command
omp plugin uninstall memctl-pi
```

## 行为

| 事件 | 动作 |
| --- | --- |
| `session_start` | 读取 `ctx.sessionManager.getSessionId()`，后台执行 `memctl start --agent <host> --sid <id> --json` |
| `before_agent_start` | 最多等待 8 秒，把正文写入 `systemPromptOptions.sections["memctl"]` |
| `agent_settled` | 后台执行一次 `memctl sync --agent <host> --sid <id>`；并发事件只保留一个 pending |
| `agent_end` | 旧版 pi 的兼容回退，仅在空闲且无排队消息时同步 |
| `session_shutdown` | `quit`/`new`/`resume`/`fork` 分离执行 `memctl end`；`reload` 只清理子进程 |

正文保持字节稳定，不加入时间戳或每轮变化的值。正文为空时删除 section；`sections` 不可用时回退到替换 `systemPrompt`。

## 二进制与配置

`memctl` 按以下顺序解析：

1. `MEMCTL_PI_BIN` 环境变量
2. `~/.agent-memory/bin/memctl`
3. `/opt/homebrew/bin/memctl`、`/usr/local/bin/memctl`
4. `PATH`；Windows 下尝试 `memctl.exe`、`memctl.cmd`

仓库路径、设备名和 Git 凭据由 memctl 现有配置与环境决定。插件不读取项目配置，不执行任意命令，也不注册模型工具。

## 限制

- 只做桥接：不实现记忆写入、检索、向量库或 Dreaming。
- `memctl end` 在 `session_shutdown` 内只做有界等待；崩溃、`SIGKILL` 或宿主提前退出不保证执行。
- 未验证的 pi 版本不会被 `memctl install --status` 报告为 `configured`。
- 宿主识别顺序为 `MEMCTL_PI_AGENT`、进程可执行文件名、`PI_CODING_AGENT_DIR`，最后回退为 `pi`。OMP 18.17.0 不提供 `systemPromptOptions.sections`，实际走替换 `systemPrompt` 的回退路径。
- 不与旧 `pi-yaml-hooks` 的 `agent-memory-*` 条目同时启用。

## 验证

在源码仓库中运行：

```sh
make pi-plugin-test
```

包内只包含 `index.js`、`README.md`、`LICENSE` 与 `package.json`，没有运行时依赖和安装脚本。

## 许可证

MIT。
