#!/usr/bin/env bash
# 为下一轮开发启动一个新的独立 Agent 会话。
#
# 契约：docs/06-development-loop.md 第 6 节「接续的执行机制」。
# 本脚本校验接续前置条件和交接提示词结构，然后调用 Orca 启动新会话。
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
  --prompt <text>        交接提示词正文
  --prompt-file <path>   交接提示词文件，按原样读取

提示词必须以 Markdown 标题或加粗行给出以下四个小节：
  有效规则与边界、本轮结果与证据、剩余事项与依赖、下一轮目标

选项：
  --agent <id>            Agent 标识，默认 pi
  --worktree <selector>   工作区选择器，默认 active，即当前目录所属的 Orca 工作区
  --model <id>            模型标识，透传给 orca agent launch
  --effort <level>        推理强度，透传；必须同时给出 --model
  --focus                 启动后切换到新会话标签页
  --retry-launch <id>     用 operationId 重放同一次已接受的启动，不发起新启动
  --dry-run               只打印将执行的命令参数，不启动会话，不检查接续前置条件
  -h, --help              显示本说明
EOF
}

agent=pi
worktree=active
prompt_text=
prompt_file=
model=
effort=
focus=0
retry_launch=
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
	--retry-launch)
		retry_launch="${2:-}"
		shift 2 || die "参数 --retry-launch 缺少取值"
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
if [ -z "$retry_launch" ] && [ "$dry_run" = 0 ]; then
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

args=("$orca_cmd" agent launch --agent "$agent" --worktree "$worktree" --prompt "$prompt_body" --json)
[ -z "$model" ] || args+=(--model "$model")
[ -z "$effort" ] || args+=(--effort "$effort")
if [ "$focus" = 1 ]; then
	args+=(--focus)
fi
[ -z "$retry_launch" ] || args+=(--retry-launch "$retry_launch")

if [ "$dry_run" = 1 ]; then
	printf '命令参数预览（未执行，按参数逐行输出）：\n'
	for arg in "${args[@]}"; do
		printf '  %s\n' "$arg"
	done
	exit 0
fi

orca_status=0
result="$("${args[@]}")" || orca_status=$?
if [ "$orca_status" -ne 0 ]; then
	die "Orca 启动失败（退出码 ${orca_status}）。保留交接提示词；若输出中有 operationId，用 --retry-launch <operationId> 重放同一次启动，不要重新发起启动"
fi

printf '%s\n' "$result"
printf '新会话启动请求已发出。本轮会话到此结束开发，只向用户报告结果。\n' >&2
