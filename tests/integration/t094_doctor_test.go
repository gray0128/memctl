package integration_test

import (
	"net/http"
	"testing"
)

// TestT094DoctorReportsExpiredEntry covers the API side of the EXPIRED_ENTRY
// hint: a repository with an expired time-limited entry reports it once, as
// info, and the diagnostic does not change the repository. See
// docs/10-memory-lifecycle.md section 3.3.
func TestT094DoctorReportsExpiredEntry(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.update(t, "MEMORY.md", t094MemoryContent())

	var report struct {
		Issues []struct {
			Code        string `json:"code"`
			Severity    string `json:"severity"`
			FilePath    string `json:"filePath"`
			LineNumber  int    `json:"lineNumber"`
			AutoFixable bool   `json:"autoFixable"`
		} `json:"issues"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/doctor"), &report)

	expired := 0
	for _, issue := range report.Issues {
		if issue.Code != "EXPIRED_ENTRY" {
			continue
		}
		expired++
		if issue.Severity != "info" {
			t.Fatalf("EXPIRED_ENTRY severity = %q, want info", issue.Severity)
		}
		if issue.AutoFixable {
			t.Fatal("EXPIRED_ENTRY must not be auto-fixable")
		}
		if issue.FilePath != "MEMORY.md" || issue.LineNumber == 0 {
			t.Fatalf("EXPIRED_ENTRY location = %s:%d", issue.FilePath, issue.LineNumber)
		}
	}
	if expired != 1 {
		t.Fatalf("EXPIRED_ENTRY issues = %d, want 1", expired)
	}

	// The diagnostic is read-only: the expired entry stays on disk.
	file := fixture.get(t, "/api/v1/files/MEMORY.md")
	mustStatus(t, file.status, http.StatusOK)
}
