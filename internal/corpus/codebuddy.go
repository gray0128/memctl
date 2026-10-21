package corpus

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

// This file implements the CodeBuddy Code session format. WorkBuddy runs the
// same agent core, so both hosts share this parser and differ only in their
// store root and agent name. The format was verified against local CodeBuddy
// and WorkBuddy logs, as recorded in docs/12-session-corpus-dreaming.md section
// 2.2.1.
//
// Every line carries `sessionId`, `cwd` and a millisecond epoch `timestamp`.
// A new session may open with a `session-meta` line that has no `cwd`; it is
// ignored for the dialogue and only contributes the session id.

// parseCodeBuddy reads one CodeBuddy or WorkBuddy transcript. User text,
// assistant text and one tool signature per call are kept; reasoning events,
// tool results and host echoes are dropped.
func parseCodeBuddy(c Collector, r io.Reader) (parseInput, bool) {
	out := parseInput{}
	scanner := newLineScanner(r)
	var pending *Turn
	for scanner.scan() {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		out.newest = newestTime(out.newest, millisTimeField(value, "timestamp"))
		if cwd := stringField(value, "cwd"); cwd != "" && out.cwd == "" {
			out.cwd = cwd
		}
		if sid := stringField(value, "sessionId"); sid != "" && out.sessionID == "" {
			out.sessionID = sid
		}
		switch stringField(value, "type") {
		case "message":
			switch stringField(value, "role") {
			case "user":
				if codeBuddyEcho(value) {
					continue
				}
				text := codeBuddyUserText(codeBuddyText(value, "input_text"))
				if text == "" || looksLikeInjectedContext(text) {
					continue
				}
				c.addUser(&pending, &out, text)
			case "assistant":
				if pending == nil {
					continue
				}
				pending.Assistant = appendText(pending.Assistant, codeBuddyText(value, "output_text"))
			}
		case "function_call":
			if pending == nil {
				continue
			}
			if signature := codeBuddyToolSignature(value); signature != "" {
				pending.Tools = append(pending.Tools, signature)
			}
		case "summary":
			// A summary line means the host compacted the context, so the
			// transcript no longer holds the session's earlier turns.
			out.truncated = true
		}
	}
	if pending != nil {
		if turn, ok := c.finalizeTurn(suspiciousMatcher, *pending); ok {
			out.turns = append(out.turns, turn)
		}
	}
	out.truncated = out.truncated || scanner.truncated
	return out, len(out.turns) > 0
}

// codeBuddyText collects the text blocks of one message. CodeBuddy names the
// user block `input_text` and the assistant block `output_text`; a bare `text`
// block is accepted for both.
func codeBuddyText(value map[string]json.RawMessage, want string) string {
	blocks, ok := arrayField(value, "content")
	if !ok {
		return stringField(value, "content")
	}
	var text string
	for _, raw := range blocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		switch stringField(block, "type") {
		case want, "text":
			text = appendText(text, stringField(block, "text"))
		}
	}
	return text
}

// codeBuddyUserText strips the injected blocks a user message may carry and
// unwraps the `<user_query>` envelope. WorkBuddy puts its per-turn context and
// the real request in one event, so testing the raw text for host prefixes
// would drop the session's first prompt.
func codeBuddyUserText(text string) string {
	text = strings.TrimSpace(Clean(text))
	if strings.HasPrefix(text, "<user_query>") && strings.HasSuffix(text, "</user_query>") {
		text = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "<user_query>"), "</user_query>"))
	}
	return text
}

// codeBuddyEcho reports whether a user-role message is a host echo rather than
// user speech. CodeBuddy marks command echoes with `providerData.skipRun`;
// hyphenated host tags are covered by looksLikeInjectedContext.
func codeBuddyEcho(value map[string]json.RawMessage) bool {
	provider, ok := objectField(value, "providerData")
	if !ok {
		return false
	}
	return isTrue(provider, "skipRun")
}

// codeBuddyToolSignature renders one function_call as `name: target`. The
// target is the first documented argument present; an argument string that is
// not JSON yields the bare tool name.
func codeBuddyToolSignature(value map[string]json.RawMessage) string {
	name := stringField(value, "name")
	if name == "" {
		return ""
	}
	arguments, ok := codeBuddyArguments(value)
	if !ok {
		return name
	}
	for _, key := range []string{"command", "file_path", "path", "pattern", "url"} {
		if target := singleLine(stringField(arguments, key)); target != "" {
			return name + ": " + target
		}
	}
	return name
}

// codeBuddyArguments decodes the arguments field, which CodeBuddy records as a
// JSON-encoded string and some versions write as an object.
func codeBuddyArguments(value map[string]json.RawMessage) (map[string]json.RawMessage, bool) {
	if arguments, ok := objectField(value, "arguments"); ok {
		return arguments, true
	}
	encoded := stringField(value, "arguments")
	if encoded == "" {
		return nil, false
	}
	var arguments map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &arguments); err != nil {
		return nil, false
	}
	return arguments, true
}

// millisTimeField parses one epoch-millisecond timestamp field. CodeBuddy
// writes numbers rather than RFC 3339 strings, so timeField cannot read it.
func millisTimeField(value map[string]json.RawMessage, key string) time.Time {
	raw, ok := value[key]
	if !ok {
		return time.Time{}
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return time.Time{}
	}
	millis, err := number.Int64()
	if err != nil || millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis)
}

// codeBuddyDir returns the store directory name CodeBuddy and WorkBuddy use for
// one working directory. Only path separators become dashes: unlike Claude
// Code, underscores, spaces and non-ASCII characters are preserved. The log's
// own `cwd` remains authoritative, so the escaping is only a lookup shortcut.
func codeBuddyDir(cwd string) string {
	clean := strings.ReplaceAll(cwd, "\\", "/")
	clean = strings.Trim(filepath.ToSlash(clean), "/")
	return strings.ReplaceAll(clean, "/", "-")
}
