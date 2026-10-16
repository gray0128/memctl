package git

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// MaxDiffPatchBytes bounds the total patch text returned for one commit. The
// documented limit is 4 MiB in docs/02-domain-models-and-contracts.md section
// 4.1; an oversized diff is refused instead of truncated.
const MaxDiffPatchBytes = 4 << 20

// commitArg matches an abbreviated or full object name. It rejects option-like
// values such as `--help` before they reach Git, and it accepts both SHA-1 and
// SHA-256 object ids.
var commitArg = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// FileDiff is one changed file of a commit as defined by
// docs/02-domain-models-and-contracts.md section 2.3. Binary files report
// Binary true, zero counts and an empty Patch because Git produces no text
// hunks for them.
type FileDiff struct {
	Path      string
	Additions int
	Deletions int
	Patch     string
	Binary    bool
}

// CommitDiff is the resolved commit hash together with its per-file diffs.
type CommitDiff struct {
	Hash  string
	Files []FileDiff
}

// DiffCommit resolves one commit and returns its per-file text diffs. It uses
// `-m --first-parent` so a merge commit previews the change against its first
// parent instead of producing no output. External diff drivers and textconv
// filters are disabled: memory content must not activate a user-configured
// program from a read-only view.
func DiffCommit(ctx context.Context, dir, commit string) (CommitDiff, error) {
	if !commitArg.MatchString(commit) {
		return CommitDiff{}, memerr.New(memerr.InvalidArgument,
			"commit %q is not a hexadecimal object name", commit)
	}
	resolved, err := Run(ctx, dir, "rev-parse", "--verify", "--quiet", commit+"^{commit}")
	if err != nil {
		return CommitDiff{}, memerr.New(memerr.NotFound, "commit %q does not exist", commit)
	}
	resolved = strings.TrimSpace(resolved)

	stats, order, err := diffNumstat(ctx, dir, resolved)
	if err != nil {
		return CommitDiff{}, err
	}

	files := make([]FileDiff, 0, len(order))
	totalPatch := 0
	for _, path := range order {
		file := stats[path]
		if !file.Binary {
			patch, _, overflow, err := runGitBounded(ctx, dir, Options{ExtraEnv: []string{"GIT_LITERAL_PATHSPECS=1"}}, MaxDiffPatchBytes-totalPatch,
				"diff-tree", "--no-commit-id", "-r", "--no-renames", "--root", "--no-color",
				"--no-ext-diff", "--no-textconv", "-m", "--first-parent", "-p",
				resolved, "--", path)
			if err != nil {
				return CommitDiff{}, err
			}
			if overflow {
				return CommitDiff{}, memerr.New(memerr.LimitExceeded,
					"the diff for commit %s exceeds the %d byte limit", resolved, MaxDiffPatchBytes)
			}
			totalPatch += len(patch)
			file.Patch = patch
		}
		files = append(files, file)
	}
	return CommitDiff{Hash: resolved, Files: files}, nil
}

// diffNumstat returns the documented additions/deletions per changed path,
// preserving Git's output order. A `-` count marks a binary file.
func diffNumstat(ctx context.Context, dir, commit string) (map[string]FileDiff, []string, error) {
	out, _, err := runGitRaw(ctx, dir, Options{}, "diff-tree",
		"--no-commit-id", "-r", "--no-renames", "--root", "--numstat", "-z",
		"--no-ext-diff", "--no-textconv", "-m", "--first-parent", commit)
	if err != nil {
		return nil, nil, err
	}

	stats := map[string]FileDiff{}
	var order []string
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\t", 3)
		if len(fields) != 3 || fields[2] == "" {
			continue
		}
		file := FileDiff{Path: fields[2]}
		if fields[0] == "-" || fields[1] == "-" {
			file.Binary = true
		} else {
			file.Additions, _ = strconv.Atoi(fields[0])
			file.Deletions, _ = strconv.Atoi(fields[1])
		}
		stats[file.Path] = file
		order = append(order, file.Path)
	}
	return stats, order, nil
}
