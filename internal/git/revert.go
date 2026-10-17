package git

import (
	"context"
	"errors"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// ResolveCommit resolves one object name to a full commit hash. Option-like and
// non-hexadecimal values are refused before they reach Git, and a name that
// does not resolve to a commit is NotFound.
func ResolveCommit(ctx context.Context, dir, rev string) (string, error) {
	if !commitArg.MatchString(rev) {
		return "", memerr.New(memerr.InvalidArgument,
			"commit %q is not a hexadecimal object name", rev)
	}
	out, err := Run(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	resolved := strings.TrimSpace(out)
	if err != nil || resolved == "" {
		return "", memerr.New(memerr.NotFound, "commit %q does not exist", rev)
	}
	return resolved, nil
}

// CommitParents returns the parent hashes of one commit. A root commit has no
// parents; a merge commit has two or more.
func CommitParents(ctx context.Context, dir, commit string) ([]string, error) {
	out, err := Run(ctx, dir, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return nil, memerr.New(memerr.NotFound, "commit %q does not exist", commit)
	}
	return fields[1:], nil
}

// IsAncestor reports whether rev is an ancestor of ref. It reads Git's
// documented exit code instead of comparing merge-base output, so two histories
// without a common ancestor answer "no" and a real Git failure stays an error.
func IsAncestor(ctx context.Context, dir, rev, ref string) (bool, error) {
	_, err := Run(ctx, dir, "merge-base", "--is-ancestor", rev, ref)
	if err == nil {
		return true, nil
	}
	var execErr *Error
	if errors.As(err, &execErr) && execErr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// Revert applies the inverse of one single-parent commit and creates the
// resulting commit without opening an editor. Git either commits atomically or
// stops with an unmerged index and REVERT_HEAD; the application service decides
// whether to diagnose and abort a stopped revert.
func Revert(ctx context.Context, dir, commit string) error {
	_, err := Run(ctx, dir, "revert", "--no-edit", commit)
	return err
}

// RevertAbort aborts the revert this process started.
func RevertAbort(ctx context.Context, dir string) error {
	_, err := Run(ctx, dir, "revert", "--abort")
	return err
}
