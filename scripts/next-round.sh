#!/usr/bin/env bash
# 为下一轮开发启动一个新的独立 Agent 会话。
#
# 契约：docs/06-development-loop.md 第 6 节「接续的执行机制」。
# 本脚本校验接续前置条件和交接提示词结构，然后按 orca-cli 完全交接（Full Handoff）规范
# 调用 orca terminal create + wait (tui-idle) + send 启动新会话并投递交接提示词。
# 本脚本不生成交接提示词，也不能强制旧会话停止开发。
set -euo pipefail

readonly REQUIRED_SECTIONS=("有效规则与边界" "本轮结果与证据" "剩余事项与依赖" "下一轮目标")

die() {
	printf '错误：%s\n' "$1" >&2
	exit 1
}

usage() {
	cat <<'EOF'
用法：scripts/next-round.sh (--prompt <text> | --prompt-file <path>) [选项]

在当前 Orca 工作区启动一个全新的 Agent 会话，接续下一轮开发。

交接提示词（二选一，必需）：
  --prompt <text>          交接提示词正文
  --prompt-file <path>     交接提示词文件，按原样读取

提示词必须以 Markdown 标题或加粗行给出以下四个小节：
  有效规则与边界、本轮结果与证据、剩余事项与依赖、下一轮目标

选项：
  --agent <id>             Agent 标识，默认 pi
  --worktree <selector>    工作区选择器，默认 active，即当前目录所属的 Orca 工作区
  --model <id>             模型标识，拼入 Agent 启动命令
  --effort <level>         推理强度，拼入 Agent 启动命令；必须同时给出 --model
  --focus                  启动后切换到新会话终端标签页
  --terminal <handle>      重放提示词投递时指定的目标终端句柄（需与 --retry-request 同用）
  --retry-request <id>     用 requestId 在已创建终端上重放同一次提示词投递，不新建终端
  --retry-launch <id>      --retry-request 的兼容别名（需与 --terminal 同用）
  --dry-run                只打印将执行的命令参数，不启动会话，不检查接续前置条件
  -h, --help               显示本说明
EOF
}

quote_sh_arg() {
	local val="$1"
	if [ -z "$val" ]; then
		printf "''"
		return
	fi
	if [[ "$val" =~ ^[A-Za-z0-9_./:=+-]+$ ]]; then
		printf '%s' "$val"
		return
	fi
	local escaped="${val//\'/\'\"\'\"\'}"
	printf "'%s'" "$escaped"
}

build_agent_command() {
	local target_agent="$1"
	local target_model="$2"
	local target_effort="$3"
	local orca_bin="$4"
	local cmd_tokens=()

	case "$target_agent" in
	antigravity)
		cmd_tokens=("agy")
		;;
	cursor)
		cmd_tokens=("cursor-agent")
		;;
	command-code)
		cmd_tokens=("command-code" "--trust")
		;;
	opencode2)
		cmd_tokens=("opencode2" "--standalone")
		;;
	muse)
		cmd_tokens=("muse" "--trust-workspace")
		;;
	dsh)
		cmd_tokens=("dsh-tui" ".")
		;;
	kiro)
		cmd_tokens=("kiro-cli" "chat" "--tui")
		;;
	hermes)
		cmd_tokens=("hermes" "--tui")
		;;
	rovo)
		cmd_tokens=("acli" "rovodev" "run")
		;;
	continue)
		cmd_tokens=("cn")
		;;
	aug)
		cmd_tokens=("auggie")
		;;
	mistral-vibe)
		cmd_tokens=("vibe")
		;;
	qwen-code)
		cmd_tokens=("qwen")
		;;
	mimo-code)
		cmd_tokens=("mimo")
		;;
	qoder)
		cmd_tokens=("qodercli")
		;;
	qoder-cn)
		cmd_tokens=("qoderclicn")
		;;
	trae)
		cmd_tokens=("traecli")
		;;
	claude-agent-teams)
		cmd_tokens=("$orca_bin" "claude-teams")
		;;
	*)
		cmd_tokens=("$target_agent")
		;;
	esac

	if [ -n "$target_model" ]; then
		case "$target_agent" in
		codex | gemini | grok)
			cmd_tokens+=("-m" "$target_model")
			;;
		*)
			cmd_tokens+=("--model" "$target_model")
			;;
		esac
	fi

	if [ -n "$target_effort" ]; then
		case "$target_agent" in
		pi | omp)
			cmd_tokens+=("--thinking" "$target_effort")
			;;
		codex)
			cmd_tokens+=("-c" "model_reasoning_effort=${target_effort}")
			;;
		grok)
			cmd_tokens+=("--reasoning-effort" "$target_effort")
			;;
		*)
			cmd_tokens+=("--effort" "$target_effort")
			;;
		esac
	fi

	local formatted=()
	local tok
	for tok in "${cmd_tokens[@]}"; do
		formatted+=("$(quote_sh_arg "$tok")")
	done
	local IFS=' '
	printf '%s' "${formatted[*]}"
}

agent=pi
worktree=active
prompt_text=
prompt_file=
model=
effort=
focus=0
terminal_handle=
retry_request=
dry_run=0

while [ $# -gt 0 ]; do
	case "$1" in
	--prompt)
		prompt_text="${2:-}"
		shift 2 || die "参数 --prompt 缺少取值"
		;;
	--prompt-file)
		prompt_file="${2:-}"
		shift 2 || die "参数 --prompt-file 缺少取值"
		;;
	--agent)
		agent="${2:-}"
		shift 2 || die "参数 --agent 缺少取值"
		;;
	--worktree)
		worktree="${2:-}"
		shift 2 || die "参数 --worktree 缺少取值"
		;;
	--model)
		model="${2:-}"
		shift 2 || die "参数 --model 缺少取值"
		;;
	--effort)
		effort="${2:-}"
		shift 2 || die "参数 --effort 缺少取值"
		;;
	--focus)
		focus=1
		shift
		;;
	--terminal)
		terminal_handle="${2:-}"
		shift 2 || die "参数 --terminal 缺少取值"
		;;
	--retry-request | --retry-launch)
		flag_name="$1"
		retry_request="${2:-}"
		shift 2 || die "参数 ${flag_name} 缺少取值"
		;;
	--dry-run)
		dry_run=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		die "未知参数：$1"
		;;
	esac
done

[ -n "$agent" ] || die "缺少 --agent 的取值"
[ -n "$worktree" ] || die "缺少 --worktree 的取值"
[ -z "$effort" ] || [ -n "$model" ] || die "--effort 必须与 --model 同时给出"
if [ -n "$retry_request" ] && [ -z "$terminal_handle" ]; then
	die "--retry-request（或 --retry-launch）必须与 --terminal <handle> 同时给出"
fi
if [ -n "$terminal_handle" ] && [ -z "$retry_request" ]; then
	die "--terminal 仅用于配合 --retry-request（或 --retry-launch）重放提示词投递"
fi

# 交接提示词来源与结构校验。
check_file=
temp_file=
if [ -n "$prompt_file" ]; then
	[ -z "$prompt_text" ] || die "--prompt 与 --prompt-file 不能同时使用"
	[ -f "$prompt_file" ] || die "交接提示词文件不存在：$prompt_file"
	check_file="$prompt_file"
elif [ -n "$prompt_text" ]; then
	temp_file="$(mktemp)"
	trap 'rm -f "$temp_file"' EXIT
	printf '%s\n' "$prompt_text" >"$temp_file"
	check_file="$temp_file"
else
	die "缺少交接提示词：请使用 --prompt <text> 或 --prompt-file <path>"
fi

grep -q '[^[:space:]]' "$check_file" || die "交接提示词为空"

missing=()
for section in "${REQUIRED_SECTIONS[@]}"; do
	grep -qE "^#{1,6}[[:space:]]*${section}|^\*\*${section}\*\*" "$check_file" || missing+=("$section")
done
if [ "${#missing[@]}" -gt 0 ]; then
	die "交接提示词缺少必需小节：${missing[*]}"
fi

prompt_body="$(cat "$check_file")"

# 接续前置条件：本轮变更已提交并推送。重放已接受的启动或仅打印命令时跳过。
if [ -z "$retry_request" ] && [ "$dry_run" = 0 ]; then
	git rev-parse --git-dir >/dev/null 2>&1 || die "当前目录不在 Git 仓库内"
	[ -z "$(git status --porcelain)" ] || die "工作树不干净：先提交并推送本轮变更"
	upstream="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null || true)"
	[ -n "$upstream" ] || die "当前分支没有上游分支：先推送本轮变更"
	[ "$(git rev-parse HEAD)" = "$(git rev-parse "$upstream")" ] || die "HEAD 与 $upstream 不一致：先推送本轮变更"
fi

# Orca 可执行文件按 orca-cli Skill 的解析顺序确定。
if [ -n "${ORCA_CLI_COMMAND:-}" ]; then
	orca_cmd="$ORCA_CLI_COMMAND"
elif [ -n "${ORCA_DEV_REPO_ROOT:-}" ]; then
	orca_cmd=orca-dev
elif [ "$(uname -s)" = Linux ]; then
	orca_cmd=orca-ide
else
	orca_cmd=orca
fi
command -v "$orca_cmd" >/dev/null 2>&1 || die "找不到 Orca 可执行文件：$orca_cmd"

agent_cmd="$(build_agent_command "$agent" "$model" "$effort" "$orca_cmd")"

if [ -n "$retry_request" ]; then
	send_args=("$orca_cmd" terminal send --terminal "$terminal_handle" "--text=$prompt_body" --enter --retry-request "$retry_request" --json)
	if [ "$dry_run" = 1 ]; then
		printf '命令参数预览（未执行，重放模式，按参数逐行输出）：\n'
		for arg in "${send_args[@]}"; do
			printf '  %s\n' "$arg"
		done
		exit 0
	fi

	send_status=0
	send_out="$("${send_args[@]}")" || send_status=$?
	if [ "$send_status" -ne 0 ]; then
		[ -z "$send_out" ] || printf '%s\n' "$send_out" >&2
		die "Orca 提示词重放失败（退出码 ${send_status}，终端 ${terminal_handle}，请求 ${retry_request}）。保留交接提示词；请先用 ${orca_cmd} terminal read --terminal ${terminal_handle} 检查终端状态，若仍需重放可继续使用 --terminal ${terminal_handle} --retry-request ${retry_request}"
	fi

	printf '%s\n' "$send_out"
	printf '交接提示词重放回执：terminal=%s, requestId=%s。本轮会话到此结束开发，只向用户报告结果。\n' "$terminal_handle" "$retry_request" >&2
	exit 0
fi

create_args=("$orca_cmd" terminal create --worktree "$worktree" "--command=$agent_cmd")
if [ "$focus" = 1 ]; then
	create_args+=(--focus)
fi
create_args+=(--json)

if [ "$dry_run" = 1 ]; then
	wait_preview_args=("$orca_cmd" terminal wait --terminal "<handle>" --for tui-idle --timeout-ms 60000 --json)
	send_preview_args=("$orca_cmd" terminal send --terminal "<handle>" "--text=$prompt_body" --enter --json)
	printf '命令参数预览（未执行，按步骤与参数逐行输出）：\n'
	printf '[1/3] create:\n'
	for arg in "${create_args[@]}"; do
		printf '  %s\n' "$arg"
	done
	printf '[2/3] wait:\n'
	for arg in "${wait_preview_args[@]}"; do
		printf '  %s\n' "$arg"
	done
	printf '[3/3] send:\n'
	for arg in "${send_preview_args[@]}"; do
		printf '  %s\n' "$arg"
	done
	exit 0
fi

create_status=0
create_out="$("${create_args[@]}")" || create_status=$?
if [ "$create_status" -ne 0 ]; then
	[ -z "$create_out" ] || printf '%s\n' "$create_out" >&2
	die "Orca 创建终端失败（退出码 ${create_status}）。尚未发送交接提示词，保留提示词并排查原因后重试"
fi

created_handle="$(printf '%s\n' "$create_out" | sed -n 's/.*"handle"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)"
if [ -z "$created_handle" ]; then
	printf '%s\n' "$create_out" >&2
	die "无法从 Orca 输出中解析终端句柄（handle）。尚未发送交接提示词，请检查上方输出"
fi

wait_satisfied=0
for timeout_ms in 60000 120000; do
	wait_status=0
	wait_out="$("$orca_cmd" terminal wait --terminal "$created_handle" --for tui-idle --timeout-ms "$timeout_ms" --json)" || wait_status=$?
	if [ "$wait_status" -eq 0 ] && printf '%s\n' "$wait_out" | grep -q '"satisfied"[[:space:]]*:[[:space:]]*true'; then
		wait_satisfied=1
		break
	fi
done

if [ "$wait_satisfied" -ne 1 ]; then
	[ -z "${wait_out:-}" ] || printf '%s\n' "$wait_out" >&2
	die "终端 ${created_handle} 未在时限内进入 tui-idle 状态。尚未发送交接提示词；请检查该终端状态或关闭后重试"
fi

send_status=0
send_out="$("$orca_cmd" terminal send --terminal "$created_handle" "--text=$prompt_body" --enter --json)" || send_status=$?
if [ "$send_status" -ne 0 ]; then
	[ -z "$send_out" ] || printf '%s\n' "$send_out" >&2
	req_id="$(printf '%s\n' "$send_out" | sed -nE 's/.*"(orchestrationRequestId|requestId)"[[:space:]]*:[[:space:]]*"([^"]*)".*/\2/p' | head -n 1)"
	if [ -n "$req_id" ]; then
		die "Orca 提示词投递失败（退出码 ${send_status}，终端 ${created_handle}，requestId ${req_id}）。保留交接提示词；请使用 --terminal ${created_handle} --retry-request ${req_id} 重放同一次投递，不要重新创建终端"
	fi
	die "Orca 提示词投递失败（退出码 ${send_status}，终端 ${created_handle}）。保留交接提示词；若上方输出含 requestId，请使用 --terminal ${created_handle} --retry-request <id> 重放同一次投递，否则先用 ${orca_cmd} terminal read --terminal ${created_handle} 核查终端状态"
fi

req_id="$(printf '%s\n' "$send_out" | sed -nE 's/.*"(orchestrationRequestId|requestId)"[[:space:]]*:[[:space:]]*"([^"]*)".*/\2/p' | head -n 1)"
printf '%s\n' "$send_out"
printf '新会话启动回执：handle=%s, tui-idle=satisfied:true, accepted=true, requestId=%s。本轮会话到此结束开发，只向用户报告结果。\n' "$created_handle" "${req_id:-unknown}" >&2
