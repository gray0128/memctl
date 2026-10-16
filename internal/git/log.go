package git

import (
	"context"
	"strconv"
	"strings"

	"github.com/gray0128/memctl/internal/memerr"
)

// CommitInfo is one commit as read from `git log` for the read-only audit
// view. Parents, Timestamp and Files are filled from Git; Subject and Body are
// the raw commit message parts and are never reworded.
type CommitInfo struct {
	Hash      string
	Parents   []string
	Timestamp string
	Subject   string
	Body      string
	Files     []string
}

// Log returns up to limit commits reachable from HEAD in reverse chronological
// order. When before is set, commits at or after that hash are skipped so the
// value can be used as a stable pagination cursor. limit must be positive; the
// caller enforces the documented maximum.
func Log(ctx context.Context, dir string, limit int, before string) ([]CommitInfo, error) {
	if limit <= 0 {
		return nil, memerr.New(memerr.InvalidArgument, "git log requires a positive limit")
	}
	// One extra record tells the caller whether another page exists.
	count := limit + 1
	args := []string{
		"log",
		"-n", strconv.Itoa(count),
		"--no-color",
		"--no-renames",
		"--format=%x1e%H%x1f%P%x1f%aI%x1f%s%x1f%b%x1f",
		"--name-only",
	}
	if before != "" {
		// before is a client-supplied pagination cursor. It must be a bare
		// object name: an option-like value such as `--output=<file>` would be
		// interpreted by Git and could write an arbitrary path. --end-of-options
		// keeps the revision parser from treating any later value as an option.
		if !commitArg.MatchString(before) {
			return nil, memerr.New(memerr.InvalidArgument,
				"before %q is not a hexadecimal object name", before)
		}
		args = append(args, "--end-of-options", before)
	}
	args = append(args, "--")
	out, err := Run(ctx, dir, args...)
	if err != nil {
		return nil, err
	}

	commits := parseLog(out)
	if before != "" {
		// `git log <before>` includes <before> itself; drop it so the next page
		// starts strictly older than the cursor.
		if len(commits) > 0 && commits[0].Hash == before {
			commits = commits[1:]
		}
	}
	if len(commits) > limit {
		commits = commits[:limit]
	}
	return commits, nil
}

func parseLog(out string) []CommitInfo {
	var commits []CommitInfo
	for _, record := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(record) == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 6)
		if len(fields) != 6 {
			continue
		}
		commit := CommitInfo{
			Hash:      strings.TrimSpace(fields[0]),
			Timestamp: strings.TrimSpace(fields[2]),
			Subject:   fields[3],
			Body:      strings.TrimRight(fields[4], "\r\n"),
		}
		if parents := strings.Fields(fields[1]); len(parents) > 0 {
			commit.Parents = parents
		}
		for _, name := range strings.Fields(fields[5]) {
			commit.Files = append(commit.Files, name)
		}
		if commit.Hash == "" {
			continue
		}
		commits = append(commits, commit)
	}
	return commits
}
