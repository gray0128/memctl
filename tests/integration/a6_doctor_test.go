package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// This file verifies stage A-6 (T020) through the CLI binary: F03 (orphan
// reachability) and R04 (doctor report, exit codes and read-only behavior).
// Every run uses a temporary HOME and memory repository with background
// synchronization disabled, so no network or real credential source is touched.

type doctorReport struct {
	Revision  string `json:"revision"`
	CheckedAt string `json:"checkedAt"`
	Issues    []struct {
		Code        string `json:"code"`
		Severity    string `json:"severity"`
		FilePath    string `json:"filePath"`
		LineNumber  int    `json:"lineNumber"`
		Message     string `json:"message"`
		Suggestion  string `json:"suggestion"`
		AutoFixable bool   `json:"autoFixable"`
	} `json:"issues"`
	Counts struct {
		Error   int `json:"error"`
		Warning int `json:"warning"`
		Info    int `json:"info"`
		Total   int `json:"total"`
	} `json:"counts"`
}

func decodeDoctorReport(t *testing.T, stdout string) doctorReport {
	t.Helper()
	var report doctorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("doctor JSON is invalid: %v\n%s", err, stdout)
	}
	return report
}

func hasIssue(report doctorReport, code, filePath string) bool {
	for _, issue := range report.Issues {
		if issue.Code == code && issue.FilePath == filePath {
			return true
		}
	}
	return false
}

// commitRepoFiles stages and commits the given paths so the diagnostic subject
// is a stable Git revision.
func commitRepoFiles(t *testing.T, env []string, repo string, message string, paths ...string) {
	t.Helper()
	args := append([]string{"add", "--"}, paths...)
	gitOutput(t, env, repo, args...)
	gitOutput(t, env, repo, "commit", "-q", "-m", message)
}

// repoStateSnapshot captures the parts R04 says the read-only check must not
// change.
func repoStateSnapshot(t *testing.T, env []string, repo string) map[string]string {
	t.Helper()
	return map[string]string{
		"head":   gitOutput(t, env, repo, "rev-parse", "HEAD"),
		"status": gitOutput(t, env, repo, "status", "--porcelain"),
		"memory": mustReadFile(t, filepath.Join(repo, "MEMORY.md")),
		"topic":  mustReadFile(t, filepath.Join(repo, "topic.md")),
		"orphan": mustReadFile(t, filepath.Join(repo, "orphan.md")),
	}
}

// TestA6DoctorReadOnlyDiagnosisCoversR04 verifies the report shape, the
// severity-based exit code and that a read-only check leaves the files and the
// Git state unchanged.
func TestA6DoctorReadOnlyDiagnosisCoversR04(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Memory\n\n## Index\n- [[topic]]\n- [[gone]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "topic.md"), []byte("# Topic\n\n- a fact without provenance\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "orphan.md"), []byte("# Orphan\n\n- disconnected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitRepoFiles(t, env, repo, "diagnostic subject", "MEMORY.md", "topic.md", "orphan.md")

	before := repoStateSnapshot(t, env, repo)

	errorResult := runMemctl(t, bin, home, env, "doctor", "--json")
	if errorResult.code != 1 {
		t.Fatalf("doctor with an error-level issue exit code = %d, want 1 (stdout=%q stderr=%q)", errorResult.code, errorResult.stdout, errorResult.stderr)
	}
	errorReport := decodeDoctorReport(t, errorResult.stdout)
	if errorReport.Counts.Error == 0 {
		t.Fatalf("expected an error-level count, got %+v", errorReport.Counts)
	}
	if errorReport.Counts.Total != len(errorReport.Issues) {
		t.Fatalf("total %d does not match %d issues", errorReport.Counts.Total, len(errorReport.Issues))
	}
	if sum := errorReport.Counts.Error + errorReport.Counts.Warning + errorReport.Counts.Info; sum != errorReport.Counts.Total {
		t.Fatalf("severity counts %d do not sum to total %d", sum, errorReport.Counts.Total)
	}
	if !hasIssue(errorReport, "MISSING_LINK", "MEMORY.md") {
		t.Fatalf("dead link not reported with a path: %+v", errorReport.Issues)
	}
	for _, issue := range errorReport.Issues {
		if issue.Code == "MISSING_LINK" && issue.LineNumber != 5 {
			t.Fatalf("dead link line = %d, want 5", issue.LineNumber)
		}
		// A-6 has no deterministic repairs, so autoFixable must stay false.
		if issue.AutoFixable {
			t.Fatalf("A-6 diagnosis must not claim a fixable issue: %+v", issue)
		}
	}
	if !strings.HasPrefix(errorReport.Revision, "sha256:") || errorReport.CheckedAt == "" {
		t.Fatalf("report lacks revision/checkedAt: %+v", errorReport)
	}

	plain := runMemctl(t, bin, home, env, "doctor")
	if plain.code != 1 {
		t.Fatalf("plain doctor exit code = %d, want 1 (stderr=%q)", plain.code, plain.stderr)
	}
	if !strings.Contains(plain.stdout, "MISSING_LINK") || !strings.Contains(plain.stdout, "error:") {
		t.Fatalf("plain doctor output lacks the diagnosis summary:\n%s", plain.stdout)
	}

	if after := repoStateSnapshot(t, env, repo); !reflect.DeepEqual(before, after) {
		t.Fatalf("doctor changed the repository:\nbefore=%v\nafter=%v", before, after)
	}

	// With no error-level issue, warnings and info still return 0.
	if err := os.WriteFile(filepath.Join(repo, "MEMORY.md"), []byte("# Memory\n\n## Index\n- [[topic]]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitRepoFiles(t, env, repo, "remove dead link", "MEMORY.md")

	cleanResult := runMemctl(t, bin, home, env, "doctor", "--json")
	if cleanResult.code != 0 {
		t.Fatalf("doctor with only warnings/info exit code = %d, want 0 (stderr=%q)", cleanResult.code, cleanResult.stderr)
	}
	cleanReport := decodeDoctorReport(t, cleanResult.stdout)
	if cleanReport.Counts.Error != 0 {
		t.Fatalf("expected no error-level issue, got %+v", cleanReport.Counts)
	}
	if cleanReport.Counts.Warning == 0 {
		t.Fatalf("expected the disconnected file to warn, got %+v", cleanReport.Counts)
	}
	if !hasIssue(cleanReport, "ORPHAN_FILE", "orphan.md") {
		t.Fatalf("orphan file not reported: %+v", cleanReport.Issues)
	}
}

// TestA6DoctorOrphanRingCoversF03 verifies that a ring disconnected from the
// entry point is flagged while an indirectly referenced file is not.
func TestA6DoctorOrphanRingCoversF03(t *testing.T) {
	bin := buildMemctlBinary(t)
	root := t.TempDir()
	home, repo, env := a5LocalRepo(t, bin, root, "home")

	files := map[string]string{
		"MEMORY.md": "# Memory\n\n## Index\n- [[topic]]\n",
		"topic.md":  "# Topic\n\n- see [[leaf]]\n",
		"leaf.md":   "# Leaf\n\n- reached only through topic\n",
		"ring1.md":  "# Ring one\n\n- [[ring2]]\n",
		"ring2.md":  "# Ring two\n\n- [[ring1]]\n",
	}
	paths := make([]string, 0, len(files))
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(repo, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, rel)
	}
	commitRepoFiles(t, env, repo, "ring fixture", paths...)

	result := runMemctl(t, bin, home, env, "doctor", "--json")
	report := decodeDoctorReport(t, result.stdout)
	if result.code != 0 {
		t.Fatalf("doctor exit code = %d, want 0 (stderr=%q)", result.code, result.stderr)
	}
	for _, rel := range []string{"ring1.md", "ring2.md"} {
		if !hasIssue(report, "ORPHAN_FILE", rel) {
			t.Fatalf("ring file %s was not reported as orphan: %+v", rel, report.Issues)
		}
	}
	for _, rel := range []string{"topic.md", "leaf.md", "MEMORY.md"} {
		if hasIssue(report, "ORPHAN_FILE", rel) {
			t.Fatalf("reachable file %s was wrongly reported as orphan", rel)
		}
	}
	if report.Counts.Error != 0 {
		t.Fatalf("an orphan must be a warning, got %+v", report.Counts)
	}
}
