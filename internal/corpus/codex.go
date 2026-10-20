package corpus

import (
	"encoding/json"
	"io"
	"strings"
)

// parseCodex reads $HOME/.codex/sessions/**/*.jsonl. The first line is a
// session_meta record carrying the session id and cwd; the dialogue arrives as
// response_item records, and the final assistant text may also be repeated in
// an event_msg task_complete record.
func parseCodex(c Collector, r io.Reader) (parseInput, bool) {
	out := parseInput{}
	scanner := newLineScanner(r)
	var pending *Turn
	first := true
	for scanner.scan() {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		out.newest = newestTime(out.newest, timeField(value, "timestamp"))
		kind := stringField(value, "type")
		if first && kind == "session_meta" {
			first = false
			if payload, ok := objectField(value, "payload"); ok {
				out.sessionID = stringField(payload, "id")
				if out.sessionID == "" {
					out.sessionID = stringField(payload, "session_id")
				}
				out.cwd = stringField(payload, "cwd")
			}
			continue
		}
		first = false

		switch kind {
		case "turn_context":
			if payload, ok := objectField(value, "payload"); ok && out.cwd == "" {
				out.cwd = stringField(payload, "cwd")
			}
		case "response_item":
			payload, ok := objectField(value, "payload")
			if !ok {
				continue
			}
			switch stringField(payload, "type") {
			case "message":
				switch stringField(payload, "role") {
				case "user":
					text := codexContentText(payload)
					if text == "" || looksLikeInjectedContext(text) {
						continue
					}
					c.addUser(&pending, &out, text)
				case "assistant":
					// A turn starts with user text. Assistant output before any
					// accepted user message belongs to the harness session
					// setup and cannot be attributed to a person.
					if pending == nil {
						continue
					}
					if text := codexContentText(payload); text != "" {
						pending.Assistant = appendText(pending.Assistant, text)
					}
				}
			case "function_call", "custom_tool_call", "local_shell_call":
				// A tool call before any user message belongs to the harness
				// setup, not to a turn the collector can attribute.
				if pending == nil {
					continue
				}
				pending.Tools = append(pending.Tools, codexToolSignature(payload))
			}
		case "event_msg":
			payload, ok := objectField(value, "payload")
			if !ok {
				continue
			}
			if stringField(payload, "type") != "task_complete" {
				continue
			}
			text := stringField(payload, "last_agent_message")
			if text == "" {
				continue
			}
			if pending == nil {
				pending = &Turn{}
			}
			if pending.Assistant == "" {
				pending.Assistant = text
			}
		}
	}
	if pending != nil {
		if turn, ok := c.finalizeTurn(suspiciousMatcher, *pending); ok {
			out.turns = append(out.turns, turn)
		}
	}
	out.truncated = scanner.truncated
	return out, len(out.turns) > 0
}

// codexContentText joins the text blocks of one response_item message. Images
// and other block types are dropped: they are not text memory material.
func codexContentText(payload map[string]json.RawMessage) string {
	blocks, ok := arrayField(payload, "content")
	if !ok {
		return strings.TrimSpace(stringField(payload, "text"))
	}
	var text string
	for _, raw := range blocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		switch stringField(block, "type") {
		case "input_text", "output_text", "text":
			text = appendText(text, stringField(block, "text"))
		}
	}
	return text
}

// codexToolSignature renders one tool call. Codex records a shell command
// either as plain `command` arguments or as a JavaScript snippet that calls
// tools.exec_command; the first command inside that snippet is used so the
// signature names what ran rather than the wrapper.
func codexToolSignature(payload map[string]json.RawMessage) string {
	name := stringField(payload, "name")
	if name == "" {
		name = stringField(payload, "type")
	}
	target := ""
	if arguments, ok := objectField(payload, "arguments"); ok {
		target = firstCommand(arguments)
	}
	if target == "" {
		if input, ok := objectField(payload, "action"); ok {
			target = firstCommand(input)
		}
	}
	if target == "" {
		target = firstCommandString(stringField(payload, "input"))
	}
	if target == "" {
		target = firstCommand(payload)
	}
	if target == "" {
		return name
	}
	return name + ": " + target
}

// firstCommand reads a command-like field from one object.
func firstCommand(value map[string]json.RawMessage) string {
	for _, key := range []string{"command", "cmd", "file_path", "path", "pattern", "url"} {
		if raw, ok := value[key]; ok {
			if text := firstCommandString(string(raw)); text != "" {
				return text
			}
		}
	}
	return ""
}

// firstCommandString extracts one command or path from a JSON string, a JSON
// array of strings or a JavaScript snippet.
func firstCommandString(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return ""
	}
	if raw[0] == '[' {
		var values []string
		if err := json.Unmarshal([]byte(raw), &values); err == nil {
			for _, value := range values {
				if value = singleLine(value); value != "" {
					return value
				}
			}
			return ""
		}
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			return singleLine(value)
		}
		return ""
	}
	return firstCommandInCode(raw)
}

// firstCommandInCode pulls the first exec_command payload out of a Codex
// JavaScript tool input. It is a bounded heuristic over the documented wrapper
// shape, not a JavaScript parser; when the shape is not recognised the caller
// keeps the tool name alone.
func firstCommandInCode(code string) string {
	const marker = "exec_command("
	index := strings.Index(code, marker)
	if index < 0 {
		return ""
	}
	rest := code[index+len(marker):]
	for _, key := range []string{"cmd:", "command:"} {
		at := strings.Index(rest, key)
		if at < 0 {
			continue
		}
		return quotedValue(rest[at+len(key):])
	}
	return ""
}

// quotedValue reads the first JSON or JavaScript quoted string at the start of
// text after an optional colon and whitespace.
func quotedValue(text string) string {
	text = strings.TrimLeft(text, " \t\r\n")
	if text == "" || (text[0] != '"' && text[0] != '\'' && text[0] != '`') {
		return ""
	}
	quote := text[0]
	end := strings.IndexByte(text[1:], quote)
	if end < 0 {
		return ""
	}
	value := text[1 : 1+end]
	value = strings.ReplaceAll(value, `\n`, " ")
	value = strings.ReplaceAll(value, `\"`, `"`)
	return singleLine(value)
}

// leadingTagName returns the tag name of a text that starts with an XML-ish
// tag, so a harness wrapper can be recognised by its name.
func leadingTagName(text string) (string, bool) {
	if !strings.HasPrefix(text, "<") {
		return "", false
	}
	rest := text[1:]
	end := strings.IndexAny(rest, "> \t\r\n")
	if end <= 0 {
		return "", false
	}
	name := rest[:end]
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "!") || strings.HasPrefix(name, "?") {
		return "", false
	}
	return name, true
}

// looksLikeInjectedContext filters the harness instructions and UI events that
// hosts record as user-role messages. They are not user speech and would
// dominate a digest.
func looksLikeInjectedContext(text string) bool {
	trimmed := strings.TrimSpace(text)
	// A message that opens with an underscored tag is a host event, such as
	// `<external_codex_apps_open_page>{"page_id":null}</external_codex_apps_open_page>`.
	// Harness wrappers use snake_case tag names; person-typed markup rarely does.
	if name, ok := leadingTagName(trimmed); ok && strings.Contains(name, "_") {
		return true
	}
	for _, prefix := range []string{
		"# AGENTS.md instructions",
		"# AGENT.md instructions",
		"<INSTRUCTIONS>",
		"<user_instructions>",
		"<environment_context>",
		"<system-reminder>",
		// Harness runtime banners that hosts record as user-role messages.
		"Current runtime context.",
		"Time sampled while preparing",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
