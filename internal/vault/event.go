package vault

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/memerr"
)

// CheckKindStatus validates the optional `kind` and `status` values a caller
// asks memctl to write. Empty values stay empty; anything else must be a
// recognized value, so a typo is refused instead of silently becoming an
// unknown key. It returns the trimmed values.
func CheckKindStatus(kind, status string) (string, string, error) {
	kind = strings.TrimSpace(kind)
	status = strings.TrimSpace(status)
	if kind != "" && !ValidKind(kind) {
		return "", "", memerr.New(memerr.InvalidArgument,
			"kind %q must be one of %s, %s or %s", kind, KindState, KindEvent, KindProcedural)
	}
	if status != "" && !ValidStatus(status) {
		return "", "", memerr.New(memerr.InvalidArgument,
			"status %q must be %s or %s", status, StatusVerified, StatusHypothesis)
	}
	return kind, status, nil
}

// The `kind: event` guard of docs/18-agent-memory-review-and-optimization.md
// section 4.4.2. An event entry records what happened (a decision, a release,
// an incident); it is appended to, never rewritten. A write may move it to
// another managed file, but it must keep the same normalized fact text, the
// same `added` date and the `kind: event` marker somewhere in the files the
// write produces.

// eventKey is the identity an event keeps across a move.
func eventKey(entry MemoryEntry) string {
	return NormalizeFactText(entry.FactText) + "\x00" + entry.AddedDate
}

// EventRemovals lists the `kind: event` entries of before that no longer
// appear in after. before maps every path a write touches to its previous
// content (absent for a created file); after maps the same paths to their new
// content (absent for a deleted file). Files outside the write are unchanged,
// so they neither remove nor provide an event. An event present in after under
// another path of the same write counts as moved and is not reported. Each
// description is "path: fact (added date)", with the fact shortened.
func EventRemovals(before, after map[string][]byte) []string {
	type origin struct {
		path  string
		entry MemoryEntry
	}
	missing := map[string][]origin{}
	paths := make([]string, 0, len(before))
	for rel := range before {
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		for _, entry := range ParseFile(rel, "", before[rel]).Entries {
			if entry.IsEvent() {
				key := eventKey(entry)
				missing[key] = append(missing[key], origin{path: rel, entry: entry})
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	for rel, content := range after {
		for _, entry := range ParseFile(rel, "", content).Entries {
			if !entry.IsEvent() {
				continue
			}
			key := eventKey(entry)
			if queue := missing[key]; len(queue) > 0 {
				missing[key] = queue[1:]
			}
		}
	}
	var removed []string
	for _, rel := range paths {
		for _, entry := range ParseFile(rel, "", before[rel]).Entries {
			if !entry.IsEvent() {
				continue
			}
			key := eventKey(entry)
			queue := missing[key]
			if len(queue) == 0 || queue[0].path != rel || queue[0].entry.LineNumber != entry.LineNumber {
				continue
			}
			missing[key] = queue[1:]
			removed = append(removed, rel+": "+shortFact(entry.FactText)+" (added "+entry.AddedDate+")")
		}
	}
	return removed
}

// shortFact bounds a fact quoted in an error message.
func shortFact(fact string) string {
	const limit = 60
	if utf8.RuneCountInString(fact) <= limit {
		return fact
	}
	runes := []rune(fact)
	return string(runes[:limit]) + "..."
}
