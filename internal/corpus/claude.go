package corpus

import (
	"encoding/json"
	"io"
	"strings"
)

// This file implements the four host formats verified against real local
// samples, as recorded in docs/12-session-corpus-dreaming.md section 2.2.1.
// Each parser extracts user text, the assistant's final text and one tool
// signature per call (name, first command line or root-relative path, exit
// status). Tool output, file content and reasoning blocks are never retained.

// parseClaudeCode reads $HOME/.claude/projects/<escaped cwd>/<sid>.jsonl. Each
// line is one event with a `type`; `user` and `assistant` events carry an
// Anthropic message whose content is either a string or a block array.
func parseClaudeCode(c Collector, r io.Reader) (parseInput, bool) {
	out := parseInput{}
	scanner := newLineScanner(r)
	var pending *Turn
	for scanner.scan() {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		out.newest = newestTime(out.newest, timeField(value, "timestamp"))
		if cwd := stringField(value, "cwd"); cwd != "" && out.cwd == "" {
			out.cwd = cwd
		}
		if sid := stringField(value, "sessionId"); sid != "" && out.sessionID == "" {
			out.sessionID = sid
		}
		// A sidechain is a sub-agent transcript kept in the same file; its
		// turns are not part of the parent session's dialogue.
		if isTrue(value, "isSidechain") {
			continue
		}
		switch stringField(value, "type") {
		case "user":
			message, ok := objectField(value, "message")
			if !ok {
				continue
			}
			if isToolResultClaude(message) {
				continue
			}
			text := claudeText(message)
			if text == "" {
				continue
			}
			c.addUser(&pending, &out, text)
		case "assistant":
			message, ok := objectField(value, "message")
			if !ok {
				continue
			}
			if pending == nil {
				continue
			}
			if blocks, ok := arrayField(message, "content"); ok {
				for _, raw := range blocks {
					var block map[string]json.RawMessage
					if err := json.Unmarshal(raw, &block); err != nil {
						continue
					}
					switch stringField(block, "type") {
					case "text":
						pending.Assistant = appendText(pending.Assistant, stringField(block, "text"))
					case "tool_use":
						pending.Tools = append(pending.Tools, claudeToolSignature(stringField(block, "name"), block))
					}
				}
				continue
			}
			pending.Assistant = appendText(pending.Assistant, claudeText(message))
		case "system":
			if stringField(value, "subtype") == "compact_boundary" {
				out.truncated = true
			}
		}
		if strings.Contains(stringField(value, "summary"), "compacted") {
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

// claudeText extracts the text of one Anthropic message whose content is a
// string or a block array.
func claudeText(message map[string]json.RawMessage) string {
	if blocks, ok := arrayField(message, "content"); ok {
		var text string
		for _, raw := range blocks {
			var block map[string]json.RawMessage
			if err := json.Unmarshal(raw, &block); err != nil {
				continue
			}
			if stringField(block, "type") == "text" {
				text = appendText(text, stringField(block, "text"))
			}
		}
		return text
	}
	return stringField(message, "content")
}

// isToolResultClaude reports whether a user event is a tool result envelope.
// Those events are tool output, which the digest never keeps.
func isToolResultClaude(message map[string]json.RawMessage) bool {
	blocks, ok := arrayField(message, "content")
	if !ok {
		return false
	}
	for _, raw := range blocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		if stringField(block, "type") == "tool_result" {
			return true
		}
	}
	return false
}

// claudeToolSignature renders one tool_use block as
// `name: target (run|exit status)`.
func claudeToolSignature(name string, block map[string]json.RawMessage) string {
	target := ""
	if input, ok := objectField(block, "input"); ok {
		for _, key := range []string{"command", "file_path", "path", "pattern", "url"} {
			if value := singleLine(stringField(input, key)); value != "" {
				target = value
				break
			}
		}
	}
	if target == "" {
		return name
	}
	return name + ": " + target
}

// isTrue reports whether an optional boolean field is true.
func isTrue(value map[string]json.RawMessage, key string) bool {
	raw, ok := value[key]
	if !ok {
		return false
	}
	var flag bool
	if err := json.Unmarshal(raw, &flag); err != nil {
		return false
	}
	return flag
}

// appendText joins two text fragments with a blank line so a multi-block
// message reads as paragraphs rather than as one run-on line.
func appendText(current, extra string) string {
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return current
	}
	if current == "" {
		return extra
	}
	return current + "\n\n" + extra
}
