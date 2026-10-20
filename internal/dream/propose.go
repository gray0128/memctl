package dream

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file implements the Go-side provenance guard and deterministic assembly
// of docs/12-session-corpus-dreaming.md section 2.3 steps 2-4. The current agent
// contributes facts and integer session indices only; every `[source: ...; added:
// ...]` suffix is built here from the authoritative digests, and the resulting
// line must already appear verbatim in the file content the agent submitted.
// A violation is refused before the proposal is persisted, so a rejected input
// never leaves a reviewable artifact behind.

// ProposeLocal validates one DreamProposeInput against the current local
// baseline, persists a ready `local-agent` proposal and returns it. It never
// writes the main repository and never calls a model.
func (s *Service) ProposeLocal(ctx context.Context, input DreamProposeInput) (ProposeResult, error) {
	if err := validateProposeShape(input); err != nil {
		return ProposeResult{}, err
	}
	base, head, revision, err := s.fixLocalBaseline(ctx)
	if err != nil {
		return ProposeResult{}, err
	}
	if input.BaseHead != "" && input.BaseHead != head {
		return ProposeResult{}, memerr.New(memerr.RevisionMismatch,
			"the repository HEAD moved since prepare returned baseHead; run `memctl dream --prepare` again")
	}
	if input.BaseRevision != "" && input.BaseRevision != revision {
		return ProposeResult{}, memerr.New(memerr.RevisionMismatch,
			"the repository revision moved since prepare; run `memctl dream --prepare` again")
	}

	// Session indices are resolved against the prepare snapshot, not against a
	// freshly derived list: an index is only meaningful for the exact list the
	// agent saw. A missing snapshot is the documented "prepare first" error.
	snapshot, err := s.loadPrepare()
	if err != nil {
		return ProposeResult{}, err
	}
	if err := validateSessionIndices(input, len(snapshot.Sessions)); err != nil {
		return ProposeResult{}, err
	}

	accepted, warnings, err := s.acceptCandidates(input, snapshot, base)
	if err != nil {
		return ProposeResult{}, err
	}
	targets, err := assembleTargets(base, input.Files)
	if err != nil {
		return ProposeResult{}, err
	}
	if err := verifyAssembled(accepted, targets); err != nil {
		return ProposeResult{}, err
	}
	// The remaining lifecycle guards of docs/12 section 2.3 step 3 reuse the
	// code the Actions worker applies: an expired entry still inside its 30-day
	// hidden period is never deleted, and a newly introduced dangling or
	// ambiguous link is refused instead of becoming a warning.
	if err := verifyDeleteWindow(base, targets, s.timeNow()); err != nil {
		return ProposeResult{}, err
	}
	if err := verifyTargetLinks(base, targets); err != nil {
		return ProposeResult{}, err
	}
	changes, diff, changeWarnings, err := buildChanges(base, targets)
	if err != nil {
		return ProposeResult{}, err
	}
	if len(changes) == 0 {
		return ProposeResult{}, memerr.New(memerr.InvalidArgument,
			"the proposal changes no file; submit the assembled entries in files[]")
	}
	warnings = append(warnings, changeWarnings...)
	warnings = append(warnings, input.Warnings...)

	id, err := newID("dp")
	if err != nil {
		return ProposeResult{}, err
	}
	value := record{
		Proposal: Proposal{
			ProposalID:           id,
			RepoID:               s.Source.RepoID,
			BaseRevision:         revision,
			BaseHead:             head,
			Runner:               RunnerLocal,
			Status:               StatusReady,
			PatchHash:            patchHash(changes),
			Diff:                 diff,
			Changes:              changes,
			SessionsScanned:      len(snapshot.Sessions),
			NewMemoriesExtracted: len(accepted),
			Warnings:             warnings,
			CreatedAt:            s.now(),
		},
		Planned:       plannedFiles(base, targets, changes),
		CorpusDigests: s.consumableDigests(snapshot, accepted),
	}
	if err := s.store.Save(value); err != nil {
		return ProposeResult{}, err
	}
	return ProposeResult{
		ProposalID:           id,
		Status:               StatusReady,
		Runner:               RunnerLocal,
		PatchHash:            value.PatchHash,
		SessionsScanned:      value.SessionsScanned,
		NewMemoriesExtracted: value.NewMemoriesExtracted,
		Changes:              changes,
		Diff:                 diff,
		Warnings:             warnings,
	}, nil
}

// validateProposeShape enforces the documented per-input limits before any
// repository access.
func validateProposeShape(input DreamProposeInput) error {
	if len(input.NewMemories) > MaxExtractedCandidates {
		return memerr.New(memerr.LimitExceeded,
			"the proposal extracts %d candidates; at most %d are accepted per proposal",
			len(input.NewMemories), MaxExtractedCandidates)
	}
	if len(input.Files) == 0 {
		return memerr.New(memerr.InvalidArgument, "files[] is required: it is the only source of proposal content")
	}
	for _, file := range input.Files {
		if _, err := vault.ValidateRelPath(file.Path); err != nil {
			return err
		}
		switch file.Action {
		case ChangeCreate, ChangeUpdate, ChangeDelete:
		default:
			return memerr.New(memerr.InvalidArgument,
				"file %q uses the unsupported action %q; use create, update or delete", file.Path, file.Action)
		}
	}
	for index, candidate := range input.NewMemories {
		if len(candidate.SessionIndices) == 0 {
			return memerr.New(memerr.InvalidArgument,
				"newMemories[%d] has no sessionIndices; every new memory must cite at least one session", index)
		}
	}
	return nil
}

// validateSessionIndices rejects an out-of-range index before any candidate is
// assembled.
func validateSessionIndices(input DreamProposeInput, count int) error {
	for index, candidate := range input.NewMemories {
		for _, value := range candidate.SessionIndices {
			if value < 0 || value >= count {
				return memerr.New(memerr.InvalidArgument,
					"newMemories[%d].sessionIndices contains %d, but the prepare bundle has %d session(s) indexed 0..%d",
					index, value, count, count-1)
			}
		}
	}
	return nil
}

// acceptedCandidate is one candidate that passed the provenance guard.
type acceptedCandidate struct {
	line       string
	path       string
	sessionIDs []string
}

// acceptCandidates applies the per-candidate guards: single-line fact, no
// hand-written metadata suffix, redaction and suspicious-instruction screening,
// a valid `until` not older than the cited sessions, and duplicate suppression.
// A duplicate of an existing normalized fact is skipped, not refused, and is
// exempt from the files[] comparison by construction.
func (s *Service) acceptCandidates(input DreamProposeInput, snapshot prepareSnapshot, base *baseline) ([]acceptedCandidate, []string, error) {
	known := knownFacts(base)
	var accepted []acceptedCandidate
	var warnings []string
	seen := map[string]bool{}
	for index, candidate := range input.NewMemories {
		if strings.ContainsAny(candidate.Fact, "\r\n") {
			return nil, nil, memerr.New(memerr.InvalidArgument,
				"newMemories[%d].fact must be one line without a newline", index)
		}
		fact := strings.TrimSpace(candidate.Fact)
		if fact == "" {
			return nil, nil, memerr.New(memerr.InvalidArgument,
				"newMemories[%d].fact is empty", index)
		}
		if strings.Contains(fact, "[source:") || strings.Contains(fact, "[added:") {
			return nil, nil, memerr.New(memerr.InvalidArgument,
				"newMemories[%d].fact must not carry source or added metadata; memctl assembles it from sessionIndices", index)
		}
		cleaned := corpus.Clean(fact)
		if s.SuspiciousMatcher != nil && corpus.Suspicious(s.SuspiciousMatcher, cleaned) {
			return nil, nil, memerr.New(memerr.InvalidArgument,
				"newMemories[%d].fact carries a suspicious instruction pattern and is refused", index)
		}
		redacted := corpus.Redact(cleaned)
		if redacted != fact {
			warnings = append(warnings,
				fmt.Sprintf("newMemories[%d].fact contained credentials or injected blocks and was redacted", index))
		}

		latest := ""
		var tags, sessionIDs []string
		seenIndex := map[int]bool{}
		for _, value := range candidate.SessionIndices {
			if seenIndex[value] {
				continue
			}
			seenIndex[value] = true
			session, ok := sessionByIndex(snapshot.Sessions, value)
			if !ok {
				return nil, nil, memerr.New(memerr.InvalidArgument,
					"newMemories[%d].sessionIndices references %d, which the prepare bundle does not contain", index, value)
			}
			tags = append(tags, session.SourceTag)
			sessionIDs = append(sessionIDs, session.SessionID)
			if session.Date > latest {
				latest = session.Date
			}
		}
		if strings.EqualFold(strings.TrimSpace(candidate.Kind), "time_limited") {
			if candidate.Until == "" {
				return nil, nil, memerr.New(memerr.InvalidArgument,
					"newMemories[%d] is time_limited but has no until date", index)
			}
			if _, err := time.Parse(vault.DateLayout, candidate.Until); err != nil {
				return nil, nil, memerr.New(memerr.InvalidArgument,
					"newMemories[%d].until %q is not a YYYY-MM-DD date", index, candidate.Until)
			}
			if latest != "" && candidate.Until < latest {
				return nil, nil, memerr.New(memerr.InvalidArgument,
					"newMemories[%d].until %s is earlier than the cited sessions' date %s", index, candidate.Until, latest)
			}
		}

		key := normalizeFact(redacted)
		if key != "" && (known[key] || seen[key]) {
			continue
		}
		seen[key] = true

		line := "- " + redacted + " [source: " + strings.Join(tags, "; source: ") + "; added: " + latest
		if strings.EqualFold(strings.TrimSpace(candidate.Kind), "time_limited") {
			line += "; until: " + candidate.Until
		}
		line += "]"
		accepted = append(accepted, acceptedCandidate{line: line, path: candidate.TargetPath, sessionIDs: sessionIDs})
	}
	return accepted, warnings, nil
}

// verifyDeleteWindow refuses a proposal that removes an expired time-limited
// entry before its 30-day hidden period ends. This is the same rule the apply
// path enforces, checked earlier so the current agent can correct the proposal in
// the same turn instead of after review. A changed or deleted file is compared
// line by line: an entry that is merely edited keeps its line and is not a
// deletion.
func verifyDeleteWindow(base *baseline, targets map[string]workFile, now time.Time) error {
	var deferred []string
	paths := make([]string, 0, len(base.files))
	for rel := range base.files {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		before := vault.ParseFile(rel, base.files[rel].revision, base.files[rel].content)
		if len(before.Entries) == 0 {
			continue
		}
		kept := map[string]bool{}
		if after, ok := targets[rel]; ok {
			for _, entry := range vault.ParseFile(rel, "", after.content).Entries {
				kept[entry.RawLine] = true
			}
		}
		for _, entry := range before.Entries {
			if kept[entry.RawLine] || entry.UntilDate == "" {
				continue
			}
			if vault.EntryExpired(entry.UntilDate, now) && !vault.DeletionDue(entry.UntilDate, now) {
				deferred = append(deferred, rel+": "+vault.DeletionBlockedReason(entry.UntilDate, now))
			}
		}
	}
	if len(deferred) == 0 {
		return nil
	}
	return memerr.New(memerr.DeletionDeferred,
		"the proposal removes an expired entry before its hidden period ends (%s); keep it until the named date",
		joinLimited(deferred, 3))
}

// verifyTargetLinks refuses a proposal that introduces a link problem the
// baseline did not have. A problem the repository already had is not this
// proposal's job to fix and never blocks it.
func verifyTargetLinks(base *baseline, targets map[string]workFile) error {
	known := problemSet(&fileSet{files: baselineTree(base)})
	current := problemSet(&fileSet{files: targetTree(targets)})
	var introduced []string
	for key := range current {
		if !known[key] {
			introduced = append(introduced, key)
		}
	}
	if len(introduced) == 0 {
		return nil
	}
	sort.Strings(introduced)
	return memerr.New(memerr.Unsupported,
		"the proposal introduces %d link problem(s): %s; add the [[target]] index links or restore the resolved files (INVALID_LINK)",
		len(introduced), joinLimited(introduced, 3))
}

// joinLimited joins up to limit items and appends a count of the rest, so a
// proposal touching many files produces a bounded message.
func joinLimited(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, "; ")
	}
	return strings.Join(values[:limit], "; ") + "; and more"
}

// targetTree materializes the proposed tree for the link check.
func targetTree(targets map[string]workFile) map[string][]byte {
	files := make(map[string][]byte, len(targets))
	for rel, file := range targets {
		files[rel] = file.content
	}
	return files
}

// knownFacts collects the normalized fact text of every entry in the baseline.
func knownFacts(base *baseline) map[string]bool {
	known := map[string]bool{}
	for rel, file := range base.files {
		parsed := vault.ParseFile(rel, file.revision, file.content)
		for _, entry := range parsed.Entries {
			if key := normalizeFact(entry.FactText); key != "" {
				known[key] = true
			}
		}
	}
	return known
}

// assembleTargets turns files[] into the resulting tree the proposal is built
// from. The fixed baseline supplies every file the agent did not resubmit, so a
// create/update/delete only replaces what it names.
func assembleTargets(base *baseline, files []ProposeFile) (map[string]workFile, error) {
	targets := make(map[string]workFile, len(base.files)+len(files))
	for rel, file := range base.files {
		targets[rel] = workFile{content: append([]byte(nil), file.content...), fsMode: file.fsMode}
	}
	for _, file := range files {
		if protectedPath(file.Path) {
			return nil, memerr.New(memerr.Unsupported,
				"files[] modifies the protected path %q; proposals never rewrite Git or workflow configuration", file.Path)
		}
		switch file.Action {
		case ChangeDelete:
			if file.Path == vault.EntryPointName {
				return nil, memerr.New(memerr.Unsupported,
					"files[] deletes %s; the entry file must always exist", vault.EntryPointName)
			}
			if _, ok := targets[file.Path]; !ok {
				return nil, memerr.New(memerr.InvalidArgument,
					"files[] deletes %q, which the repository does not contain", file.Path)
			}
			delete(targets, file.Path)
		case ChangeCreate, ChangeUpdate:
			if strings.TrimSpace(file.Content) == "" {
				return nil, memerr.New(memerr.InvalidArgument,
					"files[] %s %q has empty content", file.Action, file.Path)
			}
			mode := os.FileMode(0o644)
			if old, ok := base.files[file.Path]; ok {
				mode = old.fsMode
			}
			targets[file.Path] = workFile{content: []byte(file.Content), fsMode: mode}
		}
	}
	return targets, nil
}

// verifyAssembled enforces that every accepted candidate line already appears
// verbatim in the target file content the agent submitted. memctl never injects
// the line itself: the proposal is exactly what the reviewer sees.
func verifyAssembled(accepted []acceptedCandidate, targets map[string]workFile) error {
	for _, candidate := range accepted {
		if candidate.path == "" {
			return memerr.New(memerr.InvalidArgument,
				"a candidate has no targetPath; name the file that must contain its assembled line")
		}
		file, ok := targets[candidate.path]
		if !ok || file.content == nil {
			return memerr.New(memerr.InvalidArgument,
				"files[] has no create or update of %q, which must contain the assembled entry line", candidate.path)
		}
		if !containsLine(string(file.content), candidate.line) {
			return memerr.New(memerr.InvalidArgument,
				"files[] %q does not contain the assembled entry line %q verbatim; add it to that file's content",
				candidate.path, candidate.line)
		}
	}
	return nil
}

// containsLine reports whether text holds line as one complete line.
func containsLine(text, line string) bool {
	for _, candidate := range strings.Split(text, "\n") {
		if strings.TrimRight(candidate, " \t\r") == line {
			return true
		}
	}
	return false
}

// consumableDigests returns the digests whose material the accepted candidates
// actually cited, together with the content hash recorded at prepare time. Only
// cited sessions are consumed by apply: a bundle carries up to twelve digests so
// the agent can choose, and marking an unread session as mined would drop it
// from the next bundle for good.
func (s *Service) consumableDigests(snapshot prepareSnapshot, accepted []acceptedCandidate) []CorpusDigest {
	cache := s.corpusCache()
	cited := map[string]bool{}
	for _, candidate := range accepted {
		for _, id := range candidate.sessionIDs {
			cited[id] = true
		}
	}
	digests := make([]CorpusDigest, 0, len(snapshot.Digests))
	for _, digest := range snapshot.Digests {
		session, ok := snapshot.sessionByFile(digest.File)
		if !ok || !cited[session.SessionID] {
			continue
		}
		digests = append(digests, CorpusDigest{
			File:     cache.Path(session.Agent, session.SessionID),
			Revision: digestRevision(cache, session.Agent, session.SessionID),
		})
	}
	return digests
}
