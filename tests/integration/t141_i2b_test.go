package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/app"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/lock"
)

// TestT141SnapshotReadAndProjectDashboardCoversI2b verifies stage I-2b (T141):
//  1. Lock-free snapshot reads from `refs/heads/<branch>` (including mid-rebase
//     detached HEAD via `.git/rebase-merge/head-name`) for CLI `read`/`entries`,
//     MCP `memory_read`/`memory_entries`, and `start`/`context`, with `until`
//     expiry hiding preserved and writes still returning BUSY.
//  2. Current-project dashboard preloading (`----- CURRENT PROJECT MEMORY (<cwdBase>) -----`)
//     ordered by `added` descending and unrelated `## Index` folding (> 20 lines)
//     without altering `memctl read MEMORY.md`.
//  3. `MEMORY.md` truncation at 200 lines / 25 KiB preserving `## Working preferences`
//     and current-project index lines.
func TestT141SnapshotReadAndProjectDashboardCoversI2b(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o755); err != nil {
		t.Fatalf("create notes dir: %v", err)
	}

	projDir := filepath.Join(root, "atlas")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("create project dir: %v", err)
	}

	// Build MEMORY.md with Working preferences, 24 unrelated index lines, and
	// current-project index lines (including [project: atlas] metadata), plus
	// an expired entry.
	var memLines []string
	memLines = append(memLines,
		"# Memory",
		"",
		"## Working preferences",
		"- Keep interfaces minimal [source: agent://dev/claude/s0; added: 2026-04-10]",
		"- Expired preference secret [source: agent://dev/claude/s0; added: 2026-01-01; until: 2020-01-01]",
		"",
		"## Index",
	)
	for i := 1; i <= 24; i++ {
		memLines = append(memLines, fmt.Sprintf("- [[notes/unrelated-%02d]] — Unrelated topic %02d", i, i))
	}
	memLines = append(memLines,
		"- [[notes/atlas]] — Atlas main service",
		"- [[notes/ATLAS-db]] — Atlas database layer",
		"- [[notes/ops-runbook]] — Shared ops runbook [project: atlas]",
		"",
	)
	a4WriteFile(t, bin, home, repo, env, "MEMORY.md", strings.Join(memLines, "\n"))
	a4WriteFile(t, bin, home, repo, env, "notes/atlas.md", strings.Join([]string{
		"# Atlas",
		"",
		"- Older atlas fact [source: agent://dev/claude/s1; added: 2026-04-05]",
		"- Expired atlas fact [source: agent://dev/claude/s1; added: 2020-01-01; until: 2020-01-02]",
		"",
	}, "\n"))
	a4WriteFile(t, bin, home, repo, env, "notes/ATLAS-db.md", strings.Join([]string{
		"# Atlas DB",
		"",
		"- Newest atlas db fact [source: agent://dev/claude/s2; added: 2026-04-18]",
		"",
	}, "\n"))
	a4WriteFile(t, bin, home, repo, env, "notes/ops-runbook.md", strings.Join([]string{
		"# Ops Runbook",
		"",
		"- Middle atlas ops fact [source: agent://dev/claude/s3; added: 2026-04-12]",
		"",
	}, "\n"))

	ctx := context.Background()
	gitDir, err := git.GitDir(ctx, repo)
	if err != nil {
		t.Fatalf("GitDir: %v", err)
	}
	firstCommit := strings.TrimSpace(gitOutput(t, env, repo, "rev-parse", "HEAD~2"))

	// Simulate mid-rebase detached HEAD while holding the repository lock and
	// dirtying worktree files on disk.
	gitOutput(t, env, repo, "checkout", "-q", "--detach", firstCommit)
	rebaseMerge := filepath.Join(gitDir, "rebase-merge")
	if err := os.MkdirAll(rebaseMerge, 0o700); err != nil {
		t.Fatalf("mkdir rebase-merge: %v", err)
	}
	if err := os.WriteFile(filepath.Join(rebaseMerge, "head-name"), []byte("refs/heads/main\n"), 0o600); err != nil {
		t.Fatalf("write head-name: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Corrupted mid-rebase worktree\n"), 0o644); err != nil {
		t.Fatalf("dirty MEMORY.md: %v", err)
	}

	held, ok, err := lock.TryAcquire(gitDir)
	if err != nil || !ok {
		t.Fatalf("TryAcquire: ok=%v err=%v", ok, err)
	}
	lockedCleaned := false
	cleanupRebase := func() {
		if !lockedCleaned {
			lockedCleaned = true
			_ = held.Release()
			_ = os.RemoveAll(rebaseMerge)
			gitOutput(t, env, repo, "checkout", "-q", "-f", "main")
		}
	}
	defer cleanupRebase()

	// 1. CLI `read` and `entries` succeed from the branch-ref snapshot without BUSY.
	cliRead := runMemctl(t, bin, home, env, "read", "MEMORY.md")
	if cliRead.code != 0 {
		t.Fatalf("cli read while lock busy failed: code=%d stderr=%s", cliRead.code, cliRead.stderr)
	}
	if !strings.Contains(cliRead.stdout, "Keep interfaces minimal") ||
		!strings.Contains(cliRead.stdout, "[[notes/unrelated-24]]") ||
		strings.Contains(cliRead.stdout, "Corrupted mid-rebase") ||
		strings.Contains(cliRead.stdout, "Expired preference secret") {
		t.Fatalf("unexpected cli read output during lock-busy mid-rebase:\n%s", cliRead.stdout)
	}

	cliEntries := runMemctl(t, bin, home, env, "entries", "--json")
	if cliEntries.code != 0 {
		t.Fatalf("cli entries while lock busy failed: code=%d stderr=%s", cliEntries.code, cliEntries.stderr)
	}
	if !strings.Contains(cliEntries.stdout, "Newest atlas db fact") ||
		strings.Contains(cliEntries.stdout, "Expired atlas fact") ||
		strings.Contains(cliEntries.stdout, "Expired preference secret") {
		t.Fatalf("unexpected cli entries output during lock-busy mid-rebase:\n%s", cliEntries.stdout)
	}

	// 2. MCP `memory_read` and `memory_entries` succeed from snapshot while `memory_append` returns BUSY.
	responses, stderr := runMCPSession(t, bin, home, env,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_read","arguments":{"path":"notes/atlas.md"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"memory_entries","arguments":{"q":"atlas"}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"memory_append","arguments":{"path":"notes/atlas.md","fact":"blocked write","sid":"s-busy"}}}`,
	)
	if stderr != "" {
		t.Fatalf("unexpected mcp stderr: %s", stderr)
	}
	mcpRead := decodeCallResult(t, responses[0])
	if mcpRead.IsError || !strings.Contains(mcpRead.Content[0].Text, "Older atlas fact") || strings.Contains(mcpRead.Content[0].Text, "Expired atlas fact") {
		t.Fatalf("unexpected mcp memory_read result: %+v", mcpRead)
	}
	mcpEntries := decodeCallResult(t, responses[1])
	if mcpEntries.IsError || !strings.Contains(mcpEntries.Content[0].Text, "Newest atlas db fact") || strings.Contains(mcpEntries.Content[0].Text, "Expired atlas fact") {
		t.Fatalf("unexpected mcp memory_entries result: %+v", mcpEntries)
	}
	mcpWrite := decodeCallResult(t, responses[2])
	if !mcpWrite.IsError || !strings.HasPrefix(mcpWrite.Content[0].Text, "BUSY:") {
		t.Fatalf("expected mcp memory_append to fail with BUSY while lock held, got: %+v", mcpWrite)
	}

	// 3. `start --stdin` during lock-busy mid-rebase reports sync=busy, folds
	// unrelated index lines beyond 20, and preloads CURRENT PROJECT MEMORY (atlas).
	payloadBytes, _ := json.Marshal(map[string]string{
		"session_id": "sid-i2b-start",
		"cwd":        projDir,
	})
	startOut := runMemctlStdin(t, bin, home, env, string(payloadBytes), "start", "--agent", "claude", "--stdin")
	if startOut.code != 0 {
		t.Fatalf("start while lock busy failed: code=%d stderr=%s", startOut.code, startOut.stderr)
	}
	for _, want := range []string{
		"sync=busy",
		"[[notes/unrelated-20]]",
		"[+4 other project topics omitted; query with 'memctl entries --q <keyword>']",
		"[[notes/atlas]]",
		"[[notes/ATLAS-db]]",
		"[[notes/ops-runbook]]",
		"----- CURRENT PROJECT MEMORY (atlas) -----",
		"[notes/ATLAS-db.md] Newest atlas db fact",
		"[notes/ops-runbook.md] Middle atlas ops fact",
		"[notes/atlas.md] Older atlas fact",
	} {
		if !strings.Contains(startOut.stdout, want) {
			t.Fatalf("start output missing %q:\n%s", want, startOut.stdout)
		}
	}
	if strings.Contains(startOut.stdout, "[[notes/unrelated-21]]") ||
		strings.Contains(startOut.stdout, "Expired atlas fact") ||
		strings.Contains(startOut.stdout, "Corrupted mid-rebase") {
		t.Fatalf("start output leaked folded, expired, or dirty content:\n%s", startOut.stdout)
	}
	idxNew := strings.Index(startOut.stdout, "[notes/ATLAS-db.md] Newest atlas db fact")
	idxMid := strings.Index(startOut.stdout, "[notes/ops-runbook.md] Middle atlas ops fact")
	idxOld := strings.Index(startOut.stdout, "[notes/atlas.md] Older atlas fact")
	if !(idxNew < idxMid && idxMid < idxOld) {
		t.Fatalf("project entries not sorted by added date descending:\n%s", startOut.stdout)
	}

	// Release lock and restore clean branch to test > 200 line truncation.
	cleanupRebase()

	var bloated []string
	bloated = append(bloated, "# Memory", "", "## General notes")
	for i := 1; i <= 215; i++ {
		bloated = append(bloated, fmt.Sprintf("- General note %03d [source: agent://dev/claude/s1; added: 2026-04-01]", i))
	}
	bloated = append(bloated,
		"",
		"## Working preferences",
		"- Critical preference kept on truncation [source: agent://dev/claude/s1; added: 2026-04-19]",
		"",
		"## Index",
		"- [[notes/atlas]] — Atlas main service",
		"",
	)
	a4WriteFile(t, bin, home, repo, env, "MEMORY.md", strings.Join(bloated, "\n"))

	ctxPayload, _ := json.Marshal(map[string]string{
		"session_id": "sid-i2b-trunc",
		"cwd":        projDir,
	})
	truncOut := runMemctlStdin(t, bin, home, env, string(ctxPayload), "context", "--agent", "claude", "--stdin")
	if truncOut.code != 0 {
		t.Fatalf("context failed: code=%d stderr=%s", truncOut.code, truncOut.stderr)
	}
	for _, want := range []string{
		"## Working preferences",
		"Critical preference kept on truncation",
		"[[notes/atlas]] — Atlas main service",
		app.MemoryTruncatedNotice,
	} {
		if !strings.Contains(truncOut.stdout, want) {
			t.Fatalf("truncated context output missing %q:\n%s", want, truncOut.stdout)
		}
	}
}
