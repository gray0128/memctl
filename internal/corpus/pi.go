package corpus

import (
	"encoding/json"
	"io"
	"strings"
)

// parsePi reads $HOME/.pi/agent/sessions/**/*.jsonl. The first line is a
// `session` record with the id and cwd; dialogue arrives as `message` records
// whose `message.role` is user, assistant or toolResult. Assistant thinking
// blocks and toolResult content are dropped.
func parsePi(c Collector, r io.Reader) (parseInput, bool) {
	out := parseInput{}
	scanner := newLineScanner(r)
	var pending *Turn
	first := true
	for scanner.scan() {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		kind := stringField(value, "type")
		out.newest = newestTime(out.newest, timeField(value, "timestamp"))
		if first && kind == "session" {
			first = false
			out.sessionID = stringField(value, "id")
			out.cwd = stringField(value, "cwd")
			continue
		}
		first = false
		if kind != "message" {
			continue
		}
		message, ok := objectField(value, "message")
		if !ok {
			continue
		}
		switch stringField(message, "role") {
		case "user":
			text := piContentText(message)
			if text == "" || looksLikeInjectedContext(text) {
				continue
			}
			c.addUser(&pending, &out, text)
		case "assistant":
			if pending == nil {
				continue
			}
			blocks, ok := arrayField(message, "content")
			if !ok {
				pending.Assistant = appendText(pending.Assistant, stringField(message, "content"))
				continue
			}
			for _, raw := range blocks {
				var block map[string]json.RawMessage
				if err := json.Unmarshal(raw, &block); err != nil {
					continue
				}
				switch stringField(block, "type") {
				case "text":
					pending.Assistant = appendText(pending.Assistant, stringField(block, "text"))
				case "toolCall", "tool_call":
					pending.Tools = append(pending.Tools, piToolSignature(stringField(block, "name"), block))
				}
				// A thinking block is the model's internal reasoning and is
				// never kept.
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

// piContentText joins the text blocks of a user message.
func piContentText(message map[string]json.RawMessage) string {
	blocks, ok := arrayField(message, "content")
	if !ok {
		return strings.TrimSpace(stringField(message, "content"))
	}
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

// piToolSignature renders one pi tool call. pi uses `bash` and `edit`; the
// signature keeps the command or the file path, not the content.
func piToolSignature(name string, block map[string]json.RawMessage) string {
	arguments, ok := objectField(block, "arguments")
	if !ok {
		arguments = block
	}
	target := ""
	for _, key := range []string{"command", "cmd", "file_path", "path"} {
		if value := singleLine(stringField(arguments, key)); value != "" {
			target = value
			break
		}
	}
	if target == "" {
		return name
	}
	return name + ": " + target
}
