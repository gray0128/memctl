package integration_test

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/git"
)

// This file verifies the read-only business views of stage B-3 subtask 2
// (T026) through the HTTP handler: the knowledge graph, entry filters, the
// audit diff preview route added in docs/02-domain-models-and-contracts.md
// section 4.2, and the diagnosis report. It reuses the B-1 fixture so every run
// uses a temporary HOME and memory repository.

type graphPayload struct {
	Nodes []struct {
		Path     string `json:"path"`
		Kind     string `json:"kind"`
		EntryNum int    `json:"entryCount"`
		Orphan   bool   `json:"orphan"`
	} `json:"nodes"`
	Edges []struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Exists bool   `json:"exists"`
	} `json:"edges"`
}

type auditDiffPayload struct {
	Hash  string `json:"hash"`
	Files []struct {
		Path      string `json:"path"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Patch     string `json:"patch"`
		Binary    bool   `json:"binary"`
	} `json:"files"`
}

// TestB3GraphAndEntriesCoverViews checks that the snapshot carries everything
// the knowledge-graph view needs (nodes, reachable relations, dead links and
// orphan files) and that every documented entry filter is applied server-side.
func TestB3GraphAndEntriesCoverViews(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first fact [source: agent://laptop/claude/s1; added: 2026-01-02]\n- see [[missing-target]]\n")
	fixture.update(t, "MEMORY.md", "# Memory\n\n## Index\n\n- [[alpha]]\n")
	// orphan.md is never linked from the entry point.
	fixture.write(t, "orphan.md", "# Orphan\n\n- unreachable\n")

	var snapshot struct {
		Graph graphPayload `json:"graph"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/snapshot"), &snapshot)

	orphans := map[string]bool{}
	for _, node := range snapshot.Graph.Nodes {
		orphans[node.Path] = node.Orphan
	}
	if !orphans["orphan.md"] || orphans["alpha.md"] || orphans["MEMORY.md"] {
		t.Fatalf("unexpected orphan flags: %v", orphans)
	}

	deadLinks := 0
	for _, edge := range snapshot.Graph.Edges {
		if !edge.Exists && edge.From == "alpha.md" {
			deadLinks++
		}
	}
	if deadLinks != 1 {
		t.Fatalf("expected one dead link from alpha.md, got %d (%+v)", deadLinks, snapshot.Graph.Edges)
	}

	var byDevice struct {
		Total int `json:"total"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?device=laptop"), &byDevice)
	if byDevice.Total != 1 {
		t.Fatalf("device filter should match one entry, got %d", byDevice.Total)
	}

	var byDate struct {
		Total int `json:"total"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?from=2026-01-01&to=2026-01-03"), &byDate)
	if byDate.Total != 1 {
		t.Fatalf("date filter should match one entry, got %d", byDate.Total)
	}

	var byFile struct {
		Total int `json:"total"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries?file=alpha.md"), &byFile)
	if byFile.Total != 2 {
		t.Fatalf("file filter should match alpha.md entries, got %d", byFile.Total)
	}
}

// TestB3AuditDiffCoversViews checks the read-only diff preview route: a text
// commit returns per-file patches, a binary file reports binary true, and
// malformed or unknown commit ids fail with a bounded error instead of a Git
// failure.
func TestB3AuditDiffCoversViews(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- first fact\n")
	fixture.update(t, "alpha.md", "# Alpha\n\n- first fact\n- second fact\n")

	var audit struct {
		Commits []struct {
			Hash string `json:"hash"`
		} `json:"commits"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1"), &audit)
	if len(audit.Commits) != 1 {
		t.Fatalf("expected one commit, got %+v", audit)
	}
	hash := audit.Commits[0].Hash

	var diff auditDiffPayload
	fixture.decode(t, fixture.get(t, "/api/v1/audit/"+hash+"/diff"), &diff)
	if diff.Hash != hash {
		t.Fatalf("diff hash = %q, want %q", diff.Hash, hash)
	}
	if len(diff.Files) != 1 || diff.Files[0].Path != "alpha.md" {
		t.Fatalf("unexpected diff files: %+v", diff.Files)
	}
	file := diff.Files[0]
	if file.Additions == 0 || file.Deletions != 0 {
		t.Fatalf("expected one added line and no deletions, got %+v", file)
	}
	if !strings.Contains(file.Patch, "second fact") || !strings.Contains(file.Patch, "@@") {
		t.Fatalf("patch must contain the added line:\n%s", file.Patch)
	}

	// A binary change is reported without fabricated text hunks.
	binaryPath := filepath.Join(fixture.repo, "asset.bin")
	if err := os.WriteFile(binaryPath, []byte{0x00, 0x01, 0x02, 0xff}, 0o644); err != nil {
		t.Fatalf("write binary asset: %v", err)
	}
	if err := git.Add(context.Background(), fixture.repo, "asset.bin"); err != nil {
		t.Fatalf("stage binary asset: %v", err)
	}
	if _, err := git.Commit(context.Background(), fixture.repo, "add binary asset"); err != nil {
		t.Fatalf("commit binary asset: %v", err)
	}

	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1"), &audit)
	binaryHash := audit.Commits[0].Hash
	fixture.decode(t, fixture.get(t, "/api/v1/audit/"+binaryHash+"/diff"), &diff)
	if len(diff.Files) != 1 || !diff.Files[0].Binary || diff.Files[0].Patch != "" {
		t.Fatalf("binary file must be flagged without a patch: %+v", diff.Files)
	}

	// An unknown but well-formed commit id is a NotFound, and a non-hex value
	// never reaches Git.
	unknown := fixture.get(t, "/api/v1/audit/0123456789abcdef0123456789abcdef01234567/diff")
	mustStatus(t, unknown.status, http.StatusNotFound)
	mustCode(t, errorCode(t, unknown), "NOT_FOUND")

	// A patch above the documented 4 MiB limit fails in a bounded way instead
	// of buffering an unbounded diff.
	largePath := filepath.Join(fixture.repo, "large.md")
	if err := os.WriteFile(largePath, []byte(strings.Repeat("x", (4<<20)+4096)), 0o644); err != nil {
		t.Fatalf("write large file: %v", err)
	}
	if err := git.Add(context.Background(), fixture.repo, "large.md"); err != nil {
		t.Fatalf("stage large file: %v", err)
	}
	if _, err := git.Commit(context.Background(), fixture.repo, "add large file"); err != nil {
		t.Fatalf("commit large file: %v", err)
	}
	fixture.decode(t, fixture.get(t, "/api/v1/audit?limit=1"), &audit)
	large := fixture.get(t, "/api/v1/audit/"+audit.Commits[0].Hash+"/diff")
	mustStatus(t, large.status, http.StatusRequestEntityTooLarge)
	mustCode(t, errorCode(t, large), "LIMIT_EXCEEDED")

	invalid := fixture.get(t, "/api/v1/audit/--help/diff")
	mustStatus(t, invalid.status, http.StatusBadRequest)
	mustCode(t, errorCode(t, invalid), "INVALID_ARGUMENT")

	// The diff route is read-only: a write verb is rejected even with a valid
	// token when the server runs with --readonly.
	readOnly := newB1Fixture(t, true)
	readOnly.write(t, "alpha.md", "# Alpha\n\n- fact\n")
	write := readOnly.do(t, http.MethodPost,
		"http://127.0.0.1:18420/api/v1/audit/0123456789abcdef0123456789abcdef01234567/diff",
		b1Token, "127.0.0.1:18420", "")
	mustStatus(t, write.status, http.StatusForbidden)
	mustCode(t, errorCode(t, write), "READ_ONLY")
}

// TestB3DoctorCoversViews checks that the diagnosis view receives issues and
// per-severity counts without a health score.
func TestB3DoctorCoversViews(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.write(t, "alpha.md", "# Alpha\n\n- see [[missing-target]]\n")
	fixture.write(t, "orphan.md", "# Orphan\n\n- unreachable\n")

	var report struct {
		Issues []struct {
			Code     string `json:"code"`
			Severity string `json:"severity"`
		} `json:"issues"`
		Counts struct {
			Error   int `json:"error"`
			Warning int `json:"warning"`
			Info    int `json:"info"`
			Total   int `json:"total"`
		} `json:"counts"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/doctor"), &report)

	seen := map[string]string{}
	for _, issue := range report.Issues {
		seen[issue.Code] = issue.Severity
	}
	if seen["MISSING_LINK"] != "error" {
		t.Fatalf("missing link should be an error, got %q", seen["MISSING_LINK"])
	}
	if seen["ORPHAN_FILE"] != "warning" {
		t.Fatalf("orphan file should be a warning, got %q", seen["ORPHAN_FILE"])
	}
	if report.Counts.Error+report.Counts.Warning+report.Counts.Info != report.Counts.Total {
		t.Fatalf("counts must add up to the total: %+v", report.Counts)
	}
}
