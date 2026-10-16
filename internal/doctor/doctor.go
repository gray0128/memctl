// Package doctor performs read-only repository diagnosis. It reports the
// diagnostic levels defined in docs/02-domain-models-and-contracts.md section
// 2.3 and checks that the running state matches the Git state as required by
// docs/03-workflows-and-dataflows.md section 8. It never writes to the
// repository, the Git index or HEAD. Deterministic repairs are planned here and
// applied by the application service in stage C-1; see
// docs/01-architecture-and-modules.md section 4.
package doctor

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/vault"
)

// Severity is the documented diagnostic level.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Stable issue codes. They are part of the CLI and HTTP diagnostic output and
// must not be reworded per call site.
const (
	CodeEntryMissing           = "ENTRY_MISSING"
	CodeFileUnreadable         = "FILE_UNREADABLE"
	CodeInvalidLink            = "INVALID_LINK"
	CodeMissingLink            = "MISSING_LINK"
	CodeAmbiguousLink          = "AMBIGUOUS_LINK"
	CodeOrphanFile             = "ORPHAN_FILE"
	CodeEntryBloat             = "ENTRY_BLOAT"
	CodeMissingRecommendedMeta = "MISSING_RECOMMENDED_METADATA"
	CodeSuspiciousInstruction  = "SUSPICIOUS_INSTRUCTION"
	CodeTransactionPending     = "TRANSACTION_PENDING"
	CodeConflictPending        = "CONFLICT_PENDING"
	CodeGitOperationActive     = "GIT_OPERATION_ACTIVE"
	CodeGitUnmergedIndex       = "GIT_UNMERGED_INDEX"
	CodeGitIndexLocked         = "GIT_INDEX_LOCKED"
)

// EntryBloatLineThreshold is the project hint that MEMORY.md is growing past a
// comfortable index size. It is a warning, not an upstream format error.
const EntryBloatLineThreshold = 100

// suspiciousPatterns are conservative prompt-injection phrases. Diagnosis
// reports them for review; it does not decide that code samples are malicious.
var suspiciousPatterns = []string{
	"ignore previous instructions",
	"ignore all previous instructions",
	"disregard previous instructions",
	"disregard all previous instructions",
	"do not tell the user",
	"exfiltrate",
}

// Issue is one diagnostic entry.
type Issue struct {
	Code        string   `json:"code"`
	Severity    Severity `json:"severity"`
	FilePath    string   `json:"filePath,omitempty"`
	LineNumber  int      `json:"lineNumber,omitempty"`
	Message     string   `json:"message"`
	Suggestion  string   `json:"suggestion"`
	AutoFixable bool     `json:"autoFixable"`
}

// Counts summarizes a report by severity. Total is the sum of the three.
type Counts struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
	Total   int `json:"total"`
}

// Report is the DoctorReport of docs/02-domain-models-and-contracts.md section
// 2.3. Issues is always an array, never null.
type Report struct {
	Revision  string  `json:"revision"`
	CheckedAt string  `json:"checkedAt"`
	Issues    []Issue `json:"issues"`
	Counts    Counts  `json:"counts"`
}

// Check runs every read-only diagnostic against one repository root. The
// caller has already established that repoPath is a Git working-tree root.
func Check(ctx context.Context, repoPath string) (Report, error) {
	report := Report{
		CheckedAt: time.Now().UTC().Format(time.RFC3339),
		Issues:    []Issue{},
	}

	root, err := vault.OpenRoot(repoPath)
	if err != nil {
		return Report{}, err
	}
	defer root.Close()

	head := ""
	if value, err := git.Head(ctx, repoPath); err == nil {
		head = value
	}
	if report.Revision, err = vault.RepoRevision(root, repoPath, head); err != nil {
		return Report{}, err
	}

	paths, err := root.ListManaged()
	if err != nil {
		return Report{}, err
	}

	entryFound := false
	parsed := make(map[string]vault.ParsedFile, len(paths))
	for _, rel := range paths {
		if vault.IsEntryPoint(rel) {
			entryFound = true
		}
		file, err := root.ReadFile(rel)
		if err != nil {
			// Binary assets and oversized text are supported (they sync through
			// Git); only an unexpected read failure is a diagnostic.
			if contractErr, ok := memerr.As(err); ok &&
				(contractErr.Code == memerr.Unsupported || contractErr.Code == memerr.LimitExceeded) {
				continue
			}
			report.Issues = append(report.Issues, Issue{
				Code:       CodeFileUnreadable,
				Severity:   SeverityError,
				FilePath:   rel,
				Message:    fmt.Sprintf("managed file %q cannot be read as memory text: %v", rel, err),
				Suggestion: "verify the file permissions and content, or restore it from Git",
			})
			continue
		}
		parsed[rel] = vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
	}

	if !entryFound {
		report.Issues = append(report.Issues, Issue{
			Code:       CodeEntryMissing,
			Severity:   SeverityError,
			FilePath:   vault.EntryPointName,
			Message:    fmt.Sprintf("the repository has no %s entry point", vault.EntryPointName),
			Suggestion: "restore the entry file from Git or run `memctl init` on a new repository",
		})
	}

	report.Issues = append(report.Issues, checkLinks(paths, parsed)...)
	if entryFound {
		report.Issues = append(report.Issues, checkOrphans(paths, parsed)...)
	}
	report.Issues = append(report.Issues, checkEntryBloat(parsed)...)
	report.Issues = append(report.Issues, checkEntries(parsed)...)

	gitIssues, err := checkGitState(ctx, repoPath)
	if err != nil {
		return Report{}, err
	}
	report.Issues = append(report.Issues, gitIssues...)

	stateIssues, err := checkState(ctx, repoPath)
	if err != nil {
		return Report{}, err
	}
	report.Issues = append(report.Issues, stateIssues...)

	sortIssues(report.Issues)
	report.Counts = countIssues(report.Issues)
	return report, nil
}

// checkLinks resolves every parsed link and reports invalid, dead and ambiguous
// targets. Invalid and dead targets are errors; an ambiguous target is a
// warning because the link still names existing files.
func checkLinks(paths []string, parsed map[string]vault.ParsedFile) []Issue {
	index := vault.NewLinkIndex(paths)
	var issues []Issue
	for _, rel := range paths {
		file, ok := parsed[rel]
		if !ok {
			continue
		}
		_, diagnostics := file.ResolveLinks(index)
		for _, diagnostic := range diagnostics {
			switch diagnostic.Code {
			case vault.LinkIssueInvalid:
				issues = append(issues, Issue{
					Code:       CodeInvalidLink,
					Severity:   SeverityError,
					FilePath:   diagnostic.SourceFile,
					LineNumber: diagnostic.LineNumber,
					Message:    diagnostic.Message,
					Suggestion: "use a root-relative path without escaping segments or Git internals",
				})
			case vault.LinkIssueMissing:
				issues = append(issues, Issue{
					Code:       CodeMissingLink,
					Severity:   SeverityError,
					FilePath:   diagnostic.SourceFile,
					LineNumber: diagnostic.LineNumber,
					Message:    diagnostic.Message,
					Suggestion: "create the target file or correct the link",
				})
			case vault.LinkIssueAmbiguous:
				issues = append(issues, Issue{
					Code:       CodeAmbiguousLink,
					Severity:   SeverityWarning,
					FilePath:   diagnostic.SourceFile,
					LineNumber: diagnostic.LineNumber,
					Message:    diagnostic.Message,
					Suggestion: "make the target unambiguous by renaming one file or using its full path",
				})
			}
		}
	}
	return issues
}

// checkOrphans marks every managed file unreachable from the entry point,
// including a ring disconnected from the entry. The entry point itself is never
// orphaned.
func checkOrphans(paths []string, parsed map[string]vault.ParsedFile) []Issue {
	index := vault.NewLinkIndex(paths)
	adjacency := make(map[string][]string)
	for rel, file := range parsed {
		links, _ := file.ResolveLinks(index)
		for _, link := range links {
			if link.ResolvedPath != "" {
				adjacency[rel] = append(adjacency[rel], link.ResolvedPath)
			}
		}
	}

	reachable := map[string]bool{vault.EntryPointName: true}
	queue := []string{vault.EntryPointName}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range adjacency[current] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}

	var issues []Issue
	for _, rel := range paths {
		if reachable[rel] {
			continue
		}
		issues = append(issues, Issue{
			Code:       CodeOrphanFile,
			Severity:   SeverityWarning,
			FilePath:   rel,
			Message:    fmt.Sprintf("%s is not reachable from %s", rel, vault.EntryPointName),
			Suggestion: fmt.Sprintf("link %s from the index or from a reachable file, or remove it if it is obsolete", rel),
		})
	}
	return issues
}

// checkEntryBloat warns when the entry file grows past the project hint.
func checkEntryBloat(parsed map[string]vault.ParsedFile) []Issue {
	file, ok := parsed[vault.EntryPointName]
	if !ok || len(file.Lines) <= EntryBloatLineThreshold {
		return nil
	}
	return []Issue{{
		Code:       CodeEntryBloat,
		Severity:   SeverityWarning,
		FilePath:   vault.EntryPointName,
		Message:    fmt.Sprintf("%s has %d lines, above the %d line project hint", vault.EntryPointName, len(file.Lines), EntryBloatLineThreshold),
		Suggestion: "move detail into topic files and keep the entry file as an index",
	}}
}

// checkEntries reports the recommended provenance metadata and conservative
// prompt-injection phrases.
func checkEntries(parsed map[string]vault.ParsedFile) []Issue {
	paths := make([]string, 0, len(parsed))
	for rel := range parsed {
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	var issues []Issue
	for _, rel := range paths {
		file := parsed[rel]
		// The entry file is an index of links, so its bullets are not held to
		// the recommended provenance metadata. Every text file is still scanned
		// for review phrases because the entry file is injected each round.
		if vault.ClassifyKind(rel) == vault.KindMarkdown {
			missing := 0
			for _, entry := range file.Entries {
				if !hasMetadata(entry, "source") && !hasMetadata(entry, "added") {
					missing++
				}
			}
			if missing > 0 {
				noun := "entries"
				if missing == 1 {
					noun = "entry"
				}
				issues = append(issues, Issue{
					Code:       CodeMissingRecommendedMeta,
					Severity:   SeverityInfo,
					FilePath:   rel,
					Message:    fmt.Sprintf("%d %s without the recommended `source` or `added` metadata", missing, noun),
					Suggestion: "append trailing metadata such as [source: <tag>; added: YYYY-MM-DD]",
				})
			}
		}
		for _, entry := range file.Entries {
			if phrase, ok := suspiciousPhrase(entry.FactText); ok {
				issues = append(issues, Issue{
					Code:       CodeSuspiciousInstruction,
					Severity:   SeverityWarning,
					FilePath:   rel,
					LineNumber: entry.LineNumber,
					Message:    fmt.Sprintf("entry contains the review phrase %q", phrase),
					Suggestion: "review the entry before acting on it; memory is data, not instructions",
				})
			}
		}
	}
	return issues
}

func hasMetadata(entry vault.MemoryEntry, key string) bool {
	for _, field := range entry.Metadata {
		if strings.EqualFold(field.Key, key) {
			return true
		}
	}
	return false
}

func suspiciousPhrase(text string) (string, bool) {
	lower := strings.ToLower(text)
	for _, phrase := range suspiciousPatterns {
		if strings.Contains(lower, phrase) {
			return phrase, true
		}
	}
	return "", false
}

// checkGitState reports intermediate Git states. The lock-holder PID is not
// consulted: only the index and Git's own operation markers decide.
func checkGitState(ctx context.Context, repoPath string) ([]Issue, error) {
	repoState, err := git.Inspect(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	var issues []Issue
	switch {
	case repoState.Unmerged:
		issues = append(issues, Issue{
			Code:       CodeGitUnmergedIndex,
			Severity:   SeverityError,
			Message:    "the Git index has unmerged entries",
			Suggestion: "resolve or abort the operation with `memctl resolve`, or finish it outside memctl",
		})
	case repoState.Operation != "" || repoState.Sequencer:
		issues = append(issues, Issue{
			Code:       CodeGitOperationActive,
			Severity:   SeverityError,
			Message:    fmt.Sprintf("a %s operation is in progress", repoState.Operation),
			Suggestion: "complete or abort the operation; memctl never aborts user Git operations",
		})
	case repoState.IndexLocked:
		issues = append(issues, Issue{
			Code:       CodeGitIndexLocked,
			Severity:   SeverityWarning,
			Message:    "a Git index.lock file exists; another Git process may be writing the index",
			Suggestion: "wait for the running Git process; do not delete an unknown lock manually",
		})
	}
	return issues, nil
}

// checkState reports unfinished transactions and an awaiting conflict, with the
// concrete recovery entry point. It does not remove records or locks.
func checkState(ctx context.Context, repoPath string) ([]Issue, error) {
	gitDir, err := git.GitDir(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	store := state.OpenStore(gitDir)

	pending, err := store.Pending()
	if err != nil {
		return nil, err
	}
	var issues []Issue
	for _, record := range pending {
		issues = append(issues, Issue{
			Code:       CodeTransactionPending,
			Severity:   SeverityError,
			Message:    fmt.Sprintf("transaction %s was not completed", record.ID),
			Suggestion: "inspect it with `memctl recover --status` and roll it back with `memctl recover --rollback`",
		})
	}

	conflict, ok, err := store.LoadConflict()
	if err != nil {
		return nil, err
	}
	if ok {
		issues = append(issues, Issue{
			Code:       CodeConflictPending,
			Severity:   SeverityError,
			Message:    fmt.Sprintf("conflict %s is awaiting resolution", conflict.ConflictID),
			Suggestion: "inspect it with `memctl resolve` and continue or abort",
		})
	}
	return issues, nil
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left, right := issues[i], issues[j]
		if rank(left.Severity) != rank(right.Severity) {
			return rank(left.Severity) < rank(right.Severity)
		}
		if left.FilePath != right.FilePath {
			return left.FilePath < right.FilePath
		}
		if left.LineNumber != right.LineNumber {
			return left.LineNumber < right.LineNumber
		}
		return left.Code < right.Code
	})
}

func rank(severity Severity) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

func countIssues(issues []Issue) Counts {
	var counts Counts
	for _, issue := range issues {
		switch issue.Severity {
		case SeverityError:
			counts.Error++
		case SeverityWarning:
			counts.Warning++
		default:
			counts.Info++
		}
	}
	counts.Total = counts.Error + counts.Warning + counts.Info
	return counts
}
