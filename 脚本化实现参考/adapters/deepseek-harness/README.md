# DeepSeek Harness（dsh）桌面端适配

官方 harness 本身没有 hook 机制（everything is a plugin，生命周期事件靠 Cordis 插件订阅），
所以要装一个第三方 hooks 插件；"读记忆"则靠它原生支持的 `~/.dsh/AGENTS.md`。

## 方案 A（推荐）：`dsh-hooks-plugin`（KYinCode）——Claude Code 风格

- 配置文件：`~/.dsh/hooks.json`（全局），格式与 Claude Code 兼容：stdin 是 JSON（含 `session_id`、`cwd`），
  所以 memctl 的 `--stdin` 直接可用。**注意结构和 settings.json 不同：事件名在顶层，没有外层的 `"hooks"` 键。**
  顶层键只能是 Claude Code 的事件名，因此这个 JSON 里不能写 `_说明` 之类的注释键，说明都放在本文件。
- 事件映射：`SessionStart` → `memctl start --json`；`Stop` → `memctl sync`（async）；`SessionEnd` → `memctl end`。
- `--json`：把规则 + MEMORY.md 包成 `hookSpecificOutput.additionalContext` 输出。该插件文档只在 `PreToolUse`
  的示例里展示了 `additionalContext`，**SessionStart 的输出是否会注入上下文我没能确认**（它的 `docs/CONFIGURATION.md` 抓取被拒）。
  所以下面第 3 步的 AGENTS.md 指引是保底，两者同时生效也没有副作用。

## 安装步骤（桌面端 = web profile 的外壳）

1. 装插件（先读一遍源码或包内 `docs/CONFIGURATION.md`，并钉死确切版本；第三方插件会以你的权限执行命令）：
   `npx @deepseek-ai/dsh plugin --profile web add dsh-hooks-plugin@<确切版本>`
   或在应用里 Settings → Plugins → Add plugin。已有会话也会被接线，不用新建。
2. 放 hooks：`cp hooks.json ~/.dsh/hooks.json`（已有这个文件就把三个事件合并进去）。
3. 加指引：`cat AGENTS.md.snippet >> ~/.dsh/AGENTS.md`（`$DSH_HOME` 默认 `~/.dsh`；追加，不要覆盖已有内容）。
4. 按主 README 安装 memctl，并 `git clone` 记忆仓库到 `~/memory`。

## 验证

- 新开一个会话，看应用里的 🔌 Hooks 浮动控制台，或 `~/.dsh/logs/dsh-hooks/dsh-hooks.log`、`GET /dsh-hooks/recent`，
  确认 SessionStart 的退出码为 0。
- 让 agent 写一条记忆，回合结束后 `git -C ~/memory log -1` 应出现 `memory: dsh/<会话前8位>@<设备>` 的提交。
- 问 agent"你的记忆仓库里有什么"，确认它读到了 `MEMORY.md`。

## 桌面端特有的坑（最可能出问题的地方）

1. **沙箱。** 该插件文档写明 hook 命令"在触发者自己的沙箱上下文执行"，而 dsh 有沙箱 + 审批两层控制。
   如果沙箱不允许写 `~/memory`、`~/.agent-memory`，或不允许联网，memctl 的 commit / push 会静默失败
   （同时 agent 自己用文件工具写 `~/memory/MEMORY.md` 也可能要审批）。先看 hooks 日志里有没有 permission denied；
   需要的话在 dsh 的权限/沙箱设置里放行这两个目录和 git 联网。我没找到具体的配置项名，请以官方文档为准。
2. **从 Finder / Dock 启动的 App 没有你 shell 的 PATH 和环境。** memctl 已自动把 `/opt/homebrew/bin`、`/usr/local/bin`
   加进 PATH（找得到 git、jq）。git push 的凭据请用 HTTPS + `osxkeychain`，或 SSH 并配好
   `~/.ssh/config` 里的 `AddKeysToAgent yes` / `UseKeychain yes`；务必**不要从终端启动**来测试，要从 Finder 启动测一次。
3. **社区发行版 `dsh-coding`（omdsh-dev）** 把 `DSH_HOME` 隔离到 `xdg.DataHome/dsh-coding`，不在 `~/.dsh`；
   用的话 `hooks.json` 和 `AGENTS.md` 要放到那个目录。官方桌面端按文档是 `~/.dsh`。
4. dsh 仍是 developer preview，README 明说"会有破坏兼容的改动"；两个 hooks 插件也都是个人维护的小项目。升级前重新验证。

## 方案 B（备选）：`dsh-hooks`（PeterBon）

见 `cordis.patch.dsh-hooks-peterbon.yml`，文件顶部注释写了差异。它维护更活跃、有 Web 设置页，
但事件是 `turn/start` / `turn/end`，没有 stdin JSON，也不能往上下文注入内容，"读记忆"完全靠 AGENTS.md。
与方案 A 二选一。
