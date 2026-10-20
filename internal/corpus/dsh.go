package corpus

import (
	"encoding/json"
	"io"
	"time"

	"github.com/klauspost/compress/zstd"
)

// parseDSH reads a DSH session record:
// $HOME/.dsh/sessions/<escaped cwd>/session-<sid>/session.v4.jsonl.zstd. The
// stream is zstd compressed, so it is decoded here rather than by the plugin:
// the version choice, the directory layout and the metadata line all belong to
// the Go side, and a decompressed plaintext copy would never be written.
//
// The decoded size is what MaxBackfillFileBytes bounds, because a zstd stream
// cannot be read from its tail the way plain JSONL can: decoding stops at the
// limit and the digest is marked truncated.
func parseDSH(c Collector, r io.Reader) (parseInput, bool) {
	decoder, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return parseInput{}, false
	}
	defer decoder.Close()

	out := parseInput{}
	scanner := newLineScanner(decoder)
	var pending *Turn
	first := true
	for scanner.scan() {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		kind := stringField(value, "type")
		out.newest = newestTime(out.newest, dshTime(value))
		if first && kind == "session" {
			first = false
			out.sessionID = stringField(value, "id")
			out.cwd = stringField(value, "cwd")
			continue
		}
		first = false
		switch kind {
		case "user/message":
			// DSH labels every recorded message with a source kind. Only
			// `user` is text the person typed; the others are the harness's
			// instruction, runtime, skill, time and approval notices.
			if !dshFromUser(value) {
				continue
			}
			text := dshContentText(value)
			if text == "" || looksLikeInjectedContext(text) {
				continue
			}
			c.addUser(&pending, &out, text)
		case "assistant/message":
			data, ok := objectField(value, "data")
			if !ok {
				continue
			}
			message, ok := objectField(data, "message")
			if !ok {
				continue
			}
			if pending == nil {
				continue
			}
			if text := dshMessageText(message); text != "" {
				pending.Assistant = appendText(pending.Assistant, text)
			}
		case "tool/call":
			data, ok := objectField(value, "data")
			if !ok {
				continue
			}
			if pending == nil {
				continue
			}
			pending.Tools = append(pending.Tools, dshToolSignature(data))
		case "tool/result":
			// Tool output is never kept. An error result is still worth a
			// signature so a debugging turn keeps its shape.
			data, ok := objectField(value, "data")
			if !ok {
				continue
			}
			message, ok := objectField(data, "message")
			if !ok || !isTrue(message, "isError") {
				continue
			}
			if pending == nil {
				continue
			}
			pending.Tools = append(pending.Tools, "tool result: error")
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

// dshFromUser reports whether a user/message record carries the source kind of
// text the person typed.
func dshFromUser(value map[string]json.RawMessage) bool {
	data, ok := objectField(value, "data")
	if !ok {
		return false
	}
	source, ok := objectField(data, "source")
	if !ok {
		return false
	}
	return stringField(source, "kind") == "user"
}

// dshTime reads the millisecond timestamp DSH stores under `time`.
func dshTime(value map[string]json.RawMessage) time.Time {
	raw, ok := value["time"]
	if !ok {
		return time.Time{}
	}
	var millis int64
	if err := json.Unmarshal(raw, &millis); err != nil || millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis)
}

// dshContentText joins the text blocks of one user/message record.
func dshContentText(value map[string]json.RawMessage) string {
	data, ok := objectField(value, "data")
	if !ok {
		return ""
	}
	return dshBlocksText(data)
}

// dshMessageText joins the `type: "text"` blocks of an assistant message and
// drops `type: "reasoning"` blocks.
func dshMessageText(message map[string]json.RawMessage) string {
	return dshBlocksText(message)
}

// dshBlocksText joins the text blocks of one DSH content array.
func dshBlocksText(value map[string]json.RawMessage) string {
	blocks, ok := arrayField(value, "content")
	if !ok {
		return ""
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

// dshToolSignature renders one tool/call record. `arguments` is a JSON string
// holding the tool's own arguments.
func dshToolSignature(data map[string]json.RawMessage) string {
	name := stringField(data, "name")
	target := ""
	if arguments, ok := objectField(data, "arguments"); ok {
		target = firstCommand(arguments)
	}
	if target == "" {
		// DSH stores the tool arguments as a JSON string, so the inner object
		// is decoded once more before its command or path is read.
		if inner, ok := decodeLine(stringField(data, "arguments")); ok {
			target = firstCommand(inner)
		}
	}
	if target == "" {
		return name
	}
	return name + ": " + target
}
