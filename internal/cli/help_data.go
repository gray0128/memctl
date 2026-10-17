package cli

// This file is the single source of truth for the agent-oriented CLI help.
// Every command that Run dispatches must have one entry in helpCommandData and
// one group membership in helpRootData.Groups. The help text documents the
// observable contract; docs/04-ui-and-cli-specification.md remains the
// authoritative design document.

// localizedText carries the English and Chinese form of one help string.
// Only help text is localized. Command output keys, JSON fields, error codes
// and exit codes stay in English so agents can parse them regardless of locale.
type localizedText struct {
	EN string
	ZH string
}

func loc(en, zh string) localizedText { return localizedText{EN: en, ZH: zh} }

func (t localizedText) text(lang helpLang) string {
	if lang == helpLangZH {
		return t.ZH
	}
	return t.EN
}

// helpItem is one argument or flag entry.
type helpItem struct {
	Name        string
	Value       string
	Required    bool
	Description localizedText
}

// helpExample is one copy-pasteable command with an optional explanation.
type helpExample struct {
	Command string
	Note    localizedText
}

// helpExit documents one exit status.
type helpExit struct {
	Code    int
	Meaning localizedText
}

// helpCommand is one command page.
type helpCommand struct {
	Name        string
	Usage       []string
	Summary     localizedText
	Description localizedText
	Arguments   []helpItem
	Flags       []helpItem
	Input       []localizedText
	Output      []localizedText
	ExitCodes   []helpExit
	Examples    []helpExample
	Notes       []localizedText
	Related     []string
}

// helpEnv documents one environment variable.
type helpEnv struct {
	Name        string
	Default     string
	Description localizedText
}

// helpGroup groups commands in the overview.
type helpGroup struct {
	Title localizedText
	Names []string
}

// helpRoot is the overview page.
type helpRoot struct {
	Title       localizedText
	Summary     localizedText
	Usage       []string
	QuickStart  []helpExample
	Rules       []localizedText
	Groups      []helpGroup
	Environment []helpEnv
	ExitCodes   []helpExit
	Language    localizedText
	MoreHelp    localizedText
}

var helpRootData = helpRoot{
	Title: loc(
		"memctl - manage the agent memory repository",
		"memctl - 管理 Agent 记忆仓库",
	),
	Summary: loc(
		"memctl keeps agent memory as Markdown in a dedicated Git repository. It provides managed reads and writes, local commits, remote synchronization, host hooks, a local Web UI, and explicitly confirmed Dreaming proposals. Repository commands resolve the repository from AGENT_MEMORY_DIR, not from the current directory.",
		"memctl 把 Agent 记忆以 Markdown 保存在独立 Git 仓库中，提供受管读写、本地提交、远端同步、宿主 Hook、本地 Web UI 和需显式确认的 Dreaming 提案。仓库类命令从 `AGENT_MEMORY_DIR` 解析仓库，不根据当前目录猜测。",
	),
	Usage: []string{
		"memctl <subcommand> [flags] [-- command...]",
		"memctl <subcommand> --help",
		"memctl help [command] [--lang en|zh] [--json]",
	},
	QuickStart: []helpExample{
		{
			Command: "memctl init",
			Note: loc(
				"Create a local-only repository at ~/memory (or AGENT_MEMORY_DIR). No network access.",
				"在 `~/memory`（或 `AGENT_MEMORY_DIR`）创建仅本地仓库，不访问网络。",
			),
		},
		{
			Command: `memctl read MEMORY.md --json`,
			Note: loc(
				"Read a file and keep fileRevision as the write baseline.",
				"读取文件，并保留返回的 fileRevision 作为写入基线。",
			),
		},
		{
			Command: `printf '# Memory\n\n## Index\n- new entry\n' | memctl write MEMORY.md --base FILE_REVISION --stdin --agent my-agent --sid SESSION_ID`,
			Note: loc(
				"Replace FILE_REVISION with the value from step 2. SESSION_ID is the host session id; omit --sid only if you do not need a stable source tag.",
				"用第 2 步得到的值替换 FILE_REVISION。SESSION_ID 是宿主会话 ID；不需要稳定来源标签时可省略 `--sid`。",
			),
		},
		{
			Command: "memctl sync --wait",
			Note: loc(
				"Run a foreground remote sync when the next step depends on the remote.",
				"下一步依赖远端时，运行前台同步并等待结果。",
			),
		},
		{
			Command: "memctl help write",
			Note: loc(
				"Inspect flags, input, output, exit codes and examples for one command.",
				"查看单个命令的参数、输入、输出、退出码和示例。",
			),
		},
	},
	Rules: []localizedText{
		loc(
			"Read before write. Use fileRevision from `memctl read <path> --json` as `--base`; do not edit repository files directly.",
			"先读后写。把 `memctl read <path> --json` 返回的 fileRevision 作为 `--base`；不要直接编辑仓库文件。",
		),
		loc(
			"`memctl write` reads raw file text from stdin. It does not parse Hook JSON.",
			"`memctl write` 从 stdin 读取原始文件文本，不解析 Hook JSON。",
		),
		loc(
			"Use `memctl sync --wait` when a foreground result matters. `start`, `context` and `pull` run a bounded sync during the hook; the automatic `sync` and `end` paths only start a best-effort background sync.",
			"需要前台结果时使用 `memctl sync --wait`。`start`、`context` 和 `pull` 在 Hook 内执行有界同步；`sync` 和 `end` 的自动路径只尽力启动后台同步，不等待结果。",
		),
		loc(
			"`memctl sync --capture` accepts edits made serially outside memctl. Do not use it with parallel writers.",
			"`memctl sync --capture` 只接收在 memctl 之外串行完成的编辑；不要与并行写入者同时使用。",
		),
		loc(
			"Hook commands (`start`, `context`, `pull`, `sync`, `end`) suppress repository and sync failures to exit 0 when invoked with `--stdin`, so a host is not blocked. Explicit modes such as `sync --wait`, `sync --capture` and `resolve` still return 1; see each command page.",
			"带 `--stdin` 调用 Hook 命令（`start`、`context`、`pull`、`sync`、`end`）时，仓库和同步失败不阻断宿主，返回 0。`sync --wait`、`sync --capture`、`resolve` 等显式模式失败仍返回 1；详见对应命令页。",
		),
		loc(
			"`--json` output is JSON only. Diagnostics and stable `CODE: message` errors go to stderr.",
			"`--json` 只输出 JSON；诊断信息和稳定的 `CODE: message` 错误写入 stderr。",
		),
		loc(
			"Help reads only arguments and environment. It does not access the repository, Git, Node.js, or the network.",
			"帮助只读取命令行参数和环境变量，不访问仓库、Git、Node.js 或网络。",
		),
	},
	Groups: []helpGroup{
		{
			Title: loc("Repository", "仓库"),
			Names: []string{"init", "read", "write", "sync", "resolve", "recover", "doctor"},
		},
		{
			Title: loc("Host hooks and sessions", "宿主 Hook 与会话"),
			Names: []string{"start", "context", "pull", "end", "tag", "wrap"},
		},
		{
			Title: loc("Setup and maintenance", "配置与维护"),
			Names: []string{"ui", "install", "dsh", "pi", "mcp", "dream", "update"},
		},
		{
			Title: loc("Information", "信息"),
			Names: []string{"version", "help"},
		},
	},
	Environment: []helpEnv{
		{
			Name:    "AGENT_MEMORY_DIR",
			Default: "~/memory",
			Description: loc(
				"Memory repository root. A missing repository fails with NO_REPO and a `memctl init` hint.",
				"记忆仓库根目录。仓库缺失时返回 NO_REPO，并提示运行 `memctl init`。",
			),
		},
		{
			Name:    "AGENT_MEMORY_STATE",
			Default: "~/.agent-memory",
			Description: loc(
				"Runtime state root: locks, session cache, transaction records, proposals and generated plugin files.",
				"运行状态根目录：锁、会话缓存、事务记录、提案和生成的插件文件。",
			),
		},
		{
			Name:    "AGENT_MEMORY_BACKGROUND",
			Default: "on",
			Description: loc(
				"Set to 0, false, off or no to disable best-effort background sync after managed writes and automatic hooks.",
				"设为 `0`、`false`、`off` 或 `no` 可关闭受管写入与自动 Hook 之后的尽力后台同步。",
			),
		},
		{
			Name:    "AGENT_MEMORY_AGENT",
			Default: "unknown",
			Description: loc(
				"Default agent name when --agent is omitted.",
				"省略 `--agent` 时使用的默认 Agent 名称。",
			),
		},
		{
			Name:    "AGENT_MEMORY_DEVICE",
			Default: "hostname",
			Description: loc(
				"Device name used in source tags. Falls back to unknown when detection fails.",
				"来源标签中使用的设备名。探测失败时回退为 `unknown`。",
			),
		},
		{
			Name: "AGENT_MEMORY_SID",
			Description: loc(
				"Session id for source tags and provenance. --sid wins; DSH_HOOK_SESSION_ID and Hook JSON are fallbacks.",
				"用于来源标签和溯源信息的会话 ID。`--sid` 优先，`DSH_HOOK_SESSION_ID` 和 Hook JSON 是回退来源。",
			),
		},
		{
			Name: "DSH_HOOK_SESSION_ID",
			Description: loc(
				"DSH session id fallback when no explicit --sid or AGENT_MEMORY_SID is set.",
				"未设置 `--sid` 或 `AGENT_MEMORY_SID` 时，作为 DSH 会话 ID 的回退来源。",
			),
		},
		{
			Name: "MEMCTL_LANG",
			Description: loc(
				"Help language: en or zh. Overrides locale detection.",
				"帮助语言：en 或 zh。优先于 `LC_ALL`、`LC_MESSAGES` 和 `LANG`。",
			),
		},
		{
			Name: "LC_ALL / LC_MESSAGES / LANG",
			Description: loc(
				"Used only to choose the help language when --lang and MEMCTL_LANG are unset. The first non-empty value is used; a value starting with zh selects Chinese, any other value selects English.",
				"仅在未设置 `--lang` 和 `MEMCTL_LANG` 时用于选择帮助语言。使用第一个非空值：以 zh 开头选择中文，其他值（包括 C 和 POSIX）选择英文。",
			),
		},
	},
	ExitCodes: []helpExit{
		{Code: 0, Meaning: loc(
			"Success, or an automatic Hook completed without blocking the host.",
			"成功；或自动 Hook 已结束且未阻断宿主。",
		)},
		{Code: 1, Meaning: loc(
			"Command failed, or reported a negative result (doctor error, update failure, no stable session id). A stable error code appears on stderr.",
			"命令失败，或结果不符合预期（诊断存在 error 级问题、更新失败、缺少稳定会话 ID）。stderr 会给出稳定错误码。",
		)},
		{Code: 2, Meaning: loc(
			"Usage or protocol error: unknown command or flag, missing required value, or invalid Hook JSON.",
			"用法或协议错误：未知命令或选项、缺少必填值、Hook JSON 无效。",
		)},
	},
	Language: loc(
		"Help supports English and Chinese. Selection order: --lang, MEMCTL_LANG, then the first non-empty LC_ALL, LC_MESSAGES or LANG; the default is English. An invalid --lang is a usage error. A locale selects Chinese only when it starts with zh; other locales select English. Only help is localized: command output keys, JSON fields, and error codes stay in English.",
		"帮助支持英文和中文。选择顺序为 `--lang`、`MEMCTL_LANG`，然后是第一个非空的 `LC_ALL`、`LC_MESSAGES` 或 `LANG`；均未设置时使用英文。无效的 `--lang` 返回用法错误（退出码 2）。locale 以 zh 开头时选择中文，其他 locale 选择英文。只有帮助文本本地化：命令输出键、JSON 字段和错误码保持英文。",
	),
	MoreHelp: loc(
		"Run `memctl help <command>` for one command, or `memctl help --json` for the full machine-readable catalog. `memctl <command> --help` is equivalent to `memctl help <command>`.",
		"运行 `memctl help <command>` 查看单个命令，或 `memctl help --json` 获取完整机器可读目录。`memctl <command> --help` 等价于 `memctl help <command>`。",
	),
}

var helpCommandData = []helpCommand{
	{
		Name:    "help",
		Usage:   []string{"memctl help [command] [--lang en|zh] [--json]"},
		Summary: loc("Show agent-oriented CLI help.", "显示面向 Agent 的命令行帮助。"),
		Description: loc(
			"Without a command, prints the overview, quick start, rules, environment, exit codes and command list. With a command, prints usage, arguments, flags, input, output, exit codes, examples and related commands. The page is equivalent to `memctl <command> --help`. Help never accesses the repository, Git, Node.js or the network.",
			"不带命令时输出概览、快速开始、使用规则、环境变量、退出码和命令列表；带命令时输出用法、参数、选项、输入、输出、退出码、示例和相关命令。该页面等价于 `memctl <command> --help`。帮助不访问仓库、Git、Node.js 或网络。",
		),
		Arguments: []helpItem{
			{Name: "[command]", Description: loc(
				"Command name to describe. Run `memctl help` for the list.",
				"要查看的命令名。运行 `memctl help` 查看列表。",
			)},
		},
		Flags: []helpItem{
			{Name: "--lang", Value: "LANG", Description: loc(
				"Help language: en or zh. Accepted before `--` for every command, overrides the environment, and only changes help text.",
				"帮助语言：en 或 zh。可在任意命令的 `--` 之前使用，覆盖环境变量，且只影响帮助文本。",
			)},
			{Name: "--json", Description: loc(
				"Print the machine-readable help object instead of text.",
				"输出机器可读帮助对象，而不是文本。",
			)},
		},
		Output: []localizedText{
			loc("Text help on stdout, or one JSON object with schemaVersion 1.", "stdout 输出文本帮助，或输出 schemaVersion 为 1 的 JSON 对象。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Help was printed.", "已输出帮助。")},
			{Code: 2, Meaning: loc("Unknown help topic, unknown flag, or invalid --lang value.", "未知帮助主题、未知选项或 `--lang` 值无效。")},
		},
		Examples: []helpExample{
			{Command: "memctl help", Note: loc("Overview and command list.", "概览和命令列表。")},
			{Command: "memctl help write", Note: loc("One command page.", "单个命令页。")},
			{Command: "memctl help write --json", Note: loc("One command as JSON.", "以 JSON 输出单个命令。")},
			{Command: "MEMCTL_LANG=zh memctl help", Note: loc("Chinese overview.", "中文概览。")},
			{Command: "memctl help --lang zh", Note: loc("Per-invocation language override.", "单次调用的语言覆盖。")},
		},
		Notes: []localizedText{
			loc("`memctl <command> -h` and `memctl <command> --help` also open the command page.", "`memctl <command> -h` 和 `memctl <command> --help` 也会打开命令页。"),
			loc("Flags after `--` belong to the wrapped command and are never treated as help flags.", "`--` 之后的参数属于被包装命令，不会被当作帮助参数。"),
		},
		Related: []string{"version"},
	},
	{
		Name:    "version",
		Usage:   []string{"memctl version", "memctl --version", "memctl -v"},
		Summary: loc("Print the memctl version.", "输出 memctl 版本。"),
		Description: loc(
			"Prints `memctl <version>` to stdout and exits 0. It never accesses the repository, state directory, Git or Node.js.",
			"向 stdout 输出 `memctl <version>` 并返回 0。不访问仓库、状态目录、Git 或 Node.js。",
		),
		Output: []localizedText{
			loc("One line: `memctl <version>`.", "一行：`memctl <version>`。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Version printed.", "已输出版本。")},
			{Code: 2, Meaning: loc("Unexpected arguments.", "存在多余参数。")},
		},
		Examples: []helpExample{
			{Command: "memctl version"},
			{Command: "memctl --version"},
		},
		Notes: []localizedText{
			loc("`memctl version --help` shows this page.", "`memctl version --help` 显示本页。"),
		},
		Related: []string{"help"},
	},
	{
		Name: "init",
		Usage: []string{
			"memctl init",
			"memctl init <remote-url> --confirm-private",
		},
		Summary: loc("Create, clone or attach the memory repository.", "创建、克隆或接入记忆仓库。"),
		Description: loc(
			"Without a URL, creates a local-only repository at AGENT_MEMORY_DIR with an initial commit, or reuses an existing repository without changing its remote. With a URL, attaches or clones the remote: an empty remote receives the first push, a remote with history is cloned and upstream is set, and an existing local repository gets the remote attached. The URL form requires an explicit confirmation that the repository is private and owned by you. Repeated init is idempotent and never changes an existing remote.",
			"不带 URL 时，在 `AGENT_MEMORY_DIR` 创建仅本地仓库并生成初始提交；仓库已存在时直接复用，不修改 remote。带 URL 时接入或克隆远端：空远端完成首次推送，有历史的远端先克隆再建立 upstream，已有本地仓库只附加 remote。带 URL 时必须显式确认仓库为私有且归你所有。重复执行 init 是幂等的，也不会修改已有的 remote。",
		),
		Arguments: []helpItem{
			{Name: "[remote-url]", Description: loc(
				"Remote Git URL. Omit it for a local-only repository.",
				"远端 Git URL。省略时创建仅本地仓库。",
			)},
		},
		Flags: []helpItem{
			{Name: "--confirm-private", Description: loc(
				"Required with a remote URL; forbidden without one. Confirms the remote is private and owned by you.",
				"提供远端 URL 时必填，无 URL 时禁止使用。确认远端为私有且归你所有。",
			)},
		},
		Output: []localizedText{
			loc("`memory repo:`, `state:`, and when available `head:` and `remote:`.", "`memory repo:`、`state:`，可用时还有 `head:` 和 `remote:`。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Repository created, cloned or reused.", "仓库已创建、克隆或复用。")},
			{Code: 1, Meaning: loc("Configuration, Git, identity or network failure.", "配置、Git、身份或网络失败。")},
			{Code: 2, Meaning: loc("URL without confirmation, confirmation without URL, or unknown argument.", "URL 缺少确认、无 URL 却使用确认，或存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl init", Note: loc("Local-only repository with no network access.", "仅本地仓库，不访问网络。")},
			{Command: "memctl init git@github.com:me/agent-memory.git --confirm-private", Note: loc("Attach or clone a private remote.", "接入或克隆私有远端。")},
		},
		Notes: []localizedText{
			loc("An existing remote that differs from the URL is refused. Change the remote manually, then confirm the new target with init.", "已有 remote 与 URL 不同时会被拒绝。先手工修改 remote，再用 init 确认新目标。"),
			loc("Local initialization may require Git author identity. memctl does not change global Git config.", "本地初始化可能需要 Git 作者身份。memctl 不修改全局 Git 配置。"),
		},
		Related: []string{"read", "sync", "install"},
	},
	{
		Name: "read",
		Usage: []string{
			"memctl read <path>",
			"memctl read <path> --json",
		},
		Summary: loc("Read a memory file or its revision metadata.", "读取记忆文件或其修订元数据。"),
		Description: loc(
			"Reads a text file relative to the repository root. The path must stay inside the repository; symlinks or paths that escape it, and Git internal paths, are rejected. Text files are limited to 2 MiB. Text output is the raw file content. JSON output returns filePath, fileRevision and rawContent; use fileRevision as the next write baseline.",
			"读取相对于仓库根目录的文本文件。路径必须位于仓库内；越出仓库的路径、符号链接和 Git 内部路径都会被拒绝。文本文件上限为 2 MiB。文本模式输出文件原文；JSON 模式返回 filePath、fileRevision 和 rawContent，其中 fileRevision 可用作下一次写入的基线。",
		),
		Arguments: []helpItem{
			{Name: "<path>", Required: true, Description: loc(
				"Repository-relative file path, for example MEMORY.md.",
				"相对仓库根目录的文件路径，例如 MEMORY.md。",
			)},
		},
		Flags: []helpItem{
			{Name: "--json", Description: loc(
				"Return {filePath, fileRevision, rawContent} instead of raw text.",
				"返回 {filePath, fileRevision, rawContent}，而不是原文。",
			)},
		},
		Output: []localizedText{
			loc("Text: raw file content on stdout.", "文本：stdout 输出文件原文。"),
			loc("JSON: {filePath, fileRevision, rawContent}.", "JSON：{filePath, fileRevision, rawContent}。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("File read successfully.", "文件读取成功。")},
			{Code: 1, Meaning: loc("NOT_FOUND, INVALID_PATH, LIMIT_EXCEEDED or IO_FAILED.", "NOT_FOUND、INVALID_PATH、LIMIT_EXCEEDED 或 IO_FAILED。")},
			{Code: 2, Meaning: loc("Missing path, extra argument or unknown flag.", "缺少路径、存在多余参数或未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl read MEMORY.md", Note: loc("Print the file exactly as stored.", "按存储内容原样输出。")},
			{Command: "memctl read MEMORY.md --json", Note: loc("Get the fileRevision baseline.", "获取 fileRevision 基线。")},
		},
		Notes: []localizedText{
			loc("Use `absent` as --base when creating a new file.", "创建新文件时使用 `absent` 作为 `--base`。"),
			loc("Binary or oversized files are not editable text and fail instead of being truncated.", "二进制或超限文件不属于可编辑文本，会明确失败而不是截断。"),
		},
		Related: []string{"write", "help"},
	},
	{
		Name:    "write",
		Usage:   []string{"memctl write <path> --base REV --stdin [--agent NAME] [--sid ID]"},
		Summary: loc("Write a memory file with a baseline check.", "按修订基线受管写入记忆文件。"),
		Description: loc(
			"Managed write. Reads the complete new file text from stdin, verifies that the current file still matches the --base revision, applies the write atomically, creates one local Git commit, and starts a best-effort background sync unless disabled. A stale baseline returns REVISION_MISMATCH and changes nothing. stdin is raw file text, not Hook JSON.",
			"受管写入。从 stdin 读取完整的新文件文本，校验当前文件是否仍与 `--base` 修订一致，然后原子写入并生成一个本地 Git 提交，最后尽力尝试后台同步（可关闭）。旧基线返回 REVISION_MISMATCH，且不修改任何内容。stdin 是原始文件文本，不是 Hook JSON。",
		),
		Arguments: []helpItem{
			{Name: "<path>", Required: true, Description: loc(
				"Repository-relative file path.",
				"相对仓库根目录的文件路径。",
			)},
		},
		Flags: []helpItem{
			{Name: "--base", Value: "REV", Required: true, Description: loc(
				"fileRevision from `read --json`, or `absent` when creating a new file.",
				"`read --json` 返回的 fileRevision；创建新文件时使用 `absent`。",
			)},
			{Name: "--stdin", Required: true, Description: loc(
				"Read the new file text from stdin. The flag is required even for empty files.",
				"从 stdin 读取新文件文本。即使写入空文件也必须提供该参数。",
			)},
			{Name: "--agent", Value: "NAME", Description: loc(
				"Agent name for provenance. Falls back to AGENT_MEMORY_AGENT, then unknown.",
				"用于溯源的 Agent 名称。依次回退到 `AGENT_MEMORY_AGENT` 和 `unknown`。",
			)},
			{Name: "--sid", Value: "ID", Description: loc(
				"Session id for provenance. Falls back to AGENT_MEMORY_SID.",
				"用于溯源的会话 ID。回退到 `AGENT_MEMORY_SID`。",
			)},
		},
		Input: []localizedText{
			loc("Raw file text on stdin, at most 2 MiB. Bytes and trailing newline are preserved.", "stdin 上的原始文件文本，最多 2 MiB；保留字节内容和结尾换行。"),
		},
		Output: []localizedText{
			loc("`transaction:`, `commit:`, `revision:` and `sync:` lines.", "输出 `transaction:`、`commit:`、`revision:` 和 `sync:` 行。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Committed locally; background sync was started or skipped by configuration.", "已在本地提交；后台同步已启动或因配置跳过。")},
			{Code: 1, Meaning: loc("REVISION_MISMATCH, NO_REPO, INVALID_PATH, LIMIT_EXCEEDED, IO_FAILED or GIT_FAILED.", "REVISION_MISMATCH、NO_REPO、INVALID_PATH、LIMIT_EXCEEDED、IO_FAILED 或 GIT_FAILED。")},
			{Code: 2, Meaning: loc("Missing <path>, --base or --stdin, or an unknown flag.", "缺少 `<path>`、`--base` 或 `--stdin`，或存在未知选项。")},
		},
		Examples: []helpExample{
			{
				Command: `memctl read MEMORY.md --json`,
				Note:    loc("Read the current file and take fileRevision from the JSON output.", "读取当前文件，并从 JSON 输出中取 fileRevision。"),
			},
			{
				Command: `printf '# Memory\n\n## Index\n- new entry\n' | memctl write MEMORY.md --base FILE_REVISION --stdin --agent my-agent --sid "$SESSION_ID"`,
				Note:    loc("Replace FILE_REVISION with the value from the previous step.", "用上一步得到的值替换 FILE_REVISION。"),
			},
		},
		Notes: []localizedText{
			loc("Do not use shell redirection to edit files inside the repository; that bypasses the baseline and commit protocol.", "不要用 shell 重定向直接编辑仓库内文件；这会绕过基线校验和提交协议。"),
			loc("Set AGENT_MEMORY_BACKGROUND=0 to keep the write local. The commit is still created.", "设置 `AGENT_MEMORY_BACKGROUND=0` 可只做本地写入，仍会创建提交。"),
		},
		Related: []string{"read", "sync", "recover"},
	},
	{
		Name: "sync",
		Usage: []string{
			"memctl sync",
			"memctl sync --wait",
			"memctl sync --capture [--wait]",
			"memctl sync --agy",
		},
		Summary: loc("Synchronize with the remote, or capture serial external edits.", "同步远端，或接收在 memctl 之外完成的编辑。"),
		Description: loc(
			"With no mode, starts a detached best-effort background sync and returns immediately with empty stdout; this is the automatic Hook path. `--wait` runs the remote sync in the foreground and reports the result. `--capture` commits edits made serially outside memctl, then starts background sync unless `--wait` is also set. Capture is refused while Git is in a merge, rebase, revert or cherry-pick state. `--agy` prints `{}` for Antigravity hooks.",
			"不带模式时启动一个分离的后台同步并立即返回，stdout 为空；这是自动 Hook 路径，尽力执行，不等待结果。`--wait` 在前台执行远端同步并报告结果。`--capture` 提交在 memctl 之外串行完成的编辑，然后启动后台同步；同时提供 `--wait` 时改为前台同步。Git 处于 merge、rebase、revert 或 cherry-pick 状态时拒绝 `--capture`。`--agy` 为 Antigravity Hook 输出 `{}`。",
		),
		Flags: []helpItem{
			{Name: "--wait", Description: loc(
				"Run the remote sync in the foreground and report branch, head, remote/local state, reason and ahead/behind.",
				"在前台执行远端同步，输出 branch、head、remote/local 状态、原因、conflict 和 ahead/behind。",
			)},
			{Name: "--capture", Description: loc(
				"Capture and commit serial external edits. Prints `nothing to capture` when the tree is clean.",
				"接收并提交在 memctl 之外串行完成的编辑。工作树干净时输出 `nothing to capture`。",
			)},
			{Name: "--stdin", Description: loc(
				"Read at most 1 MiB of Hook JSON and use its session id. Invalid JSON exits 2.",
				"读取最多 1 MiB 的 Hook JSON 并使用其中的会话 ID。JSON 无效时返回 2。",
			)},
			{Name: "--agy", Description: loc(
				"Print `{}`, the empty Antigravity Hook envelope.",
				"输出 `{}`，即 Antigravity 的空 Hook envelope。",
			)},
			{Name: "--agent", Value: "NAME", Description: loc("Agent name for capture provenance.", "capture 溯源使用的 Agent 名称。")},
			{Name: "--sid", Value: "ID", Description: loc("Session id for capture provenance.", "capture 溯源使用的会话 ID。")},
		},
		Output: []localizedText{
			loc("Default: empty stdout; failures are reported on stderr and still exit 0 for the Hook path.", "默认：stdout 为空；失败写入 stderr，Hook 路径仍返回 0。"),
			loc("--wait: branch, head, remote, local, reason, conflict and ahead/behind when available.", "`--wait`：输出 branch、head、remote、local、reason、conflict，以及可用时的 ahead/behind。"),
			loc("--capture: transaction, commit, revision, changed and sync.", "`--capture`：输出 transaction、commit、revision、changed 和 sync。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Background sync was started or attempted (a failure is reported on stderr), capture completed, or foreground sync succeeded.", "后台同步已启动或已尝试（失败写入 stderr），`--capture` 已完成，或前台同步成功。")},
			{Code: 1, Meaning: loc("--wait or --capture failed with a stable error code.", "`--wait` 或 `--capture` 失败，并返回稳定错误码。")},
			{Code: 2, Meaning: loc("Invalid Hook JSON, unknown argument, or missing flag value.", "Hook JSON 无效、存在未知选项或缺少选项值。")},
		},
		Examples: []helpExample{
			{Command: "memctl sync --wait", Note: loc("Foreground sync; inspect the exit code and remote state.", "前台同步；检查退出码和远端状态。")},
			{Command: "memctl sync --capture --agent my-agent --sid \"$SESSION_ID\"", Note: loc("Commit serial edits made outside memctl.", "提交在 memctl 之外串行完成的编辑。")},
		},
		Notes: []localizedText{
			loc("Use `write` for memory edits. Capture is not a replacement for the managed write path.", "记忆编辑使用 `write`；`--capture` 不能替代受管写入路径。"),
			loc("Hooks should not pass --wait; use foreground sync only from an interactive or scripted caller that can wait.", "Hook 不应传 `--wait`；只有能等待的交互式或脚本调用才使用前台同步。"),
		},
		Related: []string{"write", "end", "resolve"},
	},
	{
		Name:    "end",
		Usage:   []string{"memctl end [--agent NAME] [--sid ID] [--stdin]"},
		Summary: loc("Trigger session-end synchronization.", "触发会话结束同步。"),
		Description: loc(
			"SessionEnd Hook. Starts a detached background sync, consumes an optional bounded Hook payload and exits without waiting for Git or the network. It is idempotent, does not release the repository lock and does not mark a session as closed. Turn-level durability belongs to `sync`; `end` is for host session shutdown.",
			"SessionEnd Hook。启动分离的后台同步，有界读取可选的 Hook JSON，然后立即返回，不等待 Git 或网络。该命令幂等，不释放仓库锁，也不把会话标记为关闭。回合级耐久性由 `sync` 负责，`end` 只用于宿主会话退出。",
		),
		Flags: []helpItem{
			{Name: "--stdin", Description: loc("Consume at most 1 MiB of Hook JSON. Invalid JSON exits 2.", "消费最多 1 MiB 的 Hook JSON。JSON 无效时返回 2。")},
			{Name: "--agent", Value: "NAME", Description: loc("Accepted for Hook symmetry; end does not write provenance.", "为保持 Hook 参数一致而接受；end 不写溯源信息。")},
			{Name: "--sid", Value: "ID", Description: loc("Accepted for Hook symmetry; end does not write provenance.", "为保持 Hook 参数一致而接受；end 不写溯源信息。")},
		},
		Output: []localizedText{
			loc("Empty stdout.", "stdout 为空。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Background sync was started or attempted; failures are reported on stderr and do not block the host.", "后台同步已启动或已尝试；失败写入 stderr，不阻断宿主。")},
			{Code: 2, Meaning: loc("Invalid Hook JSON or unknown argument.", "Hook JSON 无效或存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl end --stdin --agent claude-code --sid \"$AGENT_MEMORY_SID\"", Note: loc("Host SessionEnd hook.", "宿主 SessionEnd Hook。")},
		},
		Notes: []localizedText{
			loc("A crash or SIGKILL can skip end. The next `start` or `sync` retries the sync.", "崩溃或 SIGKILL 可能跳过 end；下一次 `start` 或 `sync` 会重试同步。"),
			loc("Do not use `end` to release locks or abort Git operations; it does neither.", "不要用 `end` 释放锁或中止 Git 操作；它两者都不做。"),
		},
		Related: []string{"sync", "start"},
	},
	{
		Name:    "start",
		Usage:   []string{"memctl start [--agent NAME] [--sid ID] [--stdin] [--json] [--agy]"},
		Summary: loc("SessionStart hook: sync and inject context.", "SessionStart Hook：同步并注入上下文。"),
		Description: loc(
			"SessionStart Hook. Runs a bounded sync, builds the memory context and writes it to stdout. Plain text is the default; `--json` emits the hookSpecificOutput envelope for SessionStart; `--agy` emits the Antigravity injectSteps envelope. Diagnostics go to stderr, so stdout contains only context or one JSON object.",
			"SessionStart Hook。执行有界同步，构建记忆上下文并写入 stdout。默认输出纯文本；`--json` 输出 SessionStart 的 hookSpecificOutput envelope；`--agy` 输出 Antigravity 的 injectSteps envelope。诊断写入 stderr，stdout 只包含上下文或单个 JSON 对象。",
		),
		Flags: []helpItem{
			{Name: "--agent", Value: "NAME", Description: loc("Agent name for session identity.", "用于会话身份的 Agent 名称。")},
			{Name: "--sid", Value: "ID", Description: loc("Session id. Falls back to AGENT_MEMORY_SID, DSH_HOOK_SESSION_ID, then Hook JSON.", "会话 ID。依次回退到 `AGENT_MEMORY_SID`、`DSH_HOOK_SESSION_ID` 和 Hook JSON。")},
			{Name: "--stdin", Description: loc("Read at most 1 MiB of Hook JSON. Invalid JSON exits 2.", "读取最多 1 MiB 的 Hook JSON。JSON 无效时返回 2。")},
			{Name: "--json", Description: loc("Emit the SessionStart hookSpecificOutput envelope.", "输出 SessionStart 的 hookSpecificOutput envelope。")},
			{Name: "--agy", Description: loc("Emit the Antigravity injectSteps envelope.", "输出 Antigravity 的 injectSteps envelope。")},
		},
		Output: []localizedText{
			loc("Plain text context, or one JSON object with --json/--agy. No Git output on stdout.", "纯文本上下文，或带 `--json`/`--agy` 的单个 JSON 对象。stdout 不混入 Git 输出。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Hook invocation completed; a repository or sync failure is reported without blocking the host. Manual invocation also returns 0 on success.", "Hook 调用已结束；仓库或同步失败只报告，不阻断宿主。手工调用成功时也返回 0。")},
			{Code: 1, Meaning: loc("Manual invocation (no --stdin) could not read the repository or sync.", "手工调用（未带 `--stdin`）无法读取仓库，或同步失败。")},
			{Code: 2, Meaning: loc("Unknown flag or invalid Hook JSON.", "存在未知选项或 Hook JSON 无效。")},
		},
		Examples: []helpExample{
			{Command: "memctl start --agent claude-code --sid \"$AGENT_MEMORY_SID\" --stdin --json", Note: loc("Host SessionStart hook with Hook JSON on stdin.", "宿主 SessionStart Hook，stdin 传入 Hook JSON。")},
			{Command: "memctl start --agent my-agent", Note: loc("Manual call; failures return 1.", "手工调用；失败返回 1。")},
		},
		Notes: []localizedText{
			loc("A missing repository reports NO_REPO with a `memctl init` hint. Do not ignore it in a host hook without a reason.", "仓库缺失时报告 NO_REPO 并提示运行 `memctl init`。宿主 Hook 中没有理由时不要忽略该提示。"),
			loc("Without a stable session id the context is still injected, but the source tag is unavailable and cross-process dedupe is disabled.", "缺少稳定会话 ID 时仍会注入上下文，但来源标签不可用，且不做跨进程去重。"),
		},
		Related: []string{"context", "pull", "tag", "wrap", "install"},
	},
	{
		Name:    "context",
		Usage:   []string{"memctl context [--once] [--agent NAME] [--sid ID] [--stdin] [--json]"},
		Summary: loc("UserPromptSubmit hook: sync and inject context.", "UserPromptSubmit Hook：同步并注入上下文。"),
		Description: loc(
			"UserPromptSubmit Hook. Runs the same synchronization flow as start and injects context for the current turn. `--once` suppresses repeated body output after a successful injection in this session, but the sync still runs. The JSON envelope uses the UserPromptSubmit event name.",
			"UserPromptSubmit Hook。使用与 start 相同的同步流程，并为当前回合注入上下文。`--once` 在本会话成功注入后抑制重复正文输出，但同步仍会尝试。JSON envelope 使用 UserPromptSubmit 事件名。",
		),
		Flags: []helpItem{
			{Name: "--once", Description: loc("Suppress repeated context body after a successful injection.", "成功注入后抑制重复的上下文正文。")},
			{Name: "--agent", Value: "NAME", Description: loc("Agent name for session identity.", "用于会话身份的 Agent 名称。")},
			{Name: "--sid", Value: "ID", Description: loc("Session id; same fallback order as start.", "会话 ID；回退顺序与 start 相同。")},
			{Name: "--stdin", Description: loc("Read at most 1 MiB of Hook JSON. Invalid JSON exits 2.", "读取最多 1 MiB 的 Hook JSON。JSON 无效时返回 2。")},
			{Name: "--json", Description: loc("Emit the UserPromptSubmit hookSpecificOutput envelope.", "输出 UserPromptSubmit 的 hookSpecificOutput envelope。")},
		},
		Output: []localizedText{
			loc("Context text, one JSON object with --json, or empty stdout with --once after a successful injection.", "上下文文本；带 `--json` 时为单个 JSON 对象；成功注入后带 `--once` 时 stdout 可为空。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Hook invocation completed; failures are reported without blocking the host.", "Hook 调用已结束；失败只报告，不阻断宿主。")},
			{Code: 1, Meaning: loc("Manual invocation (no --stdin) could not read the repository or sync.", "手工调用（未带 `--stdin`）无法读取仓库，或同步失败。")},
			{Code: 2, Meaning: loc("Unknown flag or invalid Hook JSON.", "存在未知选项或 Hook JSON 无效。")},
		},
		Examples: []helpExample{
			{Command: "memctl context --once --agent claude-code --sid \"$AGENT_MEMORY_SID\" --stdin --json", Note: loc("Per-turn hook that injects once per session.", "每回合触发、每会话只注入一次正文的 Hook。")},
			{Command: "memctl context --agent my-agent", Note: loc("Manual call; failures return 1.", "手工调用；失败返回 1。")},
		},
		Notes: []localizedText{
			loc("Dedupe is keyed by agent and session id. Without a stable id, the body is not deduplicated across processes.", "去重键为 Agent 和会话 ID。缺少稳定 ID 时，不跨进程去重正文。"),
		},
		Related: []string{"start", "pull", "sync"},
	},
	{
		Name:    "pull",
		Usage:   []string{"memctl pull [--once] [--agent NAME] [--sid ID] [--stdin]"},
		Summary: loc("Silent hook synchronization.", "静默 Hook 同步。"),
		Description: loc(
			"Runs the same synchronization flow as start but writes nothing to stdout; use it when a host only needs the memory repository synchronized. `--once` is accepted. `--json` and `--agy` are not supported.",
			"使用与 start 相同的同步流程，但不向 stdout 写任何内容；适用于宿主只需要同步记忆仓库的场景。接受 `--once`，不支持 `--json` 和 `--agy`。",
		),
		Flags: []helpItem{
			{Name: "--once", Description: loc("Accepted for Hook symmetry.", "为保持 Hook 参数一致而接受。")},
			{Name: "--agent", Value: "NAME", Description: loc("Agent name for session identity.", "用于会话身份的 Agent 名称。")},
			{Name: "--sid", Value: "ID", Description: loc("Session id; same fallback order as start.", "会话 ID；回退顺序与 start 相同。")},
			{Name: "--stdin", Description: loc("Read at most 1 MiB of Hook JSON. Invalid JSON exits 2.", "读取最多 1 MiB 的 Hook JSON。JSON 无效时返回 2。")},
		},
		Output: []localizedText{
			loc("Empty stdout.", "stdout 为空。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Hook invocation completed; failures are reported without blocking the host.", "Hook 调用已结束；失败只报告，不阻断宿主。")},
			{Code: 1, Meaning: loc("Manual invocation (no --stdin) could not read the repository or sync.", "手工调用（未带 `--stdin`）无法读取仓库，或同步失败。")},
			{Code: 2, Meaning: loc("Unsupported --json/--agy, unknown flag or invalid Hook JSON.", "不支持 `--json`/`--agy`、存在未知选项或 Hook JSON 无效。")},
		},
		Examples: []helpExample{
			{Command: "memctl pull --agent claude-code --sid \"$AGENT_MEMORY_SID\" --stdin", Note: loc("Silent hook sync.", "静默 Hook 同步。")},
		},
		Notes: []localizedText{
			loc("Use `sync --wait` when you need a foreground result; pull never prints one.", "需要前台结果时使用 `sync --wait`；pull 不输出结果。"),
		},
		Related: []string{"start", "context", "sync"},
	},
	{
		Name:    "tag",
		Usage:   []string{"memctl tag [--agent NAME] [--sid ID]"},
		Summary: loc("Print the canonical session source URI.", "输出当前会话的规范来源 URI。"),
		Description: loc(
			"Prints `agent://<device>/<agent>/<sid>` for the current session. The agent comes from --agent or AGENT_MEMORY_AGENT; the device comes from AGENT_MEMORY_DEVICE or the hostname; the session id must come from --sid, AGENT_MEMORY_SID or DSH_HOOK_SESSION_ID. The command never reads stdin and never guesses a session from shared state.",
			"输出当前会话的 `agent://<device>/<agent>/<sid>`。Agent 取自 `--agent` 或 `AGENT_MEMORY_AGENT`，设备取自 `AGENT_MEMORY_DEVICE` 或主机名，会话 ID 必须来自 `--sid`、`AGENT_MEMORY_SID` 或 `DSH_HOOK_SESSION_ID`。命令不读取 stdin，也不从共享状态推测会话。",
		),
		Flags: []helpItem{
			{Name: "--agent", Value: "NAME", Description: loc("Agent name; falls back to AGENT_MEMORY_AGENT.", "Agent 名称；回退到 `AGENT_MEMORY_AGENT`。")},
			{Name: "--sid", Value: "ID", Description: loc("Stable session id; required when no environment session id is set.", "稳定会话 ID；未设置环境会话 ID 时必填。")},
		},
		Output: []localizedText{
			loc("One URI line.", "一行 URI。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("URI printed.", "已输出 URI。")},
			{Code: 1, Meaning: loc("NO_SESSION_ID: no stable session id is available.", "NO_SESSION_ID：没有可用的稳定会话 ID。")},
			{Code: 2, Meaning: loc("Unknown argument or missing flag value.", "存在未知选项或缺少选项值。")},
		},
		Examples: []helpExample{
			{Command: "memctl tag --agent claude-code --sid \"$AGENT_MEMORY_SID\""},
			{Command: "memctl tag", Note: loc("Works only when AGENT_MEMORY_SID or DSH_HOOK_SESSION_ID is set.", "仅在设置了 `AGENT_MEMORY_SID` 或 `DSH_HOOK_SESSION_ID` 时可用。")},
		},
		Notes: []localizedText{
			loc("`wrap` exports AGENT_MEMORY_SID to its child, so a wrapped program can call tag without --sid.", "`wrap` 会向子进程导出 `AGENT_MEMORY_SID`，因此被包装程序可以不带 `--sid` 调用 `tag`。"),
		},
		Related: []string{"wrap", "write", "start"},
	},
	{
		Name:    "wrap",
		Usage:   []string{"memctl wrap [--agent NAME] [--sid ID] -- <command...>"},
		Summary: loc("Run a command with session identity and sync.", "以会话身份运行命令，并在前后同步。"),
		Description: loc(
			"Runs a child command with the session environment. Before the child starts, memctl attempts a bounded non-fatal sync. It then exports AGENT_MEMORY_SID, AGENT_MEMORY_AGENT, AGENT_MEMORY_DIR and AGENT_MEMORY_STATE, inherits stdio, forwards SIGINT/SIGTERM to the child, waits, and starts a best-effort background sync. The child's exit code is returned unchanged; a sync failure never overwrites it. argv is not re-split through a shell.",
			"在会话环境中运行子进程。子进程启动前尝试一次有界且不致命的同步，然后导出 `AGENT_MEMORY_SID`、`AGENT_MEMORY_AGENT`、`AGENT_MEMORY_DIR` 和 `AGENT_MEMORY_STATE`，继承 stdio，把 SIGINT 和 SIGTERM 转发给子进程；子进程结束后尽力启动后台同步。退出码原样取自子进程，同步失败不会覆盖它。命令行参数不经过 shell 重新拼接。",
		),
		Arguments: []helpItem{
			{Name: "<command...>", Required: true, Description: loc(
				"Command and arguments after the required `--` separator.",
				"必填 `--` 分隔符之后的命令和参数。",
			)},
		},
		Flags: []helpItem{
			{Name: "--agent", Value: "NAME", Description: loc("Agent name; falls back to AGENT_MEMORY_AGENT.", "Agent 名称；回退到 `AGENT_MEMORY_AGENT`。")},
			{Name: "--sid", Value: "ID", Description: loc("Session id; a random id is generated when omitted and no environment id exists.", "会话 ID；省略且环境变量中没有 ID 时生成随机 ID。")},
		},
		Input: []localizedText{
			loc("The child inherits stdin, stdout and stderr.", "子进程继承 stdin、stdout 和 stderr。"),
		},
		Output: []localizedText{
			loc("The child's output is passed through. Wrapper diagnostics and the source tag go to stderr.", "原样转发子进程的输出；包装器诊断和来源标签写入 stderr。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("The child exited 0.", "子进程退出码为 0。")},
			{Code: 1, Meaning: loc("The wrapper could not load configuration or start the child.", "包装器无法加载配置或启动子进程。")},
			{Code: 2, Meaning: loc("Missing `--`, missing command, or unknown wrapper flag.", "缺少 `--`、缺少命令，或存在未知的包装器选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl wrap --agent claude-code -- claude", Note: loc("Run a host CLI with session identity.", "带会话身份运行宿主 CLI。")},
			{Command: "memctl wrap --agent my-agent --sid \"$AGENT_MEMORY_SID\" -- ./agent.sh --flag", Note: loc("Everything after `--` goes to the child unchanged.", "`--` 之后的内容原样传给子进程。")},
		},
		Notes: []localizedText{
			loc("Use `memctl wrap --help` for this page. Flags after `--` are not parsed by memctl.", "使用 `memctl wrap --help` 查看本页；`--` 之后的参数不由 memctl 解析。"),
		},
		Related: []string{"tag", "sync", "start"},
	},
	{
		Name: "resolve",
		Usage: []string{
			"memctl resolve [--json]",
			"memctl resolve --apply FILE --conflict ID [--json]",
			"memctl resolve --abort --conflict ID [--json]",
		},
		Summary: loc("Inspect, apply or abort a rebase conflict.", "查看冲突状态、应用解决结果或中止 rebase。"),
		Description: loc(
			"The default view prints the current conflict id, phase, upstream and replay commits, and each conflicted file with base/onto/replayed states. `--apply FILE` reads a JSON object with a non-empty baseRevision and a resolvedFiles array covering every conflicted path; each file has path, action (put or delete) and rawContent. `--abort` restores the original HEAD. The view, apply and abort modes are mutually exclusive. A successful apply may enter the next conflict stage; repeat the process until no conflict remains.",
			"默认视图输出当前冲突 ID、所处阶段、upstream 提交和待重放的提交，以及每个冲突文件在 base/onto/replayed 三方的状态。`--apply FILE` 读取 JSON 对象，要求非空 baseRevision 和覆盖每个冲突路径的 resolvedFiles 数组；每项包含 path、action（put 或 delete）和 rawContent。`--abort` 恢复原始 HEAD。查看、应用和中止模式互斥。应用成功后可能进入下一冲突阶段，需要重复执行，直到没有冲突。",
		),
		Flags: []helpItem{
			{Name: "--json", Description: loc("Print the view or outcome as JSON.", "以 JSON 输出视图或结果。")},
			{Name: "--apply", Value: "FILE", Description: loc("Apply a resolution JSON file. Use `-` only as a path character; stdin is not read.", "应用解决结果 JSON 文件。`-` 按普通路径字符处理，不读取 stdin。")},
			{Name: "--abort", Description: loc("Abort the captured conflict and restore the original HEAD.", "中止已捕获的冲突并恢复原始 HEAD。")},
			{Name: "--conflict", Value: "ID", Required: true, Description: loc("Conflict id from the view. Required with --apply and --abort.", "视图中的冲突 ID。与 `--apply` 或 `--abort` 一起使用时必填。")},
		},
		Input: []localizedText{
			loc(
				"For --apply, a JSON file with baseRevision and resolvedFiles, for example {\"baseRevision\":\"...\",\"resolvedFiles\":[{\"path\":\"MEMORY.md\",\"action\":\"put\",\"rawContent\":\"...\"}]}. action is put or delete; every conflicted path must be covered.",
				"`--apply` 的 JSON 文件包含 baseRevision 和 resolvedFiles，例如 {\"baseRevision\":\"...\",\"resolvedFiles\":[{\"path\":\"MEMORY.md\",\"action\":\"put\",\"rawContent\":\"...\"}]}。action 为 put 或 delete；必须覆盖每个冲突路径。",
			),
		},
		Output: []localizedText{
			loc("Text: head, remote, conflict id and file states.", "文本：head、remote、冲突 ID 和文件状态。"),
			loc("JSON: {conflictId, head, remoteState, conflict}.", "JSON：{conflictId, head, remoteState, conflict}。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("View, apply or abort completed; a next stage may still be pending.", "查看、应用或中止已完成；可能仍有下一阶段。")},
			{Code: 1, Meaning: loc("CONFLICT_STALE, INVALID_ARGUMENT, NOT_FOUND or GIT_FAILED.", "CONFLICT_STALE、INVALID_ARGUMENT、NOT_FOUND 或 GIT_FAILED。")},
			{Code: 2, Meaning: loc("Mutually exclusive modes, missing --conflict, or unknown argument.", "同时使用互斥模式、缺少 `--conflict`，或存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl resolve --json", Note: loc("Inspect the current conflict.", "查看当前冲突。")},
			{Command: "memctl resolve --apply /tmp/resolution.json --conflict CONFLICT_ID", Note: loc("Apply a complete resolution.", "应用完整解决结果。")},
			{Command: "memctl resolve --abort --conflict CONFLICT_ID", Note: loc("Restore the original HEAD.", "恢复原始 HEAD。")},
		},
		Notes: []localizedText{
			loc("Do not run ordinary merge, rebase or revert commands while a captured conflict is active.", "已捕获冲突处于活动状态时，不要执行普通 merge、rebase 或 revert 命令。"),
			loc("Unsupported conflict types require manual handling; memctl does not claim a lossless one-click resolution.", "不支持的冲突类型需要人工处理；memctl 不提供一键无损解决。"),
		},
		Related: []string{"sync", "recover"},
	},
	{
		Name: "recover",
		Usage: []string{
			"memctl recover --status",
			"memctl recover --rollback",
		},
		Summary: loc("Inspect or roll back unfinished transactions.", "查看或回滚未完成事务。"),
		Description: loc(
			"Inspects transactions that memctl recorded but did not finish, or rolls them back to their pre-images. It does not take over an external merge, rebase or revert. Rollback refuses to overwrite a file that changed after the interruption and reports the path for manual handling.",
			"查看 memctl 已记录但未完成的事务，或将其回滚到前镜像。不接管外部的 merge、rebase 或 revert。若文件在中断后发生变化，回滚会拒绝覆盖，并报告该路径供人工处理。`--status` 与 `--rollback` 互斥，必须且只能提供其中一个。",
		),
		Flags: []helpItem{
			{Name: "--status", Description: loc("List unfinished transactions without writing. Exactly one of --status or --rollback is required.", "只列出未完成事务，不写文件。")},
			{Name: "--rollback", Description: loc("Roll back unfinished transactions. Exactly one of --status or --rollback is required.", "把未完成事务回滚到前镜像。")},
		},
		Output: []localizedText{
			loc("`no unfinished transactions`, or transaction id, start time, original head, resolution and affected files.", "输出 `no unfinished transactions`，或事务 ID、开始时间、原始 head、处理结果和受影响文件。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Status or rollback completed.", "查看或回滚已完成。")},
			{Code: 1, Meaning: loc("Recovery failed or the repository is busy.", "恢复失败，或仓库正忙。")},
			{Code: 2, Meaning: loc("Missing both modes, both modes, or an unknown flag.", "没有提供模式、同时提供两种模式，或存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl recover --status", Note: loc("Check for interrupted work after a crash.", "崩溃后检查是否有中断的工作。")},
			{Command: "memctl recover --rollback", Note: loc("Restore pre-images when no conflicting edit exists.", "在没有冲突编辑时恢复前镜像。")},
		},
		Notes: []localizedText{
			loc("Run recovery before starting a new write when a previous process was interrupted.", "上一个进程被中断时，先恢复再开始新的写入。"),
		},
		Related: []string{"resolve", "doctor"},
	},
	{
		Name:    "doctor",
		Usage:   []string{"memctl doctor [--json] [--fix]"},
		Summary: loc("Diagnose the repository; optionally apply deterministic fixes.", "诊断仓库，可选执行确定性修复。"),
		Description: loc(
			"By default, runs a read-only diagnosis of dead links, orphan files, entry growth and runtime state. `--fix` applies only deterministic repairs through the managed transaction; currently this is recreating a missing entry file. Issues without a deterministic repair are not guessed. An error-level issue makes the command exit 1; warnings and info alone exit 0.",
			"默认执行只读诊断，检查死链、孤立文件、入口膨胀和运行状态。`--fix` 只通过受管事务执行确定性修复，目前唯一的修复是重建缺失的入口文件；没有确定修复方式的问题不会被猜测处理。存在 error 级问题时返回 1，只有 warning 和 info 时返回 0。",
		),
		Flags: []helpItem{
			{Name: "--json", Description: loc("Print the diagnostic report as JSON.", "以 JSON 输出诊断报告。")},
			{Name: "--fix", Description: loc("Apply deterministic fixes and print the resulting report.", "执行确定性修复并输出修复后的报告。")},
		},
		Output: []localizedText{
			loc("Text: revision, checkedAt, counts and one line per issue with a suggestion.", "文本：revision、checkedAt、计数，以及每个问题一行并附建议。"),
			loc("JSON: {revision, checkedAt, counts, issues}; with --fix also {report, result}.", "JSON：{revision, checkedAt, counts, issues}；带 `--fix` 时还有 {report, result}。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("No error-level issue remains.", "没有剩余的 error 级问题。")},
			{Code: 1, Meaning: loc("An error-level issue exists, or a fix failed with a stable error code.", "存在 error 级问题，或修复失败并返回稳定错误码。")},
			{Code: 2, Meaning: loc("Unknown flag.", "存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl doctor", Note: loc("Read-only diagnosis.", "只读诊断。")},
			{Command: "memctl doctor --json", Note: loc("Machine-readable report.", "机器可读报告。")},
			{Command: "memctl doctor --fix", Note: loc("Apply deterministic repairs only.", "仅执行确定性修复。")},
		},
		Notes: []localizedText{
			loc("--fix binds the revision from the diagnosis it just ran. If the repository changed in between, the fix is rejected; rerun doctor.", "`--fix` 绑定刚刚执行的诊断修订。若期间仓库发生变化，修复会被拒绝；重新运行 doctor。"),
		},
		Related: []string{"recover", "ui"},
	},
	{
		Name: "dream",
		Usage: []string{
			"memctl dream --runner claude|codex",
			"memctl dream --show ID",
			"memctl dream --apply ID --patch-hash HASH",
		},
		Summary: loc("Generate, show or apply a Dreaming proposal.", "生成、查看或应用 Dreaming 提案。"),
		Description: loc(
			"Generation exports the repository to an isolated temporary directory, runs the selected runner under a platform sandbox, and persists a proposal with a fixed diff and warnings under the state directory. It does not write the main repository. Generation requires a clean repository, an installed runner and a verified sandbox; unsupported platforms return UNSUPPORTED. `--show` prints a persisted proposal. `--apply` applies exactly the reviewed proposal whose patch hash matches; it does not call the model again, and a stale baseline is rejected.",
			"生成时把仓库导出到隔离的临时目录，在平台沙箱下运行所选 runner，并把带固定 Diff 和警告的提案保存到状态目录，不写主仓库。生成要求仓库干净、runner 已安装且沙箱可用；不支持的平台返回 UNSUPPORTED。`--show` 输出已保存的提案。`--apply` 只应用 patch hash 匹配的已审阅提案，不重新调用模型；旧基线会被拒绝。`--runner`、`--show` 与 `--apply` 互斥，必须且只能提供其中一个。",
		),
		Flags: []helpItem{
			{Name: "--runner", Value: "claude|codex", Description: loc("Generate a proposal. Exactly one of --runner, --show or --apply is required. Selecting a runner confirms that the exported text may be sent to that runner's model endpoint.", "生成提案。选择 runner 即确认导出的文本可能发送给该 runner 的模型端点。")},
			{Name: "--show", Value: "ID", Description: loc("Print a persisted proposal as JSON. Exactly one of --runner, --show or --apply is required.", "以 JSON 输出已保存的提案。")},
			{Name: "--apply", Value: "ID", Description: loc("Apply a reviewed proposal through the managed transaction. Exactly one of --runner, --show or --apply is required.", "通过受管事务应用已审阅的提案。")},
			{Name: "--patch-hash", Value: "HASH", Description: loc("Fixed diff hash from --show. Required with --apply.", "来自 `--show` 的固定 Diff 哈希。与 `--apply` 一起使用时必填。")},
		},
		Output: []localizedText{
			loc("JSON proposal for generation/show, or the apply result for --apply. A failed generation may still print the persisted proposal.", "生成和查看输出 JSON 提案，应用输出应用结果。生成失败时仍可能输出已保存的提案。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Proposal generated, shown or applied.", "提案已生成、查看或应用。")},
			{Code: 1, Meaning: loc("UNSUPPORTED, NO_REPO, dirty repository, PROPOSAL_STALE, runner failure or invalid apply.", "UNSUPPORTED、NO_REPO、仓库脏、PROPOSAL_STALE、runner 失败或应用无效。")},
			{Code: 2, Meaning: loc("No mode, multiple modes, missing --patch-hash, or unknown argument.", "没有提供模式、同时提供多个模式、缺少 `--patch-hash`，或存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl dream --runner claude", Note: loc("Generate a proposal; inspect proposalId and patchHash.", "生成提案；检查 proposalId 和 patchHash。")},
			{Command: "memctl dream --show PROPOSAL_ID", Note: loc("Read the persisted proposal and diff.", "读取已保存的提案和 Diff。")},
			{Command: "memctl dream --apply PROPOSAL_ID --patch-hash HASH", Note: loc("Apply exactly the reviewed proposal.", "仅应用已审阅的提案。")},
		},
		Notes: []localizedText{
			loc("Generating sends exported repository text to the selected runner. Apply only after reviewing the diff and confirming the data boundary.", "生成会把导出的仓库文本发送给所选 runner。确认数据边界并审阅 Diff 后再应用。"),
			loc("There is no combined generate-and-apply switch. Apply always names a persisted proposal and hash.", "没有「重新生成并自动应用」的组合开关；应用时必须指定已保存的提案 ID 和哈希。"),
		},
		Related: []string{"doctor", "ui"},
	},
	{
		Name: "install",
		Usage: []string{
			"memctl install [agent...]",
			"memctl install --all",
			"memctl install <agent> --dry-run",
			"memctl install --status",
		},
		Summary: loc("Inspect or install agent hook configurations.", "查看或安装 Agent Hook 配置。"),
		Description: loc(
			"Detects supported hosts and their plugin versions, then previews or merges managed hooks and configuration into each host's files. It does not access the memory repository, does not require Git or Node.js, and does not change global Git config. `--dry-run` prints the plan without writing; `--status` only probes and reports. `--all` applies to detected hosts and skips missing ones. Explicit agents still produce a plan when the host executable is missing but the target config exists. Installs are backed up, idempotent, serialized by an install lock, and recover an interrupted earlier install before writing.",
			"探测支持的宿主及其插件版本，然后预览计划，或把受管 Hook 与配置合并进各宿主配置文件。不访问记忆仓库，不要求 Git 或 Node.js，不修改全局 Git 配置。`--dry-run` 只输出计划，不写文件；`--status` 只探测并报告。`--all` 只应用到探测到的宿主，跳过缺失的宿主。显式指定的 Agent 即使未在 PATH 中探测到宿主，只要目标配置存在仍会生成计划。安装会先备份，幂等执行，用安装锁串行化，并在写入前恢复上一次中断的安装。",
		),
		Arguments: []helpItem{
			{Name: "[agent...]", Description: loc(
				"One or more adapter names: claude-code, codex, grok, antigravity, commandcode, opencode, pi, dsh.",
				"一个或多个适配器名称：claude-code、codex、grok、antigravity、commandcode、opencode、pi、dsh。",
			)},
		},
		Flags: []helpItem{
			{Name: "--all", Description: loc("Install every detected host. Cannot be combined with explicit agent names.", "安装所有探测到的宿主。不能与显式 Agent 名称同时使用。")},
			{Name: "--dry-run", Description: loc("Print the plan and patches without writing.", "只输出计划和补丁，不写文件。")},
			{Name: "--status", Description: loc("Probe and report only; do not write. Cannot be combined with --all, --dry-run or agent names.", "只探测和报告，不写文件。不能与 `--all`、`--dry-run` 或 Agent 名称同时使用。")},
		},
		Output: []localizedText{
			loc("Per-adapter status, detected host version, plugin version, target artifacts and patch previews. With writes, also `applied` or `manual` lines and backup paths.", "每个适配器的状态、探测到的宿主版本、插件版本、目标文件和补丁预览。执行写入时还会输出 `applied` 或 `manual` 行及备份路径。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Plan or install completed; unsupported or manual adapters are reported in the output. Exit 0 does not mean every adapter was installed.", "计划或安装已完成；不支持或需人工处理的适配器会在输出中报告。退出码 0 不表示每个适配器都已安装。")},
			{Code: 1, Meaning: loc("Configuration, IO, revision mismatch or recovery failure.", "配置、IO、修订不匹配或恢复失败。")},
			{Code: 2, Meaning: loc("No agent or --all, incompatible flags, or unknown adapter.", "缺少 Agent 参数和 `--all`、选项组合不兼容，或适配器名称未知。")},
		},
		Examples: []helpExample{
			{Command: "memctl install --status", Note: loc("Read-only probe of every adapter.", "只读探测所有适配器。")},
			{Command: "memctl install claude-code --dry-run", Note: loc("Preview one host's merged configuration.", "预览单个宿主的合并配置。")},
			{Command: "memctl install claude-code", Note: loc("Apply the previewed configuration.", "应用预览过的配置。")},
			{Command: "memctl install --all", Note: loc("Apply to every detected host; missing hosts are skipped.", "应用到所有探测到的宿主，跳过缺失宿主。")},
		},
		Notes: []localizedText{
			loc("Adapter status values: configured, needs_attention, not_installed, unsupported. An unsupported adapter is reported, not treated as success.", "适配器状态值：configured、needs_attention、not_installed、unsupported。unsupported 只报告，不视为成功。"),
			loc("Run --dry-run first when a user configuration already exists, then apply the same agent to avoid surprise changes.", "用户配置已存在时，先运行 `--dry-run` 确认改动，再对同一 Agent 执行安装。"),
			loc("The DSH native plugin is installed by DSH Plugin Manager. install only probes it and never writes the DSH profile.", "DSH 原生插件由 DSH Plugin Manager 安装。`install` 只探测，不写 DSH profile。"),
			loc("The pi native plugin is installed through `pi install`. install only probes it; it never writes hooks.yaml unless legacy agent-memory-* entries already exist.", "pi 原生插件通过 `pi install` 安装。`install` 只探测；只有旧 agent-memory-* 条目已存在时才继续更新 hooks.yaml。"),
		},
		Related: []string{"dsh", "pi", "start", "context"},
	},
	{
		Name: "dsh",
		Usage: []string{
			"memctl dsh plugin-path",
			"memctl dsh plugin-dir",
		},
		Summary: loc("Print the embedded memctl-dsh plugin path.", "输出内嵌 memctl-dsh 插件路径。"),
		Description: loc(
			"Writes the embedded memctl-dsh plugin as a tarball or an unpacked directory under AGENT_MEMORY_STATE/dsh/ and prints its absolute path. It only writes the state directory: it does not run npm, access the network, or modify a DSH profile. Paste the path into the DSH plugin dialog or pass it to `dsh plugin add`.",
			"把内嵌的 memctl-dsh 插件以 tarball 或解包目录的形式写到 `AGENT_MEMORY_STATE/dsh/` 下，并输出绝对路径。只写状态目录，不运行 npm、不访问网络、不修改 DSH profile。把该路径粘贴到 DSH 插件对话框，或传给 `dsh plugin add`。`plugin-path` 与 `plugin-dir` 互斥，必须且只能提供其中一个。",
		),
		Arguments: []helpItem{
			{Name: "plugin-path", Description: loc("Write and print the absolute .tgz path. Exactly one of plugin-path or plugin-dir is required.", "写入并输出绝对 .tgz 路径。")},
			{Name: "plugin-dir", Description: loc("Write and print the absolute directory path. Exactly one of plugin-path or plugin-dir is required.", "写入并输出绝对目录路径。")},
		},
		Output: []localizedText{
			loc("One absolute path line.", "一行绝对路径。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Path written and printed.", "路径已写入并输出。")},
			{Code: 1, Meaning: loc("The state directory could not be written or the embedded source is invalid.", "无法写入状态目录或内嵌源无效。")},
			{Code: 2, Meaning: loc("Missing or unknown subcommand.", "缺少子命令或子命令未知。")},
		},
		Examples: []helpExample{
			{Command: "memctl dsh plugin-path", Note: loc("Offline tarball for the DSH add-plugin dialog.", "用于 DSH 添加插件对话框的离线 tarball。")},
			{Command: "memctl dsh plugin-dir", Note: loc("Unpacked directory form.", "解包目录形式。")},
		},
		Notes: []localizedText{
			loc("The native plugin is the supported DSH integration. `install --status` reports its detection state.", "原生插件是受支持的 DSH 接入方式；`install --status` 报告其探测状态。"),
		},
		Related: []string{"install"},
	},
	{
		Name:  "pi",
		Usage: []string{"memctl pi plugin-dir"},
		Summary: loc(
			"Print the embedded memctl-pi local package directory.",
			"输出内嵌 memctl-pi 的本地包目录。",
		),
		Description: loc(
			"Writes the embedded memctl-pi extension to AGENT_MEMORY_STATE/pi/ and prints its absolute path. It only writes the state directory: it does not run npm, access the network, or modify pi settings. Pass the path to `pi install <absolute dir>`; the directory is stable across upgrades and must stay readable.",
			"把内嵌的 memctl-pi 扩展写到 `AGENT_MEMORY_STATE/pi/`，并输出绝对路径。只写状态目录，不运行 npm、不访问网络、不修改 pi 设置。把该路径传给 `pi install <绝对目录>`；该目录在升级后保持稳定，并且必须保持可读。",
		),
		Arguments: []helpItem{
			{Name: "plugin-dir", Description: loc("Write and print the absolute local package directory.", "写入并输出本地包目录的绝对路径。")},
		},
		Output: []localizedText{
			loc("One absolute path line.", "一行绝对路径。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Directory written and printed.", "目录已写入并输出。")},
			{Code: 1, Meaning: loc("The state directory could not be written or the embedded source is invalid.", "无法写入状态目录或内嵌源无效。")},
			{Code: 2, Meaning: loc("Missing or unknown subcommand.", "缺少子命令或子命令未知。")},
		},
		Examples: []helpExample{
			{Command: "memctl pi plugin-dir", Note: loc("Print the offline local package directory.", "输出离线本地包目录。")},
			{Command: "pi install /absolute/path", Note: loc("Install the printed directory through pi.", "通过 pi 安装输出的目录。")},
		},
		Notes: []localizedText{
			loc("The npm package `memctl-pi` is the primary channel; this directory is the offline fallback. pi does not install a tarball path.", "npm 包 `memctl-pi` 是主通道；该目录是离线备用。pi 不接受 tarball 路径作为安装输入。"),
			loc("`install pi` only probes the native plugin and never writes hooks.yaml. Existing legacy agent-memory-* entries are kept and reported.", "`install pi` 只探测原生插件，不写 hooks.yaml。已有的旧 agent-memory-* 条目会保留并报告。"),
		},
		Related: []string{"install"},
	},
	{
		Name:    "update",
		Usage:   []string{"memctl update [--check]"},
		Summary: loc("Update memctl from GitHub Releases.", "从 GitHub Releases 更新 memctl。"),
		Description: loc(
			"Queries the latest release of gray0128/memctl, selects the asset for the current platform, downloads SHA256SUMS, verifies the selected asset's SHA-256, and atomically replaces the running binary after a successful check. `--check` only reports. It does not access the memory repository, Git, Node.js or credentials, and it does not execute the downloaded binary or restart the process. Supported platforms are darwin/arm64, darwin/amd64, linux/amd64 and windows/amd64; Windows in-place replacement returns UNSUPPORTED, while --check still works.",
			"查询 gray0128/memctl 的最新 Release，选择当前平台的资产，下载 SHA256SUMS，校验所选资产的 SHA-256，校验通过后原子替换当前二进制。`--check` 只报告，不写文件。不访问记忆仓库、Git、Node.js 或凭据，不执行下载的二进制，也不自动重启。支持的平台为 darwin/arm64、darwin/amd64、linux/amd64 和 windows/amd64；Windows 的就地替换返回 UNSUPPORTED，但 `--check` 仍然可用。",
		),
		Flags: []helpItem{
			{Name: "--check", Description: loc("Only report current and latest versions; never write the binary.", "只报告当前和最新版本，不写二进制。")},
		},
		Output: []localizedText{
			loc("`current:`, `latest:` and one of `updated:`, `update available` or `up to date`.", "`current:`、`latest:`，以及 `updated:`、`update available` 或 `up to date` 之一。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("Updated, or already up to date.", "已更新或已是最新版本。")},
			{Code: 1, Meaning: loc("Network, checksum, asset, permission or unsupported-platform failure. The current binary is unchanged.", "网络、校验和、资产、权限或平台不支持导致失败。当前二进制保持不变。")},
			{Code: 2, Meaning: loc("Unknown flag or unexpected argument.", "存在未知选项或多余参数。")},
		},
		Examples: []helpExample{
			{Command: "memctl update --check", Note: loc("Read-only version check.", "只读版本检查。")},
			{Command: "memctl update", Note: loc("Download, verify and replace, then restart memctl.", "下载、校验并替换；重启 memctl 后生效。")},
		},
		Notes: []localizedText{
			loc("The running process keeps using the old image until restart. memctl does not restart itself.", "正在运行的进程在重启前仍使用旧映像；memctl 不自动重启。"),
		},
		Related: []string{"version"},
	},
	{
		Name:    "ui",
		Usage:   []string{"memctl ui [--port N] [--no-open] [--readonly]"},
		Summary: loc("Serve the embedded local Web UI.", "启动内嵌本地 Web UI。"),
		Description: loc(
			"Serves the embedded Web UI on 127.0.0.1 only, default port 18420, with a per-start session token and Host/Origin checks. The process runs until SIGINT or SIGTERM. `--port 0` binds an ephemeral loopback port; `--no-open` skips opening a browser; `--readonly` rejects every write request on the server, including sync, install and Dreaming actions, instead of only hiding buttons. The memory repository must already exist.",
			"只在 127.0.0.1 上提供内嵌 Web UI，默认端口 18420，每次启动生成新的 Session Token，并校验 Host 和 Origin。进程持续运行，直到收到 SIGINT 或 SIGTERM。`--port 0` 绑定临时回环端口；`--no-open` 不自动打开浏览器；`--readonly` 在服务端拒绝所有写请求（包括同步、安装和 Dreaming），而不只是隐藏按钮。记忆仓库必须已存在。",
		),
		Flags: []helpItem{
			{Name: "--port", Value: "N", Description: loc("Loopback port from 0 to 65535. Default 18420; 0 selects an ephemeral port.", "回环端口，取值 0 至 65535。默认 18420；0 表示临时端口。")},
			{Name: "--no-open", Description: loc("Do not open a browser automatically.", "不自动打开浏览器。")},
			{Name: "--readonly", Description: loc("Reject all write requests on the server.", "在服务端拒绝所有写请求。")},
		},
		Output: []localizedText{
			loc("`memctl ui: serving http://<addr>/ (read-only: <bool>)` and browser-open diagnostics on stderr.", "`memctl ui: serving http://<addr>/ (read-only: <bool>)` 与浏览器打开诊断信息都写入 stderr。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("The server shut down cleanly after SIGINT or SIGTERM.", "收到 SIGINT 或 SIGTERM 后正常关闭。")},
			{Code: 1, Meaning: loc("The address is in use, the repository is missing, or the server failed.", "地址被占用、仓库缺失或服务失败。")},
			{Code: 2, Meaning: loc("Invalid port or unknown flag.", "端口无效或存在未知选项。")},
		},
		Examples: []helpExample{
			{Command: "memctl ui --no-open", Note: loc("Serve on the default port without opening a browser.", "使用默认端口启动，不打开浏览器。")},
			{Command: "memctl ui --port 0 --no-open", Note: loc("Bind an ephemeral port and print the URL.", "绑定临时端口并输出 URL。")},
			{Command: "memctl ui --readonly", Note: loc("Serve a read-only console.", "启动只读控制台。")},
		},
		Notes: []localizedText{
			loc("The UI binds loopback only and does not load external CDN assets. It is not a multi-user authentication system.", "Web UI 只绑定回环地址，不加载外部 CDN 资源；它不是多用户认证系统。"),
		},
		Related: []string{"doctor", "write"},
	},
	{
		Name:    "mcp",
		Usage:   []string{"memctl mcp [--agent NAME] [--sid ID]"},
		Summary: loc("Serve the memory tools over the Model Context Protocol.", "以 Model Context Protocol 提供记忆工具。"),
		Description: loc(
			"Serves two tools over newline-delimited JSON-RPC 2.0 on stdin and stdout: memory_read returns one file with its fileRevision baseline, and memory_write applies one managed write. A host starts this process as an MCP server child, so the tools run with the host's file permissions instead of the agent's bash sandbox. Storage, locking, Git commits and background synchronization still go through the same application transaction as `read` and `write`.",
			"在标准输入输出上以单行 JSON-RPC 2.0 提供两个工具：memory_read 返回一个文件及其 fileRevision 基线，memory_write 执行一次受管写入。宿主以 MCP server 子进程方式启动该命令，因此工具使用宿主的文件权限，而不是 Agent 的 bash 沙箱。存储、锁、Git 提交与后台同步仍走与 `read`、`write` 相同的应用事务。",
		),
		Flags: []helpItem{
			{Name: "--agent", Value: "NAME", Description: loc("Agent name recorded on every write. Falls back to AGENT_MEMORY_AGENT, then unknown.", "每次写入记录的 Agent 名称。依次回退到 AGENT_MEMORY_AGENT 和 unknown。")},
			{Name: "--sid", Value: "ID", Description: loc("Session id recorded on every write. Falls back to AGENT_MEMORY_SID; a missing value is not invented.", "每次写入记录的会话 id。回退到 AGENT_MEMORY_SID；缺失时不伪造。")},
		},
		Output: []localizedText{
			loc("stdout carries only JSON-RPC responses, one per line. Diagnostics go to stderr and never include memory text or tool arguments.", "stdout 只输出 JSON-RPC 响应，每行一条。诊断信息写入 stderr，不包含记忆正文或工具参数。"),
		},
		ExitCodes: []helpExit{
			{Code: 0, Meaning: loc("stdin closed or the process was interrupted.", "stdin 结束或进程被中断。")},
			{Code: 1, Meaning: loc("The configuration could not be loaded.", "无法加载配置。")},
			{Code: 2, Meaning: loc("Unknown argument or a flag without a value.", "存在未知参数或选项缺少取值。")},
		},
		Examples: []helpExample{
			{Command: "memctl mcp --agent dsh", Note: loc("Start the MCP server; the host speaks the protocol on stdin and stdout.", "启动 MCP server，由宿主在标准输入输出上使用协议。")},
			{Command: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, Note: loc("One request line; tools are memory_read and memory_write.", "一行请求；工具为 memory_read 与 memory_write。")},
		},
		Notes: []localizedText{
			loc("Tool failures return isError with the same `CODE: message` text as the CLI, so a model can act on REVISION_MISMATCH or BUSY.", "工具失败返回 isError，文本与 CLI 的 `CODE: message` 一致，使模型可以处理 REVISION_MISMATCH 或 BUSY。"),
			loc("Register this command in the host's MCP configuration. `install` only probes and plans; it does not run a host's own mcp add command.", "在宿主的 MCP 配置中注册该命令。`install` 只探测并生成计划，不运行宿主自己的 mcp add 命令。"),
		},
		Related: []string{"read", "write", "install"},
	},
}

// helpCommandNames returns the catalog names in declaration order.
func helpCommandNames() []string {
	names := make([]string, 0, len(helpCommandData))
	for _, command := range helpCommandData {
		names = append(names, command.Name)
	}
	return names
}

func lookupHelpCommand(name string) (helpCommand, bool) {
	for _, command := range helpCommandData {
		if command.Name == name {
			return command, true
		}
	}
	return helpCommand{}, false
}
