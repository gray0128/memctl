package hookio

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
)

func TestReadInputSessionIDPriority(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   string
		absent bool
	}{
		{name: "empty input", input: "", absent: true},
		{name: "blank input", input: "  \n", absent: true},
		{name: "session_id", input: `{"session_id":"a"}`, want: "a"},
		{name: "sessionId", input: `{"sessionId":"b"}`, want: "b"},
		{name: "sessionID", input: `{"sessionID":"c"}`, want: "c"},
		{name: "session_id wins over sessionId", input: `{"session_id":"a","sessionId":"b"}`, want: "a"},
		{name: "empty session_id falls through", input: `{"session_id":"","sessionId":"b"}`, want: "b"},
		{name: "null falls through", input: `{"session_id":null,"sessionId":"b"}`, want: "b"},
		{name: "unknown fields ignored", input: `{"prompt":"secret","session_id":"a"}`, want: "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in, err := ReadInput(strings.NewReader(tc.input))
			if err != nil {
				t.Fatalf("ReadInput(%q) error: %v", tc.input, err)
			}
			if tc.absent {
				if in.Present || in.SID != "" {
					t.Fatalf("ReadInput(%q) = %+v, want zero", tc.input, in)
				}
				return
			}
			if in.SID != tc.want {
				t.Fatalf("ReadInput(%q).SID = %q, want %q", tc.input, in.SID, tc.want)
			}
		})
	}
}

func TestReadInputErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		code memerr.Code
	}{
		{name: "not json", data: "not json", code: memerr.InvalidArgument},
		{name: "json array", data: `["a"]`, code: memerr.InvalidArgument},
		{name: "non-string session_id", data: `{"session_id":123}`, code: memerr.InvalidArgument},
		{name: "object session_id", data: `{"session_id":{"a":1}}`, code: memerr.InvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadInput(strings.NewReader(tc.data))
			contractErr, ok := memerr.As(err)
			if !ok || contractErr.Code != tc.code {
				t.Fatalf("ReadInput(%q) error = %v, want code %s", tc.data, err, tc.code)
			}
		})
	}
}

func TestReadInputLimit(t *testing.T) {
	payload := `{"session_id":"` + strings.Repeat("a", MaxStdinBytes) + `"}`
	_, err := ReadInput(strings.NewReader(payload))
	contractErr, ok := memerr.As(err)
	if !ok || contractErr.Code != memerr.LimitExceeded {
		t.Fatalf("oversized input error = %v, want LIMIT_EXCEEDED", err)
	}
}

func TestResolveSIDPriority(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "AGENT_MEMORY_SID":
			return "env-session"
		case "DSH_HOOK_SESSION_ID":
			return "dsh-session"
		default:
			return ""
		}
	}

	if got := ResolveSID("flag", getenv, Input{SID: "payload"}); got != "flag" {
		t.Fatalf("explicit SID did not win: %q", got)
	}
	if got := ResolveSID("", getenv, Input{SID: "payload"}); got != "env-session" {
		t.Fatalf("environment SID did not beat payload: %q", got)
	}
	if got := ResolveSID("", func(string) string { return "" }, Input{SID: "payload"}); got != "payload" {
		t.Fatalf("payload SID was not used: %q", got)
	}
	if got := ResolveSID("", func(string) string { return "" }, Input{}); got != "" {
		t.Fatalf("empty identity resolved to %q, want empty", got)
	}
}

func TestResolveAgentAndDevice(t *testing.T) {
	getenv := func(key string) string {
		switch key {
		case "AGENT_MEMORY_AGENT":
			return "from-env"
		case "AGENT_MEMORY_DEVICE":
			return "device-env"
		default:
			return ""
		}
	}
	if got := ResolveAgent("flag", getenv); got != "flag" {
		t.Fatalf("explicit agent did not win: %q", got)
	}
	if got := ResolveAgent("", getenv); got != "from-env" {
		t.Fatalf("environment agent not used: %q", got)
	}
	if got := ResolveAgent("", func(string) string { return "" }); got != DefaultAgent {
		t.Fatalf("missing agent = %q, want %q", got, DefaultAgent)
	}
	if got := ResolveDevice(getenv); got != "device-env" {
		t.Fatalf("device = %q, want device-env", got)
	}
}

func TestTagEscapesSegments(t *testing.T) {
	got := Tag("dev ice", "a/b", "s:1")
	want := "agent://dev%20ice/a%2Fb/s:1"
	if got != want {
		t.Fatalf("Tag = %q, want %q", got, want)
	}
}

func TestEncodeEnvelope(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, EventSessionStart, "hello \"world\""); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var decoded struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if decoded.HookSpecificOutput.HookEventName != EventSessionStart {
		t.Fatalf("event name = %q", decoded.HookSpecificOutput.HookEventName)
	}
	if decoded.HookSpecificOutput.AdditionalContext != "hello \"world\"" {
		t.Fatalf("context = %q", decoded.HookSpecificOutput.AdditionalContext)
	}
	if strings.Contains(buf.String(), `\u003c`) {
		t.Fatalf("envelope escaped HTML: %q", buf.String())
	}
}
