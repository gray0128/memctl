package app

import (
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// Dreaming apply deletes entries by rewriting a file, so it never passes
// through App.DeleteEntry and its DELETION_DEFERRED gate. This file restores the
// same rule for the bulk path that the operator confirmed: an expired
// time-limited entry may only disappear once its 30-day hidden period is over.
// See docs/10-memory-lifecycle.md section 3.3.

// dreamDeferredEntries lists the expired entries a proposal would remove while
// they are still inside their hidden period. A proposal that removes only
// entries past the period, or entries without `until`, is allowed.
//
// The comparison is line-based on purpose: the worker rewrites whole files, so
// "the entry is gone" is exactly "its original bullet no longer appears". An
// entry that is merely edited keeps its line and is not a deletion.
func dreamDeferredEntries(root *vault.Root, plan dream.ApplyPlan, now time.Time) ([]string, error) {
	var deferred []string
	for _, change := range plan.Changes {
		previous, err := root.ReadFile(change.Path)
		if err != nil {
			// A create has no previous content, and a missing path is what a
			// create expects; neither can remove an entry.
			continue
		}
		before := vault.ParseFile(previous.RelPath, previous.FileRevision, []byte(previous.RawContent))
		kept := map[string]bool{}
		if change.Action != dream.ChangeDelete {
			after := vault.ParseFile(change.Path, "", []byte(change.Content))
			for _, entry := range after.Entries {
				kept[entry.RawLine] = true
			}
		}
		for _, entry := range before.Entries {
			if kept[entry.RawLine] || entry.UntilDate == "" {
				continue
			}
			if vault.EntryExpired(entry.UntilDate, now) && !vault.DeletionDue(entry.UntilDate, now) {
				deferred = append(deferred, change.Path+": "+vault.DeletionBlockedReason(entry.UntilDate, now))
			}
		}
	}
	return deferred, nil
}

// requireDreamDeleteWindow refuses a proposal that removes an expired entry
// before its hidden period ends. The error names the file and the first allowed
// date so the operator can regenerate the proposal later instead of guessing.
func (a *App) requireDreamDeleteWindow(session *writeSession, plan dream.ApplyPlan) error {
	deferred, err := dreamDeferredEntries(session.root, plan, a.clock())
	if err != nil {
		return err
	}
	if len(deferred) == 0 {
		return nil
	}
	return memerr.New(memerr.DeletionDeferred,
		"the proposal removes an expired entry before its hidden period ends (%s); regenerate it after that date",
		joinLimited(deferred, 3))
}

// joinLimited joins up to limit items and appends a count of the rest, so a
// proposal touching many files produces a bounded error message.
func joinLimited(values []string, limit int) string {
	if len(values) <= limit {
		return strings.Join(values, "; ")
	}
	return strings.Join(values[:limit], "; ") + "; and more"
}
