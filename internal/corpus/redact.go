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
	// The opening tag may carry attributes, as in
	// `<system-reminder data-role="user-context">`.
	regexp.MustCompile(`(?is)<system-reminder(?:\s[^>]*)?>.*?</system-reminder>`),
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
	// Common cloud, platform and model token prefixes.
	{regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`), redacted},
	{regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}`), redacted},
	{regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`), redacted},
	{regexp.MustCompile(`glpat-[A-Za-z0-9_-]{16,}`), redacted},
	{regexp.MustCompile(`hf_[A-Za-z0-9]{16,}`), redacted},
	{regexp.MustCompile(`npm_[A-Za-z0-9]{16,}`), redacted},
	{regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), redacted},
	{regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`), redacted},
	{regexp.MustCompile(`AKID[0-9A-Za-z]{16,36}`), redacted},
	{regexp.MustCompile(`LTAI[0-9A-Za-z]{12,30}`), redacted},
	{regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`), redacted},
	// JWT and Authorization / Bearer / Basic headers.
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), redacted},
	{regexp.MustCompile(`(?i)(["']?\bauthorization["']?\s*[:=]\s*["']?(?:basic|bearer|token|digest)\s+)[A-Za-z0-9._~+/=-]{4,}["']?`), "${1}" + redacted},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}`), "Bearer " + redacted},
	{regexp.MustCompile(`(?i)\bbasic\s+[A-Za-z0-9+/=]{12,}`), "Basic " + redacted},
	{regexp.MustCompile(`(?i)(["']?\bauthorization["']?\s*[:=]\s*)(?:"[^"\r\n]{6,}"|'[^'\r\n]{6,}'|[A-Za-z0-9._~+/=-]{8,})`), "${1}" + redacted},
	// Chinese credential labels.
	{regexp.MustCompile(`(密码|口令|密钥|令牌)(\s*[:=：]\s*)(?:"[^"\r\n]{4,}"|'[^'\r\n]{4,}'|[^\s"',;，；。})\]]{4,})`), "${1}${2}" + redacted},
	// Credentials embedded in a URL (`scheme://user:pass@host`).
	{regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/\s:@]+:[^/\s:@]+@`), "${1}" + redacted + "@"},
}

// genericKVPattern matches JSON or plain key/value assignments whose key name
// carries a credential-like stem (`token`, `secret`, `key`, `password`,
// `passwd`, `pwd`). Benign words and short/placeholder values are filtered by
// redactGenericKV.
var genericKVPattern = regexp.MustCompile(`(?i)(["']?\b[a-z0-9_.-]*(?:token|secret|key|passw(?:or)?d|pwd)[a-z0-9_.-]*\b["']?\s*[:=]\s*)("[^"\r\n]*"|'[^'\r\n]*'|[^\s"',;&}\]]+)`)

var benignKeyNames = map[string]bool{
	"monkey": true, "donkey": true, "turkey": true, "hotkey": true, "hot_key": true,
	"turnkey": true, "keyboard": true, "keypad": true, "keychain": true, "keynote": true,
	"keystroke": true, "keyword": true, "keywords": true, "keymap": true, "keyframe": true,
	"keybinding": true, "keybindings": true,
	"cwdkey": true, "cwd_key": true, "primary_key": true, "primarykey": true,
	"foreign_key": true, "foreignkey": true, "sort_key": true, "sortkey": true,
	"partition_key": true, "partitionkey": true, "cache_key": true, "cachekey": true,
	"public_key": true, "publickey": true, "pubkey": true, "ssh_public_key": true,
	"host_key": true, "label_key": true, "tag_key": true, "meta_key": true,
	"config_key": true, "setting_key": true, "index_key": true, "group_key": true,
	"order_key": true, "filter_key": true, "search_key": true, "lookup_key": true,
	"state_key": true, "storage_key": true, "query_key": true, "header_key": true,
	"param_key": true, "field_key": true, "property_key": true, "item_key": true,
	"node_key": true, "edge_key": true, "page_key": true, "tab_key": true,
	"shortcut_key": true, "routing_key": true, "routingkey": true, "file_key": true, "filekey": true,
	"max_tokens": true, "maxtokens": true, "max_output_tokens": true, "maxoutputtokens": true,
	"max_completion_tokens": true, "input_tokens": true, "inputtokens": true,
	"output_tokens": true, "outputtokens": true, "total_tokens": true, "totaltokens": true,
	"prompt_tokens": true, "completion_tokens": true, "cache_read_tokens": true,
	"cache_creation_tokens": true, "cached_tokens": true, "reasoning_tokens": true,
	"token_count": true, "tokencount": true, "token_limit": true, "tokenlimit": true,
}

var benignLiteralValues = map[string]bool{
	"true": true, "false": true, "null": true, "nil": true, "none": true,
	"undefined": true, "empty": true, "redacted": true, "[redacted]": true,
}

func redactGenericKV(text string) string {
	return genericKVPattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := genericKVPattern.FindStringSubmatch(match)
		if len(sub) != 3 {
			return match
		}
		prefix, rawVal := sub[1], sub[2]
		keyName := strings.ToLower(strings.Trim(strings.TrimRight(prefix, " \t:="), "\"'"))
		if benignKeyNames[keyName] || strings.HasSuffix(keyName, "_tokens") || strings.HasSuffix(keyName, "tokens") {
			return match
		}
		val := strings.TrimSpace(strings.Trim(rawVal, "\"'"))
		if val == "" || strings.Contains(val, redacted) || benignLiteralValues[strings.ToLower(val)] {
			return match
		}
		minLen := 6
		if strings.Contains(keyName, "passw") || strings.Contains(keyName, "pwd") {
			minLen = 4
		}
		if len(val) < minLen {
			return match
		}
		return prefix + redacted
	})
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
	return redactGenericKV(text)
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
		if text == "" {
			continue
		}
		if matcher(text) {
			return true
		}
		if normalized := normalizeSuspiciousInput(text); normalized != "" && normalized != text && matcher(normalized) {
			return true
		}
	}
	return false
}

// normalizeSuspiciousInput folds fullwidth ASCII runes, strips zero-width
// characters and collapses consecutive whitespace using the standard library.
func normalizeSuspiciousInput(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\u200B' || r == '\u200C' || r == '\u200D' || r == '\u200E' || r == '\u200F' ||
			r == '\u2060' || r == '\uFEFF' || r == '\u00AD':
			continue
		case r == '\u3000':
			b.WriteByte(' ')
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
