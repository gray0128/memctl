package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file verifies the T144 review hints (stage I step I-4 of
// docs/05-validation-and-delivery.md, design in
// docs/18-agent-memory-review-and-optimization.md sections 4.4.2 and 4.4.3)
// through the real binary: `doctor --json` reports UNCONFIRMED_HYPOTHESIS,
// TOPIC_BLOAT and NEAR_DUPLICATE as warnings with exit code 0, and
// `dream --prepare` carries the same findings, recalls the unexpired
// hypothesis first and adds the hypothesis review task.

type t144Bundle struct {
	Rules struct {
		Tasks []string `json:"tasks"`
	} `json:"rules"`
	Findings []struct {
		Code     string `json:"code"`
		Path     string `json:"path"`
		Line     int    `json:"lineNumber"`
		Severity string `json:"severity"`
	} `json:"findings"`
	RelatedExistingEntries []struct {
		FilePath string `json:"filePath"`
		FactText string `json:"factText"`
		RawLine  string `json:"rawLine"`
	} `json:"relatedExistingEntries"`
}

func TestT144DoctorAndPrepareReviewHintsCoversI4(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	env := h3Env(home)
	project := filepath.Join(t.TempDir(), "proxy")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}

	added := h3Day(-10)
	var long strings.Builder
	long.WriteString("# Archive\n\n")
	for index := 0; index < 151; index++ {
		fmt.Fprintf(&long, "- archived note number %d [source: test; added: %s]\n", index, added)
	}
	files := map[string]string{
		"MEMORY.md": "# Memory\n\n## Index\n- [[notes/net]] — 网络排障\n- [[notes/release]] — 发布\n- [[notes/archive]] — 归档\n",
		"notes/net.md": "# Net\n\n" +
			"- 代理端口是 7890 [source: test; added: " + added + "]\n" +
			"- 花园番茄减产疑似与浇水时间有关 [source: test; added: " + added + "; status: hypothesis]\n" +
			"- 过期的猜测 [source: test; added: " + h3Day(-60) + "; until: " + h3Day(-40) + "; status: hypothesis]\n",
		"notes/release.md": "# Release\n\n" +
			"- the release pipeline publishes binaries from signed tags only [source: test; added: " + added + "]\n" +
			"- the release pipeline publishes binaries from signed tags [source: test; added: " + h3Day(-5) + "]\n",
		"notes/archive.md": long.String(),
	}
	paths := make([]string, 0, len(files))
	for rel, content := range files {
		path := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, rel)
	}
	commitRepoFiles(t, env, repo, "t144 fixture", paths...)

	// 1. doctor: three warnings, no error, exit code 0.
	result := runMemctl(t, bin, home, env, "doctor", "--json")
	if result.code != 0 {
		t.Fatalf("doctor exit code = %d, want 0 (stderr=%q)", result.code, result.stderr)
	}
	report := decodeDoctorReport(t, result.stdout)
	if report.Counts.Error != 0 {
		t.Fatalf("doctor reported errors: %+v", report.Issues)
	}
	for _, want := range []struct{ code, path string }{
		{"UNCONFIRMED_HYPOTHESIS", "notes/net.md"},
		{"TOPIC_BLOAT", "notes/archive.md"},
		{"NEAR_DUPLICATE", "notes/release.md"},
	} {
		if !hasIssue(report, want.code, want.path) {
			t.Fatalf("doctor lacks %s on %s: %+v", want.code, want.path, report.Issues)
		}
	}
	hypotheses := 0
	for _, issue := range report.Issues {
		switch issue.Code {
		case "UNCONFIRMED_HYPOTHESIS", "TOPIC_BLOAT", "NEAR_DUPLICATE":
			if issue.Severity != "warning" || issue.AutoFixable {
				t.Fatalf("issue = %+v, want a non-fixable warning", issue)
			}
		}
		if issue.Code == "UNCONFIRMED_HYPOTHESIS" {
			hypotheses++
			if issue.LineNumber != 4 {
				t.Fatalf("hypothesis issue = %+v, want line 4", issue)
			}
		}
	}
	if hypotheses != 1 {
		t.Fatalf("UNCONFIRMED_HYPOTHESIS count = %d, want 1 (the expired one is hidden)", hypotheses)
	}

	// 2. prepare: same findings, hypothesis recalled first, review task.
	writeH3Digest(t, home, "claude-code", "sess-proxy", project, 0, "代理端口 7890 不通，改用 7891")
	result = runMemctl(t, bin, project, env, "dream", "--prepare")
	if result.code != 0 {
		t.Fatalf("dream --prepare exited %d: %s", result.code, result.stderr)
	}
	var bundle t144Bundle
	if err := json.Unmarshal([]byte(result.stdout), &bundle); err != nil {
		t.Fatalf("decode bundle: %v (%s)", err, result.stdout)
	}
	if len(bundle.RelatedExistingEntries) == 0 || len(bundle.RelatedExistingEntries) > 20 ||
		!strings.Contains(bundle.RelatedExistingEntries[0].FactText, "番茄") {
		t.Fatalf("relatedExistingEntries = %+v, want the hypothesis first", bundle.RelatedExistingEntries)
	}
	for _, entry := range bundle.RelatedExistingEntries {
		if strings.Contains(entry.FactText, "过期的猜测") {
			t.Fatal("an expired hypothesis was recalled")
		}
	}
	if !strings.Contains(strings.Join(bundle.Rules.Tasks, "\n"), "UNCONFIRMED_HYPOTHESIS") {
		t.Fatalf("rules.tasks = %q", bundle.Rules.Tasks)
	}
	seen := map[string]bool{}
	for _, finding := range bundle.Findings {
		switch finding.Code {
		case "UNCONFIRMED_HYPOTHESIS", "TOPIC_BLOAT", "NEAR_DUPLICATE":
			if finding.Severity != "warning" {
				t.Fatalf("finding = %+v, want warning", finding)
			}
			seen[finding.Code+" "+finding.Path] = true
		}
	}
	for _, key := range []string{"UNCONFIRMED_HYPOTHESIS notes/net.md", "TOPIC_BLOAT notes/archive.md", "NEAR_DUPLICATE notes/release.md"} {
		if !seen[key] {
			t.Fatalf("prepare findings lack %s: %+v", key, bundle.Findings)
		}
	}
}
