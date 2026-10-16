package app

import (
	"context"
	"sort"
	"strings"

	"github.com/gray0128/memctl/internal/config"
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
}

// EntryQuery filters the paged entry list.
type EntryQuery struct {
	Q      string
	Agent  string
	Device string
	File   string
	From   string
	To     string
	Limit  int
	Offset int
}

// EntryPage is the paged entry result. Total counts every matching entry, not
// only the current page.
type EntryPage struct {
	Revision string           `json:"revision"`
	Items    []MemoryEntryDTO `json:"items"`
	Total    int              `json:"total"`
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
// transaction-intermediate tree; callers map an active state to BUSY.
func (a *App) Snapshot(ctx context.Context) (Snapshot, error) {
	session, err := a.beginRead(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer session.Close()
	ctx = session.ctx

	scan, err := scanVault(session)
	if err != nil {
		return Snapshot{}, err
	}
	syncState, err := a.syncState(ctx, session, scan.head)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		RepoID:     config.RepoID(session.repoPath),
		RepoPath:   session.repoPath,
		Revision:   scan.revision,
		Stale:      false,
		EntryPoint: vault.EntryPointName,
		Files:      scan.files,
		Graph:      scan.graph,
		SyncState:  syncState,
		Health:     scan.health,
	}, nil
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

	session, err := a.beginRead(ctx)
	if err != nil {
		return EntryPage{}, err
	}
	defer session.Close()

	scan, err := scanVault(session)
	if err != nil {
		return EntryPage{}, err
	}
	filtered := filterEntries(scan.entries, query)
	total := len(filtered)
	page := []MemoryEntryDTO{}
	if query.Offset < total {
		end := query.Offset + query.Limit
		if end > total {
			end = total
		}
		page = filtered[query.Offset:end]
	}
	return EntryPage{Revision: scan.revision, Items: page, Total: total}, nil
}

// Audit returns one page of commits for the read-only audit view.
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

	commits, err := git.Log(ctx, session.repoPath, query.Limit, query.Before)
	if err != nil {
		return AuditPage{}, err
	}
	page := AuditPage{Commits: []AuditCommitDTO{}}
	for _, commit := range commits {
		dto := toAuditCommit(commit)
		if query.Agent != "" && !strings.EqualFold(dto.Agent, query.Agent) {
			continue
		}
		page.Commits = append(page.Commits, dto)
	}
	if len(commits) == query.Limit {
		page.NextCursor = commits[len(commits)-1].Hash
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
	session, err := a.beginResolve(ctx)
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
	head     string
	revision string
	files    []MemoryFileDTO
	graph    KnowledgeGraphDTO
	entries  []MemoryEntryDTO
	health   HealthDTO
}

func scanVault(session *writeSession) (*vaultScan, error) {
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
		head:  head,
		files: []MemoryFileDTO{},
		graph: KnowledgeGraphDTO{Nodes: []GraphNodeDTO{}, Edges: []GraphEdgeDTO{}},
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
		links, diagnostics := parsed.ResolveLinks(index)
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
			entry := toEntryDTO(parsed.Entries[i])
			fileDTO.Entries = append(fileDTO.Entries, entry)
			scan.entries = append(scan.entries, entry)
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

func filterEntries(entries []MemoryEntryDTO, query EntryQuery) []MemoryEntryDTO {
	filtered := make([]MemoryEntryDTO, 0, len(entries))
	lowerQuery := strings.ToLower(query.Q)
	for _, entry := range entries {
		if lowerQuery != "" && !strings.Contains(strings.ToLower(entry.FactText), lowerQuery) {
			continue
		}
		if query.File != "" && entry.FilePath != query.File {
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
		filtered = append(filtered, entry)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].FilePath != filtered[j].FilePath {
			return filtered[i].FilePath < filtered[j].FilePath
		}
		return filtered[i].LineNumber < filtered[j].LineNumber
	})
	return filtered
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
