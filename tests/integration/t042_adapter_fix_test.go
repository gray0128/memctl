package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestT042AntigravityHookProtocol covers the host-specific fixes for
// Antigravity 1.4: the camelCase conversationId is a stable session id and
// `start --agy` emits the injectSteps envelope the host consumes.
func TestT042AntigravityHookProtocol(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, _, env := a5LocalRepo(t, bin, root, "home")
	payload := `{"conversationId":"agy-session"}`

	start := runMemctlStdin(t, bin, home, env, payload, "start", "--agent", "antigravity", "--agy", "--stdin")
	if start.code != 0 {
		t.Fatalf("start --agy: code=%d stderr=%q", start.code, start.stderr)
	}
	var decoded struct {
		InjectSteps []struct {
			SystemMessage struct {
				SystemMessage string `json:"systemMessage"`
			} `json:"systemMessage"`
		} `json:"injectSteps"`
	}
	if err := json.Unmarshal([]byte(start.stdout), &decoded); err != nil {
		t.Fatalf("start --agy stdout is not the injectSteps envelope: %v\n%s", err, start.stdout)
	}
	if len(decoded.InjectSteps) != 1 {
		t.Fatalf("injectSteps length = %d, want 1:\n%s", len(decoded.InjectSteps), start.stdout)
	}
	context := decoded.InjectSteps[0].SystemMessage.SystemMessage
	if !strings.Contains(context, "source tag: agent://test-device/antigravity/agy-session") {
		t.Fatalf("conversationId was not used as the session id:\n%s", context)
	}

	sync := runMemctlStdin(t, bin, home, env, payload, "sync", "--agent", "antigravity", "--agy", "--stdin")
	if sync.code != 0 {
		t.Fatalf("sync --agy: code=%d stderr=%q", sync.code, sync.stderr)
	}
	if strings.TrimSpace(sync.stdout) != "{}" {
		t.Fatalf("sync --agy stdout = %q, want {}", sync.stdout)
	}
}
