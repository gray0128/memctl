package corpus

import (
	"encoding/json"
	"strconv"
	"strings"
)

// This file keeps the bounded tool outcome evidence of
// docs/18-agent-memory-review-and-optimization.md section 4.3.2. A digest never
// stores full tool output; it keeps a short summary of a failed call, or of the
// last verification command of a turn, because that is the objective record
// offline Dreaming needs to prove an old memory wrong. Every summary is
// redacted, screened for injected instructions and cut to MaxToolOutcomeBytes.

// outcomeKeyLines bounds the output lines one summary keeps.
const outcomeKeyLines = 3

// outcomeErrorMarkers flag the output lines that explain a failure. They are
// preferred over the plain tail of the output.
var outcomeErrorMarkers = []string{
	"error", "fail", "panic", "fatal", "exception", "denied", "not found",
	"cannot", "refused", "timeout", "timed out", "invalid", "错误", "失败", "异常",
}

// verificationCommands are command fragments that mark a check whose result is
// evidence even when it passes: the turn's last one is kept.
var verificationCommands = []string{
	"go test", "go vet", "go build", "gofmt -l", "make test", "make check", "make lint",
	"npm test", "npm run test", "npm run build", "npm run lint", "pnpm test", "pnpm run test",
	"yarn test", "vitest", "jest", "pytest", "cargo test", "cargo check", "cargo build",
	"tsc", "eslint", "ruff", "mypy",
}

// isVerificationCommand reports whether a tool signature runs a check.
func isVerificationCommand(signature string) bool {
	lower := strings.ToLower(signature)
	for _, fragment := range verificationCommands {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

// recordOutcome folds one tool result into the pending turn. A failed result
// is always kept; a successful one only when its call is a verification
// command, and then it replaces the turn's previous verification summary.
// label is the tool signature of the call when the parser knows it.
func recordOutcome(pending *Turn, label, output string, failed bool) {
	if pending == nil {
		return
	}
	summary := outcomeSummary(label, output, failed)
	if summary == "" {
		return
	}
	if failed {
		pending.ToolOutcomes = append(pending.ToolOutcomes, summary)
		return
	}
	if isVerificationCommand(label) {
		pending.verifyOutcome = summary
	}
}

// lastToolSignature returns the most recent tool signature of a pending turn,
// for hosts whose tool result does not name its call.
func lastToolSignature(pending *Turn) string {
	if pending == nil || len(pending.Tools) == 0 {
		return ""
	}
	return pending.Tools[len(pending.Tools)-1]
}

// outcomeSummary renders `<label> -> failed|ok: <key lines>`. Key lines are
// the last lines carrying an error marker, or the output tail when none does.
func outcomeSummary(label, output string, failed bool) string {
	label = singleLine(label)
	if label == "" {
		label = "tool"
	}
	status := "ok"
	if failed {
		status = "failed"
	}
	lines := keyOutputLines(output)
	if len(lines) == 0 {
		if !failed {
			return ""
		}
		return label + " -> " + status
	}
	return label + " -> " + status + ": " + strings.Join(lines, " | ")
}

// keyOutputLines picks at most outcomeKeyLines non-empty lines from output.
func keyOutputLines(output string) []string {
	var all []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line != "" {
			all = append(all, line)
		}
	}
	if len(all) == 0 {
		return nil
	}
	var marked []string
	for _, line := range all {
		lower := strings.ToLower(line)
		for _, marker := range outcomeErrorMarkers {
			if strings.Contains(lower, marker) {
				marked = append(marked, line)
				break
			}
		}
	}
	pick := marked
	if len(pick) == 0 {
		pick = all
	}
	if len(pick) > outcomeKeyLines {
		pick = pick[len(pick)-outcomeKeyLines:]
	}
	return pick
}

// boundOutcomes applies redaction, the suspicious-instruction screen,
// MaxToolOutcomeBytes and MaxToolOutcomesPerTurn to the raw summaries of one
// turn. The most recent summaries are kept. A summary that carries an injected
// instruction is dropped, not rewritten.
func boundOutcomes(matcher func(string) bool, raw []string) []string {
	var kept []string
	for _, summary := range raw {
		if Suspicious(matcher, summary) {
			continue
		}
		bounded := truncateText(Redact(Clean(summary)), MaxToolOutcomeBytes)
		if bounded == "" || Suspicious(matcher, bounded) {
			continue
		}
		kept = append(kept, bounded)
	}
	if len(kept) > MaxToolOutcomesPerTurn {
		kept = kept[len(kept)-MaxToolOutcomesPerTurn:]
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// toolResultText joins the text of a tool result whose content is a string or
// an array of text blocks.
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var joined string
	for _, block := range blocks {
		if kind := stringField(block, "type"); kind == "text" || kind == "" {
			joined = appendText(joined, stringField(block, "text"))
		}
	}
	return joined
}

// codexCallOutput reads a Codex function_call_output payload. The output is
// either a JSON string with `output` and `metadata.exit_code`, or plain text
// that may start with an `Exit code: N` line. It returns the output text and
// whether the call failed; a call without an exit code is not a failure.
func codexCallOutput(payload map[string]json.RawMessage) (string, bool) {
	raw := stringField(payload, "output")
	if raw == "" {
		if nested, ok := objectField(payload, "output"); ok {
			return codexStructuredOutput(nested)
		}
		return "", false
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal([]byte(trimmed), &nested); err == nil {
			return codexStructuredOutput(nested)
		}
	}
	return raw, exitCodeFailed(raw)
}

// codexStructuredOutput reads the `{output, metadata: {exit_code}}` shape.
func codexStructuredOutput(value map[string]json.RawMessage) (string, bool) {
	text := stringField(value, "output")
	failed := false
	if metadata, ok := objectField(value, "metadata"); ok {
		if raw, ok := metadata["exit_code"]; ok {
			var code int
			if err := json.Unmarshal(raw, &code); err == nil {
				failed = code != 0
			}
		}
	}
	return text, failed
}

// exitCodeFailed reads an `Exit code: N` or `Process exited with code N` line
// from plain tool output.
func exitCodeFailed(text string) bool {
	for _, line := range strings.SplitN(text, "\n", 8) {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"Exit code:", "Process exited with code"} {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			if fields := strings.Fields(value); len(fields) > 0 {
				if code, err := strconv.Atoi(strings.TrimRight(fields[0], ".,;")); err == nil {
					return code != 0
				}
			}
		}
	}
	return false
}
