package app

import (
	"context"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// PutFile is the managed single-file write behind PUT /api/v1/files/{path...}.
// expectedRevision is a fileRevision or the absent literal. An empty value means
// the caller supplied no precondition, which the HTTP layer reports as 428.
func (a *App) PutFile(ctx context.Context, path, expectedRevision, rawContent, agent, sessionID, device string) (MutationResult, error) {
	if expectedRevision == "" {
		return MutationResult{}, memerr.New(memerr.PreconditionRequired,
			"writing a file requires If-Match with a fileRevision, or If-None-Match: * to create it")
	}
	return a.Apply(ctx, MutationRequest{
		Changes: []FileChange{{
			Path:             path,
			ExpectedRevision: expectedRevision,
			Action:           ActionPut,
			RawContent:       rawContent,
		}},
		Agent:     agent,
		SessionID: sessionID,
		Device:    device,
	})
}

// MoveFileRequest is the input of POST /api/v1/files:move. BaseRevision binds
// the whole-repository baseline because the move rewrites every parsed link
// that resolved to the source file.
type MoveFileRequest struct {
	FromPath     string
	ToPath       string
	BaseRevision string
	Agent        string
	SessionID    string
	Device       string
}

// MoveFile moves one managed file and rewrites every parsed link that resolved
// to it, as one multi-file transaction. It refuses to overwrite an existing
// destination and requires the repository baseRevision.
func (a *App) MoveFile(ctx context.Context, req MoveFileRequest) (MutationResult, error) {
	if req.BaseRevision == "" {
		return MutationResult{}, memerr.New(memerr.PreconditionRequired,
			"moving a file requires the repository baseRevision")
	}
	from, err := vault.ValidateRelPath(req.FromPath)
	if err != nil {
		return MutationResult{}, err
	}
	to, err := vault.ValidateRelPath(req.ToPath)
	if err != nil {
		return MutationResult{}, err
	}
	if from == to {
		return MutationResult{}, memerr.New(memerr.InvalidArgument,
			"source and destination path are identical: %q", from)
	}
	if vault.IsEntryPoint(from) {
		return MutationResult{}, memerr.New(memerr.InvalidArgument,
			"the entry file %s cannot be moved; memory repositories require it at the root", vault.EntryPointName)
	}

	session, err := a.beginWrite(ctx)
	if err != nil {
		return MutationResult{}, err
	}
	defer session.Close()
	ctx = session.ctx

	if err := a.requireWriteBaseline(ctx, session, req.BaseRevision); err != nil {
		return MutationResult{}, err
	}
	plans, err := planMove(session, from, to)
	if err != nil {
		return MutationResult{}, err
	}
	result, err := a.commit(ctx, session, "move", MutationRequest{Agent: req.Agent, SessionID: req.SessionID, Device: req.Device}, plans)
	if err != nil {
		return result, err
	}
	return a.finishWrite(session, result), nil
}

// planMove builds the complete change list of one move: the move itself plus a
// put for every file whose parsed links pointed at the source. Resolution uses
// the file set before the move, so only links that actually resolved to the
// source are rewritten.
func planMove(session *writeSession, from, to string) ([]plannedChange, error) {
	source, err := session.root.ReadFile(from)
	if err != nil {
		return nil, err
	}
	sourceMeta, err := session.root.Stat(from)
	if err != nil {
		return nil, err
	}
	if sourceMeta.Links > 1 {
		return nil, memerr.New(memerr.Unsupported,
			"path %q has %d hard links; refusing to move content shared with another name", from, sourceMeta.Links)
	}
	if err := requireAbsentDestination(session, from, to); err != nil {
		return nil, err
	}
	if err := session.root.RequireParentDir(to); err != nil {
		return nil, err
	}

	paths, err := session.root.ListManaged()
	if err != nil {
		return nil, err
	}
	index := vault.NewLinkIndex(paths)

	moveContent, _ := vault.RewriteLinks(vault.ParseFile(from, source.FileRevision, []byte(source.RawContent)), index, from, to)
	gitMode := vault.GitMode(source.Mode)
	plans := []plannedChange{{
		path:            to,
		action:          actionMove,
		content:         moveContent,
		before:          []byte(source.RawContent),
		existedBefore:   true,
		fsMode:          source.Mode,
		gitMode:         gitMode,
		plannedRevision: vault.FileRevision(gitMode, []byte(moveContent)),
		moveFrom:        from,
	}}

	total := len(moveContent)
	count := 1
	for _, rel := range paths {
		if rel == from {
			continue
		}
		file, err := session.root.ReadFile(rel)
		if err != nil {
			// Binary assets and oversized text cannot contain parsed links.
			if contractErr, ok := memerr.As(err); ok &&
				(contractErr.Code == memerr.Unsupported || contractErr.Code == memerr.LimitExceeded) {
				continue
			}
			return nil, err
		}
		rewritten, changed := vault.RewriteLinks(
			vault.ParseFile(rel, file.FileRevision, []byte(file.RawContent)), index, from, to)
		if !changed {
			continue
		}
		meta, err := session.root.Stat(rel)
		if err != nil {
			return nil, err
		}
		if meta.Links > 1 {
			return nil, memerr.New(memerr.Unsupported,
				"path %q has %d hard links; refusing to rewrite content shared with another name", rel, meta.Links)
		}
		count++
		total += len(rewritten)
		if count > MaxMutationFiles {
			return nil, memerr.New(memerr.LimitExceeded,
				"the move rewrites %d files, exceeding the %d file limit", count, MaxMutationFiles)
		}
		if total > MaxMutationBytes {
			return nil, memerr.New(memerr.LimitExceeded,
				"the move rewrites %d bytes, exceeding the %d byte per-request limit", total, MaxMutationBytes)
		}
		fileGitMode := vault.GitMode(file.Mode)
		plans = append(plans, plannedChange{
			path:            rel,
			action:          ActionPut,
			content:         rewritten,
			before:          []byte(file.RawContent),
			existedBefore:   true,
			fsMode:          file.Mode,
			gitMode:         fileGitMode,
			plannedRevision: vault.FileRevision(fileGitMode, []byte(rewritten)),
		})
	}
	return plans, nil
}

// requireAbsentDestination refuses to overwrite an existing target. A rename
// that changes only letter case is the one allowed exception: on a
// case-insensitive filesystem the source already answers to the destination
// spelling, and Root.RenameFile performs it through an intermediate name.
func requireAbsentDestination(session *writeSession, from, to string) error {
	if strings.EqualFold(from, to) {
		return nil
	}
	meta, err := session.root.Stat(to)
	if err != nil {
		return err
	}
	if meta.Exists {
		return memerr.New(memerr.InvalidArgument,
			"destination %q already exists; moving never overwrites an existing file", to)
	}
	return nil
}
