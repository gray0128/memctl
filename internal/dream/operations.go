package dream

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file assembles a local proposal from structured operations, from
// docs/18-agent-memory-review-and-optimization.md section 4.3.2. The current
// agent names what to change (append, replace or delete one entry) and which
// sessions prove it; memctl builds every `[source: ...; added: ...]` suffix
// itself and edits the baseline files in memory without touching any other
// byte. The assembled tree then goes through exactly the same guards as a
// files[] proposal: provenance, redaction and suspicious-instruction checks,
// parent directories, the 30-day deletion window, links and orphans, the
// MEMORY.md size warning and the patch hash.

// Operation kinds of DreamOperation.Op.
const (
	OpAppendEntry  = "append_entry"
	OpReplaceEntry = "replace_entry"
	OpDeleteEntry  = "delete_entry"
)

// MaxProposeOperations bounds the operations of one proposal. It matches the
// newMemories bound, so both channels admit the same amount of change.
const MaxProposeOperations = MaxExtractedCandidates

// DreamOperation is one structured change of DreamProposeInput.operations.
//
//   - append_entry: path, fact and sessionIndices are required; heading, until
//     and indexSummary are optional. The entry is appended at the end of the
//     heading's section (or of the file); a new file is created when path does
//     not exist yet and its parent directory does. A path other than MEMORY.md
//     that MEMORY.md does not link yet gets one `- [[target]]` line, with
//     ` — <indexSummary>` when given, at the end of MEMORY.md's `## Index`.
//   - replace_entry: path, fact, sessionIndices and one of targetEntryId or
//     targetRawLine are required; until is optional. The target line is
//     replaced by the new fact with freshly assembled metadata.
//   - delete_entry: path and one of targetEntryId or targetRawLine are
//     required; reason and sessionIndices are optional. The target line is
//     removed.
//
// targetEntryId is the entry id of the prepare bundle baseline (the id field of
// relatedExistingEntries); targetRawLine is the exact entry line of that
// baseline file. Both must resolve to exactly one entry.
type DreamOperation struct {
	Op             string `json:"op"`
	Path           string `json:"path"`
	Heading        string `json:"heading,omitempty"`
	Fact           string `json:"fact,omitempty"`
	Until          string `json:"until,omitempty"`
	IndexSummary   string `json:"indexSummary,omitempty"`
	TargetEntryID  string `json:"targetEntryId,omitempty"`
	TargetRawLine  string `json:"targetRawLine,omitempty"`
	Reason         string `json:"reason,omitempty"`
	SessionIndices []int  `json:"sessionIndices,omitempty"`
}

// validateOperationsShape enforces the per-input rules of the operations
// channel before any repository access.
func validateOperationsShape(input DreamProposeInput) error {
	if len(input.Files) > 0 || len(input.NewMemories) > 0 {
		return memerr.New(memerr.InvalidArgument,
			"operations[] cannot be combined with files[] or newMemories[]; submit either structured operations or whole files")
	}
	if len(input.Operations) > MaxProposeOperations {
		return memerr.New(memerr.LimitExceeded,
			"the proposal carries %d operations; at most %d are accepted per proposal",
			len(input.Operations), MaxProposeOperations)
	}
	for index, op := range input.Operations {
		if _, err := vault.ValidateRelPath(op.Path); err != nil {
			return memerr.New(memerr.InvalidArgument, "operations[%d].path: %v", index, err)
		}
		switch op.Op {
		case OpAppendEntry, OpReplaceEntry:
			if len(op.SessionIndices) == 0 {
				return memerr.New(memerr.InvalidArgument,
					"operations[%d] (%s) has no sessionIndices; every written fact must cite at least one session", index, op.Op)
			}
		case OpDeleteEntry:
		default:
			return memerr.New(memerr.InvalidArgument,
				"operations[%d] uses the unsupported op %q; use append_entry, replace_entry or delete_entry", index, op.Op)
		}
		if op.Op != OpAppendEntry && op.TargetEntryID == "" && op.TargetRawLine == "" {
			return memerr.New(memerr.InvalidArgument,
				"operations[%d] (%s) needs targetEntryId or targetRawLine", index, op.Op)
		}
	}
	return nil
}

// fileEdit collects the operations of one target file. Replacements and
// deletions address baseline line numbers, so they are applied before the
// appends and never shift each other.
type fileEdit struct {
	replace map[int]string
	remove  map[int]bool
	appends []appendLine
	summary string
}

// appendLine is one entry line appended to a section.
type appendLine struct {
	heading string
	line    string
}

// assembleOperations turns operations[] into the target tree. It returns the
// tree, the accepted lines the provenance guard must find verbatim, the cited
// sessions that apply marks consumed, and review warnings.
func (s *Service) assembleOperations(ops []DreamOperation, snapshot prepareSnapshot, base *baseline) (map[string]workFile, []acceptedCandidate, []acceptedCandidate, []string, error) {
	now := s.timeNow()
	known := knownFacts(base)
	seen := map[string]bool{}
	edits := map[string]*fileEdit{}
	var accepted, cited []acceptedCandidate
	var warnings []string
	editFor := func(rel string) *fileEdit {
		if edits[rel] == nil {
			edits[rel] = &fileEdit{replace: map[int]string{}, remove: map[int]bool{}}
		}
		return edits[rel]
	}

	for index, op := range ops {
		rel, err := vault.ValidateRelPath(op.Path)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		if protectedPath(rel) {
			return nil, nil, nil, nil, memerr.New(memerr.Unsupported,
				"operations[%d] modifies the protected path %q; proposals never rewrite Git or workflow configuration", index, rel)
		}
		if kind := vault.ClassifyKind(rel); kind != vault.KindEntryPoint && kind != vault.KindMarkdown {
			return nil, nil, nil, nil, memerr.New(memerr.InvalidArgument,
				"operations[%d].path %q is not a Markdown memory file", index, rel)
		}
		if base.skipped[rel] {
			return nil, nil, nil, nil, memerr.New(memerr.Unsupported,
				"operations[%d].path %q is not exported to proposals", index, rel)
		}

		var tags, sessionIDs []string
		latest := ""
		if len(op.SessionIndices) > 0 {
			tags, sessionIDs, latest, err = citedSessions(index, op.SessionIndices, snapshot)
			if err != nil {
				return nil, nil, nil, nil, err
			}
		}

		switch op.Op {
		case OpAppendEntry:
			fact, err := s.operationFact(index, op.Fact)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if err := singleLineField(index, "heading", op.Heading); err != nil {
				return nil, nil, nil, nil, err
			}
			summary, err := s.operationIndexSummary(index, op.IndexSummary)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			until, err := operationUntil(index, op.Until, latest)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			key := normalizeFact(fact)
			if key != "" && (known[key] || seen[key]) {
				warnings = append(warnings, fmt.Sprintf(
					"operations[%d] duplicates an existing entry and was skipped", index))
				continue
			}
			seen[key] = true
			if _, exists := base.files[rel]; !exists {
				parent := path.Dir(rel)
				if parent != "." && !base.dirs[parent] {
					return nil, nil, nil, nil, memerr.New(memerr.NotFound,
						"operations[%d].path %q sits under %s/, which does not exist; memctl does not create directories, so create the directory first",
						index, rel, parent)
				}
			}
			line := assembleEntryLine(fact, tags, latest, until)
			edit := editFor(rel)
			edit.appends = append(edit.appends, appendLine{heading: op.Heading, line: line})
			if edit.summary == "" {
				edit.summary = summary
			}
			candidate := acceptedCandidate{line: line, path: rel, sessionIDs: sessionIDs}
			accepted = append(accepted, candidate)
			cited = append(cited, candidate)

		case OpReplaceEntry, OpDeleteEntry:
			entry, err := resolveTarget(index, op, rel, base)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			if entry.UntilDate != "" && vault.EntryExpired(entry.UntilDate, now) && !vault.DeletionDue(entry.UntilDate, now) {
				return nil, nil, nil, nil, memerr.New(memerr.DeletionDeferred,
					"operations[%d] (%s) targets an expired entry of %s that is still inside its hidden period: %s",
					index, op.Op, rel, vault.DeletionBlockedReason(entry.UntilDate, now))
			}
			edit := editFor(rel)
			if _, taken := edit.replace[entry.LineNumber]; taken || edit.remove[entry.LineNumber] {
				return nil, nil, nil, nil, memerr.New(memerr.InvalidArgument,
					"operations[%d] targets line %d of %s, which another operation already changes", index, entry.LineNumber, rel)
			}
			if op.Op == OpDeleteEntry {
				edit.remove[entry.LineNumber] = true
				if len(sessionIDs) > 0 {
					cited = append(cited, acceptedCandidate{path: rel, sessionIDs: sessionIDs})
				}
				continue
			}
			fact, err := s.operationFact(index, op.Fact)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			until, err := operationUntil(index, op.Until, latest)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			line := assembleEntryLine(fact, tags, latest, until)
			edit.replace[entry.LineNumber] = line
			candidate := acceptedCandidate{line: line, path: rel, sessionIDs: sessionIDs}
			accepted = append(accepted, candidate)
			cited = append(cited, candidate)
		}
	}

	targets := make(map[string]workFile, len(base.files)+len(edits))
	for rel, file := range base.files {
		targets[rel] = workFile{content: append([]byte(nil), file.content...), fsMode: file.fsMode}
	}
	paths := make([]string, 0, len(edits))
	for rel := range edits {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		targets[rel] = applyFileEdit(rel, base, edits[rel])
	}
	if err := linkAppendedFiles(targets, paths, edits); err != nil {
		return nil, nil, nil, nil, err
	}
	return targets, accepted, cited, warnings, nil
}

// citedSessions resolves sessionIndices against the prepare snapshot and
// returns the source tags, the session ids and the newest session date.
func citedSessions(index int, indices []int, snapshot prepareSnapshot) ([]string, []string, string, error) {
	var tags, ids []string
	latest := ""
	seen := map[int]bool{}
	for _, value := range indices {
		if seen[value] {
			continue
		}
		seen[value] = true
		session, ok := sessionByIndex(snapshot.Sessions, value)
		if !ok {
			return nil, nil, "", memerr.New(memerr.InvalidArgument,
				"operations[%d].sessionIndices references %d, which the prepare bundle does not contain", index, value)
		}
		tags = append(tags, session.SourceTag)
		ids = append(ids, session.SessionID)
		if session.Date > latest {
			latest = session.Date
		}
	}
	return tags, ids, latest, nil
}

// operationFact validates one fact: a single non-empty line without a
// hand-written metadata suffix, the expired placeholder, an unredacted
// credential or an injected instruction.
func (s *Service) operationFact(index int, raw string) (string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return "", memerr.New(memerr.InvalidArgument, "operations[%d].fact must be one line without a newline", index)
	}
	fact := strings.TrimSpace(raw)
	if fact == "" {
		return "", memerr.New(memerr.InvalidArgument, "operations[%d].fact is empty", index)
	}
	lower := strings.ToLower(fact)
	if strings.Contains(lower, "[source:") || strings.Contains(lower, "[added:") {
		return "", memerr.New(memerr.InvalidArgument,
			"operations[%d].fact must not carry source or added metadata; memctl assembles it from sessionIndices", index)
	}
	if strings.Contains(fact, vault.ExpiredFactPlaceholder) {
		return "", memerr.New(memerr.InvalidArgument,
			"operations[%d].fact must not contain the expired entry placeholder", index)
	}
	cleaned := corpus.Clean(fact)
	if corpus.Suspicious(s.suspiciousMatcher(), fact, cleaned) {
		return "", memerr.New(memerr.InvalidArgument,
			"operations[%d].fact carries a suspicious instruction pattern and is refused", index)
	}
	if cleaned != fact || corpus.Redact(cleaned) != cleaned {
		return "", memerr.New(memerr.InvalidArgument,
			"operations[%d].fact contains an unredacted credential or an injected block; remove it and submit again", index)
	}
	return fact, nil
}

// operationIndexSummary validates the optional MEMORY.md index summary with
// the same rules as a fact, except that it carries no metadata at all.
func (s *Service) operationIndexSummary(index int, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if err := singleLineField(index, "indexSummary", raw); err != nil {
		return "", err
	}
	summary := strings.TrimSpace(raw)
	cleaned := corpus.Clean(summary)
	if corpus.Suspicious(s.suspiciousMatcher(), summary, cleaned) || cleaned != summary || corpus.Redact(cleaned) != cleaned ||
		strings.Contains(summary, "[") {
		return "", memerr.New(memerr.InvalidArgument,
			"operations[%d].indexSummary must be plain text without brackets, credentials or instructions", index)
	}
	return summary, nil
}

// singleLineField refuses a newline in a single-line operation field.
func singleLineField(index int, field, value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return memerr.New(memerr.InvalidArgument, "operations[%d].%s must be a single line", index, field)
	}
	return nil
}

// operationUntil validates the optional until date: YYYY-MM-DD and not earlier
// than the cited sessions.
func operationUntil(index int, raw, latest string) (string, error) {
	until := strings.TrimSpace(raw)
	if until == "" {
		return "", nil
	}
	if _, err := time.Parse(vault.DateLayout, until); err != nil {
		return "", memerr.New(memerr.InvalidArgument, "operations[%d].until %q is not a YYYY-MM-DD date", index, raw)
	}
	if latest != "" && until < latest {
		return "", memerr.New(memerr.InvalidArgument,
			"operations[%d].until %s is earlier than the cited sessions' date %s", index, until, latest)
	}
	return until, nil
}

// assembleEntryLine builds the authoritative entry line, in the same layout as
// a newMemories candidate.
func assembleEntryLine(fact string, tags []string, added, until string) string {
	line := "- " + fact + " [source: " + strings.Join(tags, "; source: ") + "; added: " + added
	if until != "" {
		line += "; until: " + until
	}
	return line + "]"
}

// resolveTarget finds the single baseline entry an operation targets.
func resolveTarget(index int, op DreamOperation, rel string, base *baseline) (vault.MemoryEntry, error) {
	file, ok := base.files[rel]
	if !ok {
		return vault.MemoryEntry{}, memerr.New(memerr.NotFound,
			"operations[%d] (%s) targets %q, which the repository does not contain", index, op.Op, rel)
	}
	entries := vault.ParseFile(rel, file.revision, file.content).Entries
	var byID, byLine []vault.MemoryEntry
	wantLine := strings.TrimRight(op.TargetRawLine, " \t\r")
	for _, entry := range entries {
		if op.TargetEntryID != "" && entry.ID == op.TargetEntryID {
			byID = append(byID, entry)
		}
		if wantLine != "" && strings.TrimRight(entry.RawLine, " \t\r") == wantLine {
			byLine = append(byLine, entry)
		}
	}
	if op.TargetEntryID != "" && len(byID) == 0 {
		return vault.MemoryEntry{}, memerr.New(memerr.NotFound,
			"operations[%d].targetEntryId %s is not an entry of %s at the prepare baseline", index, op.TargetEntryID, rel)
	}
	if wantLine != "" {
		if len(byLine) == 0 {
			return vault.MemoryEntry{}, memerr.New(memerr.NotFound,
				"operations[%d].targetRawLine is not an entry line of %s at the prepare baseline", index, rel)
		}
		if len(byLine) > 1 && op.TargetEntryID == "" {
			return vault.MemoryEntry{}, memerr.New(memerr.InvalidArgument,
				"operations[%d].targetRawLine matches %d entries of %s; pass targetEntryId instead", index, len(byLine), rel)
		}
	}
	if op.TargetEntryID != "" {
		entry := byID[0]
		if wantLine != "" && strings.TrimRight(entry.RawLine, " \t\r") != wantLine {
			return vault.MemoryEntry{}, memerr.New(memerr.InvalidArgument,
				"operations[%d] targetEntryId and targetRawLine name different entries of %s", index, rel)
		}
		return entry, nil
	}
	return byLine[0], nil
}

// applyFileEdit applies one file's replacements, deletions and appends to its
// baseline content, or builds a new file for appends to an absent path.
func applyFileEdit(rel string, base *baseline, edit *fileEdit) workFile {
	mode := os.FileMode(0o644)
	var content string
	if file, ok := base.files[rel]; ok {
		mode = file.fsMode
		parsed := vault.ParseFile(rel, file.revision, file.content)
		var b strings.Builder
		for _, line := range parsed.Lines {
			if edit.remove[line.Number] {
				continue
			}
			if replacement, ok := edit.replace[line.Number]; ok {
				b.WriteString(replacement)
			} else {
				b.WriteString(line.Text)
			}
			b.WriteString(line.Ending)
		}
		content = b.String()
	}
	for index, appended := range edit.appends {
		if content == "" && index == 0 {
			if _, existed := base.files[rel]; !existed {
				content = newOperationFileContent(rel, appended)
				continue
			}
		}
		content, _ = vault.ParseFile(rel, "", []byte(content)).WithAppendedLineInSection(appended.heading, appended.line)
	}
	return workFile{content: []byte(content), fsMode: mode}
}

// newOperationFileContent builds a file created by its first appended entry:
// a level-1 title (the heading, or the file name) and the entry line.
func newOperationFileContent(rel string, appended appendLine) string {
	heading := appended.heading
	if strings.TrimSpace(heading) == "" {
		base := path.Base(rel)
		heading = strings.TrimSuffix(base, path.Ext(base))
	}
	var b strings.Builder
	if title, hashes, _ := vault.ParseHeadingSpec(heading, 1); title != "" {
		b.WriteString(hashes)
		b.WriteString(" ")
		b.WriteString(title)
		b.WriteString("\n\n")
	}
	b.WriteString(appended.line)
	b.WriteString("\n")
	return b.String()
}

// linkAppendedFiles adds one `## Index` line to MEMORY.md for every appended
// file that MEMORY.md does not link yet. It is idempotent: an existing link,
// in any section, is kept and nothing is added.
func linkAppendedFiles(targets map[string]workFile, paths []string, edits map[string]*fileEdit) error {
	entry, ok := targets[vault.EntryPointName]
	if !ok {
		return memerr.New(memerr.NotFound, "the repository has no %s to index the new entries from", vault.EntryPointName)
	}
	all := make([]string, 0, len(targets))
	for rel := range targets {
		all = append(all, rel)
	}
	sort.Strings(all)
	index := vault.NewLinkIndex(all)
	content := entry.content
	changed := false
	for _, rel := range paths {
		if rel == vault.EntryPointName || len(edits[rel].appends) == 0 {
			continue
		}
		parsed := vault.ParseFile(vault.EntryPointName, "", content)
		linked := false
		for _, link := range parsed.Links {
			if index.Resolve(link.RawTarget).ResolvedPath == rel {
				linked = true
				break
			}
		}
		if linked {
			continue
		}
		target := vault.LinkTargetFor("", rel)
		if resolution := index.Resolve(target); resolution.ResolvedPath != rel {
			target = rel
		}
		line := "- [[" + target + "]]"
		if summary := edits[rel].summary; summary != "" {
			line += " — " + summary
		}
		updated, _ := parsed.WithAppendedLineInSection("## Index", line)
		content = []byte(updated)
		changed = true
	}
	if changed {
		targets[vault.EntryPointName] = workFile{content: content, fsMode: entry.fsMode}
	}
	return nil
}
