# T060：pi 专用插件实现与隔离宿主验证（已完成）

环境：macOS arm64，Go 1.27.1，Node v26.10.0 / npm 11.19.1，pi `1.3.0`。所有宿主实验使用隔离 `HOME` 与 `PI_CODING_AGENT_DIR`、临时状态目录、假 memctl 和本地 mock OpenAI 兼容端点；未修改本机 `~/.pi/agent`。

## 已验证

### 插件单元测试（H12–H15 的单元部分）

`node --test plugins/pi/test/*.test.js`：15 项通过。覆盖：

- `session_start` 读取 SID、以固定参数和 `cwd` 启动 `start`，缓存正文；首个 `before_agent_start` 最多等待 8 秒。
- `sections["memctl"]` 注入；重复注入文本不变；空正文删除 section；`reload` 保留旧快照直到新正文到达。
- `sections` 缺失或 `forceSystemPrompt` 时回退到替换 `systemPrompt`，回退只记录一次。
- `agent_settled` 只同步一次，并发 settled 合并为一个 pending；`agent_end` 仅在空闲且无排队消息时同步，且不会被 `agent_settled` 重复。
- `session_shutdown` 的 `quit`/`new`/`resume`/`fork` 执行一次分离的 `end`；`reload` 不执行；shutdown 取消未完成的 `start`/`sync` 子进程。
- 二进制缺失只记录一次；`start` 子进程错误、非零退出、非法 JSON、超时和输出超限都不抛出宿主异常；非零退出但 envelope 合法时保留正文并记录同步错误。
- `dispose` 结束所有活动会话并清理状态；`MEMCTL_PI_BIN`、`~/.agent-memory/bin/memctl` 与 `PATH` 的解析顺序。

### 适配器与包清单（I07–I10）

`go test ./internal/... ./plugins/... -count=1` 与 `go test ./tests/integration -count=1`（269.3 秒）通过。新增用例覆盖：

- `memctl-pi` 从 npm 清单、`$STATE/pi/package.json` 和 `~/.pi/agent/settings.json` 的本地包声明探测；相对 settings 路径按 settings 所在目录解析。
- 原生插件存在时 `configured`、`artifacts` 为空；原生与 `agent-memory-*` 条目共存时 `needs_attention`。
- 只有 `pi-yaml-hooks` 包、没有 `hooks.yaml` 条目不构成旧接入，`install pi` 不创建 `hooks.yaml`。
- 旧条目和旧插件同时存在时保留合并计划并标记 deprecated；旧条目存在但旧插件缺失时 `needs_attention`。
- pi `0.85.0` 低于宿主下界 `0.86.1`、原生插件 `0.3.0` 低于最低版本 `0.4.0`、`hooks.yaml` 无法解析时均报告 `needs_attention`。
- `CapabilitiesFor` 按接入路径返回：原生 `[plugin]`，旧路径 `[hooks]`。
- `memctl pi plugin-dir` 输出 `$STATE/pi` 的绝对路径，写入 `package.json`、`index.js`、`README.md`，不创建 `~/.pi`；参数错误返回 `2`。

`npm pack --dry-run` 的包内容为 `README.md`、`index.js`、`package.json`，无 `dependencies`、无 `install`/`postinstall`/`prepare` 脚本。

### 隔离 pi 安装（I09）

在临时 `HOME` 与 `PI_CODING_AGENT_DIR` 下：

```text
pi install /Users/libo/Documents/github/memctl/plugins/pi
pi list
pi remove /Users/libo/Documents/github/memctl/plugins/pi
```

安装、列出和卸载均成功。pi 把本地包写成相对 settings 的路径（`../../../../Users/libo/Documents/github/memctl/plugins/pi`）；随后 `memctl install --status` 在隔离 HOME 下仍识别为：

```text
pi: configured host=pi@1.3.0 plugin@0.4.0
  note: memctl-pi is installed; legacy host configuration is not needed
```

### 隔离会话（H12、H13 的隔离部分）

隔离 agent 目录配置本地 mock `openai-completions` 端点，扩展通过 `pi install` 加载，`MEMCTL_PI_BIN` 指向记录参数的假 memctl。执行 `pi --model mock/mock-1 --no-tools -p "hello probe"`：

```text
probe reply
memctl-pi: session 01a14a9d-... sync exited with SIGTERM:
```

假 memctl 记录：

```text
start --agent pi --sid 01a14a9d-4c00-7410-95da-168c96db4c11 --json
end --agent pi --sid 01a14a9d-4c00-7410-95da-168c96db4c11
```

mock 端点收到的 system prompt 包含：

```text
[agent-memory] probe rules
</memctl>
```

结论：`session_start` 的 `start`、section 注入和 `session_shutdown` 的 `end` 在真实 pi `1.3.0` 进程中生效。`-p` 模式在 `agent_settled` 后立即退出，`sync` 被子进程清理取消，最终同步由 `end` 执行；这是 §6.9 的设计行为，交互式会话的 `sync` 完成路径未在本轮验证。

### 本机真实探测（只读）

本机 `pi 1.3.0` 安装 `pi-yaml-hooks@2026.9.26` 且 `hooks.yaml` 含 `agent-memory-*` 条目，未安装 `memctl-pi`。`install --status` 报告：

```text
pi: configured host=pi@1.3.0 plugin@2026.9.26
```

未写入或删除任何真实 pi 配置。

## 未验证

- 真实模型端点与真实 pi 会话中的 `agent_settled` 同步提交、恢复、分叉和子代理行为；`F-5` 跟踪。
- `agent_end` 回退路径只在单元测试中验证，未在低版本 pi 上运行。
- npm 发布、包名最终确认、许可证选择与 `pi update --extensions` 升级路径。
- Windows、Linux 的进程清理与 pi 运行。
- `sections` 缺失的宿主级回退只由单元测试覆盖。
