package dream

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/doctor"
)

// This file covers the Dreaming side of the T144 review hints
// (docs/18-agent-memory-review-and-optimization.md sections 4.4.2 and 4.4.3):
// staticFindings reports TOPIC_BLOAT, NEAR_DUPLICATE and
// UNCONFIRMED_HYPOTHESIS with the doctor's predicates, and prepare recalls
// unexpired hypotheses first with a matching rules task.

var t144DiagNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func findingsByCode(findings []staticFinding, code string) []staticFinding {
	var out []staticFinding
	for _, finding := range findings {
		if finding.Code == code {
			out = append(out, finding)
		}
	}
	return out
}

func TestStaticFindingsReportT144ReviewHints(t *testing.T) {
	var long strings.Builder
	long.WriteString("# Long\n\n")
	for index := 0; index < doctor.MaxTopicLines; index++ {
		fmt.Fprintf(&long, "line %d\n", index)
	}
	files := &fileSet{files: map[string][]byte{
		"MEMORY.md": []byte("# Memory\n\n## Index\n- [[notes/a]]\n- [[notes/b]]\n- [[notes/long]]\n"),
		"notes/a.md": []byte("# A\n\n" +
			"- the release pipeline publishes binaries from signed tags only [source: s; added: 2026-11-01]\n" +
			"- 根因疑似 MTU 过小 [source: s; added: 2026-11-01; status: hypothesis]\n" +
			"- 过期猜测 [source: s; added: 2026-09-01; until: 2026-10-01; status: hypothesis]\n"),
		"notes/b.md":    []byte("# B\n\n- the release pipeline publishes binaries from signed tags [source: s; added: 2026-11-05]\n"),
		"notes/long.md": []byte(long.String()),
	}}
	findings := staticFindings(files, t144DiagNow)

	bloat := findingsByCode(findings, doctor.CodeTopicBloat)
	if len(bloat) != 1 || bloat[0].Path != "notes/long.md" {
		t.Fatalf("TOPIC_BLOAT = %+v", bloat)
	}
	near := findingsByCode(findings, doctor.CodeNearDuplicate)
	if len(near) != 1 || near[0].Path != "notes/a.md" || near[0].Line != 3 ||
		len(near[0].Subjects) != 2 || near[0].Subjects[0] != "notes/a.md:3" || near[0].Subjects[1] != "notes/b.md:3" {
		t.Fatalf("NEAR_DUPLICATE = %+v", near)
	}
	hypotheses := findingsByCode(findings, doctor.CodeUnconfirmedHypothesis)
	if len(hypotheses) != 1 || hypotheses[0].Path != "notes/a.md" || hypotheses[0].Line != 4 {
		t.Fatalf("UNCONFIRMED_HYPOTHESIS = %+v", hypotheses)
	}
	for _, code := range []string{doctor.CodeTopicBloat, doctor.CodeNearDuplicate, doctor.CodeUnconfirmedHypothesis} {
		if findingSeverity(code) != "warning" {
			t.Fatalf("%s severity = %q, want warning", code, findingSeverity(code))
		}
	}
}

func TestRelatedExistingEntriesListHypothesesFirst(t *testing.T) {
	notes := "# Cache\n\n" +
		"- redis eviction policy is allkeys-lru [source: test; added: 2026-10-01]\n" +
		"- 花园浇水时间疑似影响番茄 [source: test; added: 2026-10-01; status: hypothesis]\n" +
		"- 过期的猜测 gardening [source: test; added: 2026-09-01; until: 2026-10-01; status: hypothesis]\n"
	base := &baseline{files: map[string]baselineFile{
		"MEMORY.md":      {content: []byte("# Memory\n\n## Index\n- [[notes/cache]]\n"), revision: "r1"},
		"notes/cache.md": {content: []byte(notes), revision: "r2"},
	}}
	sessions := []BundleSession{{CwdBase: "api", Turns: []corpus.Turn{{User: "redis eviction policy broke", Assistant: "set eviction policy to allkeys-lru"}}}}
	related := relatedExistingEntries(base, sessions, t144DiagNow)
	if len(related) != 2 || !strings.Contains(related[0].FactText, "番茄") || !strings.Contains(related[1].FactText, "redis") {
		t.Fatalf("related = %+v, want the unrelated hypothesis first and the expired one left out", related)
	}
	if got := relatedExistingEntries(base, nil, t144DiagNow); got != nil {
		t.Fatalf("a bundle without sessions recalled %d entries", len(got))
	}

	var many strings.Builder
	many.WriteString("# Many\n\n- redis eviction policy is allkeys-lru [source: test; added: 2026-10-01]\n")
	for index := 0; index < MaxRelatedExistingEntries+5; index++ {
		fmt.Fprintf(&many, "- hypothesis number %d about cold storage [source: test; added: 2026-10-01; status: hypothesis]\n", index)
	}
	base.files["notes/cache.md"] = baselineFile{content: []byte(many.String()), revision: "r3"}
	related = relatedExistingEntries(base, sessions, t144DiagNow)
	if len(related) != MaxRelatedExistingEntries {
		t.Fatalf("related = %d, want the cap %d", len(related), MaxRelatedExistingEntries)
	}
	for _, entry := range related {
		if !strings.Contains(entry.RawLine, "status: hypothesis") {
			t.Fatalf("a non-hypothesis entry took a slot ahead of hypotheses: %+v", entry)
		}
	}
	if count := len(hypothesisEntries(base, t144DiagNow)); count != MaxRelatedExistingEntries+5 {
		t.Fatalf("hypothesisEntries = %d", count)
	}
}

func TestPrepareRecallsHypothesesAndAddsTask(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	writeFile(t, filepath.Join(repo, "MEMORY.md"), "# Memory\n\n## Index\n- [[notes/net]] — 网络排障\n")
	writeFile(t, filepath.Join(repo, "notes", "net.md"), "# Net\n\n"+
		"- 代理端口是 7890 [source: test; added: 2026-10-01]\n"+
		"- 丢包根因疑似 MTU 过小 [source: test; added: 2026-10-01; status: hypothesis]\n")
	env := gitEnv(t)
	gitRun(t, env, repo, "add", "--", "MEMORY.md", "notes/net.md")
	gitRun(t, env, repo, "commit", "-q", "-m", "add net notes")
	service := newTestService(t, repo, t.TempDir())
	service.Now = func() time.Time { return t144DiagNow }
	project := filepath.Join(t.TempDir(), "proxy")
	digest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         "claude-code",
		SessionID:     "sess-net",
		SourceTag:     "agent://device-1/claude-code/sess-net",
		Date:          "2026-11-11",
		UpdatedAt:     "2026-11-11T10:00:00Z",
		CwdBase:       corpus.CwdBase(project),
		CwdKey:        corpus.CwdKey(project),
		Turns:         []corpus.Turn{{User: "代理端口 7890 不通", Assistant: "改用 7891 后恢复"}},
	}
	if err := service.corpusCache().Save(digest); err != nil {
		t.Fatalf("save digest: %v", err)
	}
	bundle, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: project})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	if len(bundle.RelatedExistingEntries) == 0 || !strings.Contains(bundle.RelatedExistingEntries[0].FactText, "MTU") {
		t.Fatalf("relatedExistingEntries = %+v, want the hypothesis first", bundle.RelatedExistingEntries)
	}
	joined := strings.Join(bundle.Rules.Tasks, "\n")
	if !strings.Contains(joined, doctor.CodeUnconfirmedHypothesis) || !strings.Contains(joined, "1 unexpired") {
		t.Fatalf("rules.tasks = %q", bundle.Rules.Tasks)
	}
	found := false
	for _, finding := range bundle.Findings {
		if finding.Code == doctor.CodeUnconfirmedHypothesis && finding.Path == "notes/net.md" && finding.Line == 4 && finding.Severity == "warning" {
			found = true
		}
	}
	if !found {
		t.Fatalf("findings = %+v", bundle.Findings)
	}
	if got := prepareRuleTasks(&baseline{files: map[string]baselineFile{}}, t144DiagNow); len(got) != len(prepareTasks) {
		t.Fatalf("a baseline without hypotheses got %d tasks", len(got))
	}
}
