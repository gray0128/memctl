package vault

import (
	"strings"
	"testing"
	"time"
)

func testClock(t *testing.T) time.Time {
	t.Helper()
	// A fixed local noon keeps the test independent of the machine clock.
	return time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
}

// TestEntryExpiredCoversUntilBoundary covers the documented expiry boundary:
// the whole `until` day is still visible and hiding starts the next day. A
// missing or invalid value never expires.
func TestEntryExpiredCoversUntilBoundary(t *testing.T) {
	now := testClock(t)
	cases := []struct {
		until string
		want  bool
	}{
		{"", false},
		{"not-a-date", false},
		{"2026-10-20", false},
		{"2026-10-19", false},
		{"2026-10-18", true},
	}
	for _, tc := range cases {
		if got := EntryExpired(tc.until, now); got != tc.want {
			t.Errorf("EntryExpired(%q) = %v, want %v", tc.until, got, tc.want)
		}
	}
	// The boundary is the start of the next local day.
	late := time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local)
	if !EntryExpired("2026-10-19", late) {
		t.Error("an entry should expire at the start of the day after until")
	}
	justBefore := time.Date(2026, 10, 19, 23, 59, 59, 0, time.Local)
	if EntryExpired("2026-10-19", justBefore) {
		t.Error("an entry must stay visible through the whole until day")
	}
}

// TestParseFileReadsUntilMetadata covers the parser side: a legal `until`
// becomes UntilDate, an illegal one is ignored, and the metadata bytes are kept.
func TestParseFileReadsUntilMetadata(t *testing.T) {
	content := "# Memory\n\n" +
		"- active fact [until: 2026-12-31; source: agent://dev/claude/s1; added: 2026-10-01]\n" +
		"- expired fact [until: 2026-10-01]\n" +
		"- broken until [until: tomorrow]\n" +
		"- no until [source: agent://dev/claude/s1]\n"
	parsed := ParseFile("MEMORY.md", "sha256:rev", []byte(content))
	if parsed.Raw() != content {
		t.Fatal("parse is not lossless")
	}
	want := []string{"2026-12-31", "2026-10-01", "", ""}
	if len(parsed.Entries) != len(want) {
		t.Fatalf("parsed %d entries, want %d", len(parsed.Entries), len(want))
	}
	for i, entry := range parsed.Entries {
		if entry.UntilDate != want[i] {
			t.Errorf("entry %d UntilDate = %q, want %q", i, entry.UntilDate, want[i])
		}
	}
	if len(parsed.Entries[2].Metadata) != 1 || parsed.Entries[2].Metadata[0].Key != UntilKey {
		t.Errorf("an invalid until must stay in metadata untouched: %+v", parsed.Entries[2].Metadata)
	}
}

// TestHideExpiredEntriesCoversReadProjection covers the hide projection: only
// expired bullets change, the metadata region and every other byte survive, and
// a file without expired entries round-trips.
func TestHideExpiredEntriesCoversReadProjection(t *testing.T) {
	content := "# Memory\n\n" +
		"- active fact [until: 2026-12-31; source: agent://dev/claude/s1]\n" +
		"- expired fact [until: 2026-10-01; source: agent://dev/claude/s1; added: 2026-09-01]\n" +
		"- plain fact\n" +
		"Some prose [not: metadata]\n"
	hidden, count := HideExpiredEntries("MEMORY.md", "sha256:rev", []byte(content), testClock(t))
	if count != 1 {
		t.Fatalf("hidden %d entries, want 1", count)
	}
	if strings.Contains(hidden, "expired fact") {
		t.Fatal("the expired fact must not appear in the hidden content")
	}
	if !strings.Contains(hidden, "- active fact [until: 2026-12-31; source: agent://dev/claude/s1]") {
		t.Fatalf("active entry changed:\n%s", hidden)
	}
	if !strings.Contains(hidden, ExpiredFactPlaceholder+" [until: 2026-10-01; source: agent://dev/claude/s1; added: 2026-09-01]") {
		t.Fatalf("metadata of the hidden entry was not preserved:\n%s", hidden)
	}
	if !strings.Contains(hidden, "- plain fact") || !strings.Contains(hidden, "Some prose [not: metadata]") {
		t.Fatalf("other lines changed:\n%s", hidden)
	}

	unchanged, count := HideExpiredEntries("MEMORY.md", "sha256:rev", []byte("# Memory\n\n- plain\n"), testClock(t))
	if count != 0 || unchanged != "# Memory\n\n- plain\n" {
		t.Fatalf("a file without expired entries must round-trip: count=%d content=%q", count, unchanged)
	}

	if _, count := HideExpiredEntries("MEMORY.md", "sha256:rev", []byte(content), time.Time{}); count != 0 {
		t.Fatal("a zero clock must not hide anything")
	}
}

// TestDeletionWindowCoversHiddenPeriod covers the 30-day hidden period: hiding
// starts the day after until, and deletion stays blocked for 30 more days.
func TestDeletionWindowCoversHiddenPeriod(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	if DeletionDue("2026-10-01", now) {
		t.Fatal("an entry must stay in its hidden period for 30 days")
	}
	if DeletionDue("2999-01-01", now) {
		t.Fatal("an entry that has not expired cannot be deleted")
	}
	if DeletionDue("", now) {
		t.Fatal("an entry without until never becomes deletable by expiry")
	}
	if got := DeletionBlockedReason("2026-10-01", now); got != "the entry is in its hidden period until 2026-11-01" {
		t.Fatalf("blocked reason = %q", got)
	}
	if DeletionBlockedReason("2026-09-01", now) != "" {
		t.Fatal("an entry past its hidden period should be deletable")
	}
	// The period starts the day after until: until 2026-09-19 expires on the
	// 20th and becomes deletable on 2026-10-20.
	deletable, ok := DeletableAt("2026-09-19", now)
	if !ok || deletable.Format(DateLayout) != "2026-10-20" {
		t.Fatalf("DeletableAt = %v, %v", deletable, ok)
	}
	if DeletionDue("2026-09-19", time.Date(2026, 10, 20, 0, 0, 0, 0, time.Local)) != true {
		t.Fatal("the entry should become deletable exactly on the boundary date")
	}
}
