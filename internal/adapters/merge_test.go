package adapters

import (
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/assets"
)

func loadAsset(t *testing.T, name string) []byte {
	t.Helper()
	data, err := assets.AdapterFile(name)
	if err != nil {
		t.Fatalf("AdapterFile(%q) error: %v", name, err)
	}
	return data
}

func mustMerge(t *testing.T, spec ArtifactSpec, src string) (string, bool) {
	t.Helper()
	asset := loadAsset(t, spec.Asset)
	out, changed, err := mergeContent(formatForPath(expectedPath(spec)), spec, asset, []byte(src))
	if err != nil {
		t.Fatalf("mergeContent error: %v", err)
	}
	return string(out), changed
}

// expectedPath gives mergeContent a representative target path so the format is
// inferred the same way the real plan does.
func expectedPath(spec ArtifactSpec) string {
	switch spec.Asset {
	case "pi/hooks.yaml":
		return "hooks.yaml"
	case "codex/features.toml":
		return "config.toml"
	case "opencode/agent-memory.ts":
		return "agent-memory.ts"
	case "codex/hooks.json", "dsh/hooks.json", "grok/hooks.json", "antigravity/hooks.json",
		"commandcode/hooks.json", "claude-code/hooks.json":
		return "hooks.json"
	default:
		return "settings.json"
	}
}

func claudeSpec() ArtifactSpec {
	return ArtifactSpec{Kind: KindMerge, ContainerPath: []string{"hooks"}, Asset: "claude-code/hooks.json"}
}

func TestJSONMergePreservesUserConfigAndIsIdempotent(t *testing.T) {
	src := `{
  // keep the user comment
  "model": "opus",
  "hooks": {
    "SessionStart": [
      { "hooks": [{ "type": "command", "command": "user-session-hook" }] }
    ],
    "PreToolUse": [
      { "matcher": "Bash", "hooks": [{ "type": "command", "command": "guard" }] }
    ]
  }
}
`
	merged, changed := mustMerge(t, claudeSpec(), src)
	if !changed {
		t.Fatal("first merge must change the file")
	}
	for _, want := range []string{
		"// keep the user comment",
		`"model": "opus"`,
		"user-session-hook",
		`"PreToolUse"`,
		`start --agent claude-code --stdin`,
		`sync --agent claude-code --stdin`,
		`end --agent claude-code --stdin`,
	} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged output missing %q:\n%s", want, merged)
		}
	}

	second, changed := mustMerge(t, claudeSpec(), merged)
	if changed {
		t.Fatalf("second merge must be a no-op, got:\n%s", second)
	}
}

func TestJSONMergeCreatesContainerAndReplacesManagedEntry(t *testing.T) {
	src := `{
  "model": "opus"
}`
	merged, changed := mustMerge(t, claudeSpec(), src)
	if !changed || !strings.Contains(merged, `"hooks"`) {
		t.Fatalf("merge did not create the hooks container:\n%s", merged)
	}

	upgrade := `{
  "hooks": {
    "Stop": [
      { "hooks": [{ "type": "command", "command": "old /opt/memctl sync --agent claude-code" }] }
    ]
  }
}`
	merged, changed = mustMerge(t, claudeSpec(), upgrade)
	if !changed {
		t.Fatal("a stale managed entry must be replaced")
	}
	if strings.Contains(merged, "/opt/memctl") {
		t.Fatalf("the stale managed entry was not removed:\n%s", merged)
	}
	if strings.Count(merged, "memctl") != 3 {
		t.Fatalf("expected exactly three managed commands, got:\n%s", merged)
	}
}

func TestJSONMergeRefusesUnsafeStructures(t *testing.T) {
	spec := claudeSpec()
	if _, _, err := mergeContent(FormatJSON, spec, loadAsset(t, spec.Asset), []byte(`{"hooks":"nope"}`)); !IsManualMerge(err) {
		t.Fatalf("hooks value that is not an object must ask for a manual patch, got %v", err)
	}
	if _, _, err := mergeContent(FormatJSON, spec, loadAsset(t, spec.Asset), []byte(`[]`)); !IsManualMerge(err) {
		t.Fatalf("non-object top level must ask for a manual patch, got %v", err)
	}
	if _, _, err := mergeContent(FormatJSON, spec, loadAsset(t, spec.Asset), []byte("{\n  \"a\": 1\n  // gap comment\n}")); !IsManualMerge(err) {
		t.Fatalf("a comment in the insertion gap must ask for a manual patch, got %v", err)
	}
}

func TestYAMLMergePreservesUserItemsAndIsIdempotent(t *testing.T) {
	spec := ArtifactSpec{Kind: KindMerge, Asset: "pi/hooks.yaml"}
	src := `# user header
hooks:
  - id: user-hook
    event: session.idle
    actions:
      - bash:
          command: echo hi
`
	merged, changed := mustMerge(t, spec, src)
	if !changed {
		t.Fatal("first merge must change the file")
	}
	for _, want := range []string{
		"# user header",
		"- id: user-hook",
		"- id: agent-memory-context",
		"- id: agent-memory-sync",
		"- id: agent-memory-end",
	} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged output missing %q:\n%s", want, merged)
		}
	}

	second, changed := mustMerge(t, spec, merged)
	if changed {
		t.Fatalf("second merge must be a no-op, got:\n%s", second)
	}
}

func TestYAMLMergeAppendsBlockWhenMissing(t *testing.T) {
	spec := ArtifactSpec{Kind: KindMerge, Asset: "pi/hooks.yaml"}
	merged, changed := mustMerge(t, spec, "other: value\n")
	if !changed || !strings.Contains(merged, "hooks:") || !strings.Contains(merged, "agent-memory-context") {
		t.Fatalf("append merge failed:\n%s", merged)
	}
	if !strings.Contains(merged, "other: value") {
		t.Fatalf("existing content was dropped:\n%s", merged)
	}
}

func TestYAMLMergeRefusesInlineValue(t *testing.T) {
	spec := ArtifactSpec{Kind: KindMerge, Asset: "pi/hooks.yaml"}
	if _, _, err := mergeContent(FormatYAML, spec, loadAsset(t, spec.Asset), []byte("hooks: []\n")); !IsManualMerge(err) {
		t.Fatalf("inline hooks value must ask for a manual patch, got %v", err)
	}
}

func TestTOMLMergeInsertsKeyAndIsIdempotent(t *testing.T) {
	spec := ArtifactSpec{Kind: KindMerge, Asset: "codex/features.toml"}
	src := "# codex config\nmodel = \"gpt\"\n\n[features]\nweb_search = true\n"
	merged, changed := mustMerge(t, spec, src)
	if !changed || !strings.Contains(merged, "hooks = true") {
		t.Fatalf("toml merge failed:\n%s", merged)
	}
	if !strings.Contains(merged, "# codex config") || !strings.Contains(merged, "web_search = true") {
		t.Fatalf("toml merge dropped unrelated content:\n%s", merged)
	}

	second, changed := mustMerge(t, spec, merged)
	if changed {
		t.Fatalf("second toml merge must be a no-op, got:\n%s", second)
	}
}

func TestTOMLMergeAppendsTableAndRefusesDifferentValue(t *testing.T) {
	spec := ArtifactSpec{Kind: KindMerge, Asset: "codex/features.toml"}
	merged, changed := mustMerge(t, spec, "model = \"gpt\"\n")
	if !changed || !strings.Contains(merged, "[features]") {
		t.Fatalf("toml append failed:\n%s", merged)
	}

	if _, _, err := mergeContent(FormatTOML, spec, loadAsset(t, spec.Asset), []byte("[features]\nhooks = false\n")); !IsManualMerge(err) {
		t.Fatalf("an explicit different value must ask for a manual patch, got %v", err)
	}
}

func TestCopyMergeRespectsUnmanagedFiles(t *testing.T) {
	spec := ArtifactSpec{Kind: KindCopy, Asset: "opencode/agent-memory.ts"}
	asset := loadAsset(t, spec.Asset)
	if _, changed, err := mergeContent(FormatText, spec, asset, nil); err != nil || !changed {
		t.Fatalf("absent copy target must be created, changed=%v err=%v", changed, err)
	}
	if _, changed, err := mergeContent(FormatText, spec, asset, asset); err != nil || changed {
		t.Fatalf("identical copy target must be a no-op, changed=%v err=%v", changed, err)
	}
	if _, _, err := mergeContent(FormatText, spec, asset, []byte("// user file\n")); !IsManualMerge(err) {
		t.Fatalf("an unmanaged copy target must ask for a manual patch, got %v", err)
	}
}

func TestLineDiffReportsAddedLines(t *testing.T) {
	diff := LineDiff("a\nb\n", "a\nc\nb\n")
	var added, removed int
	for _, line := range diff {
		switch line.Kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	if added != 1 || removed != 0 {
		t.Fatalf("unexpected diff: added=%d removed=%d", added, removed)
	}
}

// TestYAMLMergeRecognizesManagedPrefixAndIDComments covers the documented
// `agent-memory-` id prefix: a managed entry keeps its identity even when the
// id line carries a comment, a stale managed id is replaced instead of
// duplicated, and the merge is idempotent.
func TestYAMLMergeRecognizesManagedPrefixAndIDComments(t *testing.T) {
	spec := ArtifactSpec{Kind: KindMerge, Asset: "pi/hooks.yaml"}
	src := `# user header
hooks:
  - id: user-hook
    event: session.idle
    actions:
      - bash:
          command: echo hi
  - id: agent-memory-sync # keep this one
    event: session.idle
    actions:
      - bash:
          command: memctl sync
  - id: agent-memory-legacy
    event: session.deleted
    actions:
      - bash:
          command: memctl end
`
	merged, changed := mustMerge(t, spec, src)
	if !changed {
		t.Fatal("first merge must change the file")
	}
	if strings.Contains(merged, "agent-memory-legacy") {
		t.Fatalf("stale managed entry survived the merge:\n%s", merged)
	}
	if count := strings.Count(merged, "id: agent-memory-"); count != 3 {
		t.Fatalf("managed entries = %d, want 3:\n%s", count, merged)
	}
	if !strings.Contains(merged, "- id: user-hook") {
		t.Fatalf("user item dropped:\n%s", merged)
	}
	second, changed := mustMerge(t, spec, merged)
	if changed {
		t.Fatalf("second merge must be a no-op, got:\n%s", second)
	}
}

func antigravitySpec() ArtifactSpec {
	return ArtifactSpec{Kind: KindMerge, ContainerPath: []string{"agent-memory"}, Asset: "antigravity/hooks.json"}
}

// TestAntigravityNamedHookMerge covers the real Antigravity 1.4 layout:
// ~/.gemini/config/hooks.json is a map of hook names, and memctl owns the
// "agent-memory" entry without touching other named hooks.
func TestAntigravityNamedHookMerge(t *testing.T) {
	src := `{
  "orca-status": {
    "Stop": [
      { "type": "command", "command": "orca-hook" }
    ]
  }
}
`
	merged, changed := mustMerge(t, antigravitySpec(), src)
	if !changed {
		t.Fatal("first antigravity merge must change the file")
	}
	for _, want := range []string{
		`"orca-status"`,
		`"agent-memory"`,
		"start --agent antigravity --stdin --agy",
		"sync --agent antigravity --stdin --agy",
		`"PostInvocation"`,
		`"Stop"`,
	} {
		if !strings.Contains(merged, want) {
			t.Fatalf("merged output missing %q:\n%s", want, merged)
		}
	}
	if count := strings.Count(merged, "memctl"); count != 3 {
		t.Fatalf("expected three managed antigravity commands, got %d:\n%s", count, merged)
	}

	second, changed := mustMerge(t, antigravitySpec(), merged)
	if changed {
		t.Fatalf("second antigravity merge must be a no-op, got:\n%s", second)
	}

	stale := `{
  "agent-memory": {
    "SessionStart": [
      { "type": "command", "command": "old /opt/memctl start --agent antigravity --stdin" }
    ]
  }
}
`
	merged, changed = mustMerge(t, antigravitySpec(), stale)
	if !changed || strings.Contains(merged, "/opt/memctl") {
		t.Fatalf("stale antigravity command was not replaced:\n%s", merged)
	}
	if count := strings.Count(merged, "memctl"); count != 3 {
		t.Fatalf("expected exactly three managed antigravity commands after replacement, got %d:\n%s", count, merged)
	}
}
