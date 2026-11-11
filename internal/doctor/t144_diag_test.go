package doctor_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/vault"
)

// This file covers the T144 review hints of
// docs/18-agent-memory-review-and-optimization.md sections 4.4.2 and 4.4.3:
// UNCONFIRMED_HYPOTHESIS, TOPIC_BLOAT and NEAR_DUPLICATE are warnings that
// never turn a healthy repository into an error report.

var t144Now = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func issuesWithCode(report doctor.Report, code string) []doctor.Issue {
	var out []doctor.Issue
	for _, issue := range report.Issues {
		if issue.Code == code {
			out = append(out, issue)
		}
	}
	return out
}

func checkT144(t *testing.T, dir string) doctor.Report {
	t.Helper()
	report, err := doctor.CheckAt(context.Background(), dir, func() time.Time { return t144Now })
	if err != nil {
		t.Fatalf("CheckAt: %v", err)
	}
	return report
}

func TestUnconfirmedHypothesisWarnsOnlyForUnexpiredHypotheses(t *testing.T) {
	dir := newDoctorRepo(t)
	writeRepoFile(t, dir, "MEMORY.md", "# Memory\n\n## Index\n- [[notes/net]]\n")
	writeRepoFile(t, dir, "notes/net.md", "# Net\n\n"+
		"- 根因疑似 MTU 过小 [source: s; added: 2026-11-01; status: hypothesis]\n"+
		"- 根因已确认是 DNS 劫持 [source: s; added: 2026-11-01; status: verified]\n"+
		"- 旧猜测已过期 [source: s; added: 2026-09-01; until: 2026-10-01; status: hypothesis]\n"+
		"- 有期限的猜测 [source: s; added: 2026-11-01; until: 2026-12-01; status: hypothesis]\n"+
		"- 未知状态值 [source: s; added: 2026-11-01; status: maybe]\n")
	report := checkT144(t, dir)
	issues := issuesWithCode(report, doctor.CodeUnconfirmedHypothesis)
	if len(issues) != 2 || issues[0].LineNumber != 3 || issues[1].LineNumber != 6 {
		t.Fatalf("UNCONFIRMED_HYPOTHESIS = %+v, want lines 3 and 6", issues)
	}
	for _, issue := range issues {
		if issue.Severity != doctor.SeverityWarning || issue.FilePath != "notes/net.md" || issue.Suggestion == "" || issue.AutoFixable {
			t.Fatalf("issue = %+v", issue)
		}
	}
	if report.Counts.Error != 0 {
		t.Fatalf("a hypothesis produced an error: %+v", report.Issues)
	}
	if doctor.AutoFixable(doctor.CodeUnconfirmedHypothesis) {
		t.Fatal("UNCONFIRMED_HYPOTHESIS must not be auto-fixable")
	}
}

func TestTopicBloatBoundsLinesAndBytes(t *testing.T) {
	dir := newDoctorRepo(t)
	writeRepoFile(t, dir, "MEMORY.md", "# Memory\n\n## Index\n- [[notes/long]]\n- [[notes/wide]]\n- [[notes/edge]]\n")

	var long strings.Builder
	for line := 0; line < doctor.MaxTopicLines+1; line++ {
		fmt.Fprintf(&long, "line %d\n", line)
	}
	writeRepoFile(t, dir, "notes/long.md", long.String())
	writeRepoFile(t, dir, "notes/wide.md", "# Wide\n\n"+strings.Repeat("x", doctor.MaxTopicBytes)+"\n")
	// Exactly at both bounds is not bloat.
	edge := strings.Repeat("y\n", doctor.MaxTopicLines)
	writeRepoFile(t, dir, "notes/edge.md", edge)

	report := checkT144(t, dir)
	issues := issuesWithCode(report, doctor.CodeTopicBloat)
	if len(issues) != 2 || issues[0].FilePath != "notes/long.md" || issues[1].FilePath != "notes/wide.md" {
		t.Fatalf("TOPIC_BLOAT = %+v", issues)
	}
	for _, issue := range issues {
		if issue.Severity != doctor.SeverityWarning || issue.Suggestion == "" {
			t.Fatalf("issue = %+v", issue)
		}
	}
	if report.Counts.Error != 0 {
		t.Fatalf("topic bloat produced an error: %+v", report.Issues)
	}
	if doctor.TopicBloat(vault.EntryPointName, 1000, 1<<20) {
		t.Fatal("MEMORY.md is measured by ENTRY_BLOAT, not TOPIC_BLOAT")
	}
	if doctor.TopicBloat("scripts/run.sh", 1000, 1<<20) {
		t.Fatal("a script is not a topic file")
	}
}

func TestNearDuplicateReportsSimilarBusinessEntries(t *testing.T) {
	dir := newDoctorRepo(t)
	writeRepoFile(t, dir, "MEMORY.md", "# Memory\n\n## Index\n"+
		"- [[notes/a]] release process uses signed tags pushed to origin main\n"+
		"- [[notes/b]] release process uses signed tags pushed to origin main\n\n"+
		"## Working preferences\n\n"+
		"- always run go vet before pushing a release tag to origin [source: s; added: 2026-11-01]\n")
	writeRepoFile(t, dir, "notes/a.md", "# A\n\n"+
		"- the release pipeline publishes binaries from signed tags only [source: s; added: 2026-11-01]\n"+
		"- short fact [source: s; added: 2026-11-01]\n"+
		"- short fact [source: s; added: 2026-11-02]\n"+
		"- expired release pipeline publishes binaries from signed tags only [source: s; added: 2026-09-01; until: 2026-10-01]\n"+
		"- released version one point two to the npm registry today [source: s; added: 2026-10-01; kind: event]\n")
	writeRepoFile(t, dir, "notes/b.md", "# B\n\n"+
		"- the release pipeline publishes binaries from signed tags [source: s; added: 2026-11-05]\n"+
		"- released version one point three to the npm registry today [source: s; added: 2026-10-08; kind: event]\n"+
		"- unrelated gardening note about tomatoes and basil [source: s; added: 2026-11-01]\n")

	report := checkT144(t, dir)
	issues := issuesWithCode(report, doctor.CodeNearDuplicate)
	if len(issues) != 1 {
		t.Fatalf("NEAR_DUPLICATE = %+v, want exactly the pipeline pair", issues)
	}
	issue := issues[0]
	if issue.FilePath != "notes/a.md" || issue.LineNumber != 3 || issue.Severity != doctor.SeverityWarning ||
		!strings.Contains(issue.Message, "notes/b.md:3") || issue.Suggestion == "" {
		t.Fatalf("issue = %+v", issue)
	}
	if report.Counts.Error != 0 {
		t.Fatalf("near duplicates produced an error: %+v", report.Issues)
	}
}

func TestNearDuplicatesAreStableAndBounded(t *testing.T) {
	var notes strings.Builder
	notes.WriteString("# Cache\n\n")
	// 12 identical-shape facts yield 66 pairs, above the listing limit.
	for index := 0; index < 12; index++ {
		fmt.Fprintf(&notes, "- redis eviction policy is allkeys lru on cluster nodes [source: s; added: 2026-11-%02d]\n", index+1)
	}
	file := vault.ParseFile("notes/cache.md", "", []byte(notes.String()))
	first, total := doctor.NearDuplicates([]vault.ParsedFile{file}, t144Now)
	if total != 66 || len(first) != doctor.MaxNearDuplicatePairs {
		t.Fatalf("pairs = %d of %d, want %d of 66", len(first), total, doctor.MaxNearDuplicatePairs)
	}
	for index, pair := range first {
		if pair.Left.LineNumber >= pair.Right.LineNumber || pair.Similarity < doctor.NearDuplicateThreshold {
			t.Fatalf("pair %d = %+v", index, pair)
		}
		if index > 0 {
			prev := first[index-1]
			if prev.Left.LineNumber > pair.Left.LineNumber ||
				(prev.Left.LineNumber == pair.Left.LineNumber && prev.Right.LineNumber >= pair.Right.LineNumber) {
				t.Fatalf("pairs are not in stable order at %d", index)
			}
		}
	}
	second, _ := doctor.NearDuplicates([]vault.ParsedFile{file}, t144Now)
	for index := range first {
		if first[index].Left.LineNumber != second[index].Left.LineNumber || first[index].Right.LineNumber != second[index].Right.LineNumber {
			t.Fatal("two runs listed different pairs")
		}
	}

	dir := newDoctorRepo(t)
	writeRepoFile(t, dir, "MEMORY.md", "# Memory\n\n## Index\n- [[notes/cache]]\n")
	writeRepoFile(t, dir, "notes/cache.md", notes.String())
	issues := issuesWithCode(checkT144(t, dir), doctor.CodeNearDuplicate)
	if len(issues) != doctor.MaxNearDuplicatePairs+1 {
		t.Fatalf("NEAR_DUPLICATE issues = %d, want the limit plus one summary", len(issues))
	}
	summary := issues[0] // the summary has no file path and sorts first
	if summary.FilePath != "" || !strings.Contains(summary.Message, "16 more") {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestT144HintsKeepHealthyRepositoryErrorFree(t *testing.T) {
	dir := newDoctorRepo(t)
	writeRepoFile(t, dir, "MEMORY.md", "# Memory\n\n## Index\n- [[notes/a]]\n")
	writeRepoFile(t, dir, "notes/a.md", "# A\n\n- 一条普通事实 [source: s; added: 2026-11-01]\n")
	report := checkT144(t, dir)
	for _, code := range []string{doctor.CodeUnconfirmedHypothesis, doctor.CodeTopicBloat, doctor.CodeNearDuplicate} {
		if got := issuesWithCode(report, code); len(got) != 0 {
			t.Fatalf("%s reported on a clean repository: %+v", code, got)
		}
	}
}
