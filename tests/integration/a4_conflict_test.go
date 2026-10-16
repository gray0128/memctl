package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file verifies the A-4 conflict scenarios C01-C04: multi-stage replay
// snapshots, stale-baseline rejection, conflict type handling and abort/restart
// across processes.

type conflictFixture struct {
	t     *testing.T
	bin   string
	root  string
	url   string
	home1 string
	repo1 string
	home2 string
	repo2 string
}

func newConflictFixture(t *testing.T) *conflictFixture {
	t.Helper()
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	f := &conflictFixture{t: t, bin: bin, root: root, url: newBareRemote(t, root, "memory.git")}
	f.home1 = a4Home(t, root, "home1")
	f.home2 = a4Home(t, root, "home2")
	f.repo1 = filepath.Join(f.home1, "memory")
	f.repo2 = filepath.Join(f.home2, "memory")
	if r := a4InitRemote(t, bin, f.home1, f.repo1, f.url); r.code != 0 {
		t.Fatalf("device1 init: code=%d stderr=%q", r.code, r.stderr)
	}
	if r := a4InitRemote(t, bin, f.home2, f.repo2, f.url); r.code != 0 {
		t.Fatalf("device2 init: code=%d stderr=%q", r.code, r.stderr)
	}
	return f
}

func (f *conflictFixture) env1() []string { return a4Env(f.home1, f.repo1) }
func (f *conflictFixture) env2() []string { return a4Env(f.home2, f.repo2) }

func (f *conflictFixture) write(which int, content string) {
	f.t.Helper()
	home, repo, env := f.home1, f.repo1, f.env1()
	if which == 2 {
		home, repo, env = f.home2, f.repo2, f.env2()
	}
	a4WriteFile(f.t, f.bin, home, repo, env, "MEMORY.md", content)
}

func (f *conflictFixture) sync(which int) cliResult {
	f.t.Helper()
	home, env := f.home1, f.env1()
	if which == 2 {
		home, env = f.home2, f.env2()
	}
	return runMemctl(f.t, f.bin, home, env, "sync", "--wait")
}

func (f *conflictFixture) resolve(which int, args ...string) cliResult {
	f.t.Helper()
	home, env := f.home1, f.env1()
	if which == 2 {
		home, env = f.home2, f.env2()
	}
	return runMemctl(f.t, f.bin, home, env, append([]string{"resolve"}, args...)...)
}

func (f *conflictFixture) conflictView(which int) map[string]any {
	f.t.Helper()
	result := f.resolve(which, "--json")
	if result.code != 0 {
		f.t.Fatalf("resolve --json: code=%d stderr=%q", result.code, result.stderr)
	}
	var view map[string]any
	if err := json.Unmarshal([]byte(result.stdout), &view); err != nil {
		f.t.Fatalf("resolve --json is not valid JSON: %v\n%s", err, result.stdout)
	}
	return view
}

func (f *conflictFixture) apply(which int, conflictID, baseRevision, content string) cliResult {
	f.t.Helper()
	payload := map[string]any{
		"baseRevision": baseRevision,
		"resolvedFiles": []map[string]any{
			{"path": "MEMORY.md", "action": "put", "rawContent": content},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		f.t.Fatal(err)
	}
	path := filepath.Join(f.root, "payload-"+conflictID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
	return f.resolve(which, "--apply", path, "--conflict", conflictID)
}

// a4WriteFile writes one managed file through the CLI using the current baseline.
// A missing file is written with the absent baseline.
func a4WriteFile(t *testing.T, bin, home, repo string, env []string, path, content string) {
	t.Helper()
	baseline, err := readBaseline(t, bin, home, repo, env, path)
	if err != nil {
		baseline = "absent"
	}
	result := runMemctlStdin(t, bin, home, env, content, "write", path, "--base", baseline, "--stdin", "--agent", "test", "--sid", "s1")
	if result.code != 0 {
		t.Fatalf("write %s: code=%d stderr=%q", path, result.code, result.stderr)
	}
}

func readBaseline(t *testing.T, bin, home, repo string, env []string, path string) (string, error) {
	t.Helper()
	result := runMemctl(t, bin, home, env, "read", path, "--json")
	if result.code != 0 {
		return "", os.ErrNotExist
	}
	var payload struct {
		FileRevision string `json:"fileRevision"`
		RawContent   string `json:"rawContent"`
	}
	if err := json.Unmarshal([]byte(result.stdout), &payload); err != nil {
		return "", err
	}
	return payload.FileRevision, nil
}

// TestA4ConflictTwoStagesCoversC01 covers two consecutive conflicting commits:
// each stage exposes the correct base/onto/replayed sides and the phase moves
// from captured to resolving.
func TestA4ConflictTwoStagesCoversC01(t *testing.T) {
	f := newConflictFixture(t)

	// device2 has two commits on the old base; device1 changed the same file.
	f.write(2, "# Memory\n\n## Index\n- two-a\n")
	f.write(2, "# Memory\n\n## Index\n- two-b\n")
	f.write(1, "# Memory\n\n## Index\n- one\n")
	if result := f.sync(1); result.code != 0 {
		t.Fatalf("device1 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	if result := f.sync(2); result.code != 1 || !strings.Contains(result.stderr, "CONTENT_CONFLICT") {
		t.Fatalf("device2 sync: code=%d stderr=%q, want CONTENT_CONFLICT", result.code, result.stderr)
	}

	first := f.conflictView(2)
	if first["phase"] != "captured" {
		t.Fatalf("first phase = %v, want captured", first["phase"])
	}
	firstFiles := first["files"].([]any)
	side := firstFiles[0].(map[string]any)
	if side["onto"].(map[string]any)["content"] != "# Memory\n\n## Index\n- one\n" {
		t.Fatalf("onto side = %v", side["onto"])
	}
	if side["replayed"].(map[string]any)["content"] != "# Memory\n\n## Index\n- two-a\n" {
		t.Fatalf("replayed side = %v", side["replayed"])
	}
	firstID := first["conflictId"].(string)
	firstBase := first["baseRevision"].(string)

	// Resolve the first stage to a content that drops two-a, so the second
	// commit no longer applies and conflicts again.
	result := f.apply(2, firstID, firstBase, "# Memory\n\n## Index\n- one\n")
	if result.code != 0 {
		t.Fatalf("apply stage 1: code=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if !strings.Contains(result.stdout, "remote: conflict") {
		t.Fatalf("apply stage 1 stdout = %q, want a further conflict", result.stdout)
	}

	second := f.conflictView(2)
	if second["phase"] != "resolving" {
		t.Fatalf("second phase = %v, want resolving", second["phase"])
	}
	if second["conflictId"] == firstID {
		t.Fatal("second stage reused the first conflict id")
	}
	secondFiles := second["files"].([]any)
	secondSide := secondFiles[0].(map[string]any)
	if secondSide["base"].(map[string]any)["content"] != "# Memory\n\n## Index\n- two-a\n" {
		t.Fatalf("second base side = %v", secondSide["base"])
	}
	if secondSide["replayed"].(map[string]any)["content"] != "# Memory\n\n## Index\n- two-b\n" {
		t.Fatalf("second replayed side = %v", secondSide["replayed"])
	}

	final := f.apply(2, second["conflictId"].(string), second["baseRevision"].(string), "# Memory\n\n## Index\n- one\n- two-b\n")
	if final.code != 0 || !strings.Contains(final.stdout, "remote: synced") {
		t.Fatalf("apply stage 2: code=%d stdout=%q stderr=%q", final.code, final.stdout, final.stderr)
	}
	if got := mustReadFile(t, filepath.Join(f.repo2, "MEMORY.md")); got != "# Memory\n\n## Index\n- one\n- two-b\n" {
		t.Fatalf("final content = %q", got)
	}
	if status := gitOutput(t, f.env2(), f.repo2, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean: %q", status)
	}
}

// TestA4ConflictStaleCoversC02 covers a captured stage whose base no longer
// matches the repository: the old result is rejected without overwriting the
// new history.
func TestA4ConflictStaleCoversC02(t *testing.T) {
	f := newConflictFixture(t)
	f.write(2, "# Memory\n\n## Index\n- two\n")
	f.write(1, "# Memory\n\n## Index\n- one\n")
	if result := f.sync(1); result.code != 0 {
		t.Fatalf("device1 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	if result := f.sync(2); result.code != 1 {
		t.Fatalf("device2 sync: code=%d stderr=%q", result.code, result.stderr)
	}
	view := f.conflictView(2)
	id := view["conflictId"].(string)
	base := view["baseRevision"].(string)

	// An external commit moves HEAD while the conflict is still captured.
	gitOutput(t, f.env2(), f.repo2, "commit", "--allow-empty", "-q", "-m", "external commit")
	headAfter := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD")

	result := f.apply(2, id, base, "# Memory\n\n## Index\n- resolved\n")
	if result.code != 1 || !strings.Contains(result.stderr, "CONFLICT_STALE") {
		t.Fatalf("stale apply: code=%d stdout=%q stderr=%q, want CONFLICT_STALE", result.code, result.stdout, result.stderr)
	}
	if head := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD"); head != headAfter {
		t.Fatalf("stale apply moved HEAD: %s -> %s", headAfter, head)
	}
	if got := mustReadFile(t, filepath.Join(f.repo2, "MEMORY.md")); got != "# Memory\n\n## Index\n- two\n" {
		t.Fatalf("stale apply rewrote content: %q", got)
	}
	if status := gitOutput(t, f.env2(), f.repo2, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after stale apply: %q", status)
	}
}

// TestA4ConflictTypesCoversC03 distinguishes delete/modify from an empty file
// and refuses unsupported binary conflicts.
func TestA4ConflictTypesCoversC03(t *testing.T) {
	t.Run("delete versus modify", func(t *testing.T) {
		f := newConflictFixture(t)
		// Create and share notes.md.
		a4WriteFile(t, f.bin, f.home1, f.repo1, f.env1(), "notes.md", "v1\n")
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 sync: %q", r.stderr)
		}
		if r := f.sync(2); r.code != 0 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		// device1 deletes notes.md through an explicit capture.
		if err := os.Remove(filepath.Join(f.repo1, "notes.md")); err != nil {
			t.Fatal(err)
		}
		if r := runMemctl(t, f.bin, f.home1, f.env1(), "sync", "--capture"); r.code != 0 {
			t.Fatalf("capture delete: %q", r.stderr)
		}
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 push: %q", r.stderr)
		}
		// device2 modifies the path it still has.
		a4WriteFile(t, f.bin, f.home2, f.repo2, f.env2(), "notes.md", "v2\n")
		if r := f.sync(2); r.code != 1 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		view := f.conflictView(2)
		files := view["files"].([]any)
		file := files[0].(map[string]any)
		if file["path"] != "notes.md" || file["kind"] != "delete_modify" {
			t.Fatalf("delete/modify conflict = %v", file)
		}
		if file["replayed"].(map[string]any)["exists"] != true || file["onto"].(map[string]any)["exists"] != false {
			t.Fatalf("delete/modify sides = %v", file)
		}
	})

	t.Run("empty file versus content", func(t *testing.T) {
		f := newConflictFixture(t)
		a4WriteFile(t, f.bin, f.home1, f.repo1, f.env1(), "empty.md", "v1\n")
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 sync: %q", r.stderr)
		}
		if r := f.sync(2); r.code != 0 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		a4WriteFile(t, f.bin, f.home1, f.repo1, f.env1(), "empty.md", "content\n")
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 push: %q", r.stderr)
		}
		// device2 empties the file from the v1 baseline.
		a4WriteFile(t, f.bin, f.home2, f.repo2, f.env2(), "empty.md", "")
		if r := f.sync(2); r.code != 1 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		view := f.conflictView(2)
		files := view["files"].([]any)
		file := files[0].(map[string]any)
		if file["kind"] != "text" {
			t.Fatalf("empty-file conflict kind = %v", file["kind"])
		}
		sides := file["replayed"].(map[string]any)
		if sides["exists"] != true || sides["content"] != "" {
			t.Fatalf("empty side = %v, want exists=true and empty content", sides)
		}
	})

	t.Run("rename conflict is refused", func(t *testing.T) {
		f := newConflictFixture(t)
		a4WriteFile(t, f.bin, f.home1, f.repo1, f.env1(), "notes.md", "v1\n")
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 sync: %q", r.stderr)
		}
		if r := f.sync(2); r.code != 0 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		// Both devices rename the same file differently.
		gitOutput(t, f.env1(), f.repo1, "mv", "notes.md", "a.md")
		gitOutput(t, f.env1(), f.repo1, "commit", "-qam", "rename a")
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 push: %q", r.stderr)
		}
		gitOutput(t, f.env2(), f.repo2, "mv", "notes.md", "b.md")
		gitOutput(t, f.env2(), f.repo2, "commit", "-qam", "rename b")
		if r := f.sync(2); r.code != 1 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		view := f.conflictView(2)
		files := view["files"].([]any)
		unsupported := false
		for _, raw := range files {
			if raw.(map[string]any)["kind"] == "unsupported" {
				unsupported = true
			}
		}
		if !unsupported {
			t.Fatalf("rename conflict kinds = %v, want an unsupported entry", files)
		}
		apply := f.apply(2, view["conflictId"].(string), view["baseRevision"].(string), "x")
		if apply.code != 1 || !strings.Contains(apply.stderr, "UNSUPPORTED") {
			t.Fatalf("rename apply: code=%d stderr=%q, want UNSUPPORTED", apply.code, apply.stderr)
		}
		abort := f.resolve(2, "--abort", "--conflict", view["conflictId"].(string))
		if abort.code != 0 || !strings.Contains(abort.stdout, "remote: pending") {
			t.Fatalf("rename abort: code=%d stdout=%q stderr=%q", abort.code, abort.stdout, abort.stderr)
		}
	})

	t.Run("binary conflict is refused", func(t *testing.T) {
		f := newConflictFixture(t)
		writeBinary(t, filepath.Join(f.repo1, "blob.bin"), []byte{0, 1, 2})
		if r := runMemctl(t, f.bin, f.home1, f.env1(), "sync", "--capture"); r.code != 0 {
			t.Fatalf("device1 capture: %q", r.stderr)
		}
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 push: %q", r.stderr)
		}
		if r := f.sync(2); r.code != 0 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		writeBinary(t, filepath.Join(f.repo1, "blob.bin"), []byte{0, 9, 9})
		if r := runMemctl(t, f.bin, f.home1, f.env1(), "sync", "--capture"); r.code != 0 {
			t.Fatalf("device1 recapture: %q", r.stderr)
		}
		if r := f.sync(1); r.code != 0 {
			t.Fatalf("device1 repush: %q", r.stderr)
		}
		writeBinary(t, filepath.Join(f.repo2, "blob.bin"), []byte{0, 7, 7})
		if r := runMemctl(t, f.bin, f.home2, f.env2(), "sync", "--capture"); r.code != 0 {
			t.Fatalf("device2 capture: %q", r.stderr)
		}
		if r := f.sync(2); r.code != 1 {
			t.Fatalf("device2 sync: %q", r.stderr)
		}
		view := f.conflictView(2)
		files := view["files"].([]any)
		file := files[0].(map[string]any)
		if file["kind"] != "binary" {
			t.Fatalf("binary conflict kind = %v", file["kind"])
		}
		if file["replayed"].(map[string]any)["content"] != nil {
			t.Fatalf("binary side leaked content: %v", file["replayed"])
		}
		apply := f.apply(2, view["conflictId"].(string), view["baseRevision"].(string), "text")
		if apply.code != 1 || !strings.Contains(apply.stderr, "UNSUPPORTED") {
			t.Fatalf("binary apply: code=%d stderr=%q, want UNSUPPORTED", apply.code, apply.stderr)
		}
	})
}

// TestA4ResolveAbortCoversC04 covers aborting an owned replay and re-reading a
// persisted stage from a new process.
func TestA4ResolveAbortCoversC04(t *testing.T) {
	f := newConflictFixture(t)
	f.write(2, "# Memory\n\n## Index\n- two-a\n")
	f.write(2, "# Memory\n\n## Index\n- two-b\n")
	f.write(1, "# Memory\n\n## Index\n- one\n")
	if r := f.sync(1); r.code != 0 {
		t.Fatalf("device1 sync: %q", r.stderr)
	}
	if r := f.sync(2); r.code != 1 {
		t.Fatalf("device2 sync: %q", r.stderr)
	}
	originalHead := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD")

	first := f.conflictView(2)
	if result := f.apply(2, first["conflictId"].(string), first["baseRevision"].(string), "# Memory\n\n## Index\n- one\n"); result.code != 0 {
		t.Fatalf("apply stage 1: %q", result.stderr)
	}
	// A fresh process sees the persisted resolving phase.
	resolving := f.conflictView(2)
	if resolving["phase"] != "resolving" || resolving["expectedHead"] != gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD") {
		t.Fatalf("persisted resolving stage = %v", resolving)
	}

	abort := f.resolve(2, "--abort", "--conflict", resolving["conflictId"].(string))
	if abort.code != 0 || !strings.Contains(abort.stdout, "remote: pending") {
		t.Fatalf("abort: code=%d stdout=%q stderr=%q", abort.code, abort.stdout, abort.stderr)
	}
	if head := gitOutput(t, f.env2(), f.repo2, "rev-parse", "HEAD"); head != originalHead {
		t.Fatalf("abort did not restore the original HEAD: %s -> %s", originalHead, head)
	}
	if status := gitOutput(t, f.env2(), f.repo2, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree not clean after abort: %q", status)
	}
	if result := f.resolve(2, "--json"); result.code != 1 || !strings.Contains(result.stderr, "NOT_FOUND") {
		t.Fatalf("resolve after abort: code=%d stderr=%q, want NOT_FOUND", result.code, result.stderr)
	}
}

func writeBinary(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
