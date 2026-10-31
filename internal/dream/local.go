package dream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file implements the local interactive Dreaming path of
// docs/12-session-corpus-dreaming.md section 2.3: `prepare` fixes a baseline and
// returns the memory files, the static diagnosis and the unconsumed session
// digests; `propose` validates the current agent's structured candidates,
// assembles the authoritative `[source: ...; added: ...]` line from integer
// session indices and persisting a `local-agent` proposal. Neither path calls a
// model or touches the main repository, and neither requires a remote or the
// managed Actions workflow.

// Local limits from docs/12-session-corpus-dreaming.md sections 2.2.3 and 2.3.
const (
	// MaxCorpusSessions bounds how many digests one prepare bundle carries.
	MaxCorpusSessions = 12
	// MaxCorpusBytes bounds the total serialized digest size of one bundle.
	MaxCorpusBytes = 96 << 10
	// MaxExtractedCandidates bounds the newMemories entries of one proposal.
	MaxExtractedCandidates = 30
	// entryPointHintLines is the documented MEMORY.md hint threshold carried in
	// the prepare rules. The warning itself comes from EntryBloatLines.
	entryPointHintLines = EntryBloatLines
	// deletionGraceDays is the documented hidden period of an expired entry.
	deletionGraceDays = 30
	// implicitPreferenceMinSessions is the documented cross-session gate for an
	// implicit behavioural preference.
	implicitPreferenceMinSessions = 2
)

// ProjectRef is the project filter of one prepare bundle. CwdKey is the
// authoritative filter key; CwdBase is display only.
type ProjectRef struct {
	CwdBase string `json:"cwdBase"`
	CwdKey  string `json:"cwdKey"`
}

// Rules are the documented extraction rules returned with a bundle, so the
// current agent and the user read the same limits the code enforces.
type Rules struct {
	EntryPointHintLines        int `json:"entryPointHintLines"`
	DeletionGraceDays          int `json:"deletionGraceDays"`
	ImplicitPreferenceSessions int `json:"implicitPreferenceMinSessions"`
}

// BundleFinding is one static diagnosis candidate of the fixed baseline. It is
// the same DoctorReport issue shape, so the agent sees one vocabulary.
type BundleFinding struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Line     int    `json:"lineNumber,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// BundleFile is one managed memory file of the fixed baseline.
type BundleFile struct {
	Path     string `json:"path"`
	Revision string `json:"revision"`
	Content  string `json:"content"`
}

// BundleSession is one unconsumed SessionDigest of the bundle. SessionIndex is
// the only handle the agent may use to cite a session; the source tag and the
// date are assembled by ProposeLocal from these fields.
type BundleSession struct {
	SessionIndex      int           `json:"sessionIndex"`
	Agent             string        `json:"agent"`
	SessionID         string        `json:"sessionId"`
	SourceTag         string        `json:"sourceTag"`
	Date              string        `json:"date"`
	CwdBase           string        `json:"cwdBase"`
	CwdKey            string        `json:"cwdKey"`
	Truncated         bool          `json:"truncated"`
	WroteMemoryInSess bool          `json:"wroteMemoryInSession"`
	Turns             []corpus.Turn `json:"turns"`
}

// DreamPrepareBundle is the prepare output of
// docs/12-session-corpus-dreaming.md section 2.3 step 1.
type DreamPrepareBundle struct {
	Today               string          `json:"today"`
	BaseHead            string          `json:"baseHead"`
	BaseRevision        string          `json:"baseRevision"`
	ProjectFilter       ProjectRef      `json:"projectFilter"`
	RemainingUnconsumed int             `json:"remainingUnconsumed"`
	RedactionNotice     string          `json:"redactionNotice"`
	Rules               Rules           `json:"rules"`
	Findings            []BundleFinding `json:"findings"`
	Files               []BundleFile    `json:"files"`
	Sessions            []BundleSession `json:"sessions"`
}

// PrepareRequest is one local prepare call. Project selects a project by
// cwdBase or full cwdKey; empty keeps the current project first. Cwd is the
// project whose sessions are collected and ranked first.
type PrepareRequest struct {
	Project  string
	Cwd      string
	Device   string
	Agent    string
	SID      string
	MaxBytes int
}

// NewCandidate is one new memory candidate of DreamProposeInput. It is a source
// binding declaration: it never contributes bytes to the proposal files.
type NewCandidate struct {
	Kind           string `json:"kind"`
	TargetPath     string `json:"targetPath"`
	Fact           string `json:"fact"`
	SessionIndices []int  `json:"sessionIndices"`
	Until          string `json:"until,omitempty"`
	Rationale      string `json:"rationale"`
}

// ProposeFile is one target file of the proposal. content is authoritative.
type ProposeFile struct {
	Path    string `json:"path"`
	Action  string `json:"action"`
	Content string `json:"content"`
}

// DreamProposeInput is the structured proposal input of
// docs/12-session-corpus-dreaming.md section 2.3 step 2.
type DreamProposeInput struct {
	BaseHead     string         `json:"baseHead"`
	BaseRevision string         `json:"baseRevision"`
	NewMemories  []NewCandidate `json:"newMemories"`
	Files        []ProposeFile  `json:"files"`
	Warnings     []string       `json:"warnings"`
}

// ProposeResult is the accepted proposal of one local propose call. It carries
// the same fields the remote path returns plus the two local counters.
type ProposeResult struct {
	ProposalID           string   `json:"proposalId"`
	Status               string   `json:"status"`
	Runner               string   `json:"runner"`
	PatchHash            string   `json:"patchHash"`
	SessionsScanned      int      `json:"sessionsScanned"`
	NewMemoriesExtracted int      `json:"newMemoriesExtracted"`
	Changes              []Change `json:"changes"`
	Diff                 string   `json:"diff"`
	Warnings             []string `json:"warnings"`
}

// PrepareLocal fixes the local baseline, sweeps the session stores and returns
// the prepare bundle. It requires only a readable, clean memory repository: the
// remote and the managed workflow are preconditions of the Actions path only.
// The bundle's session list is persisted as the snapshot a following propose
// resolves its integer indices against.
func (s *Service) PrepareLocal(ctx context.Context, req PrepareRequest) (DreamPrepareBundle, error) {
	base, head, revision, err := s.fixLocalBaseline(ctx)
	if err != nil {
		return DreamPrepareBundle{}, err
	}
	cwd := strings.TrimSpace(req.Cwd)
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return DreamPrepareBundle{}, memerr.Wrap(memerr.IOFailed, err, "cannot resolve the current directory")
		}
	}
	// A Delta thread works in a per-thread checkout, so the project key comes
	// from the checkout's project root rather than from the raw process
	// directory. A plain repository yields the same path it started with.
	cwd = corpus.ResolveProjectRoot(ctx, git.Run, cwd)
	cwdKey := corpus.CwdKey(cwd)

	// The documented start-up work: refresh the current session digest, sweep
	// the local session stores, then drop expired cache entries.
	if corpus.Enabled(os.Getenv) {
		refreshCurrentSession(ctx, s, req, cwd)
		cache := s.corpusCache()
		corpus.Backfill(cache, corpus.BackfillRequest{
			Cwd:         cwd,
			Device:      req.Device,
			WroteMemory: s.wroteMemory,
			Now:         s.timeNow,
		})
		cache.Prune(s.timeNow(), s.proposalExists)
	}

	sessions, digests, remaining, err := s.selectSessions(req, cwdKey)
	if err != nil {
		return DreamPrepareBundle{}, err
	}
	bundle := DreamPrepareBundle{
		Today:               s.timeNow().UTC().Format("2006-01-02"),
		BaseHead:            head,
		BaseRevision:        revision,
		ProjectFilter:       ProjectRef{CwdBase: corpus.CwdBase(cwd), CwdKey: cwdKey},
		RemainingUnconsumed: remaining,
		RedactionNotice:     corpus.RedactionNotice,
		Rules: Rules{
			EntryPointHintLines:        entryPointHintLines,
			DeletionGraceDays:          deletionGraceDays,
			ImplicitPreferenceSessions: implicitPreferenceMinSessions,
		},
		Findings: bundleFindings(base, s.timeNow()),
		Files:    bundleFiles(base),
		Sessions: sessions,
	}
	if bundle.Files == nil {
		bundle.Files = []BundleFile{}
	}
	if bundle.Sessions == nil {
		bundle.Sessions = []BundleSession{}
	}
	// The snapshot is what propose resolves session indices against. A failed
	// write is not fatal: propose then reports "run prepare again" instead of
	// silently resolving indices against another bundle.
	s.savePrepare(prepareSnapshot{
		BaseHead:     head,
		BaseRevision: revision,
		Sessions:     sessions,
		Digests:      digests,
		CreatedAt:    s.now(),
	})
	return bundle, nil
}

// fixLocalBaseline acquires the repository lock and exports the managed files
// after proving the repository is readable, clean and free of an unfinished
// transaction or conflict. It deliberately skips the remote and workflow
// readiness checks of the Actions path: those are preconditions of a remote run,
// not of a local one.
func (s *Service) fixLocalBaseline(ctx context.Context) (base *baseline, head, revision string, err error) {
	unlock, ctx, err := s.lockCleanRepository(ctx, "preparing a proposal", true)
	if err != nil {
		return nil, "", "", err
	}
	defer unlock()

	root, err := vault.OpenRoot(s.Source.RepoPath)
	if err != nil {
		return nil, "", "", err
	}
	defer root.Close()
	if head, err = git.Head(ctx, s.Source.RepoPath); err != nil {
		return nil, "", "", err
	}
	if revision, err = vault.RepoRevision(root, s.Source.RepoPath, head); err != nil {
		return nil, "", "", err
	}
	base, err = exportBaseline(root)
	if err != nil {
		return nil, "", "", err
	}
	base.head = head
	return base, head, revision, nil
}

// refreshCurrentSession re-distils the calling session when the caller knows a
// stable agent and session id. A failure keeps the previous digest: prepare
// must still return a bundle from the cache.
func refreshCurrentSession(ctx context.Context, s *Service, req PrepareRequest, cwd string) {
	if req.Agent == "" || req.SID == "" || !corpus.Supports(req.Agent) {
		return
	}
	digest, ok := collectCurrentSession(ctx, s, req, cwd)
	if ok && !digest.WroteMemoryInSess && s.wroteMemory(digest.SessionID) {
		digest.WroteMemoryInSess = true
		_ = s.corpusCache().Save(digest)
	}
}

// collectCurrentSession runs the one collection the calling host supports.
// Delta has no log file, so its session is read from the host's own CLI stream;
// every other host is read from the log file its payload or store names. A
// failure of either path yields ok=false and no error: prepare reports the
// bundle it already has.
func collectCurrentSession(ctx context.Context, s *Service, req PrepareRequest, cwd string) (corpus.Digest, bool) {
	if req.Agent == "delta" {
		digest, ok, err := corpus.CollectDelta(ctx, s.deltaRunner(), s.corpusCache(), corpus.DeltaRequest{
			ThreadID: req.SID,
			Cwd:      cwd,
			Device:   req.Device,
			Now:      s.timeNow,
		})
		if err != nil || !ok {
			return corpus.Digest{}, false
		}
		return digest, true
	}
	path := corpus.LocateSession(req.Agent, req.SID, cwd, "")
	if path == "" {
		return corpus.Digest{}, false
	}
	writer := corpus.Writer{Cache: s.corpusCache(), Now: s.timeNow}
	return writer.Collect(corpus.RealTimeRequest{
		Agent:          req.Agent,
		SID:            req.SID,
		TranscriptPath: path,
		Cwd:            cwd,
		Device:         req.Device,
	})
}

// selectSessions picks the unconsumed digests of one bundle in the documented
// order and reports how many more stayed outside the budget. `--project` may
// name a cwdBase or a full cwdKey; a name matching several projects is refused
// because the two clones would silently mix.
func (s *Service) selectSessions(req PrepareRequest, cwdKey string) ([]BundleSession, []CorpusDigest, int, error) {
	cache := s.corpusCache()
	all := cache.List()
	unconsumed := make([]corpus.Digest, 0, len(all))
	for _, digest := range all {
		if digest.ConsumedBy != "" || len(digest.Turns) == 0 {
			continue
		}
		unconsumed = append(unconsumed, digest)
	}

	project := strings.TrimSpace(req.Project)
	var selected []corpus.Digest
	remaining := 0
	if project != "" {
		filtered, err := filterProject(unconsumed, project)
		if err != nil {
			return nil, nil, 0, err
		}
		selected = filtered
	} else {
		// The current project comes first, then every other project in
		// updatedAt order. Cache.List already sorts by updatedAt descending, so
		// two stable partitions keep that order inside each group.
		var preferred, others []corpus.Digest
		for _, digest := range unconsumed {
			if digest.CwdKey == cwdKey {
				preferred = append(preferred, digest)
			} else {
				others = append(others, digest)
			}
		}
		selected = append(preferred, others...)
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 || maxBytes > MaxCorpusBytes {
		maxBytes = MaxCorpusBytes
	}

	sessions := make([]BundleSession, 0, MaxCorpusSessions)
	digests := make([]CorpusDigest, 0, MaxCorpusSessions)
	total := 0
	used := 0
	for _, digest := range selected {
		if len(sessions) >= MaxCorpusSessions {
			remaining++
			continue
		}
		size := digestSize(digest)
		// The first session always enters: one digest is at most
		// MaxSessionDigestBytes, which is below the documented bundle budget, so
		// an empty bundle cannot be caused by the byte limit.
		if len(sessions) > 0 && total+size > maxBytes {
			remaining++
			continue
		}
		total += size
		sessions = append(sessions, bundleSession(used, digest))
		digests = append(digests, CorpusDigest{
			File:     cache.Path(digest.Agent, digest.SessionID),
			Revision: digestRevision(cache, digest.Agent, digest.SessionID),
		})
		used++
	}
	return sessions, digests, remaining, nil
}

// filterProject resolves a --project value and keeps only that project. A
// cwdBase matching several cwdKeys is ambiguous and lists the candidates.
func filterProject(digests []corpus.Digest, project string) ([]corpus.Digest, error) {
	if strings.HasPrefix(project, "sha256:") {
		var filtered []corpus.Digest
		for _, digest := range digests {
			if digest.CwdKey == project {
				filtered = append(filtered, digest)
			}
		}
		return filtered, nil
	}
	keys := map[string]bool{}
	for _, digest := range digests {
		if digest.CwdBase == project && digest.CwdKey != "" {
			keys[digest.CwdKey] = true
		}
	}
	if len(keys) > 1 {
		candidates := make([]string, 0, len(keys))
		for key := range keys {
			candidates = append(candidates, key)
		}
		sort.Strings(candidates)
		return nil, memerr.New(memerr.InvalidArgument,
			"project name %q matches %d working directories; pass one cwdKey instead: %s",
			project, len(candidates), strings.Join(candidates, ", "))
	}
	if len(keys) == 0 {
		return nil, nil
	}
	var filtered []corpus.Digest
	for _, digest := range digests {
		if digest.CwdBase == project {
			filtered = append(filtered, digest)
		}
	}
	return filtered, nil
}

// bundleSession projects one digest onto the bundle shape.
func bundleSession(index int, digest corpus.Digest) BundleSession {
	turns := digest.Turns
	if turns == nil {
		turns = []corpus.Turn{}
	}
	return BundleSession{
		SessionIndex:      index,
		Agent:             digest.Agent,
		SessionID:         digest.SessionID,
		SourceTag:         digest.SourceTag,
		Date:              digest.Date,
		CwdBase:           digest.CwdBase,
		CwdKey:            digest.CwdKey,
		Truncated:         digest.Truncated,
		WroteMemoryInSess: digest.WroteMemoryInSess,
		Turns:             turns,
	}
}

// digestSize is the serialized size of one digest, which is what the bundle
// byte budget measures.
func digestSize(digest corpus.Digest) int {
	data, err := json.Marshal(digest)
	if err != nil {
		return corpus.MaxSessionDigestBytes
	}
	return len(data)
}

// digestRevision hashes the digest file bytes exactly as they are on disk, so
// the apply write-back can prove the proposal read the same material.
func digestRevision(cache *corpus.Cache, agent, sessionID string) string {
	data, err := os.ReadFile(cache.Path(agent, sessionID))
	if err != nil {
		return ""
	}
	return contentHash(data)
}

// contentHash is the documented revision form of a corpus file.
func contentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// wroteMemory reports whether a session already produced a commit carrying its
// trailer, so the agent can weigh that digest lower.
func (s *Service) wroteMemory(sid string) bool {
	if s.SessionWroteMemory == nil || sid == "" {
		return false
	}
	return s.SessionWroteMemory(sid)
}

// proposalExists reports whether a proposal record still exists, so a consumed
// digest survives the cache TTL exactly as long as its proposal does.
func (s *Service) proposalExists(id string) bool {
	_, err := s.store.Load(id)
	return err == nil
}

// corpusCache binds the digest cache of the repository this service serves.
func (s *Service) corpusCache() *corpus.Cache {
	return corpus.OpenCache(s.Source.StateDir, s.Source.RepoID)
}

// bundleFindings projects the doctor report onto the bundle finding shape. It
// is the same static diagnosis the Actions worker starts from.
func bundleFindings(base *baseline, now time.Time) []BundleFinding {
	findings := staticFindings(&fileSet{files: baselineTree(base)}, now)
	out := make([]BundleFinding, 0, len(findings))
	for _, finding := range findings {
		out = append(out, BundleFinding{
			Code:     finding.Code,
			Path:     finding.Path,
			Line:     finding.Line,
			Message:  finding.Message,
			Severity: findingSeverity(finding.Code),
		})
	}
	return out
}

// findingSeverity mirrors the documented diagnostic levels for the codes the
// static analysis produces.
func findingSeverity(code string) string {
	switch code {
	case "ENTRY_BLOAT", "ORPHAN_FILE", "AMBIGUOUS_LINK", "DUPLICATE_ENTRY":
		return "warning"
	case "INVALID_LINK", "MISSING_LINK":
		return "error"
	default:
		return "info"
	}
}

// baselineTree materializes the fixed baseline as the worker's file set.
func baselineTree(base *baseline) map[string][]byte {
	files := make(map[string][]byte, len(base.files))
	for rel, file := range base.files {
		files[rel] = append([]byte(nil), file.content...)
	}
	return files
}

// bundleFiles materializes the fixed baseline in path order.
func bundleFiles(base *baseline) []BundleFile {
	paths := make([]string, 0, len(base.files))
	for rel := range base.files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	files := make([]BundleFile, 0, len(paths))
	for _, rel := range paths {
		file := base.files[rel]
		files = append(files, BundleFile{
			Path:     rel,
			Revision: file.revision,
			Content:  string(file.content),
		})
	}
	return files
}

// deltaRunner returns the injected Delta stream opener, or the host CLI when the
// caller did not replace it. Tests inject a deterministic stream so a prepare
// never depends on a Delta installation.
func (s *Service) deltaRunner() corpus.DeltaRunner {
	if s.DeltaRunner != nil {
		return s.DeltaRunner
	}
	return corpus.DeltaCLIRunner(corpus.DeltaCLI)
}
