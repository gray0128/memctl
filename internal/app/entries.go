package app

import (
	"context"
	"path"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// entryAddedFormat is the documented date layout for the recommended `added`
// metadata.
const entryAddedFormat = "2006-01-02"

// CreateEntryRequest is the input of POST /api/v1/entries. ExpectedRevision is
// the target file's fileRevision or the absent literal for a new file.
type CreateEntryRequest struct {
	FilePath         string
	Heading          string
	FactText         string
	Sources          []string
	ExpectedRevision string
	Agent            string
	SessionID        string
	Device           string
}

// PatchEntryRequest is the input of PATCH /api/v1/entries/{id}. FileRevision is
// the If-Match baseline of the source file; BaseRevision is required when the
// entry moves to another file.
type PatchEntryRequest struct {
	ID             string
	FilePath       string
	FactText       string
	MoveToFilePath string
	BaseRevision   string
	FileRevision   string
	Agent          string
	SessionID      string
	Device         string
}

// DeleteEntryRequest is the input of DELETE /api/v1/entries/{id}.
type DeleteEntryRequest struct {
	ID           string
	FilePath     string
	FileRevision string
	Agent        string
	SessionID    string
	Device       string
}

// EntryResult is the response of the entry create and patch routes: the
// resulting entry plus the transaction that produced it.
type EntryResult struct {
	Entry  MemoryEntryDTO
	Result MutationResult
}

// CreateEntry appends one bullet to an existing file or creates the target file
// with it. The caller supplies the file baseline through ExpectedRevision.
func (a *App) CreateEntry(ctx context.Context, req CreateEntryRequest) (EntryResult, error) {
	if req.ExpectedRevision == "" {
		return EntryResult{}, memerr.New(memerr.PreconditionRequired,
			"creating an entry requires expectedRevision, using absent to create the target file")
	}
	factText := strings.TrimSpace(req.FactText)
	if factText == "" {
		return EntryResult{}, memerr.New(memerr.InvalidArgument, "factText must not be empty")
	}
	if err := requireSingleLine("factText", factText); err != nil {
		return EntryResult{}, err
	}
	if err := requireSingleLine("heading", req.Heading); err != nil {
		return EntryResult{}, err
	}
	for _, source := range req.Sources {
		if err := requireSingleLine("sources", source); err != nil {
			return EntryResult{}, err
		}
	}

	session, err := a.beginWrite(ctx)
	if err != nil {
		return EntryResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, ""); err != nil {
		return EntryResult{}, err
	}
	change, lineNumber, err := planEntryCreate(session, req, factText)
	if err != nil {
		return EntryResult{}, err
	}
	plans, err := session.plan(MutationRequest{Changes: []FileChange{change}})
	if err != nil {
		return EntryResult{}, err
	}
	result, err := a.commit(ctx, session, "entry-create", MutationRequest{Agent: req.Agent, SessionID: req.SessionID, Device: req.Device}, plans)
	if err != nil {
		return EntryResult{}, err
	}
	entry, entryErr := session.entryAt(change.Path, lineNumber)
	a.finishWrite(session, result)
	if entryErr != nil {
		// The transaction is committed; report it next to the read failure
		// instead of pretending nothing was written.
		return EntryResult{Result: result}, entryErr
	}
	return EntryResult{Entry: entry, Result: result}, nil
}

// PatchEntry rewrites one entry's fact text, optionally moving the entry to
// another file. A move requires the whole-repository baseRevision; moving to an
// absent path creates that file, which is how the graph split is backed.
func (a *App) PatchEntry(ctx context.Context, req PatchEntryRequest) (EntryResult, error) {
	if req.FileRevision == "" {
		return EntryResult{}, memerr.New(memerr.PreconditionRequired,
			"patching an entry requires If-Match with the fileRevision it was read from")
	}
	factText := strings.TrimSpace(req.FactText)
	if factText == "" {
		return EntryResult{}, memerr.New(memerr.InvalidArgument, "factText must not be empty")
	}
	if err := requireSingleLine("factText", factText); err != nil {
		return EntryResult{}, err
	}
	moving := req.MoveToFilePath != "" && req.MoveToFilePath != req.FilePath
	if moving && req.BaseRevision == "" {
		return EntryResult{}, memerr.New(memerr.PreconditionRequired,
			"moving an entry across files requires the repository baseRevision")
	}

	session, err := a.beginWrite(ctx)
	if err != nil {
		return EntryResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, req.BaseRevision); err != nil {
		return EntryResult{}, err
	}
	change, entryLine, err := planEntryPatch(session, req, factText, moving)
	if err != nil {
		return EntryResult{}, err
	}
	plans, err := session.plan(MutationRequest{Changes: change})
	if err != nil {
		return EntryResult{}, err
	}
	result, err := a.commit(ctx, session, "entry-patch", MutationRequest{Agent: req.Agent, SessionID: req.SessionID, Device: req.Device}, plans)
	if err != nil {
		return EntryResult{}, err
	}
	entryPath := req.FilePath
	if moving {
		entryPath = req.MoveToFilePath
	}
	entry, entryErr := session.entryAt(entryPath, entryLine)
	a.finishWrite(session, result)
	if entryErr != nil {
		// The transaction is committed; report it next to the read failure
		// instead of pretending nothing was written.
		return EntryResult{Result: result}, entryErr
	}
	return EntryResult{Entry: entry, Result: result}, nil
}

// DeleteEntry removes one entry's physical line from its file.
func (a *App) DeleteEntry(ctx context.Context, req DeleteEntryRequest) (MutationResult, error) {
	if req.FileRevision == "" {
		return MutationResult{}, memerr.New(memerr.PreconditionRequired,
			"deleting an entry requires If-Match with the fileRevision it was read from")
	}

	session, err := a.beginWrite(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, ""); err != nil {
		return MutationResult{}, err
	}
	change, err := planEntryDelete(session, req)
	if err != nil {
		return MutationResult{}, err
	}
	// An expired time-limited entry stays visible to the delete path only after
	// its hidden period. Deleting it earlier would erase a fact the operator
	// may still be relying on; the error names the date so the retry is
	// deliberate. See docs/10-memory-lifecycle.md section 3.3.
	if err := a.requireDeleteWindow(session, req); err != nil {
		return MutationResult{}, err
	}
	plans, err := session.plan(MutationRequest{Changes: []FileChange{change}})
	if err != nil {
		return MutationResult{}, err
	}
	result, err := a.commit(ctx, session, "entry-delete", MutationRequest{Agent: req.Agent, SessionID: req.SessionID, Device: req.Device}, plans)
	if err != nil {
		return MutationResult{}, err
	}
	return a.finishWrite(session, result), nil
}

// planEntryCreate validates the target baseline and returns the single put that
// creates or appends the entry, together with the new entry's line number.
func planEntryCreate(session *writeSession, req CreateEntryRequest, factText string) (FileChange, int, error) {
	relPath, err := vault.ValidateRelPath(req.FilePath)
	if err != nil {
		return FileChange{}, 0, err
	}
	line := buildEntryLine(factText, req.Sources, time.Now().UTC().Format(entryAddedFormat))

	if req.ExpectedRevision == vault.AbsentRevision {
		content := newEntryFileContent(req.Heading, line)
		return FileChange{
			Path:             relPath,
			ExpectedRevision: vault.AbsentRevision,
			Action:           ActionPut,
			RawContent:       content,
		}, strings.Count(content, "\n"), nil
	}

	file, err := session.root.ReadFile(relPath)
	if err != nil {
		return FileChange{}, 0, err
	}
	parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
	content, lineNumber := parsed.WithAppendedLine(line)
	return FileChange{
		Path:             relPath,
		ExpectedRevision: req.ExpectedRevision,
		Action:           ActionPut,
		RawContent:       content,
	}, lineNumber, nil
}

// planEntryPatch returns the changes of one patch. An in-place patch is one put;
// a cross-file move is a put of the source without the line plus a put of the
// destination with the rewritten line. It also returns the new entry's line
// number in its final file.
func planEntryPatch(session *writeSession, req PatchEntryRequest, factText string, moving bool) ([]FileChange, int, error) {
	sourcePath, err := vault.ValidateRelPath(req.FilePath)
	if err != nil {
		return nil, 0, err
	}
	source, err := session.root.ReadFile(sourcePath)
	if err != nil {
		return nil, 0, err
	}
	if source.FileRevision != req.FileRevision {
		return nil, 0, memerr.New(memerr.RevisionMismatch,
			"path %q changed since it was read; reread the entry before patching it", sourcePath)
	}
	parsed := vault.ParseFile(source.RelPath, source.FileRevision, []byte(source.RawContent))
	entry, ok := findEntry(parsed, req.ID)
	if !ok {
		return nil, 0, memerr.New(memerr.NotFound,
			"entry %s is not present in %s at this revision", req.ID, sourcePath)
	}
	newLine, ok := vault.ReplaceEntryFact(entry.RawLine, factText)
	if !ok {
		return nil, 0, memerr.New(memerr.InvalidArgument, "entry %s is not a bullet line", req.ID)
	}

	if !moving {
		content, ok := parsed.WithReplacedLine(entry.LineNumber, newLine)
		if !ok {
			return nil, 0, memerr.New(memerr.NotFound, "entry %s is not present in %s", req.ID, sourcePath)
		}
		return []FileChange{{
			Path:             sourcePath,
			ExpectedRevision: source.FileRevision,
			Action:           ActionPut,
			RawContent:       content,
		}}, entry.LineNumber, nil
	}

	targetPath, err := vault.ValidateRelPath(req.MoveToFilePath)
	if err != nil {
		return nil, 0, err
	}
	sourceContent, ok := parsed.WithoutLine(entry.LineNumber)
	if !ok {
		return nil, 0, memerr.New(memerr.NotFound, "entry %s is not present in %s", req.ID, sourcePath)
	}
	changes := []FileChange{{
		Path:             sourcePath,
		ExpectedRevision: source.FileRevision,
		Action:           ActionPut,
		RawContent:       sourceContent,
	}}

	targetMeta, err := session.root.Stat(targetPath)
	if err != nil {
		return nil, 0, err
	}
	var targetContent string
	var targetLine int
	var targetRevision string
	if targetMeta.Exists {
		target, err := session.root.ReadFile(targetPath)
		if err != nil {
			return nil, 0, err
		}
		targetContent, targetLine = vault.ParseFile(targetPath, target.FileRevision, []byte(target.RawContent)).WithAppendedLine(newLine)
		targetRevision = target.FileRevision
	} else {
		targetContent = newEntryFileContent(headingFromPath(targetPath), newLine)
		targetLine = strings.Count(targetContent, "\n")
		targetRevision = vault.AbsentRevision
	}
	changes = append(changes, FileChange{
		Path:             targetPath,
		ExpectedRevision: targetRevision,
		Action:           ActionPut,
		RawContent:       targetContent,
	})
	return changes, targetLine, nil
}

// requireDeleteWindow refuses to delete an entry whose `until` has expired but
// whose 30-day hidden period has not finished. Entries without `until`, or with
// a future one, are deleted normally: the rule only protects time-limited
// entries from being removed in the same round they were hidden.
func (a *App) requireDeleteWindow(session *writeSession, req DeleteEntryRequest) error {
	relPath, err := vault.ValidateRelPath(req.FilePath)
	if err != nil {
		return err
	}
	file, err := session.root.ReadFile(relPath)
	if err != nil {
		return err
	}
	parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
	entry, ok := findEntry(parsed, req.ID)
	if !ok || entry.UntilDate == "" {
		return nil
	}
	now := a.clock()
	if !vault.EntryExpired(entry.UntilDate, now) || vault.DeletionDue(entry.UntilDate, now) {
		return nil
	}
	return memerr.New(memerr.DeletionDeferred, "entry %s cannot be deleted yet: %s",
		req.ID, vault.DeletionBlockedReason(entry.UntilDate, now))
}

// planEntryDelete returns the put of the source file without the entry line.
func planEntryDelete(session *writeSession, req DeleteEntryRequest) (FileChange, error) {
	relPath, err := vault.ValidateRelPath(req.FilePath)
	if err != nil {
		return FileChange{}, err
	}
	source, err := session.root.ReadFile(relPath)
	if err != nil {
		return FileChange{}, err
	}
	if source.FileRevision != req.FileRevision {
		return FileChange{}, memerr.New(memerr.RevisionMismatch,
			"path %q changed since it was read; reread the entry before deleting it", relPath)
	}
	parsed := vault.ParseFile(source.RelPath, source.FileRevision, []byte(source.RawContent))
	entry, ok := findEntry(parsed, req.ID)
	if !ok {
		return FileChange{}, memerr.New(memerr.NotFound,
			"entry %s is not present in %s at this revision", req.ID, relPath)
	}
	content, ok := parsed.WithoutLine(entry.LineNumber)
	if !ok {
		return FileChange{}, memerr.New(memerr.NotFound, "entry %s is not present in %s", req.ID, relPath)
	}
	return FileChange{
		Path:             relPath,
		ExpectedRevision: source.FileRevision,
		Action:           ActionPut,
		RawContent:       content,
	}, nil
}

// entryAt reads one file after a committed transaction and returns the entry on
// the given line. The entry ID is derived from the new file revision, so it is
// never the pre-edit ID.
func (s *writeSession) entryAt(relPath string, lineNumber int) (MemoryEntryDTO, error) {
	file, err := s.root.ReadFile(relPath)
	if err != nil {
		return MemoryEntryDTO{}, err
	}
	parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
	for _, entry := range parsed.Entries {
		if entry.LineNumber == lineNumber {
			return toEntryDTO(entry), nil
		}
	}
	return MemoryEntryDTO{}, memerr.New(memerr.NotFound,
		"entry on line %d of %s was not found after the transaction", lineNumber, relPath)
}

func findEntry(parsed vault.ParsedFile, id string) (vault.MemoryEntry, bool) {
	for _, entry := range parsed.Entries {
		if entry.ID == id {
			return entry, true
		}
	}
	return vault.MemoryEntry{}, false
}

// requireSingleLine rejects text that would break the single-line bullet format
// of docs/02-domain-models-and-contracts.md section 1.2. A newline in a fact,
// heading or source would shift line numbers and corrupt the trailing metadata
// of every following line, so it is refused before the transaction starts.
func requireSingleLine(field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return memerr.New(memerr.InvalidArgument, "%s must be a single line", field)
	}
	return nil
}

// buildEntryLine builds one bullet with the recommended trailing metadata. The
// `added` date is the provided date; source values are kept verbatim.
func buildEntryLine(factText string, sources []string, added string) string {
	line := "- " + strings.TrimSpace(factText)
	parts := make([]string, 0, len(sources)+1)
	for _, source := range sources {
		if source = strings.TrimSpace(source); source != "" {
			parts = append(parts, "source: "+source)
		}
	}
	if added != "" {
		parts = append(parts, "added: "+added)
	}
	if len(parts) > 0 {
		line += " [" + strings.Join(parts, "; ") + "]"
	}
	return line
}

// newEntryFileContent builds the content of a file created for a new entry.
func newEntryFileContent(heading, entryLine string) string {
	var b strings.Builder
	if heading = strings.TrimSpace(heading); heading != "" {
		b.WriteString("# ")
		b.WriteString(heading)
		b.WriteString("\n\n")
	}
	b.WriteString(entryLine)
	b.WriteString("\n")
	return b.String()
}

// headingFromPath derives a deterministic title from a new file's base name.
func headingFromPath(relPath string) string {
	base := path.Base(relPath)
	return strings.TrimSuffix(base, path.Ext(base))
}
