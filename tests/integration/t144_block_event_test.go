package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestT144BlockEntriesAndEventGuardCoversI4 verifies stage I-4 (T144,
// docs/18 section 4.4.1 and 4.4.2) through the CLI binary:
//  1. indented lines under a bullet are its detailLines, searchable through
//     `entries --q`, and an untouched entry keeps its id when other lines of
//     the file change;
//  2. `add --kind/--status` writes the optional metadata and rejects unknown
//     values;
//  3. `write` refuses to delete a `kind: event` entry with EVENT_PROTECTED
//     unless `--force-delete-event` is given.
func TestT144BlockEntriesAndEventGuardCoversI4(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatalf("create notes dir: %v", err)
	}

	add := runMemctl(t, bin, home, env, "add", "notes/release.md", "--fact", "Released v1.0 to production",
		"--kind", "event", "--status", "verified", "--agent", "test", "--sid", "s1", "--json")
	if add.code != 0 {
		t.Fatalf("add: code=%d stderr=%q", add.code, add.stderr)
	}
	if !strings.Contains(add.stdout, `"kind":"event"`) || !strings.Contains(add.stdout, "; kind: event; status: verified]") {
		t.Fatalf("add output lacks kind/status: %s", add.stdout)
	}
	if bad := runMemctl(t, bin, home, env, "add", "notes/release.md", "--fact", "x", "--kind", "milestone"); bad.code != 1 || !strings.Contains(bad.stderr, "INVALID_ARGUMENT") {
		t.Fatalf("unknown kind: code=%d stderr=%q", bad.code, bad.stderr)
	}

	release := readRevision(t, bin, home, repo, env, "notes/release.md")
	recipe := "- Fix the pool timeout [source: agent://test-device/test/s1; added: 2026-10-01]\n" +
		"  1. Inspect pgbouncer stats\n" +
		"  2. Raise statement_timeout\n"
	a4WriteFile(t, bin, home, repo, env, "notes/release.md", release.RawContent+recipe)

	type entriesPayload struct {
		Items []struct {
			ID          string   `json:"id"`
			FactText    string   `json:"factText"`
			LineNumber  int      `json:"lineNumber"`
			DetailLines []string `json:"detailLines"`
			Kind        string   `json:"kind"`
		} `json:"items"`
	}
	search := func(q string) entriesPayload {
		t.Helper()
		result := runMemctl(t, bin, home, env, "entries", "--q", q, "--json")
		if result.code != 0 {
			t.Fatalf("entries --q %s: code=%d stderr=%q", q, result.code, result.stderr)
		}
		var payload entriesPayload
		if err := json.Unmarshal([]byte(result.stdout), &payload); err != nil {
			t.Fatalf("entries JSON: %v\n%s", err, result.stdout)
		}
		return payload
	}
	hit := search("pgbouncer")
	if len(hit.Items) == 0 || hit.Items[0].FactText != "Fix the pool timeout" || len(hit.Items[0].DetailLines) != 2 {
		t.Fatalf("detail keyword search = %+v", hit.Items)
	}
	recipeID, recipeLine := hit.Items[0].ID, hit.Items[0].LineNumber

	// Inserting lines above the recipe moves it but keeps its id.
	current := readRevision(t, bin, home, repo, env, "notes/release.md")
	shifted := strings.Replace(current.RawContent, "\n\n", "\n\nIntro paragraph.\n\n", 1)
	a4WriteFile(t, bin, home, repo, env, "notes/release.md", shifted)
	moved := search("pgbouncer")
	if len(moved.Items) == 0 || moved.Items[0].ID != recipeID || moved.Items[0].LineNumber == recipeLine {
		t.Fatalf("id not stable across an unrelated edit: before %s@%d after %+v", recipeID, recipeLine, moved.Items)
	}

	// Dropping the event line is refused; the force flag allows it.
	current = readRevision(t, bin, home, repo, env, "notes/release.md")
	var kept []string
	for _, line := range strings.Split(current.RawContent, "\n") {
		if !strings.Contains(line, "Released v1.0") {
			kept = append(kept, line)
		}
	}
	withoutEvent := strings.Join(kept, "\n")
	refused := runMemctlStdin(t, bin, home, env, withoutEvent, "write", "notes/release.md", "--base", current.FileRevision, "--stdin", "--agent", "test", "--sid", "s1")
	if refused.code != 1 || !strings.Contains(refused.stderr, "EVENT_PROTECTED") {
		t.Fatalf("event delete: code=%d stderr=%q", refused.code, refused.stderr)
	}
	if after := readRevision(t, bin, home, repo, env, "notes/release.md"); after.RawContent != current.RawContent {
		t.Fatal("a refused write changed the file")
	}
	forced := runMemctlStdin(t, bin, home, env, withoutEvent, "write", "notes/release.md", "--base", current.FileRevision, "--stdin", "--force-delete-event", "--agent", "test", "--sid", "s1")
	if forced.code != 0 {
		t.Fatalf("forced event delete: code=%d stderr=%q", forced.code, forced.stderr)
	}
	if after := readRevision(t, bin, home, repo, env, "notes/release.md"); strings.Contains(after.RawContent, "Released v1.0") || !strings.Contains(after.RawContent, "  2. Raise statement_timeout") {
		t.Fatalf("forced write result:\n%s", after.RawContent)
	}
}
