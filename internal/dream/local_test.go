package dream

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/vault"
)

// This file verifies the local interactive Dreaming path of
// docs/12-session-corpus-dreaming.md section 2.3 against D07 and D08 without a
// remote, a workflow file or a model endpoint: prepare fixes a local baseline,
// propose applies the provenance guard, and the consumption write-back is
// re-entrant and skips a digest that changed after the proposal was built.

// initLocalRepo creates a clean memory repository with no remote and no managed
// workflow, and returns its path. It is the documented local-only precondition:
// prepare must succeed here, which is exactly what the Actions path refuses.
func initLocalRepo(t *testing.T) string {
	t.Helper()
	env := gitEnv(t)
	repo := filepath.Join(t.TempDir(), "memory")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	gitRun(t, env, repo, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(repo, "MEMORY.md"), "# Memory\n\n## Index\n- [[topic]]\n")
	writeFile(t, filepath.Join(repo, "topic.md"), "# Topic\n- an existing fact [source: test; added: 2026-01-01]\n")
	gitRun(t, env, repo, "add", "--", "MEMORY.md", "topic.md")
	gitRun(t, env, repo, "commit", "-q", "-m", "init")
	// A managed repository does not create directory structure, so a topic file
	// under notes/ needs notes/ to exist before a proposal may target it. Git
	// does not track an empty directory, so this one stays untracked without
	// making the worktree dirty.
	if err := os.MkdirAll(filepath.Join(repo, "notes"), 0o700); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	return repo
}

// seedDigest writes one unconsumed digest into the repository cache.
func seedDigest(t *testing.T, service *Service, agent, sid, cwd, date, user string) corpus.Digest {
	t.Helper()
	digest := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         agent,
		SessionID:     sid,
		SourceTag:     "agent://device-1/" + agent + "/" + sid,
		Date:          date,
		UpdatedAt:     date + "T10:00:00Z",
		CwdBase:       corpus.CwdBase(cwd),
		CwdKey:        corpus.CwdKey(cwd),
		Turns:         []corpus.Turn{{User: user, Assistant: "ok"}},
	}
	if err := service.corpusCache().Save(digest); err != nil {
		t.Fatalf("save digest: %v", err)
	}
	return digest
}

// hermeticHome points the host session stores at an empty directory so prepare
// never reads the developer's real sessions.
func hermeticHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestPrepareLocalNeedsNoRemoteCoversD07(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	service := newTestService(t, repo, t.TempDir())
	project := filepath.Join(t.TempDir(), "memctl")
	seedDigest(t, service, "claude-code", "sess-a", project, "2026-10-20", "记住：发布只推标签")
	seedDigest(t, service, "codex", "sess-b", project, "2026-10-19", "不要用 make release 发布")

	bundle, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: project})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	if bundle.BaseHead == "" || bundle.BaseRevision == "" {
		t.Fatalf("bundle has no baseline: %+v", bundle)
	}
	if bundle.Today == "" || bundle.ProjectFilter.CwdKey != corpus.CwdKey(project) {
		t.Fatalf("bundle metadata = %+v", bundle)
	}
	if got, want := bundle.ProjectFilter.CwdBase, "memctl"; got != want {
		t.Fatalf("project cwdBase = %q, want %q", got, want)
	}
	if len(bundle.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want 2", bundle.Sessions)
	}
	// The list is ordered by updatedAt descending, so the newest digest is 0.
	if bundle.Sessions[0].SessionID != "sess-a" || bundle.Sessions[1].SessionID != "sess-b" {
		t.Fatalf("session order = %s, %s", bundle.Sessions[0].SessionID, bundle.Sessions[1].SessionID)
	}
	if bundle.Sessions[0].SessionIndex != 0 || bundle.Sessions[1].SessionIndex != 1 {
		t.Fatalf("session indices = %d, %d", bundle.Sessions[0].SessionIndex, bundle.Sessions[1].SessionIndex)
	}
	if bundle.RemainingUnconsumed != 0 {
		t.Fatalf("remainingUnconsumed = %d, want 0", bundle.RemainingUnconsumed)
	}
	if bundle.RedactionNotice == "" || bundle.Rules.DeletionGraceDays != 30 ||
		bundle.Rules.ImplicitPreferenceSessions != 2 || bundle.Rules.EntryPointHintLines != 100 {
		t.Fatalf("rules = %+v", bundle.Rules)
	}
	if len(bundle.Files) != 2 {
		t.Fatalf("files = %+v, want MEMORY.md and topic.md", bundle.Files)
	}
	for _, file := range bundle.Files {
		if file.Revision == "" || file.Content == "" {
			t.Fatalf("file %q has no revision or content", file.Path)
		}
	}
	if len(bundle.Findings) == 0 {
		// topic.md has no heading problem and MEMORY.md is short, so the only
		// expected finding is the missing recommended metadata of the fact line.
		t.Fatalf("findings = %+v, want at least MISSING_RECOMMENDED_METADATA", bundle.Findings)
	}
	// The local baseline path must not need the Actions readiness of docs/11.
	if code, reason := service.BackendStatus(context.Background()); code == "" {
		t.Fatalf("a remote-less repository unexpectedly reports a ready Actions backend (%q)", reason)
	}
}

func TestPrepareLocalRanksCurrentProjectFirstCoversD07(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	service := newTestService(t, repo, t.TempDir())
	current := filepath.Join(t.TempDir(), "memctl")
	other := filepath.Join(t.TempDir(), "other")
	seedDigest(t, service, "claude-code", "other-new", other, "2026-10-20", "other project session")
	seedDigest(t, service, "claude-code", "current-old", current, "2026-10-18", "current project session")

	bundle, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: current})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	if len(bundle.Sessions) != 2 {
		t.Fatalf("sessions = %+v, want 2", bundle.Sessions)
	}
	if bundle.Sessions[0].SessionID != "current-old" {
		t.Fatalf("first session = %q, want the current project's session", bundle.Sessions[0].SessionID)
	}
	if bundle.Sessions[1].SessionID != "other-new" {
		t.Fatalf("second session = %q, want the other project's session", bundle.Sessions[1].SessionID)
	}

	// An explicit --project keeps only that project.
	filtered, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: current, Project: "other"})
	if err != nil {
		t.Fatalf("PrepareLocal --project: %v", err)
	}
	if len(filtered.Sessions) != 1 || filtered.Sessions[0].SessionID != "other-new" {
		t.Fatalf("filtered sessions = %+v", filtered.Sessions)
	}
	byKey, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: current, Project: corpus.CwdKey(current)})
	if err != nil {
		t.Fatalf("PrepareLocal full cwdKey: %v", err)
	}
	if len(byKey.Sessions) != 1 || byKey.Sessions[0].SessionID != "current-old" {
		t.Fatalf("cwdKey filtered sessions = %+v", byKey.Sessions)
	}
}

func TestPrepareLocalRefusesAmbiguousProjectCoversD07(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	service := newTestService(t, repo, t.TempDir())
	// Two clones share the basename but are distinct projects.
	first := filepath.Join(t.TempDir(), "a", "memctl")
	second := filepath.Join(t.TempDir(), "b", "memctl")
	seedDigest(t, service, "claude-code", "clone-a", first, "2026-10-20", "clone a")
	seedDigest(t, service, "claude-code", "clone-b", second, "2026-10-20", "clone b")

	_, err := service.PrepareLocal(context.Background(), PrepareRequest{Project: "memctl"})
	if !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("ambiguous project: err=%v, want INVALID_ARGUMENT", err)
	}
	for _, key := range []string{corpus.CwdKey(first), corpus.CwdKey(second)} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("error %q does not list the candidate %s", err, key)
		}
	}
}

func TestPrepareLocalRefusesDirtyRepository(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	service := newTestService(t, repo, t.TempDir())
	writeFile(t, filepath.Join(repo, "dirty.md"), "# Dirty\n")
	_, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: repo})
	if !contractCode(err, "EXTERNAL_CHANGES") {
		t.Fatalf("dirty repository: err=%v, want EXTERNAL_CHANGES", err)
	}
}

// prepareFixture prepares one bundle over two digests and returns the service
// and the bundle.
func prepareFixture(t *testing.T) (*Service, DreamPrepareBundle) {
	t.Helper()
	hermeticHome(t)
	repo := initLocalRepo(t)
	service := newTestService(t, repo, t.TempDir())
	project := filepath.Join(t.TempDir(), "memctl")
	seedDigest(t, service, "claude-code", "sess-a", project, "2026-10-18", "记住：发布只推标签")
	seedDigest(t, service, "codex", "sess-b", project, "2026-10-20", "不要用 make release 发布")
	bundle, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: project})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	return service, bundle
}

// proposeInput builds a valid input for the fixture bundle with one candidate
// citing the given sessions.
func proposeInput(bundle DreamPrepareBundle, indices []int, fact, entry, topic string) DreamProposeInput {
	return DreamProposeInput{
		BaseHead:     bundle.BaseHead,
		BaseRevision: bundle.BaseRevision,
		NewMemories: []NewCandidate{{
			Kind:           "state",
			TargetPath:     "notes/release.md",
			Fact:           fact,
			SessionIndices: indices,
			Rationale:      "the user corrected the release flow",
		}},
		Files: []ProposeFile{
			{Path: vault.EntryPointName, Action: ChangeUpdate, Content: entry},
			{Path: "notes/release.md", Action: ChangeCreate, Content: topic},
		},
	}
}

// assembledLine is what the guard should build for one candidate.
func assembledLine(bundle DreamPrepareBundle, indices []int, fact string) string {
	latest := ""
	var tags []string
	for _, index := range indices {
		session := bundle.Sessions[index]
		tags = append(tags, session.SourceTag)
		if session.Date > latest {
			latest = session.Date
		}
	}
	return "- " + fact + " [source: " + strings.Join(tags, "; source: ") + "; added: " + latest + "]"
}

func TestProposeLocalAssemblesMultiSessionSourceCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	fact := "发布只推标签，禁止在本地运行 make release"
	line := assembledLine(bundle, []int{0, 1}, fact)
	if !strings.Contains(line, "; source: ") || !strings.Contains(line, "added: 2026-10-20") {
		t.Fatalf("assembled line = %q", line)
	}
	input := proposeInput(bundle, []int{0, 1}, fact,
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n",
		"# Release\n\n## 规则\n"+line+"\n")

	result, err := service.ProposeLocal(context.Background(), input)
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}
	if result.Status != StatusReady || result.Runner != RunnerLocal {
		t.Fatalf("result = %+v", result)
	}
	if result.PatchHash == "" || len(result.Changes) != 2 {
		t.Fatalf("result changes = %+v", result.Changes)
	}
	if result.SessionsScanned != 2 || result.NewMemoriesExtracted != 1 {
		t.Fatalf("counters = %d/%d", result.SessionsScanned, result.NewMemoriesExtracted)
	}

	stored, err := service.Load(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if stored.Runner != RunnerLocal || stored.Status != StatusReady || stored.PatchHash != result.PatchHash {
		t.Fatalf("stored proposal = %+v", stored)
	}
	if stored.SessionsScanned != 2 || stored.NewMemoriesExtracted != 1 {
		t.Fatalf("stored counters = %d/%d", stored.SessionsScanned, stored.NewMemoriesExtracted)
	}
	record, err := service.store.Load(result.ProposalID)
	if err != nil {
		t.Fatalf("store load: %v", err)
	}
	if len(record.CorpusDigests) != 2 {
		t.Fatalf("corpus digests = %+v, want both cited sessions", record.CorpusDigests)
	}
	for _, digest := range record.CorpusDigests {
		if digest.Revision == "" || !strings.HasPrefix(digest.Revision, "sha256:") {
			t.Fatalf("digest revision = %+v", digest)
		}
	}
}

func TestProposeLocalRefusesProvenanceViolationsCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	ctx := context.Background()
	entry := "# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n"

	cases := []struct {
		name  string
		input func() DreamProposeInput
		code  string
	}{
		{
			name: "out of range index",
			input: func() DreamProposeInput {
				return proposeInput(bundle, []int{7}, "fact", entry, "# Release\n")
			},
			code: "INVALID_ARGUMENT",
		},
		{
			name: "forged source metadata in fact",
			input: func() DreamProposeInput {
				input := proposeInput(bundle, []int{0}, "fact [source: agent://fake/x/y; added: 2020-01-01]", entry, "# Release\n")
				return input
			},
			code: "INVALID_ARGUMENT",
		},
		{
			name: "multi line fact",
			input: func() DreamProposeInput {
				return proposeInput(bundle, []int{0}, "fact one\nfact two", entry, "# Release\n")
			},
			code: "INVALID_ARGUMENT",
		},
		{
			name: "empty session indices",
			input: func() DreamProposeInput {
				input := proposeInput(bundle, nil, "fact", entry, "# Release\n")
				return input
			},
			code: "INVALID_ARGUMENT",
		},
		{
			name: "files missing the assembled line",
			input: func() DreamProposeInput {
				// The topic file names the fact but not the assembled line, so
				// the guard refuses instead of injecting it.
				return proposeInput(bundle, []int{0}, "发布只推标签", entry, "# Release\n- 发布只推标签\n")
			},
			code: "INVALID_ARGUMENT",
		},
		{
			name: "target file absent from files",
			input: func() DreamProposeInput {
				input := proposeInput(bundle, []int{0}, "发布只推标签", entry, "# Release\n")
				input.Files = input.Files[:1]
				return input
			},
			code: "INVALID_ARGUMENT",
		},
		{
			name: "protected path",
			input: func() DreamProposeInput {
				input := proposeInput(bundle, []int{0}, "发布只推标签", entry, "# Release\n")
				input.Files = append(input.Files, ProposeFile{Path: ".github/workflows/memctl-dream.yml", Action: ChangeUpdate, Content: "x"})
				return input
			},
			// The repository path boundary already refuses `.github`, so the
			// proposal never even reaches the protected-path check.
			code: "INVALID_PATH",
		},
		{
			name: "deleting the entry file",
			input: func() DreamProposeInput {
				input := proposeInput(bundle, []int{0}, "发布只推标签", entry, "# Release\n")
				input.Files = []ProposeFile{{Path: vault.EntryPointName, Action: ChangeDelete}}
				return input
			},
			code: "UNSUPPORTED",
		},
		{
			name: "stale base revision",
			input: func() DreamProposeInput {
				input := proposeInput(bundle, []int{0}, "发布只推标签", entry, "# Release\n")
				input.BaseRevision = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
				return input
			},
			code: "REVISION_MISMATCH",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := service.ProposeLocal(ctx, testCase.input())
			if !contractCode(err, testCase.code) {
				t.Fatalf("err = %v, want %s", err, testCase.code)
			}
			// Nothing is persisted for a refused input: the proposal directory
			// must not gain a record.
			if proposals := service.List(0); len(proposals) != 0 {
				t.Fatalf("a refused proposal was persisted: %+v", proposals)
			}
		})
	}
}

func TestProposeLocalSkipsDuplicateFactCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	// "an existing fact" already exists in topic.md, so the normalized text is a
	// duplicate. The candidate is skipped and exempt from the files[] compare;
	// only the explicit index edit the agent submitted remains.
	input := DreamProposeInput{
		BaseHead:     bundle.BaseHead,
		BaseRevision: bundle.BaseRevision,
		NewMemories: []NewCandidate{{
			Kind:           "state",
			TargetPath:     "topic.md",
			Fact:           "an existing fact",
			SessionIndices: []int{0},
			Rationale:      "already recorded",
		}},
		Files: []ProposeFile{{
			Path:    vault.EntryPointName,
			Action:  ChangeUpdate,
			Content: "# Memory\n\n## Index\n- [[topic]]\n- [[notes/extra]]\n",
		}, {
			Path:    "notes/extra.md",
			Action:  ChangeCreate,
			Content: "# Extra\n",
		}},
	}

	result, err := service.ProposeLocal(context.Background(), input)
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}
	if result.NewMemoriesExtracted != 0 {
		t.Fatalf("duplicate candidate counted as extracted: %d", result.NewMemoriesExtracted)
	}
	if len(result.Changes) != 2 {
		t.Fatalf("changes = %+v, want the index edit and the new file only", result.Changes)
	}
}

func TestProposeLocalTimeLimitedUntilCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	ctx := context.Background()
	// Sessions are ordered by updatedAt, so index 0 is the 2026-10-20 session.
	if bundle.Sessions[0].Date != "2026-10-20" {
		t.Fatalf("unexpected session order: %+v", bundle.Sessions)
	}
	input := proposeInput(bundle, []int{0}, "冻结发布直到新版流水线验收",
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n", "# Release\n")
	input.NewMemories[0].Kind = "time_limited"
	input.NewMemories[0].Until = "2026-10-19"
	if _, err := service.ProposeLocal(ctx, input); !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("until before the cited session date: err=%v, want INVALID_ARGUMENT", err)
	}

	input.NewMemories[0].Until = "2026-11-20"
	input.Files[1].Content = "# Release\n" +
		"- 冻结发布直到新版流水线验收 [source: " + bundle.Sessions[0].SourceTag + "; added: 2026-10-20; until: 2026-11-20]\n"
	result, err := service.ProposeLocal(ctx, input)
	if err != nil {
		t.Fatalf("ProposeLocal time_limited: %v", err)
	}
	if result.NewMemoriesExtracted != 1 {
		t.Fatalf("extracted = %d", result.NewMemoriesExtracted)
	}
	// A malformed until date is refused with the same code.
	input.NewMemories[0].Until = "2026/11/20"
	if _, err := service.ProposeLocal(ctx, input); !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("malformed until: err=%v, want INVALID_ARGUMENT", err)
	}
}

func TestConsumeCorpusDigestsIsReentrantCoversD08(t *testing.T) {
	service, bundle := prepareFixture(t)
	fact := "发布只推标签"
	line := assembledLine(bundle, []int{0, 1}, fact)
	result, err := service.ProposeLocal(context.Background(), proposeInput(bundle, []int{0, 1}, fact,
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n",
		"# Release\n"+line+"\n"))
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}

	consumed, err := service.ConsumeCorpusDigests(result.ProposalID)
	if err != nil {
		t.Fatalf("ConsumeCorpusDigests: %v", err)
	}
	if len(consumed.Consumed) != 2 || len(consumed.Skipped) != 0 {
		t.Fatalf("consume = %+v", consumed)
	}
	for _, digest := range service.corpusCache().List() {
		if digest.ConsumedBy != result.ProposalID || digest.ConsumedRevision == "" {
			t.Fatalf("digest %s not marked: %+v", digest.SessionID, digest)
		}
	}
	marked := map[string]string{}
	for _, digest := range service.corpusCache().List() {
		marked[digest.SessionID] = digest.ConsumedRevision
	}

	// A second call is idempotent and keeps the recorded revision.
	again, err := service.ConsumeCorpusDigests(result.ProposalID)
	if err != nil {
		t.Fatalf("repeat consume: %v", err)
	}
	if len(again.Consumed) != 2 || len(again.Skipped) != 0 {
		t.Fatalf("repeat consume = %+v", again)
	}
	for _, digest := range service.corpusCache().List() {
		if digest.ConsumedRevision != marked[digest.SessionID] {
			t.Fatalf("consumedRevision was rewritten for %s", digest.SessionID)
		}
	}
}

func TestConsumeSkipsChangedDigestCoversD08(t *testing.T) {
	service, bundle := prepareFixture(t)
	fact := "发布只推标签"
	line := assembledLine(bundle, []int{0}, fact)
	result, err := service.ProposeLocal(context.Background(), proposeInput(bundle, []int{0}, fact,
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n",
		"# Release\n"+line+"\n"))
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}

	// Re-distil one cited session after the proposal was built.
	cited := bundle.Sessions[0]
	changed := corpus.Digest{
		SchemaVersion: corpus.SchemaVersion,
		Agent:         cited.Agent,
		SessionID:     cited.SessionID,
		SourceTag:     cited.SourceTag,
		Date:          cited.Date,
		UpdatedAt:     cited.Date + "T23:00:00Z",
		CwdBase:       cited.CwdBase,
		CwdKey:        cited.CwdKey,
		Turns:         []corpus.Turn{{User: "a later turn", Assistant: "ok"}},
	}
	if err := service.corpusCache().Save(changed); err != nil {
		t.Fatalf("save changed digest: %v", err)
	}

	consumed, err := service.ConsumeCorpusDigests(result.ProposalID)
	if err != nil {
		t.Fatalf("ConsumeCorpusDigests: %v", err)
	}
	if len(consumed.Skipped) != 1 || len(consumed.Consumed) != 0 {
		t.Fatalf("consume = %+v, want the changed digest skipped", consumed)
	}
	for _, digest := range service.corpusCache().List() {
		if digest.ConsumedBy != "" || digest.ConsumedRevision != "" {
			t.Fatalf("a changed digest was marked as consumed: %+v", digest)
		}
	}
	// The skip is reported on the proposal warnings.
	stored, err := service.Load(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	found := false
	for _, warning := range stored.Warnings {
		if strings.Contains(warning, "changed after the proposal was built") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want a changed-digest note", stored.Warnings)
	}
}

func TestProposeLocalRequiresPrepareSnapshotsCoversD07(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	service := newTestService(t, repo, t.TempDir())
	input := DreamProposeInput{
		NewMemories: []NewCandidate{{
			Kind:           "state",
			TargetPath:     "topic.md",
			Fact:           "new fact",
			SessionIndices: []int{0},
			Rationale:      "test",
		}},
		Files: []ProposeFile{{Path: "topic.md", Action: ChangeUpdate, Content: "# Topic\n- new fact\n"}},
	}
	_, err := service.ProposeLocal(context.Background(), input)
	if !contractCode(err, "INVALID_ARGUMENT") {
		t.Fatalf("propose without prepare: err=%v, want INVALID_ARGUMENT", err)
	}
	if !strings.Contains(err.Error(), "--prepare") {
		t.Fatalf("error does not tell the operator to prepare: %v", err)
	}
}

func TestDreamListOrdersByCreatedAt(t *testing.T) {
	service, bundle := prepareFixture(t)
	fact := "first fact"
	line := assembledLine(bundle, []int{0}, fact)
	first, err := service.ProposeLocal(context.Background(), proposeInput(bundle, []int{0}, fact,
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n", "# Release\n"+line+"\n"))
	if err != nil {
		t.Fatalf("first propose: %v", err)
	}
	// A second proposal needs its own baseline prepare because the repository is
	// still at the same commit; reusing the same prepare snapshot is valid.
	secondInput := proposeInput(bundle, []int{1}, "second fact",
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/second]]\n",
		"# Second\n"+assembledLine(bundle, []int{1}, "second fact")+"\n")
	secondInput.NewMemories[0].TargetPath = "notes/second.md"
	secondInput.Files[1].Path = "notes/second.md"
	second, err := service.ProposeLocal(context.Background(), secondInput)
	if err != nil {
		t.Fatalf("second propose: %v", err)
	}
	proposals := service.List(0)
	if len(proposals) != 2 {
		t.Fatalf("proposals = %d, want 2", len(proposals))
	}
	ids := map[string]bool{proposals[0].ProposalID: true, proposals[1].ProposalID: true}
	if !ids[first.ProposalID] || !ids[second.ProposalID] {
		t.Fatalf("listed proposal ids = %v", ids)
	}
	if proposals[0].CreatedAt < proposals[1].CreatedAt {
		t.Fatalf("list is not ordered by createdAt descending: %s < %s", proposals[0].CreatedAt, proposals[1].CreatedAt)
	}
	if limited := service.List(1); len(limited) != 1 {
		t.Fatalf("limit ignored: %d", len(limited))
	}
}

// TestBundleJSONShape pins the documented bundle field names so a rename cannot
// silently break the Skill and the MCP schema.
func TestBundleJSONShape(t *testing.T) {
	_, bundle := prepareFixture(t)
	data, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	for _, field := range []string{
		`"today"`, `"baseHead"`, `"baseRevision"`, `"projectFilter"`, `"remainingUnconsumed"`,
		`"redactionNotice"`, `"rules"`, `"findings"`, `"files"`, `"sessions"`,
		`"sessionIndex"`, `"sourceTag"`, `"cwdBase"`, `"wroteMemoryInSession"`,
	} {
		if !strings.Contains(string(data), field) {
			t.Fatalf("bundle JSON has no %s: %s", field, data)
		}
	}
	if strings.Contains(string(data), config.RepoID(bundle.BaseHead)) {
		t.Fatal("bundle leaks a repository identifier")
	}
	if strings.Contains(string(data), "consumedBy") {
		t.Fatal("bundle leaks the consumption record")
	}
}

// TestProposeLocalRefusesPrematureExpiredDeletionCoversD07 covers the
// documented 30-day gate: an expired entry can only disappear once its hidden
// period is over, whichever path proposes the deletion.
func TestProposeLocalRefusesPrematureExpiredDeletionCoversD07(t *testing.T) {
	hermeticHome(t)
	repo := initLocalRepo(t)
	writeFile(t, filepath.Join(repo, "sprint.md"),
		"# Sprint\n- 临时冻结发布 [source: test; added: 2026-09-01; until: 2026-10-01]\n")
	env := gitEnv(t)
	gitRun(t, env, repo, "add", "--", "sprint.md")
	gitRun(t, env, repo, "commit", "-q", "-m", "add a time-limited entry")
	service := newTestService(t, repo, t.TempDir())
	project := filepath.Join(t.TempDir(), "memctl")
	seedDigest(t, service, "claude-code", "sess-a", project, "2026-10-20", "以后不再冻结发布")

	bundle, err := service.PrepareLocal(context.Background(), PrepareRequest{Cwd: project})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	memory := ""
	for _, file := range bundle.Files {
		if file.Path == "MEMORY.md" {
			memory = file.Content
		}
	}
	// The proposal must both index the file it cites and rewrite sprint.md, so
	// the only remaining finding is the premature deletion.
	indexed := strings.Replace(memory, "- [[topic]]", "- [[topic]]\n- [[sprint]]", 1)
	fact := "以后不再冻结发布"
	line := assembledLine(bundle, []int{0}, fact)
	input := DreamProposeInput{
		BaseHead:     bundle.BaseHead,
		BaseRevision: bundle.BaseRevision,
		NewMemories: []NewCandidate{{
			Kind:           "state",
			TargetPath:     "sprint.md",
			Fact:           fact,
			SessionIndices: []int{0},
			Rationale:      "the constraint was lifted",
		}},
		Files: []ProposeFile{
			{Path: "sprint.md", Action: ChangeUpdate, Content: "# Sprint\n" + line + "\n"},
			{Path: "MEMORY.md", Action: ChangeUpdate, Content: indexed},
		},
	}

	// 2026-10-20 is still inside the 30-day hidden period of a 2026-10-01
	// `until` (deletable from 2026-11-01), so removing the entry is refused with
	// DELETION_DEFERRED.
	service.Now = func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }
	_, err = service.ProposeLocal(context.Background(), input)
	if !contractCode(err, "DELETION_DEFERRED") {
		t.Fatalf("premature deletion: err=%v, want DELETION_DEFERRED", err)
	}

	// Once the hidden period is over the same proposal is accepted.
	service.Now = func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }
	if _, err := service.ProposeLocal(context.Background(), input); err != nil {
		t.Fatalf("deletion after the hidden period was refused: %v", err)
	}
}

// TestProposeLocalRefusesNewDanglingLinkCoversD07 covers the documented link
// guard: a proposal that indexes a file it never creates is refused, because the
// agent is the one that must keep the index consistent.
func TestProposeLocalRefusesNewDanglingLinkCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	input := proposeInput(bundle, []int{0}, "发布只推标签",
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/missing]]\n", "# Release\n- 发布只推标签\n")
	input.Files[1].Content = "# Release\n" + assembledLine(bundle, []int{0}, "发布只推标签") + "\n"
	_, err := service.ProposeLocal(context.Background(), input)
	if err == nil || !contractCode(err, "MISSING_LINK") {
		t.Fatalf("dangling index link: err=%v, want MISSING_LINK", err)
	}
}

// TestProposeLocalRefusesMissingParentDirCoversD07 covers the parent-directory
// guard: memctl never creates directory structure while holding the lock, so a
// proposal whose target sits under a missing directory is refused while it is
// still a proposal instead of failing later in the apply transaction.
func TestProposeLocalRefusesMissingParentDirCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	input := proposeInput(bundle, []int{0}, "发布只推标签",
		"# Memory\n\n## Index\n- [[topic]]\n- [[archive/release]]\n",
		"# Release\n"+assembledLine(bundle, []int{0}, "发布只推标签")+"\n")
	input.Files[1].Path = "archive/release.md"
	input.NewMemories[0].TargetPath = "archive/release.md"

	_, err := service.ProposeLocal(context.Background(), input)
	if err == nil || !contractCode(err, "NOT_FOUND") {
		t.Fatalf("missing parent directory: err=%v, want NOT_FOUND", err)
	}
	if !strings.Contains(err.Error(), "archive/") {
		t.Fatalf("error does not name the missing directory: %v", err)
	}
	if proposals := service.List(0); len(proposals) != 0 {
		t.Fatalf("a refused proposal was persisted: %+v", proposals)
	}

	// The same proposal is accepted once the directory exists.
	if err := os.MkdirAll(filepath.Join(service.Source.RepoPath, "archive"), 0o700); err != nil {
		t.Fatalf("create archive: %v", err)
	}
	bundle, err = service.PrepareLocal(context.Background(), PrepareRequest{Cwd: service.Source.RepoPath})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	input.BaseHead = bundle.BaseHead
	input.BaseRevision = bundle.BaseRevision
	if _, err := service.ProposeLocal(context.Background(), input); err != nil {
		t.Fatalf("proposal into an existing directory was refused: %v", err)
	}
}

// TestProposeLocalRefusesNewOrphanCoversD07 covers the orphan half of the
// structural guard: a topic file the proposal creates but never links from the
// entry file would be invisible to every read surface, so it is refused instead
// of becoming a warning.
func TestProposeLocalRefusesNewOrphanCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	input := proposeInput(bundle, []int{0}, "发布只推标签",
		"# Memory\n\n## Index\n- [[topic]]\n",
		"# Release\n"+assembledLine(bundle, []int{0}, "发布只推标签")+"\n")
	_, err := service.ProposeLocal(context.Background(), input)
	if err == nil || !contractCode(err, "UNSUPPORTED") {
		t.Fatalf("orphan topic file: err=%v, want UNSUPPORTED", err)
	}
	if !strings.Contains(err.Error(), "ORPHAN_FILE notes/release.md") {
		t.Fatalf("error does not name the orphan: %v", err)
	}
	// A file the baseline already had orphaned never blocks an unrelated
	// proposal: fixing every pre-existing orphan is not this proposal's job.
	service, bundle = prepareFixture(t)
	writeFile(t, filepath.Join(service.Source.RepoPath, "stray.md"), "# Stray\n")
	gitRun(t, gitEnv(t), service.Source.RepoPath, "add", "--", "stray.md")
	gitRun(t, gitEnv(t), service.Source.RepoPath, "commit", "-q", "-m", "add stray")
	bundle, err = service.PrepareLocal(context.Background(), PrepareRequest{Cwd: service.Source.RepoPath})
	if err != nil {
		t.Fatalf("PrepareLocal: %v", err)
	}
	input = proposeInput(bundle, []int{0}, "发布只推标签",
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n",
		"# Release\n"+assembledLine(bundle, []int{0}, "发布只推标签")+"\n")
	if _, err := service.ProposeLocal(context.Background(), input); err != nil {
		t.Fatalf("a pre-existing orphan blocked an unrelated proposal: %v", err)
	}
}

// TestProposeLocalWarnsOnEntryBloatCoversD07 covers the documented hint
// threshold: MEMORY.md above EntryBloatLines enters the proposal warnings but
// never blocks generation.
func TestProposeLocalWarnsOnEntryBloatCoversD07(t *testing.T) {
	service, bundle := prepareFixture(t)
	entry := "# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n"
	for i := 0; i < EntryBloatLines; i++ {
		entry += "- filler line " + strings.Repeat("x", 4) + "\n"
	}
	result, err := service.ProposeLocal(context.Background(), proposeInput(bundle, []int{0}, "发布只推标签", entry,
		"# Release\n"+assembledLine(bundle, []int{0}, "发布只推标签")+"\n"))
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}
	var found bool
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "more than") && strings.Contains(warning, "lines") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings do not mention the entry threshold: %v", result.Warnings)
	}

	// A short entry file produces no such warning.
	service, bundle = prepareFixture(t)
	result, err = service.ProposeLocal(context.Background(), proposeInput(bundle, []int{0}, "发布只推标签",
		"# Memory\n\n## Index\n- [[topic]]\n- [[notes/release]]\n",
		"# Release\n"+assembledLine(bundle, []int{0}, "发布只推标签")+"\n"))
	if err != nil {
		t.Fatalf("ProposeLocal: %v", err)
	}
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "more than") {
			t.Fatalf("a short entry file was reported as bloated: %v", result.Warnings)
		}
	}
}
