package app

import (
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// A whole-file write is built from a projected read: App.ReadFile replaces the
// fact text of every expired entry with vault.ExpiredFactPlaceholder but keeps
// the on-disk fileRevision as the baseline. Without the steps below, writing
// that content back would overwrite the hidden facts with the placeholder, and
// dropping a placeholder line would delete an entry without the 30-day hidden
// period that DeleteEntry and ApplyDream enforce. See
// docs/10-memory-lifecycle.md section 3.3 and
// docs/18-agent-memory-review-and-optimization.md section 2.1.1.

// reconcileExpiredWrites runs on the validated plans of one Apply request. It
// first restores each unchanged placeholder line to the original expired entry
// on disk, then refuses the request when it still removes an expired entry
// inside its hidden period. A put whose content changes is re-planned with the
// restored content and its new revision.
func (a *App) reconcileExpiredWrites(plans []plannedChange) error {
	now := a.clock()
	var deferred []string
	for i := range plans {
		plan := &plans[i]
		if !expiryApplies(plan.path) {
			continue
		}
		switch plan.action {
		case ActionPut:
			restored, err := restoreExpiredPlaceholders(plan.path, plan.before, plan.content, now)
			if err != nil {
				return err
			}
			if restored != plan.content {
				plan.content = restored
				plan.plannedRevision = vault.FileRevision(plan.gitMode, []byte(restored))
			}
			deferred = append(deferred, expiredRemovals(plan.path, plan.before, plan.content, false, now)...)
		case ActionDelete:
			deferred = append(deferred, expiredRemovals(plan.path, plan.before, "", true, now)...)
		}
	}
	if len(deferred) == 0 {
		return nil
	}
	return memerr.New(memerr.DeletionDeferred,
		"the write removes an expired entry before its hidden period ends (%s); keep the line or its placeholder, and delete it after that date",
		joinLimited(deferred, 3))
}

// expiryApplies reports whether a path gets the read-time hide projection, so
// only content that a projected read can produce is reconciled.
func expiryApplies(relPath string) bool {
	kind := vault.ClassifyKind(relPath)
	return kind == vault.KindEntryPoint || kind == vault.KindMarkdown
}

// restoreExpiredPlaceholders replaces every placeholder line in content with
// the original line of the expired entry it hides on disk, followed by the
// entry's DetailLines that the projection left out. A placeholder line
// matches when it is byte for byte the projection of an expired entry in
// before, that is the placeholder followed by the unchanged trailing metadata.
// Several entries with the same projection are restored in their on-disk
// order. A placeholder line that matches no remaining expired entry is not the
// output of a read, for example a fabricated or duplicated placeholder or one
// whose metadata was edited, and the write is refused with INVALID_ARGUMENT.
func restoreExpiredPlaceholders(relPath string, before []byte, content string, now time.Time) (string, error) {
	if !strings.Contains(content, vault.ExpiredFactPlaceholder) {
		return content, nil
	}
	originals := map[string][]hiddenBlock{}
	if before != nil && !now.IsZero() {
		parsed := vault.ParseFile(relPath, "", before)
		for _, entry := range parsed.Entries {
			if entry.UntilDate == "" || !vault.EntryExpired(entry.UntilDate, now) {
				continue
			}
			hidden := vault.HideExpiredLine(entry.RawLine)
			block := hiddenBlock{raw: entry.RawLine, ending: parsed.Lines[entry.LineNumber-1].Ending}
			for number := entry.LineNumber + 1; number <= entry.LastLine(); number++ {
				block.details = append(block.details, parsed.Lines[number-1])
			}
			originals[hidden] = append(originals[hidden], block)
		}
	}
	var b strings.Builder
	b.Grow(len(content))
	for _, line := range vault.ParseFile(relPath, "", []byte(content)).Lines {
		text := line.Text
		if strings.Contains(text, vault.ExpiredFactPlaceholder) {
			queue := originals[text]
			if len(queue) == 0 {
				return "", memerr.New(memerr.InvalidArgument,
					"line %d of %q carries the expired-entry placeholder but matches no hidden expired entry of the file on disk; reread the file and keep placeholder lines unchanged",
					line.Number, relPath)
			}
			originals[text] = queue[1:]
			block := queue[0]
			b.WriteString(block.raw)
			if len(block.details) == 0 {
				b.WriteString(line.Ending)
				continue
			}
			// The projection gave the bullet the terminator of its whole
			// block; the bullet and all but the last detail get their own
			// on-disk terminators back.
			b.WriteString(block.ending)
			for i, detail := range block.details {
				b.WriteString(detail.Text)
				if i < len(block.details)-1 {
					b.WriteString(detail.Ending)
				}
			}
			b.WriteString(line.Ending)
			continue
		}
		b.WriteString(text)
		b.WriteString(line.Ending)
	}
	return b.String(), nil
}

// hiddenBlock is the on-disk text of one expired entry: its bullet with its
// terminator and the detail lines the read projection left out.
type hiddenBlock struct {
	raw     string
	ending  string
	details []vault.Line
}

// expiredRemovals lists the expired entries of before that are inside their
// hidden period and no longer appear in after. deleted marks a change that
// removes the whole file. The comparison is by raw line: a rewrite keeps an
// entry exactly when its original bullet still appears, so an entry that is
// rewritten in place counts as removed. Entries without `until`, entries that
// have not expired and entries past their hidden period are never reported.
func expiredRemovals(relPath string, before []byte, after string, deleted bool, now time.Time) []string {
	if before == nil {
		return nil
	}
	kept := map[string]bool{}
	if !deleted {
		for _, entry := range vault.ParseFile(relPath, "", []byte(after)).Entries {
			kept[entry.RawLine] = true
		}
	}
	var deferred []string
	for _, entry := range vault.ParseFile(relPath, "", before).Entries {
		if kept[entry.RawLine] || entry.UntilDate == "" {
			continue
		}
		if vault.EntryExpired(entry.UntilDate, now) && !vault.DeletionDue(entry.UntilDate, now) {
			deferred = append(deferred, relPath+": "+vault.DeletionBlockedReason(entry.UntilDate, now))
		}
	}
	return deferred
}
