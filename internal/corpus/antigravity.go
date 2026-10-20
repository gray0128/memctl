package corpus

import (
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// This file reads the Antigravity transcript of
// docs/12-session-corpus-dreaming.md section 2.2.1:
// $HOME/.gemini/antigravity/brain/<sid>/.system_generated/logs/transcript.jsonl.
// Each line is one event; `USER_INPUT` records carry the person's request and
// `PLANNER_RESPONSE` records carry the model's text plus its tool calls. Every
// other record type holds tool output, system notices or checkpoints, which the
// digest never keeps.

// Antigravity wraps the person's words in a request element and appends host
// metadata elements to the same record. Only the request body is user speech;
// the metadata is machine bookkeeping.
var (
	antigravityRequest  = regexp.MustCompile(`(?s)<USER_REQUEST>(.*?)</USER_REQUEST>`)
	antigravityMetadata = regexp.MustCompile(`(?s)<ADDITIONAL_METADATA>.*?</ADDITIONAL_METADATA>`)
	antigravitySettings = regexp.MustCompile(`(?s)<USER_SETTINGS_CHANGE>.*?</USER_SETTINGS_CHANGE>`)
)

// antigravitySniffLines bounds how far the backfill reads to find the session's
// working directory. The directory is not in a metadata line, so a sweep stops
// after this many records rather than decoding a whole unrelated session.
const antigravitySniffLines = 400

// parseAntigravity reads one Antigravity transcript. The session id is the
// brain directory name, which the caller passes as the fallback id; the
// working directory only appears in `run_command` tool arguments.
func parseAntigravity(c Collector, r io.Reader) (parseInput, bool) {
	out := parseInput{}
	scanner := newLineScanner(r)
	var pending *Turn
	for scanner.scan() {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		out.newest = newestTime(out.newest, timeField(value, "created_at"))
		if cwd := antigravityCwd(value); cwd != "" && out.cwd == "" {
			out.cwd = cwd
		}
		switch stringField(value, "type") {
		case "USER_INPUT":
			// The host also records non-person input; only an explicit user
			// request is speech that can carry a durable preference.
			if stringField(value, "source") != "USER_EXPLICIT" {
				continue
			}
			text := antigravityUserText(stringField(value, "content"))
			if text == "" || looksLikeInjectedContext(text) {
				continue
			}
			c.addUser(&pending, &out, text)
		case "PLANNER_RESPONSE":
			calls, _ := arrayField(value, "tool_calls")
			// A planner response before any accepted user message belongs to
			// the harness setup and cannot be attributed to a person.
			if pending == nil {
				continue
			}
			if text := strings.TrimSpace(stringField(value, "content")); text != "" {
				pending.Assistant = appendText(pending.Assistant, text)
			}
			for _, raw := range calls {
				var call map[string]json.RawMessage
				if err := json.Unmarshal(raw, &call); err != nil {
					continue
				}
				pending.Tools = append(pending.Tools, antigravityToolSignature(call))
			}
			// A `thinking` field is the model's internal reasoning and is
			// never kept.
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

// antigravityUserText keeps the request body and drops the host metadata that
// Antigravity appends to the same record.
func antigravityUserText(content string) string {
	if match := antigravityRequest.FindStringSubmatch(content); match != nil {
		content = match[1]
	} else {
		content = antigravityMetadata.ReplaceAllString(content, " ")
		content = antigravitySettings.ReplaceAllString(content, " ")
	}
	return strings.TrimSpace(content)
}

// antigravityCwd reads the working directory a run_command call was given. The
// transcript has no session metadata line, so this tool argument is the only
// project hint the record carries.
func antigravityCwd(value map[string]json.RawMessage) string {
	calls, ok := arrayField(value, "tool_calls")
	if !ok {
		return ""
	}
	for _, raw := range calls {
		var call map[string]json.RawMessage
		if err := json.Unmarshal(raw, &call); err != nil {
			continue
		}
		if stringField(call, "name") != "run_command" {
			continue
		}
		if args, ok := objectField(call, "args"); ok {
			if cwd := antigravityArg(args, "Cwd"); cwd != "" {
				return cwd
			}
		}
	}
	return ""
}

// antigravityToolSignature renders one tool call as `name: target`. The first
// path or command shaped argument is enough to recognise what the turn did.
func antigravityToolSignature(call map[string]json.RawMessage) string {
	name := stringField(call, "name")
	args, ok := objectField(call, "args")
	if !ok {
		return name
	}
	for _, key := range []string{"CommandLine", "AbsolutePath", "DirectoryPath", "Query"} {
		if target := antigravityArg(args, key); target != "" {
			return name + ": " + target
		}
	}
	return name
}

// antigravityArg decodes one argument. Antigravity stores every argument as a
// JSON string whose own value is quoted once more, so the decoded value keeps
// its surrounding double quotes until they are stripped here.
func antigravityArg(args map[string]json.RawMessage, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	value := firstCommandString(string(raw))
	return strings.Trim(value, `"`)
}

// sniffAntigravity returns the project directory of a transcript and an empty
// session id: the id is the directory name, which the caller already has. It
// stops at the first `run_command`, so an unrelated session is closed early.
func sniffAntigravity(r io.Reader) (string, string) {
	scanner := newLineScanner(r)
	for line := 0; line < antigravitySniffLines && scanner.scan(); line++ {
		value, ok := decodeLine(scanner.text())
		if !ok {
			continue
		}
		if cwd := antigravityCwd(value); cwd != "" {
			return cwd, ""
		}
	}
	return "", ""
}
