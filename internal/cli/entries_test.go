package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// entriesRepo creates a committed memory repository and points the CLI
// environment at it. It returns the repository path.
func entriesRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
			"GIT_CONFIG_NOSYSTEM=1",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"),
		[]byte("# Memory\n\n## Index\n- [[notes/prefs]]\n"), 0o644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes", "prefs.md"), []byte(
		"# Preferences\n\n- 编辑器偏好是 Vim [source: dsh; added: 2026-01-05]\n"+
			"- 终端使用 Ghostty [source: dsh; added: 2026-06-01]\n"), 0o644); err != nil {
		t.Fatalf("write prefs: %v", err)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "init")

	t.Setenv("AGENT_MEMORY_DIR", dir)
	t.Setenv("AGENT_MEMORY_STATE", filepath.Join(t.TempDir(), "state"))
	return dir
}

// TestEntriesCLIFilters covers the read-only entry query: the same filters the
// HTTP route and the MCP tool use, with text and JSON output.
func TestEntriesCLIFilters(t *testing.T) {
	entriesRepo(t)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"entries"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("entries: code=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "notes/prefs.md:3") || !strings.Contains(out, "编辑器偏好是 Vim") {
		t.Fatalf("text output is missing an entry:\n%s", out)
	}
	// MEMORY.md's index bullet is itself an entry, so a fresh two-file fixture
	// reports three.
	if !strings.Contains(out, "--- 3 of 3 entries") {
		t.Fatalf("text output is missing the trailer:\n%s", out)
	}

	// --q is a case-insensitive substring over the fact text.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"entries", "--q", "vim"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("--q: code=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "Ghostty") || !strings.Contains(stdout.String(), "Vim") {
		t.Fatalf("--q did not filter: %s", stdout.String())
	}

	// --file narrows to one file and --json returns the page shape.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"entries", "--file", "notes/prefs.md", "--limit", "1", "--json"},
		strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("--json: code=%d stderr=%q", code, stderr.String())
	}
	var page struct {
		Revision string `json:"revision"`
		Total    int    `json:"total"`
		Items    []struct {
			FilePath string `json:"filePath"`
			FactText string `json:"factText"`
		} `json:"items"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &page); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, stdout.String())
	}
	// Total counts every match, not only the returned page.
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].FilePath != "notes/prefs.md" {
		t.Fatalf("unexpected page %+v", page)
	}
	if page.Revision == "" {
		t.Fatal("page has no revision")
	}

	// A filter that matches nothing is a successful empty result.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"entries", "--q", "no-such-fact"}, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitOK {
		t.Fatalf("empty result: code=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "no matching entries") {
		t.Fatalf("empty result is not reported: %q", stdout.String())
	}
}

// TestEntriesCLIUsageErrors covers the flags that must fail before touching the
// repository.
func TestEntriesCLIUsageErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cases := [][]string{
		{"entries", "--q"},
		{"entries", "--limit", "abc"},
		{"entries", "--offset", "1.5"},
		{"entries", "--unknown"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := Run(args, strings.NewReader(""), &stdout, &stderr, "v-test"); code != ExitUsageError {
			t.Fatalf("%v: code=%d, want %d (stderr=%q)", args, code, ExitUsageError, stderr.String())
		}
		if stdout.Len() != 0 {
			t.Fatalf("%v: stdout = %q, want empty", args, stdout.String())
		}
	}
}

// TestEntriesRejectsPageBeyondLimit covers the documented page bound.
func TestEntriesRejectsPageBeyondLimit(t *testing.T) {
	entriesRepo(t)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"entries", "--limit", "201"}, strings.NewReader(""), &stdout, &stderr, "v-test")
	if code != ExitError {
		t.Fatalf("limit 201: code=%d, want %d", code, ExitError)
	}
	if !strings.Contains(stderr.String(), "LIMIT_EXCEEDED") {
		t.Fatalf("limit 201 error is not targeted: %q", stderr.String())
	}
}
