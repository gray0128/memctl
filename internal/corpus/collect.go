package corpus

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/hookio"
)

// Collector distills one host session log into a SessionDigest. Every parser
// returns what it could read; a format mismatch yields a zero digest and a
// false result instead of a guess, per
// docs/12-session-corpus-dreaming.md section 3.2 rule 6.
type Collector struct {
	// Device is the source-tag device segment, resolved by the caller.
	Device string
	// Cwd is the current working directory used for the default project key
	// when a log carries no directory of its own.
	Cwd string
	// Now is the injectable clock.
	Now func() time.Time
}

// parseInput is what one host parser returns before the digest is assembled.
type parseInput struct {
	// sessionID and cwd come from the log's own first-line metadata.
	sessionID string
	cwd       string
	newest    time.Time
	truncated bool
	turns     []Turn
	// wroteMemory reports whether the session already produced a commit whose
	// message carries this session's trailer.
	wroteMemory bool
}

// suspiciousMatcher is the doctor SUSPICIOUS_INSTRUCTION pattern reused so the
// two surfaces cannot drift apart. It is assigned by the package that owns the
// pattern list.
var suspiciousMatcher func(string) bool

// parseFunc reads one host log stream.
type parseFunc func(c Collector, r io.Reader) (parseInput, bool)

// parsers maps the documented agent name to its verified parser. A host that
// is not in this map is not parsed: docs/12 lists it as unverified or
// unsupported and the collector stays silent instead of guessing fields.
var parsers = map[string]parseFunc{
	"claude-code": parseClaudeCode,
	"codex":       parseCodex,
	"pi":          parsePi,
	"dsh":         parseDSH,
	"antigravity": parseAntigravity,
	// WorkBuddy runs the CodeBuddy agent core, so both hosts share one parser
	// and differ only in their store root and agent name.
	"codebuddy": parseCodeBuddy,
	"workbuddy": parseCodeBuddy,
}

// Supports reports whether the host's log format is implemented.
func Supports(agent string) bool {
	_, ok := parsers[agent]
	return ok
}

// Collect distills one log stream for the given host. The returned digest uses
// the stream's session id when it has one and the supplied fallback otherwise;
// fallback is what a host plugin knows before reading the file.
func (c Collector) Collect(agent, fallbackSID string, r io.Reader) (Digest, bool) {
	parse, ok := parsers[agent]
	if !ok {
		return Digest{}, false
	}
	parsed, ok := parse(c, r)
	if !ok {
		return Digest{}, false
	}
	sid := parsed.sessionID
	if sid == "" {
		sid = fallbackSID
	}
	if sid == "" {
		return Digest{}, false
	}
	turns := boundTurns(parsed.turns)
	if len(turns) == 0 {
		return Digest{}, false
	}
	cwd := parsed.cwd
	if cwd == "" {
		cwd = c.Cwd
	}
	now := c.now()
	return Digest{
		SchemaVersion:     SchemaVersion,
		Agent:             agent,
		SessionID:         sid,
		SourceTag:         hookio.Tag(c.Device, agent, sid),
		Date:              digestDate(parsed.newest, now),
		UpdatedAt:         now.UTC().Format(time.RFC3339),
		CwdBase:           CwdBase(cwd),
		CwdKey:            CwdKey(cwd),
		Truncated:         parsed.truncated,
		WroteMemoryInSess: parsed.wroteMemory,
		Turns:             turns,
	}, true
}

// now returns the collector clock.
func (c Collector) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// decodeLine unmarshals one JSONL line into a generic object. A line that is
// not a JSON object is skipped: logs interleave non-JSON diagnostics.
func decodeLine(line string) (map[string]json.RawMessage, bool) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return nil, false
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &value); err != nil {
		return nil, false
	}
	return value, true
}

// stringField reads one optional string field.
func stringField(value map[string]json.RawMessage, key string) string {
	raw, ok := value[key]
	if !ok {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}
	return text
}

// objectField reads one optional object field.
func objectField(value map[string]json.RawMessage, key string) (map[string]json.RawMessage, bool) {
	raw, ok := value[key]
	if !ok {
		return nil, false
	}
	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return nil, false
	}
	return nested, true
}

// arrayField reads one optional array field.
func arrayField(value map[string]json.RawMessage, key string) ([]json.RawMessage, bool) {
	raw, ok := value[key]
	if !ok {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false
	}
	return items, true
}

// timeField parses one RFC 3339 timestamp field, ignoring a missing or invalid
// value.
func timeField(value map[string]json.RawMessage, key string) time.Time {
	text := stringField(value, key)
	if text == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, text)
	}
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// newestTime keeps the later of two timestamps.
func newestTime(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

// finalizeTurn applies cleaning, redaction and dropping to one candidate turn.
// It returns false when nothing keepable is left or the turn is suspicious.
func (c Collector) finalizeTurn(suspicious func(string) bool, turn Turn) (Turn, bool) {
	user := truncateText(cleanAndRedact(turn.User), MaxTurnTextBytes)
	assistant := truncateText(cleanAndRedact(turn.Assistant), MaxTurnTextBytes)
	tools := boundTools(turn.Tools)
	if Suspicious(suspicious, user, assistant) {
		return Turn{}, false
	}
	if user == "" && assistant == "" && len(tools) == 0 {
		return Turn{}, false
	}
	return Turn{User: user, Tools: tools, Assistant: assistant}, true
}

// addUser folds one user message into the pending turn. Consecutive user
// messages with no assistant reply in between belong to the same turn: hosts
// record one logical prompt as several events (a pasted file, then the
// question, then a runtime note), and splitting them would leave turns with no
// assistant text and a truncated question.
func (c Collector) addUser(pending **Turn, out *parseInput, text string) {
	if *pending != nil && (*pending).Assistant == "" && len((*pending).Tools) == 0 {
		(*pending).User = appendText((*pending).User, text)
		return
	}
	if *pending != nil {
		if turn, ok := c.finalizeTurn(suspiciousMatcher, **pending); ok {
			out.turns = append(out.turns, turn)
		}
	}
	*pending = &Turn{User: text}
}

// cleanAndRedact is the single cleaning pipeline both parsers and the propose
// guard use, so the two paths cannot diverge.
func cleanAndRedact(text string) string {
	return Redact(Clean(text))
}

// SetSuspiciousMatcher installs the platform's prompt-injection detector. The
// doctor package owns the phrase list; the CLI wires it once at startup.
func SetSuspiciousMatcher(matcher func(string) bool) {
	suspiciousMatcher = matcher
}
