package app

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
	"github.com/gray0128/memctl/internal/vault"
)

// Documented read limits from docs/02-domain-models-and-contracts.md section
// 4.1. The HTTP layer rejects larger values instead of truncating them.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
)

// SourceRefDTO is one parsed `source` value in a read response. Device, Agent
// and SessionID are only present for a legal agent:// source.
type SourceRefDTO struct {
	Raw       string `json:"raw"`
	Scheme    string `json:"scheme,omitempty"`
	Device    string `json:"device,omitempty"`
	Agent     string `json:"agent,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// MetadataFieldDTO is one order-preserving metadata pair.
type MetadataFieldDTO struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Raw   string `json:"raw"`
}

// WikiLinkDTO is one resolved-or-unresolved link occurrence.
type WikiLinkDTO struct {
	SourceFile   string `json:"sourceFile"`
	LineNumber   int    `json:"lineNumber"`
	RawTarget    string `json:"rawTarget"`
	Exists       bool   `json:"exists"`
	ResolvedPath string `json:"resolvedPath,omitempty"`
}

// MemoryEntryDTO is one parsed bullet line. RawLine and Metadata preserve the
// original bytes and order so the UI never rewrites untouched text.
type MemoryEntryDTO struct {
	ID           string             `json:"id"`
	FilePath     string             `json:"filePath"`
	FileRevision string             `json:"fileRevision"`
	LineNumber   int                `json:"lineNumber"`
	RawLine      string             `json:"rawLine"`
	FactText     string             `json:"factText"`
	DetailLines  []string           `json:"detailLines,omitempty"`
	Sources      []SourceRefDTO     `json:"sources"`
	Metadata     []MetadataFieldDTO `json:"metadata"`
	Heading      string             `json:"heading,omitempty"`
	AddedDate    string             `json:"addedDate,omitempty"`
	InlineLinks  []WikiLinkDTO      `json:"inlineLinks,omitempty"`
}

// MemoryFileDTO describes one managed file without forcing the first page to
// transfer the whole library text; the single-file read API returns rawContent.
type MemoryFileDTO struct {
	RelPath       string           `json:"relPath"`
	FileRevision  string           `json:"fileRevision"`
	Kind          string           `json:"kind"`
	SizeBytes     int64            `json:"sizeBytes"`
	UpdatedAt     string           `json:"updatedAt"`
	Editable      bool             `json:"editable"`
	WikiKey       string           `json:"wikiKey,omitempty"`
	Title         string           `json:"title,omitempty"`
	LineCount     int              `json:"lineCount,omitempty"`
	IndexedInRoot bool             `json:"indexedInRoot"`
	Entries       []MemoryEntryDTO `json:"entries,omitempty"`
	OutgoingLinks []WikiLinkDTO    `json:"outgoingLinks,omitempty"`
	Backlinks     []string         `json:"backlinks,omitempty"`
}

// GraphNodeDTO is one file node of the knowledge graph.
type GraphNodeDTO struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	EntryCount int    `json:"entryCount"`
	Orphan     bool   `json:"orphan"`
}

// GraphEdgeDTO is one link edge. To is the resolved path when the link is
// unambiguous and Exists; otherwise it is the raw target and Exists is false.
type GraphEdgeDTO struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Exists bool   `json:"exists"`
}

// KnowledgeGraphDTO is the reachable relation view of the repository.
type KnowledgeGraphDTO struct {
	Nodes []GraphNodeDTO `json:"nodes"`
	Edges []GraphEdgeDTO `json:"edges"`
}

// SyncStateDTO summarizes local and remote state. Unknown ahead/behind counts
// stay null instead of pretending to be zero.
type SyncStateDTO struct {
	RepoID        string `json:"repoId"`
	Branch        string `json:"branch,omitempty"`
	Head          string `json:"head,omitempty"`
	HasRemote     bool   `json:"hasRemote"`
	HasUpstream   bool   `json:"hasUpstream"`
	RemoteState   string `json:"remoteState"`
	LocalState    string `json:"localState"`
	AheadCount    *int   `json:"aheadCount"`
	BehindCount   *int   `json:"behindCount"`
	LastAttemptAt string `json:"lastAttemptAt,omitempty"`
	LastSuccessAt string `json:"lastSuccessAt,omitempty"`
	ReasonCode    string `json:"reasonCode,omitempty"`
}

// HealthDTO is the snapshot-level diagnostic summary. It counts the same
// link and orphan findings the graph already produced, so a snapshot never
// reads the repository twice.
type HealthDTO struct {
	Status  string `json:"status"`
	Error   int    `json:"error"`
	Warning int    `json:"warning"`
	Info    int    `json:"info"`
}

// Snapshot is the VaultSnapshot of docs/02-domain-models-and-contracts.md
// section 2.1.
type Snapshot struct {
	RepoID     string            `json:"repoId"`
	RepoPath   string            `json:"repoPath"`
	Revision   string            `json:"revision"`
	Stale      bool              `json:"stale"`
	EntryPoint string            `json:"entryPoint,omitempty"`
	Files      []MemoryFileDTO   `json:"files"`
	Graph      KnowledgeGraphDTO `json:"graph"`
	SyncState  SyncStateDTO      `json:"syncState"`
	Health     HealthDTO         `json:"health"`
	// HiddenExpired counts the time-limited entries excluded from this
	// snapshot because their `until` date had passed. It exists so the UI can
	// tell "no entries" apart from "entries hidden by expiry".
	HiddenExpired int `json:"hiddenExpired"`
}

// RelatedFileDTO describes a one-hop neighbor in the wiki-link graph.
type RelatedFileDTO = vault.RelatedFile

const (
	// MaxTranscriptHits bounds the number of session transcript turns returned
	// by ListEntries when IncludeTranscripts is enabled.
	MaxTranscriptHits = 10
	// MaxTranscriptSnippetBytes bounds each transcript hit snippet in bytes.
	MaxTranscriptSnippetBytes = 240
)

// TranscriptHitDTO is one matched turn from a recent (<= 14d) SessionDigest
// returned when IncludeTranscripts is true and Q is non-empty.
type TranscriptHitDTO struct {
	Agent     string  `json:"agent"`
	SessionID string  `json:"sessionId"`
	Project   string  `json:"project,omitempty"`
	CwdBase   string  `json:"cwdBase,omitempty"`
	UpdatedAt string  `json:"updatedAt,omitempty"`
	TurnIndex int     `json:"turnIndex"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
}

// EntryQuery filters the paged entry list.
type EntryQuery struct {
	Q                  string
	Agent              string
	Device             string
	File               string
	Project            string
	From               string
	To                 string
	IncludeTranscripts bool
	Limit              int
	Offset             int
}

// EntryPage is the paged entry result. Total counts every matching entry, not
// only the current page.
type EntryPage struct {
	Revision       string             `json:"revision"`
	Items          []MemoryEntryDTO   `json:"items"`
	Total          int                `json:"total"`
	RelatedFiles   []RelatedFileDTO   `json:"relatedFiles,omitempty"`
	TranscriptHits []TranscriptHitDTO `json:"transcriptHits,omitempty"`
}

// AuditQuery filters the paged commit list.
type AuditQuery struct {
	Limit  int
	Agent  string
	Before string
}

// AuditCommitDTO is one commit of the audit view.
type AuditCommitDTO struct {
	Hash      string   `json:"hash"`
	Parents   []string `json:"parents"`
	Timestamp string   `json:"timestamp"`
	Subject   string   `json:"subject"`
	Files     []string `json:"files"`
	Agent     string   `json:"agent,omitempty"`
	SessionID string   `json:"sessionId,omitempty"`
	Device    string   `json:"device,omitempty"`
	Alerts    []string `json:"alerts"`
}

// AuditPage is the paged commit result.
type AuditPage struct {
	Commits    []AuditCommitDTO `json:"commits"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// FileDiffDTO is the FileDiff of docs/02-domain-models-and-contracts.md
// section 2.3. A binary file keeps an empty Patch so the client never renders
// fabricated text hunks.
type FileDiffDTO struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch"`
	Binary    bool   `json:"binary"`
}

// AuditDiff is the response of GET /api/v1/audit/{hash}/diff. Hash is the
// full commit id the abbreviated request resolved to.
type AuditDiff struct {
	Hash  string        `json:"hash"`
	Files []FileDiffDTO `json:"files"`
}

// Snapshot scans the repository read-only and returns the documented snapshot.
// It takes the repository lock without waiting and refuses to read a
// transaction-intermediate tree; callers map an active state to BUSY. When a
// protected state blocks the read, the most recent complete snapshot is
// returned with Stale=true; only a repository with no cached snapshot reports
// the protected state as an error.
func (a *App) Snapshot(ctx context.Context) (Snapshot, error) {
	session, err := a.beginRead(ctx)
	if err != nil {
		if stale, ok := a.staleSnapshot(err); ok {
			return stale, nil
		}
		return Snapshot{}, err
	}
	defer session.Close()
	ctx = session.ctx

	scan, err := scanVault(session, a.clock())
	if err != nil {
		return Snapshot{}, err
	}
	syncState, err := a.syncState(ctx, session, scan.head)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{
		RepoID:        config.RepoID(session.repoPath),
		RepoPath:      session.repoPath,
		Revision:      scan.revision,
		Stale:         false,
		EntryPoint:    vault.EntryPointName,
		Files:         scan.files,
		Graph:         scan.graph,
		SyncState:     syncState,
		Health:        scan.health,
		HiddenExpired: scan.hiddenExpired,
	}
	a.cacheSnapshot(snapshot)
	return snapshot, nil
}

// cacheSnapshot stores one complete snapshot as the most recent stale fallback.
func (a *App) cacheSnapshot(snapshot Snapshot) {
	snapshot.Stale = false
	a.snapshotMu.Lock()
	a.cachedSnapshot = &snapshot
	a.snapshotMu.Unlock()
}

// staleSnapshot returns the cached snapshot marked stale when err is one of
// the documented protected states. It reports false when the error is not a
// protected state or when no complete snapshot has been cached yet; callers
// then surface the original error, which the HTTP layer maps to BUSY.
func (a *App) staleSnapshot(err error) (Snapshot, bool) {
	contractErr, ok := memerr.As(err)
	if !ok {
		return Snapshot{}, false
	}
	switch contractErr.Code {
	case memerr.Busy, memerr.OperationActive, memerr.RecoveryRequired:
	default:
		return Snapshot{}, false
	}
	a.snapshotMu.Lock()
	defer a.snapshotMu.Unlock()
	if a.cachedSnapshot == nil {
		return Snapshot{}, false
	}
	stale := *a.cachedSnapshot
	stale.Stale = true
	return stale, true
}

// ListEntries returns one page of parsed entries with the repository revision
// they were read from.
func (a *App) ListEntries(ctx context.Context, query EntryQuery) (EntryPage, error) {
	if query.Limit == 0 {
		query.Limit = DefaultPageSize
	}
	if query.Limit < 0 {
		return EntryPage{}, memerr.New(memerr.InvalidArgument, "limit must not be negative")
	}
	if query.Limit > MaxPageSize {
		return EntryPage{}, memerr.New(memerr.LimitExceeded,
			"limit %d exceeds the maximum page size %d", query.Limit, MaxPageSize)
	}
	if query.Offset < 0 {
		return EntryPage{}, memerr.New(memerr.InvalidArgument, "offset must not be negative")
	}

	now := a.clock()
	var scan *vaultScan
	var repoPath string
	session, err := a.beginRead(ctx)
	if err != nil {
		if code, ok := memerr.As(err); ok && code.Code == memerr.Busy {
			repoPath, _, _ = a.ensureSnapshotAllowed(ctx)
			scan, err = a.scanSnapshot(ctx, now)
			if err != nil {
				return EntryPage{}, err
			}
		} else {
			return EntryPage{}, err
		}
	} else {
		defer session.Close()
		repoPath = session.repoPath
		scan, err = scanVault(session, now)
		if err != nil {
			return EntryPage{}, err
		}
	}
	filtered := filterAndRankEntries(scan, query, now)
	total := len(filtered)
	page := []MemoryEntryDTO{}
	if query.Offset < total {
		end := query.Offset + query.Limit
		if end > total {
			end = total
		}
		page = filtered[query.Offset:end]
	}

	var hitFiles []string
	seenFiles := make(map[string]bool, len(page))
	for _, item := range page {
		if !seenFiles[item.FilePath] {
			seenFiles[item.FilePath] = true
			hitFiles = append(hitFiles, item.FilePath)
		}
	}
	relatedFiles := vault.ComputeRelatedFiles(
		hitFiles,
		scan.outgoingPaths,
		scan.backlinkPaths,
		scan.indexSummaries,
		true,
	)

	var transcriptHits []TranscriptHitDTO
	if query.IncludeTranscripts && strings.TrimSpace(query.Q) != "" && repoPath != "" {
		transcriptHits = a.searchTranscripts(repoPath, query, now)
	}

	return EntryPage{
		Revision:       scan.revision,
		Items:          page,
		Total:          total,
		RelatedFiles:   relatedFiles,
		TranscriptHits: transcriptHits,
	}, nil
}

// maxAuditScanPages bounds how many raw commit pages one filtered audit
// request reads while it looks for enough matches. The default page size is 50
// and the maximum is 200, so one request reads at most 1600 raw commits before
// returning a cursor for the client to continue from.
const maxAuditScanPages = 8

// Audit returns one page of commits for the read-only audit view. When the
// agent filter drops a whole raw page, it keeps reading strictly older pages
// (bounded by maxAuditScanPages) so a matching commit on a later page is still
// returned, and nextCursor always points at the last consumed commit.
func (a *App) Audit(ctx context.Context, query AuditQuery) (AuditPage, error) {
	if query.Limit == 0 {
		query.Limit = DefaultPageSize
	}
	if query.Limit < 0 {
		return AuditPage{}, memerr.New(memerr.InvalidArgument, "limit must not be negative")
	}
	if query.Limit > MaxPageSize {
		return AuditPage{}, memerr.New(memerr.LimitExceeded,
			"limit %d exceeds the maximum page size %d", query.Limit, MaxPageSize)
	}

	session, err := a.beginRead(ctx)
	if err != nil {
		return AuditPage{}, err
	}
	defer session.Close()
	ctx = session.ctx

	page := AuditPage{Commits: []AuditCommitDTO{}}
	before := query.Before
	for scan := 0; scan < maxAuditScanPages; scan++ {
		// One extra record tells the caller whether another raw page exists.
		raw, err := git.Log(ctx, session.repoPath, query.Limit+1, before)
		if err != nil {
			return AuditPage{}, err
		}
		if len(raw) == 0 {
			// The cursor is already at the oldest commit; do not echo it back as
			// a next cursor, or the client would loop on the same empty page.
			before = ""
			break
		}
		more := len(raw) > query.Limit
		if more {
			raw = raw[:query.Limit]
		}
		for index, commit := range raw {
			dto := toAuditCommit(commit)
			if query.Agent != "" && !strings.EqualFold(dto.Agent, query.Agent) {
				continue
			}
			page.Commits = append(page.Commits, dto)
			if len(page.Commits) == query.Limit {
				// A cursor is only meaningful when an older commit remains: either
				// this raw page was truncated or it had unconsumed entries.
				if more || index < len(raw)-1 {
					page.NextCursor = commit.Hash
				}
				return page, nil
			}
		}
		if !more {
			// History is exhausted; there is no older commit to continue from.
			return page, nil
		}
		before = raw[len(raw)-1].Hash
	}
	if len(page.Commits) > 0 || before != "" {
		page.NextCursor = before
	}
	return page, nil
}

// AuditDiff returns the read-only per-file diff of one commit. The request is
// validated in Git before any output is produced; an unknown commit is a
// NotFound instead of a Git failure.
func (a *App) AuditDiff(ctx context.Context, hash string) (AuditDiff, error) {
	session, err := a.beginRead(ctx)
	if err != nil {
		return AuditDiff{}, err
	}
	defer session.Close()

	result, err := git.DiffCommit(session.ctx, session.repoPath, hash)
	if err != nil {
		return AuditDiff{}, err
	}
	files := make([]FileDiffDTO, 0, len(result.Files))
	for _, file := range result.Files {
		files = append(files, FileDiffDTO{
			Path:      file.Path,
			Additions: file.Additions,
			Deletions: file.Deletions,
			Patch:     file.Patch,
			Binary:    file.Binary,
		})
	}
	return AuditDiff{Hash: result.Hash, Files: files}, nil
}

// ConflictState returns the persisted conflict snapshot when one is awaiting
// resolution. The boolean is false for the documented 204 case.
func (a *App) ConflictState(ctx context.Context) (ConflictView, bool, error) {
	session, err := a.beginConflictRead(ctx)
	if err != nil {
		if contractErr, ok := memerr.As(err); ok && contractErr.Code == memerr.NotFound {
			return ConflictView{}, false, nil
		}
		return ConflictView{}, false, err
	}
	defer session.Close()

	conflict, ok, err := session.store.LoadConflict()
	if err != nil {
		return ConflictView{}, false, err
	}
	if !ok {
		return ConflictView{}, false, nil
	}
	return toConflictView(conflict), true, nil
}

// vaultScan is one consistent pass over the managed files.
type vaultScan struct {
	head           string
	revision       string
	files          []MemoryFileDTO
	graph          KnowledgeGraphDTO
	entries        []MemoryEntryDTO
	rawEntries     []vault.MemoryEntry
	outgoingPaths  map[string][]string
	backlinkPaths  map[string][]string
	indexSummaries map[string]string
	memoryRaw      string
	linkIndex      *vault.LinkIndex
	health         HealthDTO
	// hiddenExpired counts the time-limited entries the scan dropped because
	// their `until` date had passed.
	hiddenExpired int
}

func scanVault(session *writeSession, now time.Time) (*vaultScan, error) {
	paths, err := session.root.ListManaged()
	if err != nil {
		return nil, err
	}
	head := ""
	if value, err := git.Head(session.ctx, session.repoPath); err == nil {
		head = value
	}
	index := vault.NewLinkIndex(paths)

	scan := &vaultScan{
		head:          head,
		files:         []MemoryFileDTO{},
		outgoingPaths: map[string][]string{},
		backlinkPaths: map[string][]string{},
		linkIndex:     index,
		graph:         KnowledgeGraphDTO{Nodes: []GraphNodeDTO{}, Edges: []GraphEdgeDTO{}},
	}
	metas := make([]vault.FileMeta, 0, len(paths))
	// backlinks and reachability are derived after every file is parsed.
	backlinks := map[string][]string{}
	resolvedBySource := map[string][]WikiLinkDTO{}

	for _, rel := range paths {
		file, err := session.root.ReadFile(rel)
		if err != nil {
			contractErr, ok := memerr.As(err)
			if !ok || (contractErr.Code != memerr.Unsupported && contractErr.Code != memerr.LimitExceeded) {
				return nil, err
			}
			meta, statErr := session.root.Stat(rel)
			if statErr != nil {
				return nil, statErr
			}
			metas = append(metas, meta)
			scan.files = append(scan.files, MemoryFileDTO{
				RelPath:      meta.RelPath,
				FileRevision: meta.Revision,
				Kind:         string(vault.ClassifyKind(meta.RelPath)),
				SizeBytes:    meta.Size,
				UpdatedAt:    meta.ModTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
				Editable:     false,
			})
			continue
		}

		metas = append(metas, vault.FileMeta{
			RelPath:  file.RelPath,
			Exists:   true,
			Size:     file.SizeBytes,
			Mode:     vault.GitMode(file.Mode),
			FsMode:   file.Mode,
			ModTime:  file.ModTime,
			Revision: file.FileRevision,
		})

		parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
		// The snapshot uses one definition of "visible": an expired
		// time-limited entry is dropped here, exactly as ReadFile hides it in a
		// full-file read. See docs/10-memory-lifecycle.md section 3.3.
		links, diagnostics := parsed.ResolveLinks(index)
		if vault.IsEntryPoint(file.RelPath) {
			scan.memoryRaw = file.RawContent
			scan.indexSummaries = vault.ExtractIndexSummaries(parsed, index)
		}
		for _, diagnostic := range diagnostics {
			switch diagnostic.Code {
			case vault.LinkIssueInvalid, vault.LinkIssueMissing:
				scan.health.Error++
			case vault.LinkIssueAmbiguous:
				scan.health.Warning++
			}
		}

		fileDTO := MemoryFileDTO{
			RelPath:       file.RelPath,
			FileRevision:  file.FileRevision,
			Kind:          string(file.Kind),
			SizeBytes:     file.SizeBytes,
			UpdatedAt:     file.ModTime.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Editable:      file.Editable,
			LineCount:     len(parsed.Lines),
			WikiKey:       wikiKey(file.RelPath, file.Kind),
			Title:         documentTitle(parsed),
			IndexedInRoot: false,
		}
		for i := range parsed.Entries {
			if vault.EntryExpired(parsed.Entries[i].UntilDate, now) {
				scan.hiddenExpired++
				continue
			}
			entry := toEntryDTO(parsed.Entries[i])
			fileDTO.Entries = append(fileDTO.Entries, entry)
			scan.entries = append(scan.entries, entry)
			scan.rawEntries = append(scan.rawEntries, parsed.Entries[i])
		}
		for _, link := range links {
			linkDTO := WikiLinkDTO{
				SourceFile:   link.SourceFile,
				LineNumber:   link.LineNumber,
				RawTarget:    link.RawTarget,
				Exists:       link.Exists,
				ResolvedPath: link.ResolvedPath,
			}
			fileDTO.OutgoingLinks = append(fileDTO.OutgoingLinks, linkDTO)
			resolvedBySource[file.RelPath] = append(resolvedBySource[file.RelPath], linkDTO)
			scan.graph.Edges = append(scan.graph.Edges, GraphEdgeDTO{
				From:   link.SourceFile,
				To:     edgeTarget(linkDTO),
				Exists: link.Exists,
			})
			if link.ResolvedPath != "" {
				backlinks[link.ResolvedPath] = appendUnique(backlinks[link.ResolvedPath], link.SourceFile)
				scan.outgoingPaths[file.RelPath] = appendUnique(scan.outgoingPaths[file.RelPath], link.ResolvedPath)
				scan.backlinkPaths[link.ResolvedPath] = appendUnique(scan.backlinkPaths[link.ResolvedPath], link.SourceFile)
			}
		}
		scan.files = append(scan.files, fileDTO)
	}

	reachable := reachableFrom(vault.EntryPointName, resolvedBySource)
	orphanCount := 0
	for i := range scan.files {
		file := &scan.files[i]
		if vault.IsEntryPoint(file.RelPath) {
			file.IndexedInRoot = true
		} else {
			file.IndexedInRoot = reachable[file.RelPath]
		}
		orphan := !file.IndexedInRoot
		if orphan {
			orphanCount++
		}
		file.Backlinks = backlinks[file.RelPath]
		node := GraphNodeDTO{
			Path:       file.RelPath,
			Kind:       file.Kind,
			EntryCount: len(file.Entries),
			Orphan:     orphan,
		}
		if node.Kind == string(vault.KindEntryPoint) {
			// The entry point is by definition reachable; it is never orphan.
			node.Orphan = false
		}
		scan.graph.Nodes = append(scan.graph.Nodes, node)
	}
	if entry := findFile(scan.files, vault.EntryPointName); entry != nil && entry.LineCount > doctor.EntryBloatLineThreshold {
		scan.health.Warning++
	}
	scan.health.Warning += orphanCount
	switch {
	case scan.health.Error > 0:
		scan.health.Status = "error"
	case scan.health.Warning > 0:
		scan.health.Status = "warning"
	default:
		scan.health.Status = "ok"
	}
	scan.revision = vault.RepoRevisionFrom(head, session.repoPath, metas)
	return scan, nil
}

func (a *App) syncState(ctx context.Context, session *writeSession, head string) (SyncStateDTO, error) {
	stateDTO := SyncStateDTO{
		RepoID:      config.RepoID(session.repoPath),
		Head:        head,
		LocalState:  "clean",
		RemoteState: syncer.StateLocalOnly,
	}
	if status, err := git.StatusPorcelain(ctx, session.repoPath); err == nil && status != "" {
		stateDTO.LocalState = "external_changes"
	}
	if branch := branchName(ctx, session.repoPath); branch != "" {
		stateDTO.Branch = branch
	}
	if remotes, err := git.Remotes(ctx, session.repoPath); err == nil && len(remotes) > 0 {
		stateDTO.HasRemote = true
		stateDTO.RemoteState = syncer.StatePending
	}
	if upstream, ok, err := git.UpstreamRef(ctx, session.repoPath); err == nil && ok {
		stateDTO.HasUpstream = true
		if ahead, behind, err := git.AheadBehind(ctx, session.repoPath, upstream); err == nil {
			stateDTO.AheadCount = &ahead
			stateDTO.BehindCount = &behind
		}
	}
	cache := state.OpenCache(a.cfg.StateDir, stateDTO.RepoID)
	if record, ok, err := cache.LoadSync(); err == nil && ok {
		stateDTO.RemoteState = record.RemoteState
		stateDTO.LocalState = record.LocalState
		stateDTO.HasRemote = record.HasRemote
		stateDTO.HasUpstream = record.HasUpstream
		stateDTO.ReasonCode = record.ReasonCode
		stateDTO.LastAttemptAt = record.LastAttemptAt
		stateDTO.LastSuccessAt = record.LastSuccessAt
		if record.Head != "" {
			stateDTO.Head = record.Head
		}
		if record.Branch != "" {
			stateDTO.Branch = record.Branch
		}
		if record.HasUpstream {
			ahead, behind := record.Ahead, record.Behind
			stateDTO.AheadCount = &ahead
			stateDTO.BehindCount = &behind
		}
	}
	return stateDTO, nil
}

func filterAndRankEntries(scan *vaultScan, query EntryQuery, now time.Time) []MemoryEntryDTO {
	if scan == nil || len(scan.entries) == 0 {
		return []MemoryEntryDTO{}
	}
	project := strings.TrimSpace(query.Project)
	var projectFiles map[string]bool
	if project != "" {
		projectFiles = collectProjectIndexTargets(scan.memoryRaw, project, scan.linkIndex)
	}

	var dtoCandidates []MemoryEntryDTO
	var rawCandidates []vault.MemoryEntry
	for i, entry := range scan.entries {
		if query.File != "" && entry.FilePath != query.File {
			continue
		}
		if project != "" && !entryMatchesProject(entry, project, projectFiles) {
			continue
		}
		if query.Agent != "" && !hasSource(entry, "agent", query.Agent) {
			continue
		}
		if query.Device != "" && !hasSource(entry, "device", query.Device) {
			continue
		}
		if query.From != "" && (entry.AddedDate == "" || entry.AddedDate < query.From) {
			continue
		}
		if query.To != "" && (entry.AddedDate == "" || entry.AddedDate > query.To) {
			continue
		}
		dtoCandidates = append(dtoCandidates, entry)
		if i < len(scan.rawEntries) {
			rawCandidates = append(rawCandidates, scan.rawEntries[i])
		} else {
			rawCandidates = append(rawCandidates, vault.MemoryEntry{
				ID:           entry.ID,
				FilePath:     entry.FilePath,
				FileRevision: entry.FileRevision,
				LineNumber:   entry.LineNumber,
				RawLine:      entry.RawLine,
				FactText:     entry.FactText,
				DetailLines:  entry.DetailLines,
				Heading:      entry.Heading,
				AddedDate:    entry.AddedDate,
			})
		}
	}

	if strings.TrimSpace(query.Q) == "" {
		sort.SliceStable(dtoCandidates, func(i, j int) bool {
			if dtoCandidates[i].FilePath != dtoCandidates[j].FilePath {
				return dtoCandidates[i].FilePath < dtoCandidates[j].FilePath
			}
			return dtoCandidates[i].LineNumber < dtoCandidates[j].LineNumber
		})
		return dtoCandidates
	}

	scored := vault.RankEntries(rawCandidates, query.Q, now)
	out := make([]MemoryEntryDTO, 0, len(scored))
	for _, s := range scored {
		out = append(out, toEntryDTO(s.Entry))
	}
	return out
}

func collectProjectIndexTargets(memoryRaw, project string, linkIdx *vault.LinkIndex) map[string]bool {
	targets := make(map[string]bool)
	if memoryRaw == "" || project == "" {
		return targets
	}
	normalized := strings.ReplaceAll(memoryRaw, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	inIndex := false
	for _, line := range lines {
		clean := strings.TrimSpace(line)
		if strings.HasPrefix(clean, "# ") || strings.HasPrefix(clean, "## ") {
			inIndex = strings.EqualFold(clean, "## index")
			continue
		}
		if !inIndex {
			continue
		}
		if strings.HasPrefix(clean, "- ") || strings.HasPrefix(clean, "* ") || strings.HasPrefix(clean, "+ ") {
			if matched, resolved := matchIndexLine(line, project, linkIdx); matched {
				for _, rel := range resolved {
					if validated, err := vault.ValidateRelPath(rel); err == nil && !vault.IsEntryPoint(validated) {
						targets[validated] = true
					}
				}
			}
		}
	}
	return targets
}

func entryMatchesProject(entry MemoryEntryDTO, project string, projectFiles map[string]bool) bool {
	if isProjectNotePath(entry.FilePath, project) {
		return true
	}
	if projectFiles[entry.FilePath] {
		return true
	}
	if !vault.IsEntryPoint(entry.FilePath) {
		for _, meta := range entry.Metadata {
			if strings.EqualFold(strings.TrimSpace(meta.Key), "project") &&
				strings.EqualFold(strings.TrimSpace(meta.Value), project) {
				return true
			}
		}
	}
	return false
}

func hasSource(entry MemoryEntryDTO, field, value string) bool {
	for _, source := range entry.Sources {
		var got string
		switch field {
		case "agent":
			got = source.Agent
		case "device":
			got = source.Device
		case "session":
			got = source.SessionID
		}
		if strings.EqualFold(got, value) {
			return true
		}
	}
	return false
}

func toEntryDTO(entry vault.MemoryEntry) MemoryEntryDTO {
	dto := MemoryEntryDTO{
		ID:           entry.ID,
		FilePath:     entry.FilePath,
		FileRevision: entry.FileRevision,
		LineNumber:   entry.LineNumber,
		RawLine:      entry.RawLine,
		FactText:     entry.FactText,
		Sources:      []SourceRefDTO{},
		Metadata:     []MetadataFieldDTO{},
		Heading:      entry.Heading,
		AddedDate:    entry.AddedDate,
	}
	if len(entry.DetailLines) > 0 {
		dto.DetailLines = append([]string(nil), entry.DetailLines...)
	}
	for _, source := range entry.Sources {
		dto.Sources = append(dto.Sources, SourceRefDTO{
			Raw:       source.Raw,
			Scheme:    source.Scheme,
			Device:    source.Device,
			Agent:     source.Agent,
			SessionID: source.SessionID,
		})
	}
	for _, field := range entry.Metadata {
		dto.Metadata = append(dto.Metadata, MetadataFieldDTO{Key: field.Key, Value: field.Value, Raw: field.Raw})
	}
	for _, link := range entry.InlineLinks {
		dto.InlineLinks = append(dto.InlineLinks, WikiLinkDTO{
			SourceFile:   link.SourceFile,
			LineNumber:   link.LineNumber,
			RawTarget:    link.RawTarget,
			Exists:       link.Exists,
			ResolvedPath: link.ResolvedPath,
		})
	}
	return dto
}

func toAuditCommit(commit git.CommitInfo) AuditCommitDTO {
	dto := AuditCommitDTO{
		Hash:      commit.Hash,
		Parents:   commit.Parents,
		Timestamp: commit.Timestamp,
		Subject:   commit.Subject,
		Files:     commit.Files,
		Alerts:    []string{},
	}
	if dto.Parents == nil {
		dto.Parents = []string{}
	}
	if dto.Files == nil {
		dto.Files = []string{}
	}
	for _, line := range strings.Split(commit.Body, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Agent":
			dto.Agent = value
		case "Session":
			dto.SessionID = value
		case "Device":
			dto.Device = value
		}
	}
	if alerts := doctor.SuspiciousPhrases(commit.Subject + "\n" + commit.Body); len(alerts) > 0 {
		dto.Alerts = alerts
	}
	return dto
}

func wikiKey(relPath string, kind vault.Kind) string {
	if kind != vault.KindMarkdown {
		return ""
	}
	lower := strings.ToLower(relPath)
	switch {
	case strings.HasSuffix(lower, ".markdown"):
		return relPath[:len(relPath)-len(".markdown")]
	case strings.HasSuffix(lower, ".md"):
		return relPath[:len(relPath)-len(".md")]
	}
	return relPath
}

func documentTitle(parsed vault.ParsedFile) string {
	for _, line := range parsed.Lines {
		text := strings.TrimSpace(line.Text)
		if strings.HasPrefix(text, "# ") {
			return strings.TrimSpace(text[2:])
		}
	}
	return ""
}

// reachableFrom returns every file reachable from start by following resolved
// links. An unreachable file is the documented orphan, including files in a
// ring disconnected from the entry point.
func reachableFrom(start string, linksBySource map[string][]WikiLinkDTO) map[string]bool {
	reachable := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, link := range linksBySource[current] {
			if link.ResolvedPath == "" || reachable[link.ResolvedPath] {
				continue
			}
			reachable[link.ResolvedPath] = true
			queue = append(queue, link.ResolvedPath)
		}
	}
	return reachable
}

func edgeTarget(link WikiLinkDTO) string {
	if link.ResolvedPath != "" {
		return link.ResolvedPath
	}
	return link.RawTarget
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func findFile(files []MemoryFileDTO, relPath string) *MemoryFileDTO {
	for i := range files {
		if files[i].RelPath == relPath {
			return &files[i]
		}
	}
	return nil
}

// ensureSnapshotAllowed verifies that a lock-busy fallback read is not masking
// an unresolved conflict or an unfinished local transaction.
func (a *App) ensureSnapshotAllowed(ctx context.Context) (string, string, error) {
	repoPath, err := a.requireRepoRoot(ctx)
	if err != nil {
		return "", "", err
	}
	gitDir, err := git.GitDir(ctx, repoPath)
	if err != nil {
		return "", "", err
	}
	store := state.OpenStore(gitDir)
	if conflict, ok, err := store.LoadConflict(); err != nil {
		return "", "", err
	} else if ok {
		return "", "", memerr.New(memerr.OperationActive,
			"conflict %s is awaiting resolution; run `memctl resolve`", conflict.ConflictID)
	}
	pending, err := store.Pending()
	if err != nil {
		return "", "", err
	}
	if len(pending) > 0 {
		return "", "", memerr.New(memerr.RecoveryRequired,
			"transaction %s is unfinished; run `memctl recover --status`", pending[0].ID)
	}
	return repoPath, gitDir, nil
}

// readSnapshotFile reads one managed file from the committed Git tree of the
// current branch reference when the repository lock is held by a background
// synchronization worker. It enforces the same path, symlink, binary, size and
// expiry-hiding rules as a locked read without touching the worktree or index.
func (a *App) readSnapshotFile(ctx context.Context, relPath string) (vault.FileRead, error) {
	clean, err := vault.ValidateRelPath(relPath)
	if err != nil {
		return vault.FileRead{}, err
	}
	repoPath, gitDir, err := a.ensureSnapshotAllowed(ctx)
	if err != nil {
		return vault.FileRead{}, err
	}
	commit, err := git.SnapshotCommit(ctx, repoPath, gitDir)
	if err != nil {
		return vault.FileRead{}, err
	}
	if commit == "" {
		if clean == vault.EntryPointName {
			return a.readEntryWithoutLock()
		}
		return vault.FileRead{}, memerr.New(memerr.NotFound, "file %q does not exist", clean)
	}

	segments := strings.Split(clean, "/")
	for i := 1; i < len(segments); i++ {
		prefix := strings.Join(segments[:i], "/")
		entry, ok, err := git.LookupTreeEntry(ctx, repoPath, commit, prefix)
		if err != nil {
			return vault.FileRead{}, err
		}
		if ok && entry.Mode == "120000" {
			return vault.FileRead{}, memerr.New(memerr.Unsupported,
				"path %q traverses the symbolic link %q; links are not readable memory", clean, prefix)
		}
	}

	entry, ok, err := git.LookupTreeEntry(ctx, repoPath, commit, clean)
	if err != nil {
		return vault.FileRead{}, err
	}
	if !ok {
		return vault.FileRead{}, memerr.New(memerr.NotFound, "file %q does not exist", clean)
	}
	if entry.Type == "tree" || entry.Mode == "040000" {
		return vault.FileRead{}, memerr.New(memerr.InvalidPath, "path %q is a directory, not a file", clean)
	}
	if entry.Mode == "120000" {
		return vault.FileRead{}, memerr.New(memerr.Unsupported,
			"path %q is a symbolic link; links are not readable memory", clean)
	}
	if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
		return vault.FileRead{}, memerr.New(memerr.Unsupported, "path %q is not a regular file", clean)
	}
	if entry.Size > vault.MaxTextFileBytes {
		return vault.FileRead{}, memerr.New(memerr.LimitExceeded,
			"file %q is %d bytes, exceeding the %d byte text limit", clean, entry.Size, vault.MaxTextFileBytes)
	}

	raw, err := git.ReadBlob(ctx, repoPath, entry.Object)
	if err != nil {
		return vault.FileRead{}, err
	}
	file, err := vault.FileReadFromBlob(clean, entry.Mode, entry.Size, raw)
	if err != nil {
		return vault.FileRead{}, err
	}
	if info, statErr := os.Lstat(filepath.Join(repoPath, clean)); statErr == nil {
		file.ModTime = info.ModTime()
	} else {
		file.ModTime = a.clock().UTC()
	}
	projected, err := a.projectRead(clean, file, nil)
	if err != nil {
		return projected, err
	}
	if projected.Kind == vault.KindEntryPoint || projected.Kind == vault.KindMarkdown {
		if scan, scanErr := a.scanSnapshot(ctx, a.clock()); scanErr == nil {
			projected.RelatedFiles = vault.ComputeRelatedFiles(
				[]string{projected.RelPath},
				scan.outgoingPaths,
				scan.backlinkPaths,
				scan.indexSummaries,
				true,
			)
		}
	}
	return projected, nil
}

// scanSnapshot scans all managed files from the committed Git tree of the
// current branch reference when the repository lock is held by a background
// synchronization worker.
func (a *App) scanSnapshot(ctx context.Context, now time.Time) (*vaultScan, error) {
	repoPath, gitDir, err := a.ensureSnapshotAllowed(ctx)
	if err != nil {
		return nil, err
	}
	commit, err := git.SnapshotCommit(ctx, repoPath, gitDir)
	if err != nil {
		return nil, err
	}
	scan := &vaultScan{
		head:          commit,
		files:         []MemoryFileDTO{},
		outgoingPaths: map[string][]string{},
		backlinkPaths: map[string][]string{},
		graph:         KnowledgeGraphDTO{Nodes: []GraphNodeDTO{}, Edges: []GraphEdgeDTO{}},
	}
	if commit == "" {
		scan.revision = vault.RepoRevisionFrom("", repoPath, nil)
		return scan, nil
	}

	treeEntries, err := git.ListTreeEntries(ctx, repoPath, commit)
	if err != nil {
		return nil, err
	}
	managed := make([]git.TreeEntry, 0, len(treeEntries))
	paths := make([]string, 0, len(treeEntries))
	for _, entry := range treeEntries {
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
			continue
		}
		clean, err := vault.ValidateRelPath(entry.Path)
		if err != nil {
			continue
		}
		entry.Path = clean
		managed = append(managed, entry)
		paths = append(paths, clean)
	}
	sort.Slice(managed, func(i, j int) bool { return managed[i].Path < managed[j].Path })
	sort.Strings(paths)

	index := vault.NewLinkIndex(paths)
	scan.linkIndex = index
	metas := make([]vault.FileMeta, 0, len(managed))

	for _, entry := range managed {
		if entry.Size > vault.MaxTextFileBytes {
			bounded, err := git.ReadBlobBounded(ctx, repoPath, entry.Object, vault.MaxRevisionBytes)
			if err != nil {
				return nil, err
			}
			meta, err := vault.FileMetaFromBlob(entry.Path, entry.Mode, entry.Size, bounded)
			if err != nil {
				return nil, err
			}
			metas = append(metas, meta)
			continue
		}
		raw, err := git.ReadBlob(ctx, repoPath, entry.Object)
		if err != nil {
			return nil, err
		}
		file, err := vault.FileReadFromBlob(entry.Path, entry.Mode, entry.Size, raw)
		if err != nil {
			contractErr, ok := memerr.As(err)
			if !ok || (contractErr.Code != memerr.Unsupported && contractErr.Code != memerr.LimitExceeded) {
				return nil, err
			}
			meta, metaErr := vault.FileMetaFromBlob(entry.Path, entry.Mode, entry.Size, raw)
			if metaErr != nil {
				return nil, metaErr
			}
			metas = append(metas, meta)
			continue
		}
		metas = append(metas, vault.FileMeta{
			RelPath:  file.RelPath,
			Exists:   true,
			Size:     file.SizeBytes,
			Mode:     vault.GitMode(file.Mode),
			FsMode:   file.Mode,
			Revision: file.FileRevision,
		})
		parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
		links, _ := parsed.ResolveLinks(index)
		if vault.IsEntryPoint(file.RelPath) {
			scan.memoryRaw = file.RawContent
			scan.indexSummaries = vault.ExtractIndexSummaries(parsed, index)
		}
		for i := range parsed.Entries {
			if vault.EntryExpired(parsed.Entries[i].UntilDate, now) {
				scan.hiddenExpired++
				continue
			}
			scan.entries = append(scan.entries, toEntryDTO(parsed.Entries[i]))
			scan.rawEntries = append(scan.rawEntries, parsed.Entries[i])
		}
		for _, link := range links {
			if link.ResolvedPath != "" && link.Exists {
				scan.outgoingPaths[file.RelPath] = appendUnique(scan.outgoingPaths[file.RelPath], link.ResolvedPath)
				scan.backlinkPaths[link.ResolvedPath] = appendUnique(scan.backlinkPaths[link.ResolvedPath], link.SourceFile)
			}
		}
	}

	scan.revision = vault.RepoRevisionFrom(commit, repoPath, metas)
	return scan, nil
}

// searchTranscripts scans recent (<= 14d) SessionDigests in the local corpus
// cache using the same in-memory tokenizer and BM25 scorer, returning bounded
// redacted snippets.
func (a *App) searchTranscripts(repoPath string, query EntryQuery, now time.Time) []TranscriptHitDTO {
	if strings.TrimSpace(query.Q) == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	cache := corpus.OpenCache(a.cfg.StateDir, config.RepoID(repoPath))
	digests := cache.List()
	if len(digests) == 0 {
		return nil
	}

	projectFilter := strings.TrimSpace(query.Project)
	agentFilter := strings.TrimSpace(query.Agent)

	type turnCandidate struct {
		agent     string
		sessionID string
		cwdBase   string
		updatedAt string
		dateStr   string
		turnIndex int
		turn      corpus.Turn
	}

	var candidates []turnCandidate
	var docs [][]vault.SearchField
	var dates []string

	for _, d := range digests {
		updatedTime, ok := parseDigestTime(d.UpdatedAt, d.Date)
		if !ok {
			continue
		}
		if now.Sub(updatedTime) > corpus.CorpusStateTTL {
			continue
		}
		cwdBase := strings.TrimSpace(d.CwdBase)
		if projectFilter != "" && !strings.EqualFold(cwdBase, projectFilter) && !strings.EqualFold(strings.TrimSpace(d.CwdKey), projectFilter) {
			continue
		}
		if agentFilter != "" && !strings.EqualFold(strings.TrimSpace(d.Agent), agentFilter) {
			continue
		}
		dateStr := d.Date
		if dateStr == "" {
			dateStr = updatedTime.Format("2006-01-02")
		}
		for turnIdx, turn := range d.Turns {
			toolsText := strings.Join(turn.Tools, "\n")
			if strings.TrimSpace(turn.User) == "" && strings.TrimSpace(turn.Assistant) == "" && strings.TrimSpace(toolsText) == "" {
				continue
			}
			candidates = append(candidates, turnCandidate{
				agent:     d.Agent,
				sessionID: d.SessionID,
				cwdBase:   cwdBase,
				updatedAt: d.UpdatedAt,
				dateStr:   dateStr,
				turnIndex: turnIdx,
				turn:      turn,
			})
			docs = append(docs, []vault.SearchField{
				{Text: turn.User, Weight: 1.8},
				{Text: turn.Assistant, Weight: 1.5},
				{Text: toolsText, Weight: 1.2},
			})
			dates = append(dates, dateStr)
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	rawScores := vault.ScoreMultiFieldDocs(docs, dates, query.Q, now)
	type scoredTurn struct {
		cand      turnCandidate
		fullMatch bool
		score     float64
	}
	var matched []scoredTurn
	for i, rs := range rawScores {
		if !rs.Matched {
			continue
		}
		matched = append(matched, scoredTurn{
			cand:      candidates[i],
			fullMatch: rs.FullMatch,
			score:     rs.Score,
		})
	}
	if len(matched) == 0 {
		return nil
	}

	sort.SliceStable(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if a.fullMatch != b.fullMatch {
			return a.fullMatch
		}
		if math.Abs(a.score-b.score) > 1e-9 {
			return a.score > b.score
		}
		if a.cand.updatedAt != b.cand.updatedAt {
			return a.cand.updatedAt > b.cand.updatedAt
		}
		if a.cand.sessionID != b.cand.sessionID {
			return a.cand.sessionID < b.cand.sessionID
		}
		return a.cand.turnIndex < b.cand.turnIndex
	})

	limit := MaxTranscriptHits
	if query.Limit > 0 && query.Limit < limit {
		limit = query.Limit
	}
	if len(matched) > limit {
		matched = matched[:limit]
	}

	queryTokens := vault.TokenSet(query.Q)
	hits := make([]TranscriptHitDTO, 0, len(matched))
	for _, m := range matched {
		snippet := buildTranscriptSnippet(m.cand.turn, queryTokens)
		hits = append(hits, TranscriptHitDTO{
			Agent:     m.cand.agent,
			SessionID: m.cand.sessionID,
			Project:   m.cand.cwdBase,
			CwdBase:   m.cand.cwdBase,
			UpdatedAt: m.cand.updatedAt,
			TurnIndex: m.cand.turnIndex,
			Snippet:   snippet,
			Score:     math.Round(m.score*1000) / 1000,
		})
	}
	return hits
}

func parseDigestTime(updatedAt, dateStr string) (time.Time, bool) {
	if s := strings.TrimSpace(updatedAt); s != "" {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC(), true
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC(), true
		}
	}
	if s := strings.TrimSpace(dateStr); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func buildTranscriptSnippet(turn corpus.Turn, queryTokens map[string]struct{}) string {
	type segment struct {
		prefix string
		text   string
	}
	var segments []segment
	if u := strings.TrimSpace(turn.User); u != "" {
		segments = append(segments, segment{prefix: "user: ", text: u})
	}
	if a := strings.TrimSpace(turn.Assistant); a != "" {
		segments = append(segments, segment{prefix: "assistant: ", text: a})
	}
	if len(turn.Tools) > 0 {
		segments = append(segments, segment{prefix: "tools: ", text: strings.Join(turn.Tools, ", ")})
	}
	if len(segments) == 0 {
		return ""
	}

	bestIdx := 0
	bestHits := -1
	for i, seg := range segments {
		toks := vault.TokenSet(seg.text)
		hits := 0
		for qt := range queryTokens {
			if _, ok := toks[qt]; ok {
				hits++
			}
		}
		if hits > bestHits {
			bestHits = hits
			bestIdx = i
		}
	}

	var parts []string
	primary := segments[bestIdx]
	parts = append(parts, primary.prefix+collapseWhitespace(primary.text))
	for i, seg := range segments {
		if i == bestIdx {
			continue
		}
		parts = append(parts, seg.prefix+collapseWhitespace(seg.text))
	}
	combined := strings.Join(parts, " | ")
	redacted := corpus.Redact(combined)
	return truncateUTF8Bytes(collapseWhitespace(redacted), MaxTranscriptSnippetBytes)
}

func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	const ellipsis = "..."
	if maxBytes <= len(ellipsis) {
		return ellipsis[:maxBytes]
	}
	budget := maxBytes - len(ellipsis)
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " \t\r\n") + ellipsis
}

// listManagedPaths returns sorted managed file paths from the locked worktree
// or from the committed branch snapshot when the repository lock is busy.
func (a *App) listManagedPaths(ctx context.Context) ([]string, error) {
	session, err := a.beginRead(ctx)
	if err == nil {
		defer session.Close()
		return session.root.ListManaged()
	}
	if code, ok := memerr.As(err); !ok || code.Code != memerr.Busy {
		return nil, err
	}
	repoPath, gitDir, err := a.ensureSnapshotAllowed(ctx)
	if err != nil {
		return nil, err
	}
	commit, err := git.SnapshotCommit(ctx, repoPath, gitDir)
	if err != nil || commit == "" {
		return nil, err
	}
	treeEntries, err := git.ListTreeEntries(ctx, repoPath, commit)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(treeEntries))
	for _, entry := range treeEntries {
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") {
			continue
		}
		if clean, err := vault.ValidateRelPath(entry.Path); err == nil {
			paths = append(paths, clean)
		}
	}
	sort.Strings(paths)
	return paths, nil
}
