// Package hookio implements the Agent Memory Hook protocol: bounded stdin
// JSON parsing, session identity resolution and the stdout envelope. It is
// pure protocol conversion and never touches the memory repository. See
// docs/04-ui-and-cli-specification.md sections 2 and 3.
package hookio

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// MaxStdinBytes is the documented Hook stdin limit from
// docs/02-domain-models-and-contracts.md section 4.1.
const MaxStdinBytes = 1 << 20

// Documented identity defaults.
const (
	DefaultAgent  = "unknown"
	DefaultDevice = "unknown"
)

// Hook event names used by the JSON envelope.
const (
	EventSessionStart     = "SessionStart"
	EventUserPromptSubmit = "UserPromptSubmit"
)

// sessionIDKeys are the payload keys consumed in priority order.
var sessionIDKeys = []string{"session_id", "sessionId", "sessionID", "conversationId"}

// transcriptKeys are the payload keys that carry the host's session log path,
// in priority order. They feed the local deterministic distillation of
// docs/12-session-corpus-dreaming.md section 2.2.
var transcriptKeys = []string{"transcript_path", "transcriptPath"}

// assistantKeys are the payload keys that carry the host's final assistant
// message for this turn.
var assistantKeys = []string{"last_assistant_message", "lastAssistantMessage"}

// Input is the session identity subset of a Hook payload.
type Input struct {
	// SID is the first non-empty session id found in the payload.
	SID string
	// TranscriptPath is the host's session log path, when the payload has one.
	TranscriptPath string
	// LastAssistantMessage is the host's final assistant text for this turn.
	LastAssistantMessage string
	// Present reports whether non-empty input was consumed.
	Present bool
}

// ReadInput reads at most MaxStdinBytes from r and extracts the session id
// fields. Empty input yields a zero Input. Anything larger than the limit is
// rejected after the limit plus one byte instead of buffering without bound.
func ReadInput(r io.Reader) (Input, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxStdinBytes+1))
	if err != nil {
		return Input{}, memerr.Wrap(memerr.IOFailed, err, "cannot read the hook payload from stdin")
	}
	if int64(len(data)) > MaxStdinBytes {
		return Input{}, memerr.New(memerr.LimitExceeded,
			"the hook payload exceeds the %d byte stdin limit", MaxStdinBytes)
	}
	if strings.TrimSpace(string(data)) == "" {
		return Input{}, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Input{}, memerr.Wrap(memerr.InvalidArgument, err, "the hook payload is not a JSON object")
	}

	in := Input{Present: true}
	if value, err := firstStringField(fields, transcriptKeys); err != nil {
		return Input{}, err
	} else {
		in.TranscriptPath = value
	}
	if value, err := firstStringField(fields, assistantKeys); err != nil {
		return Input{}, err
	} else {
		in.LastAssistantMessage = value
	}
	for _, key := range sessionIDKeys {
		raw, ok := fields[key]
		if !ok || string(raw) == "null" {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return Input{}, memerr.New(memerr.InvalidArgument, "hook field %s must be a string", key)
		}
		if value == "" {
			// An empty value is skipped; the next spelling may still carry it.
			continue
		}
		in.SID = value
		break
	}
	return in, nil
}

// firstStringField returns the first non-empty string among the given keys. A
// key of another type is a protocol error, matching the session id rule.
func firstStringField(fields map[string]json.RawMessage, keys []string) (string, error) {
	for _, key := range keys {
		raw, ok := fields[key]
		if !ok || string(raw) == "null" {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return "", memerr.New(memerr.InvalidArgument, "hook field %s must be a string", key)
		}
		if value != "" {
			return value, nil
		}
	}
	return "", nil
}

// ResolveSID returns the first non-empty session id among the explicit flag,
// AGENT_MEMORY_SID, DSH_HOOK_SESSION_ID and the payload. An empty result means
// no stable session identity is available, so callers must not deduplicate
// across processes or fabricate a source.
func ResolveSID(explicit string, getenv func(string) string, in Input) string {
	for _, candidate := range []string{
		explicit,
		getenv("AGENT_MEMORY_SID"),
		getenv("DSH_HOOK_SESSION_ID"),
		in.SID,
	} {
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

// ResolveAgent returns --agent, then AGENT_MEMORY_AGENT, then the documented
// unknown default. The raw value is preserved; callers escape it per output.
func ResolveAgent(explicit string, getenv func(string) string) string {
	if explicit != "" {
		return explicit
	}
	if value := getenv("AGENT_MEMORY_AGENT"); value != "" {
		return value
	}
	return DefaultAgent
}

// ResolveDevice returns AGENT_MEMORY_DEVICE, then the OS hostname, then the
// documented unknown default. A failed hostname probe is not fatal.
func ResolveDevice(getenv func(string) string) string {
	if value := getenv("AGENT_MEMORY_DEVICE"); value != "" {
		return value
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	return DefaultDevice
}

// Tag renders the canonical agent://<device>/<agent>/<sid> source URI with
// each segment percent-escaped so different tuples cannot collide.
func Tag(device, agent, sid string) string {
	return "agent://" + url.PathEscape(device) + "/" + url.PathEscape(agent) + "/" + url.PathEscape(sid)
}

// Encode writes the hookSpecificOutput envelope used by `start --json` and
// `context --json`. stdout receives exactly one JSON object.
func Encode(w io.Writer, eventName, additionalContext string) error {
	payload := struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}{}
	payload.HookSpecificOutput.HookEventName = eventName
	payload.HookSpecificOutput.AdditionalContext = additionalContext

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot write the hook output envelope")
	}
	return nil
}

// EncodeInjectSteps writes the Antigravity SessionStart envelope. Antigravity
// expects a JSON object on stdout and only consumes system messages from
// injectSteps; `start --agy` uses this shape instead of hookSpecificOutput.
func EncodeInjectSteps(w io.Writer, additionalContext string) error {
	type systemMessage struct {
		SystemMessage string `json:"systemMessage"`
	}
	type injectStep struct {
		SystemMessage systemMessage `json:"systemMessage"`
	}
	payload := struct {
		InjectSteps []injectStep `json:"injectSteps"`
	}{
		InjectSteps: []injectStep{{SystemMessage: systemMessage{SystemMessage: additionalContext}}},
	}

	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return memerr.Wrap(memerr.IOFailed, err, "cannot write the Antigravity hook output envelope")
	}
	return nil
}
