package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
)

// This file verifies the local interactive Dreaming path of stage H-3 through
// the real binary: D07 requires that `dream --prepare` succeeds on a repository
// with no remote and no managed workflow, that it ranks the current project
// first and filters by --project, that `--propose --stdin` assembles the real
// source tags and refuses a files[] that lacks the assembled line, and that
// `--apply` marks the cited digests as consumed. D08 requires that a digest
// changed after the proposal is built is skipped and reported. No model endpoint
// and no GitHub Actions run is involved.

// h3Home builds an isolated HOME with a local-only memory repository at
// $HOME/memory and one unconsumed digest per host session.
func h3Home(t *testing.T) (home, repo string) {
	t.Helper()
	bin := buildMemctlBinary(t)
	home = t.TempDir()
	repo = filepath.Join(home, "memory")
	env := a2Env(home, repo, true)
	if result := runMemctl(t, bin, home, env, "init"); result.code != 0 {
		t.Fatalf("init exited %d: %s", result.code, result.stderr)
	}
	return home, repo
}

// h3Env is the isolated child environment of the H-3 flows.
func h3Env(home string) []string {
	return a2Env(home, filepath.Join(home, "memory"), true)
}

// writeH3Digest seeds one digest into the repository cache.
func writeH3Digest(t *testing.T, home, agent, sid, cwd, date, user string) corpus.Digest {
	t.Helper()
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(filepath.Join(home, "memory")))
	digest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         agent,
		SessionID:     sid,
		SourceTag:     "agent://" + deviceName() + "/" + agent + "/" + sid,
		Date:          date,
		UpdatedAt:     date + "T10:00:00Z",
		CwdBase:       corpus.CwdBase(cwd),
		CwdKey:        corpus.CwdKey(cwd),
		Turns:         []corpus.Turn{{User: user, Assistant: "明白了。"}},
	}
	if err := cache.Save(digest); err != nil {
		t.Fatalf("save digest: %v", err)
	}
	return digest
}

// h3Bundle is the prepare output this file inspects.
type h3Bundle struct {
	Today               string `json:"today"`
	BaseHead            string `json:"baseHead"`
	BaseRevision        string `json:"baseRevision"`
	RemainingUnconsumed int    `json:"remainingUnconsumed"`
	RedactionNotice     string `json:"redactionNotice"`
	ProjectFilter       struct {
		CwdBase string `json:"cwdBase"`
		CwdKey  string `json:"cwdKey"`
	} `json:"projectFilter"`
	Rules struct {
		EntryPointHintLines int `json:"entryPointHintLines"`
		DeletionGraceDays   int `json:"deletionGraceDays"`
	} `json:"rules"`
	Findings []struct {
		Code string `json:"code"`
		Path string `json:"path"`
	} `json:"findings"`
	Files []struct {
		Path     string `json:"path"`
		Revision string `json:"revision"`
		Content  string `json:"content"`
	} `json:"files"`
	Sessions []struct {
		SessionIndex int    `json:"sessionIndex"`
		Agent        string `json:"agent"`
		SessionID    string `json:"sessionId"`
		SourceTag    string `json:"sourceTag"`
		Date         string `json:"date"`
		CwdBase      string `json:"cwdBase"`
	} `json:"sessions"`
}

// runPrepare runs `dream --prepare` in the project directory and returns the
// decoded bundle.
func runPrepare(t *testing.T, bin, home, dir string, args ...string) h3Bundle {
	t.Helper()
	full := append([]string{"dream", "--prepare"}, args...)
	result := runMemctl(t, bin, dir, h3Env(home), full...)
	if result.code != 0 {
		t.Fatalf("dream --prepare exited %d: %s", result.code, result.stderr)
	}
	var bundle h3Bundle
	if err := json.Unmarshal([]byte(result.stdout), &bundle); err != nil {
		t.Fatalf("decode prepare bundle: %v (%s)", err, result.stdout)
	}
	return bundle
}

// fileContent returns one prepared file's content.
func (b h3Bundle) fileContent(path string) string {
	for _, file := range b.Files {
		if file.Path == path {
			return file.Content
		}
	}
	return ""
}

// TestH3PrepareWorksWithoutRemoteCoversD07 covers the documented local-only
// precondition: no remote, no .github/workflows/memctl-dream.yml, prepare still
// returns a baseline, the memory files, the sessions and the rules.
func TestH3PrepareWorksWithoutRemoteCoversD07(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project := filepath.Join(t.TempDir(), "memctl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}
	writeH3Digest(t, home, "claude-code", "sess-a", project, "2026-10-20", "记住：发布只推标签")
	writeH3Digest(t, home, "codex", "sess-b", project, "2026-10-19", "不要用 make release 发布")

	if _, err := os.Stat(filepath.Join(repo, ".github")); err == nil {
		t.Fatal("the fixture unexpectedly has a .github directory")
	}
	if out := runMemctl(t, bin, repo, h3Env(home), "remote"); out.code == 0 && strings.TrimSpace(out.stdout) != "" {
		t.Fatalf("the fixture unexpectedly has a remote: %s", out.stdout)
	}

	bundle := runPrepare(t, bin, home, repo)
	if bundle.BaseHead == "" || bundle.BaseRevision == "" || bundle.Today == "" {
		t.Fatalf("bundle = %+v", bundle)
	}
	if bundle.ProjectFilter.CwdBase != filepath.Base(repo) || bundle.ProjectFilter.CwdKey != corpus.CwdKey(config.Canonical(repo)) {
		t.Fatalf("project filter = %+v", bundle.ProjectFilter)
	}
	if bundle.Rules.EntryPointHintLines != 100 || bundle.Rules.DeletionGraceDays != 30 {
		t.Fatalf("rules = %+v", bundle.Rules)
	}
	if bundle.RedactionNotice == "" {
		t.Fatal("the bundle has no redaction notice")
	}
	if bundle.fileContent("MEMORY.md") == "" {
		t.Fatalf("files = %+v, want MEMORY.md", bundle.Files)
	}
	// The project's two sessions are what the default ranking returns; the
	// repository's own working directory has no digest of its own.
	if len(bundle.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want the two project sessions", bundle.Sessions)
	}
	if bundle.Sessions[0].SessionIndex != 0 || bundle.Sessions[1].SessionIndex != 1 {
		t.Fatalf("session indices = %d, %d", bundle.Sessions[0].SessionIndex, bundle.Sessions[1].SessionIndex)
	}
	if bundle.Sessions[0].SourceTag == "" || bundle.Sessions[0].Date == "" {
		t.Fatalf("session metadata = %+v", bundle.Sessions[0])
	}
	if bundle.RemainingUnconsumed != 0 {
		t.Fatalf("remainingUnconsumed = %d", bundle.RemainingUnconsumed)
	}
}

// TestH3PrepareProjectFilterAndAmbiguityCoversD07 covers the documented filter
// key: a cwdBase selects one project, a full cwdKey selects one clone, and two
// clones sharing a basename are refused with their candidates.
func TestH3PrepareProjectFilterAndAmbiguityCoversD07(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	root := t.TempDir()
	first := filepath.Join(root, "a", "memctl")
	second := filepath.Join(root, "b", "memctl")
	other := filepath.Join(root, "c", "other")
	for _, dir := range []string{first, second, other} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	writeH3Digest(t, home, "claude-code", "clone-a", first, "2026-10-20", "clone a")
	writeH3Digest(t, home, "claude-code", "clone-b", second, "2026-10-20", "clone b")
	writeH3Digest(t, home, "claude-code", "other-1", other, "2026-10-19", "other project")

	ambiguous := runMemctl(t, bin, repo, h3Env(home), "dream", "--prepare", "--project", "memctl")
	if ambiguous.code == 0 {
		t.Fatalf("ambiguous project accepted: %s", ambiguous.stdout)
	}
	if !strings.Contains(ambiguous.stderr, "INVALID_ARGUMENT") {
		t.Fatalf("stderr = %s, want INVALID_ARGUMENT", ambiguous.stderr)
	}
	for _, key := range []string{corpus.CwdKey(first), corpus.CwdKey(second)} {
		if !strings.Contains(ambiguous.stderr, key) {
			t.Fatalf("stderr %q does not list the candidate %s", ambiguous.stderr, key)
		}
	}

	byKey := runPrepare(t, bin, home, repo, "--project", corpus.CwdKey(first))
	if len(byKey.Sessions) != 1 || byKey.Sessions[0].CwdBase != "memctl" {
		t.Fatalf("cwdKey filter sessions = %+v", byKey.Sessions)
	}

	// --project with a basename works when it is unique.
	unique := runPrepare(t, bin, home, repo, "--project", "other")
	if len(unique.Sessions) != 1 || unique.Sessions[0].CwdBase != "other" {
		t.Fatalf("unique basename filter = %+v", unique.Sessions)
	}
}

// TestH3ProposeApplyConsumesSessionsCoversD07 covers the full CLI loop:
// prepare, propose, apply. The assembled line carries both source tags and the
// newest date, the proposal is a local-agent ready record, and apply marks both
// cited digests as consumed.
func TestH3ProposeApplyConsumesSessionsCoversD07(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project := filepath.Join(t.TempDir(), "memctl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}
	writeH3Digest(t, home, "claude-code", "sess-a", project, "2026-10-18", "记住：发布只推标签")
	writeH3Digest(t, home, "codex", "sess-b", project, "2026-10-20", "不要用 make release 发布")
	// The managed write never creates directory structure while holding the
	// repository lock, so a new topic file needs its parent directory to exist.
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("create notes: %v", err)
	}

	bundle := runPrepare(t, bin, home, project)
	if len(bundle.Sessions) != 2 {
		t.Fatalf("sessions = %+v", bundle.Sessions)
	}
	fact := "发布只推标签，禁止在本地运行 make release 上传产物"
	tags := []string{bundle.Sessions[0].SourceTag, bundle.Sessions[1].SourceTag}
	line := "- " + fact + " [source: " + strings.Join(tags, "; source: ") + "; added: 2026-10-20]"

	proposalInput := map[string]any{
		"baseHead":     bundle.BaseHead,
		"baseRevision": bundle.BaseRevision,
		"newMemories": []map[string]any{{
			"kind":           "state",
			"targetPath":     "notes/release.md",
			"fact":           fact,
			"sessionIndices": []int{0, 1},
			"rationale":      "用户在两个会话中纠正了发布流程",
		}},
		"files": []map[string]any{
			{
				"path":    "MEMORY.md",
				"action":  "update",
				"content": bundle.fileContent("MEMORY.md") + "- [[notes/release]] — 发布规则\n",
			},
			{
				"path":    "notes/release.md",
				"action":  "create",
				"content": "# Release\n\n## 规则\n" + line + "\n",
			},
		},
	}
	encoded, err := json.Marshal(proposalInput)
	if err != nil {
		t.Fatalf("encode proposal: %v", err)
	}
	proposed := runMemctlStdin(t, bin, repo, h3Env(home), string(encoded), "dream", "--propose", "--stdin")
	if proposed.code != 0 {
		t.Fatalf("dream --propose exited %d: %s", proposed.code, proposed.stderr)
	}
	var proposal struct {
		ProposalID           string `json:"proposalId"`
		Status               string `json:"status"`
		Runner               string `json:"runner"`
		PatchHash            string `json:"patchHash"`
		SessionsScanned      int    `json:"sessionsScanned"`
		NewMemoriesExtracted int    `json:"newMemoriesExtracted"`
		Changes              []struct {
			Path   string `json:"path"`
			Action string `json:"action"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(proposed.stdout), &proposal); err != nil {
		t.Fatalf("decode proposal: %v (%s)", err, proposed.stdout)
	}
	if proposal.Status != "ready" || proposal.Runner != "local-agent" || proposal.PatchHash == "" {
		t.Fatalf("proposal = %+v", proposal)
	}
	if proposal.SessionsScanned != 2 || proposal.NewMemoriesExtracted != 1 {
		t.Fatalf("counters = %d/%d", proposal.SessionsScanned, proposal.NewMemoriesExtracted)
	}
	if len(proposal.Changes) != 2 {
		t.Fatalf("changes = %+v", proposal.Changes)
	}

	// `--list` returns the persisted proposal.
	listed := runMemctl(t, bin, repo, h3Env(home), "dream", "--list")
	if listed.code != 0 {
		t.Fatalf("dream --list exited %d: %s", listed.code, listed.stderr)
	}
	var list struct {
		Proposals []struct {
			ProposalID           string `json:"proposalId"`
			Runner               string `json:"runner"`
			SessionsScanned      int    `json:"sessionsScanned"`
			NewMemoriesExtracted int    `json:"newMemoriesExtracted"`
		} `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &list); err != nil {
		t.Fatalf("decode list: %v (%s)", err, listed.stdout)
	}
	if len(list.Proposals) != 1 || list.Proposals[0].ProposalID != proposal.ProposalID {
		t.Fatalf("list = %+v", list.Proposals)
	}
	if list.Proposals[0].Runner != "local-agent" || list.Proposals[0].SessionsScanned != 2 {
		t.Fatalf("listed proposal = %+v", list.Proposals[0])
	}

	applied := runMemctl(t, bin, repo, h3Env(home), "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
	if applied.code != 0 {
		t.Fatalf("dream --apply exited %d: %s", applied.code, applied.stderr)
	}
	// Both cited digests are marked with the proposal id and the content hash
	// they had when the proposal was built.
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(config.Canonical(repo)))
	for _, session := range bundle.Sessions {
		digest, ok := cache.Load(session.Agent, session.SessionID)
		if !ok {
			t.Fatalf("digest for %s disappeared", session.Agent)
		}
		if digest.ConsumedBy != proposal.ProposalID || digest.ConsumedRevision == "" {
			t.Fatalf("digest %s not consumed: %+v", session.Agent, digest)
		}
	}

	// The applied content is on the main branch.
	read := runMemctl(t, bin, repo, h3Env(home), "read", "notes/release.md", "--json")
	if read.code != 0 {
		t.Fatalf("read exited %d: %s", read.code, read.stderr)
	}
	if !strings.Contains(read.stdout, line) {
		t.Fatalf("applied file does not contain the assembled line: %s", read.stdout)
	}
}

// TestH3ProposeRejectsMissingAssembledLineCoversD07 covers the documented
// single write channel: files[] is the only source of content, so a proposal
// whose target file lacks the assembled line is refused and nothing is
// persisted.
func TestH3ProposeRejectsMissingAssembledLineCoversD07(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project := filepath.Join(t.TempDir(), "memctl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}
	writeH3Digest(t, home, "claude-code", "sess-a", project, "2026-10-20", "记住：发布只推标签")
	// The parent-directory guard needs notes/ to exist for every case that is
	// not testing the guard itself.
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	bundle := runPrepare(t, bin, home, project)
	line := "- 发布只推标签 [source: " + bundle.Sessions[0].SourceTag + "; added: 2026-10-20]"

	cases := []struct {
		name     string
		input    map[string]any
		code     string
		contains string
	}{
		{
			name: "missing assembled line",
			input: map[string]any{
				"newMemories": []map[string]any{{
					"kind":           "state",
					"targetPath":     "notes/release.md",
					"fact":           "发布只推标签",
					"sessionIndices": []int{0},
					"rationale":      "test",
				}},
				"files": []map[string]any{
					{"path": "notes/release.md", "action": "create", "content": "# Release\n- 发布只推标签\n"},
				},
			},
			code:     "INVALID_ARGUMENT",
			contains: "does not contain the assembled entry line",
		},
		{
			name: "out of range index",
			input: map[string]any{
				"newMemories": []map[string]any{{
					"kind":           "state",
					"targetPath":     "notes/release.md",
					"fact":           "发布只推标签",
					"sessionIndices": []int{9},
					"rationale":      "test",
				}},
				"files": []map[string]any{
					{"path": "notes/release.md", "action": "create", "content": "# Release\n"},
				},
			},
			code:     "INVALID_ARGUMENT",
			contains: "sessionIndices contains 9",
		},
		{
			name: "forged source metadata",
			input: map[string]any{
				"newMemories": []map[string]any{{
					"kind":           "state",
					"targetPath":     "notes/release.md",
					"fact":           "发布只推标签 [source: agent://fake/x/y; added: 2020-01-01]",
					"sessionIndices": []int{0},
					"rationale":      "test",
				}},
				"files": []map[string]any{
					{"path": "notes/release.md", "action": "create", "content": "# Release\n"},
				},
			},
			code:     "INVALID_ARGUMENT",
			contains: "must not carry source or added metadata",
		},
		{
			// memctl never creates directory structure, so a proposal is
			// refused while it is still a proposal instead of failing in apply.
			name: "missing parent directory",
			input: map[string]any{
				"newMemories": []map[string]any{{
					"kind":           "state",
					"targetPath":     "archive/release.md",
					"fact":           "发布只推标签",
					"sessionIndices": []int{0},
					"rationale":      "test",
				}},
				"files": []map[string]any{
					{"path": "archive/release.md", "action": "create", "content": "# Release\n" + line + "\n"},
				},
			},
			code:     "NOT_FOUND",
			contains: "archive/",
		},
		{
			// A topic file that is never linked from MEMORY.md would be
			// invisible to every read surface.
			name: "orphan topic file",
			input: map[string]any{
				"newMemories": []map[string]any{{
					"kind":           "state",
					"targetPath":     "notes/release.md",
					"fact":           "发布只推标签",
					"sessionIndices": []int{0},
					"rationale":      "test",
				}},
				"files": []map[string]any{
					{"path": "notes/release.md", "action": "create", "content": "# Release\n" + line + "\n"},
				},
			},
			code:     "UNSUPPORTED",
			contains: "ORPHAN_FILE notes/release.md",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.input["baseHead"] = bundle.BaseHead
			testCase.input["baseRevision"] = bundle.BaseRevision
			encoded, err := json.Marshal(testCase.input)
			if err != nil {
				t.Fatalf("encode: %v", err)
			}
			result := runMemctlStdin(t, bin, repo, h3Env(home), string(encoded), "dream", "--propose", "--stdin")
			if result.code == 0 {
				t.Fatalf("proposal accepted: %s", result.stdout)
			}
			if !strings.Contains(result.stderr, testCase.code) || !strings.Contains(result.stderr, testCase.contains) {
				t.Fatalf("stderr = %s, want %s and %q", result.stderr, testCase.code, testCase.contains)
			}
		})
	}
	listed := runMemctl(t, bin, repo, h3Env(home), "dream", "--list")
	if strings.Contains(listed.stdout, "proposalId") {
		t.Fatalf("a refused proposal was persisted: %s", listed.stdout)
	}
}

// TestH3ApplySkipsChangedDigestCoversD08 covers the documented changed-digest
// path: the apply still commits, but the re-distilled digest is not marked as
// consumed and the skip is reported in the result warnings.
func TestH3ApplySkipsChangedDigestCoversD08(t *testing.T) {
	bin := buildMemctlBinary(t)
	home, repo := h3Home(t)
	project := filepath.Join(t.TempDir(), "memctl")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatalf("create project: %v", err)
	}
	writeH3Digest(t, home, "claude-code", "sess-a", project, "2026-10-20", "记住：发布只推标签")
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	bundle := runPrepare(t, bin, home, project)
	if len(bundle.Sessions) != 1 {
		t.Fatalf("sessions = %+v", bundle.Sessions)
	}
	fact := "发布只推标签"
	line := "- " + fact + " [source: " + bundle.Sessions[0].SourceTag + "; added: 2026-10-20]"
	input := map[string]any{
		"baseHead":     bundle.BaseHead,
		"baseRevision": bundle.BaseRevision,
		"newMemories": []map[string]any{{
			"kind":           "state",
			"targetPath":     "notes/release.md",
			"fact":           fact,
			"sessionIndices": []int{0},
			"rationale":      "test",
		}},
		"files": []map[string]any{
			{
				"path":    "MEMORY.md",
				"action":  "update",
				"content": bundle.fileContent("MEMORY.md") + "- [[notes/release]] — 发布规则\n",
			},
			{"path": "notes/release.md", "action": "create", "content": "# Release\n" + line + "\n"},
		},
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	proposed := runMemctlStdin(t, bin, repo, h3Env(home), string(encoded), "dream", "--propose", "--stdin")
	if proposed.code != 0 {
		t.Fatalf("dream --propose exited %d: %s", proposed.code, proposed.stderr)
	}
	var proposal struct {
		ProposalID string `json:"proposalId"`
		PatchHash  string `json:"patchHash"`
	}
	if err := json.Unmarshal([]byte(proposed.stdout), &proposal); err != nil {
		t.Fatalf("decode proposal: %v", err)
	}

	// Re-distil the cited session before the applied commit.
	writeH3Digest(t, home, "claude-code", "sess-a", project, "2026-10-20", "记住：发布只推标签（更新后的内容）")

	applied := runMemctl(t, bin, repo, h3Env(home), "dream", "--apply", proposal.ProposalID, "--patch-hash", proposal.PatchHash)
	if applied.code != 0 {
		t.Fatalf("dream --apply exited %d: %s", applied.code, applied.stderr)
	}
	var result struct {
		Commit   string   `json:"commit"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(applied.stdout), &result); err != nil {
		t.Fatalf("decode apply result: %v (%s)", err, applied.stdout)
	}
	if result.Commit == "" {
		t.Fatalf("apply did not commit: %s", applied.stdout)
	}
	if len(result.Warnings) == 0 {
		t.Fatalf("apply reported no skip warning: %s", applied.stdout)
	}
	joined := strings.Join(result.Warnings, "\n")
	if !strings.Contains(joined, "not marked as consumed") && !strings.Contains(joined, "changed after the proposal was built") {
		t.Fatalf("warnings = %v", result.Warnings)
	}
	cache := corpus.OpenCache(filepath.Join(home, ".agent-memory"), config.RepoID(config.Canonical(repo)))
	digest, ok := cache.Load("claude-code", "sess-a")
	if !ok {
		t.Fatal("digest disappeared")
	}
	if digest.ConsumedBy != "" || digest.ConsumedRevision != "" {
		t.Fatalf("a changed digest was marked as consumed: %+v", digest)
	}
}
