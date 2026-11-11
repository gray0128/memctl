package vault

import (
	"strings"
	"time"
)

// HideExpiredEntries renders the content of one Markdown file with every
// expired time-limited entry replaced by ExpiredFactPlaceholder. It is a pure
// read-time projection:
//
//   - only lines parsed as bullet entries with a valid, past `until` are
//     touched, and the DetailLines of such an entry are left out of the
//     projection so no expired detail leaks;
//   - the trailing metadata region and every other byte stay unchanged;
//   - a file with no expired entries round-trips byte for byte.
//
// The caller keeps the on-disk file as the write baseline, so a hidden entry is
// still present when the file is next written. See
// docs/10-memory-lifecycle.md section 3.3.
func HideExpiredEntries(relPath, fileRevision string, content []byte, now time.Time) (string, int) {
	if now.IsZero() {
		// A zero clock must not hide anything: hiding requires a definite
		// date, and showing a possibly-current fact is safer than dropping it.
		return string(content), 0
	}
	parsed := ParseFile(relPath, fileRevision, content)
	var b strings.Builder
	b.Grow(len(content))
	expired := 0
	byLine := make(map[int]MemoryEntry, len(parsed.Entries))
	for _, entry := range parsed.Entries {
		byLine[entry.LineNumber] = entry
	}
	skipUntil := 0
	for _, line := range parsed.Lines {
		if line.Number <= skipUntil {
			continue
		}
		entry, isEntry := byLine[line.Number]
		if isEntry && entry.UntilDate != "" && EntryExpired(entry.UntilDate, now) {
			b.WriteString(HideExpiredLine(line.Text))
			ending := line.Ending
			if last := entry.LastLine(); last > line.Number {
				// The bullet keeps the terminator of its block, so a block
				// that ends the file without a newline still does.
				ending = parsed.Lines[last-1].Ending
				skipUntil = last
			}
			b.WriteString(ending)
			expired++
			continue
		}
		b.WriteString(line.Text)
		b.WriteString(line.Ending)
	}
	return b.String(), expired
}
