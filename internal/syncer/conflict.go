package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strings"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/state"
)

// Conflict file kinds.
const (
	KindText         = "text"
	KindBinary       = "binary"
	KindDeleteModify = "delete_modify"
	KindUnsupported  = "unsupported"
)

// Conflict phases from docs/02-domain-models-and-contracts.md section 2.2.
const (
	PhaseCaptured  = "captured"
	PhaseResolving = "resolving"
)

// IndexRevision is the deterministic digest of every staged index entry. It is
// used to prove that a replayed conflict stage matches the captured snapshot.
func IndexRevision(ctx context.Context, repo string) (string, error) {
	entries, err := git.IndexEntries(ctx, repo)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	for _, entry := range entries {
		writeHashField(hash, entry.Path)
		writeHashField(hash, string(rune('0'+entry.Stage)))
		writeHashField(hash, entry.Mode)
		writeHashField(hash, entry.BlobID)
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func writeHashField(hash interface{ Write([]byte) (int, error) }, value string) {
	var prefix [8]byte
	binary.BigEndian.PutUint64(prefix[:], uint64(len(value)))
	_, _ = hash.Write(prefix[:])
	_, _ = hash.Write([]byte(value))
}

// CaptureStopped reads the current stopped rebase's conflict into a snapshot.
// It does not abort the rebase; the caller decides whether to keep it active.
func CaptureStopped(ctx context.Context, repo, repoID, operationID, upstream, phase string) (*state.Conflict, error) {
	replayCommit, err := git.RebaseHead(ctx, repo)
	if err != nil {
		return nil, err
	}
	return CaptureSides(ctx, repo, repoID, operationID, upstream, phase, replayCommit)
}

// CaptureSides reads the current stopped operation's three-way conflict with an
// explicit replay commit, so the rebase snapshot and the diagnostic capture of
// a conflicting revert share one implementation. It does not abort the
// operation; the caller decides whether to keep it active.
func CaptureSides(ctx context.Context, repo, repoID, operationID, upstream, phase, replayCommit string) (*state.Conflict, error) {
	unmerged, err := git.UnmergedPaths(ctx, repo)
	if err != nil {
		return nil, err
	}
	indexRevision, err := IndexRevision(ctx, repo)
	if err != nil {
		return nil, err
	}
	conflictID, err := state.NewConflictID()
	if err != nil {
		return nil, err
	}
	conflict := &state.Conflict{
		ConflictID:    conflictID,
		RepoID:        repoID,
		OriginalHead:  "",
		ExpectedHead:  "",
		UpstreamHead:  upstream,
		ReplayCommit:  replayCommit,
		IndexRevision: indexRevision,
		OperationID:   operationID,
		Phase:         phase,
		DetectedAt:    state.Now(),
	}
	for _, path := range unmerged {
		file, err := captureFile(ctx, repo, path)
		if err != nil {
			return nil, err
		}
		conflict.Files = append(conflict.Files, file)
	}
	return conflict, nil
}

func captureFile(ctx context.Context, repo, path string) (state.ConflictFile, error) {
	entries, err := git.IndexEntries(ctx, repo)
	if err != nil {
		return state.ConflictFile{}, err
	}
	file := state.ConflictFile{Path: path}
	byStage := map[int]git.IndexEntry{}
	for _, entry := range entries {
		if entry.Path == path {
			byStage[entry.Stage] = entry
		}
	}
	base, err := captureSide(ctx, repo, byStage[1])
	if err != nil {
		return state.ConflictFile{}, err
	}
	onto, err := captureSide(ctx, repo, byStage[2])
	if err != nil {
		return state.ConflictFile{}, err
	}
	replayed, err := captureSide(ctx, repo, byStage[3])
	if err != nil {
		return state.ConflictFile{}, err
	}
	file.Base, file.Onto, file.Replayed = base, onto, replayed

	switch {
	case base.Binary || onto.Binary || replayed.Binary:
		file.Kind = KindBinary
	case base.Exists && (!onto.Exists || !replayed.Exists):
		file.Kind = KindDeleteModify
	case !base.Exists && onto.Exists != replayed.Exists:
		// A path added on exactly one side of the replay is a rename or
		// rename/add shape that memctl does not resolve automatically.
		file.Kind = KindUnsupported
	default:
		file.Kind = KindText
	}
	return file, nil
}

func captureSide(ctx context.Context, repo string, entry git.IndexEntry) (state.ConflictSide, error) {
	if entry.BlobID == "" {
		return state.ConflictSide{Exists: false}, nil
	}
	content, err := git.CatFileBlob(ctx, repo, entry.BlobID)
	if err != nil {
		return state.ConflictSide{}, err
	}
	side := state.ConflictSide{
		Exists: true,
		BlobID: entry.BlobID,
		Mode:   entry.Mode,
	}
	if strings.ContainsRune(content, 0) {
		side.Binary = true
	}
	side.Content = base64.StdEncoding.EncodeToString([]byte(content))
	return side, nil
}

// DecodeSide returns the raw bytes of one captured side.
func DecodeSide(side state.ConflictSide) []byte {
	decoded, err := base64.StdEncoding.DecodeString(side.Content)
	if err != nil {
		return nil
	}
	return decoded
}
