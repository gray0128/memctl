package corpus

import (
	"regexp"
	"strings"
)

// This file implements the deterministic cleaning of
// docs/12-session-corpus-dreaming.md section 2.2.4: system and injection blocks
// are stripped, credentials are replaced by [REDACTED], and a turn carrying a
// suspicious instruction is dropped entirely. The redaction is pattern
// matching: it cannot enumerate every credential shape, which is why callers
// also report RedactionNotice.

// RedactionNotice is returned with prepare so the current agent and the user
// know the digests may still contain sensitive text that no pattern covered.
const RedactionNotice = "摘要经过确定性正则脱敏，仍可能存在未覆盖的敏感文本"

// redacted is the replacement marker of the credential rules.
const redacted = "[REDACTED]"

// blockPatterns strip whole regions before any text is kept. They are applied
// repeatedly because the injected text itself can contain nested tags.
var blockPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<system-reminder>.*?</system-reminder>`),
	regexp.MustCompile(`(?is)<MEMCTL_INSTRUCTIONS>.*?</MEMCTL_INSTRUCTIONS>`),
	regexp.MustCompile(`(?is)<INSTRUCTIONS>.*?</INSTRUCTIONS>`),
	regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
}

// bannerPattern strips the "[agent-memory] ..." injection banner, which spans to
// the end of its line.
var bannerPattern = regexp.MustCompile(`(?m)^\[agent-memory\][^\n]*$`)

// fencePattern matches a fenced code block. A block longer than
// maxFencedCodeBytes is replaced by a placeholder: tool output, diffs and whole
// files arrive that way, and keeping them would blow the digest budget.
var fencePattern = regexp.MustCompile("(?s)```[^\n]*\n(.*?)```")

// maxFencedCodeBytes bounds one retained fenced block.
const maxFencedCodeBytes = 512

// credentialPatterns are the documented redaction rules, in order. Rules with a
// capture group keep that group so a key/value assignment keeps its key and
// loses only the value.
type credentialRule struct {
	pattern     *regexp.Regexp
	replacement string
}

var credentialRules = []credentialRule{
	// Common token prefixes.
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`), redacted},
	{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), redacted},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), redacted},
	{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), redacted},
	{regexp.MustCompile(`AKIA[0-9A-Z]{16}`), redacted},
	{regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`), redacted},
	// JWT and long bearer strings.
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), redacted},
	{regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]{16,}`), "Bearer " + redacted},
	// Key/value assignments: keep the key, redact the value.
	{regexp.MustCompile(`(?i)(api[_-]?key|secret|password|passwd|access[_-]?token|refresh[_-]?token|private[_-]?key|authorization)(\s*[:=]\s*)["']?[^\s"',;]+`), "${1}${2}" + redacted},
	// Credentials embedded in a URL.
	{regexp.MustCompile(`(https?://)[^/\s:@]+:[^/\s:@]+@`), "${1}" + redacted + "@"},
}

// Clean removes the documented system and injection blocks and the redundant
// fenced tool output from one text field.
func Clean(text string) string {
	if text == "" {
		return ""
	}
	for _, pattern := range blockPatterns {
		text = pattern.ReplaceAllString(text, " ")
	}
	text = bannerPattern.ReplaceAllString(text, "")
	text = fencePattern.ReplaceAllStringFunc(text, func(block string) string {
		if len(block) <= maxFencedCodeBytes {
			return block
		}
		return "[code block omitted]"
	})
	return strings.TrimSpace(text)
}

// Redact replaces every documented credential pattern with [REDACTED]. It is
// applied to the fact text the current agent submits as well as to collected
// session text.
func Redact(text string) string {
	if text == "" {
		return ""
	}
	for _, rule := range credentialRules {
		text = rule.pattern.ReplaceAllString(text, rule.replacement)
	}
	return text
}

// Suspicious reports whether one turn carries a prompt-injection pattern. The
// turn is then dropped instead of being distilled. The pattern list is the
// doctor SUSPICIOUS_INSTRUCTION list, reused through the caller so the two
// surfaces cannot drift apart.
func Suspicious(matcher func(string) bool, texts ...string) bool {
	if matcher == nil {
		return false
	}
	for _, text := range texts {
		if text != "" && matcher(text) {
			return true
		}
	}
	return false
}
