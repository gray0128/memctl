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
//     touched;
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
	for _, line := range parsed.Lines {
		entry, isEntry := byLine[line.Number]
		if isEntry && entry.UntilDate != "" && EntryExpired(entry.UntilDate, now) {
			b.WriteString(HideExpiredLine(line.Text))
			b.WriteString(line.Ending)
			expired++
			continue
		}
		b.WriteString(line.Text)
		b.WriteString(line.Ending)
	}
	return b.String(), expired
}
