package vault_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gray0128/memctl/internal/vault"
)

// T144 (docs/18 section 4.4.1 and 4.4.2): block entries, escaped metadata,
// content-stable entry ids, kind/status and the event guard.

func TestT144DetailLinesAttachIndentedBlock(t *testing.T) {
	content := "# Notes\n\n" +
		"- Fix the pool timeout [source: agent://d/a/s; added: 2026-10-01]\n" +
		"  1. Stop the worker\n" +
		"  - check pgbouncer stats\n" +
		"  ```bash\n" +
		"  psql -c 'show pools'\n" +
		"\n" +
		"  ```\n" +
		"\tcontinued with a tab\n" +
		"- Next entry\n" +
		"  child of next\n" +
		"\n" +
		"  not attached after a blank line\n" +
		"## Heading\n" +
		"- Last entry\n" +
		"  ## indented heading is not a detail\n"
	parsed := vault.ParseFile("notes/a.md", "rev", []byte(content))
	if parsed.Raw() != content {
		t.Fatal("ParseFile is not lossless")
	}
	if len(parsed.Entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(parsed.Entries))
	}
	first := parsed.Entries[0]
	want := []string{"  1. Stop the worker", "  - check pgbouncer stats", "  ```bash", "  psql -c 'show pools'", "", "  ```", "\tcontinued with a tab"}
	if strings.Join(first.DetailLines, "|") != strings.Join(want, "|") {
		t.Fatalf("first DetailLines = %q", first.DetailLines)
	}
	if first.LastLine() != first.LineNumber+len(want) {
		t.Fatalf("LastLine = %d", first.LastLine())
	}
	if got := parsed.Entries[1].DetailLines; len(got) != 1 || got[0] != "  child of next" {
		t.Fatalf("second DetailLines = %q", got)
	}
	if got := parsed.Entries[2].DetailLines; len(got) != 0 {
		t.Fatalf("an indented heading was attached: %q", got)
	}
	if parsed.Entries[2].Heading != "indented heading is not a detail" && parsed.Entries[2].Heading != "Heading" {
		t.Fatalf("heading = %q", parsed.Entries[2].Heading)
	}
}

func TestT144DetailLinesBoundsKeepEveryByte(t *testing.T) {
	var b strings.Builder
	b.WriteString("- Long recipe\n")
	for i := 0; i < vault.MaxDetailLines+5; i++ {
		b.WriteString("  step\n")
	}
	b.WriteString("- After\n")
	content := b.String()
	parsed := vault.ParseFile("notes/a.md", "", []byte(content))
	if parsed.Raw() != content {
		t.Fatal("bounded block lost bytes")
	}
	if got := len(parsed.Entries[0].DetailLines); got != vault.MaxDetailLines {
		t.Fatalf("DetailLines = %d, want %d", got, vault.MaxDetailLines)
	}
	if len(parsed.Entries) != 2 || parsed.Entries[1].FactText != "After" {
		t.Fatalf("entries = %+v", parsed.Entries)
	}

	big := "- Big\n  " + strings.Repeat("x", vault.MaxDetailBytes) + "\n  small\n"
	parsed = vault.ParseFile("notes/a.md", "", []byte(big))
	if len(parsed.Entries[0].DetailLines) != 0 || parsed.Raw() != big {
		t.Fatalf("oversized detail line was attached: %d", len(parsed.Entries[0].DetailLines))
	}
}

func TestT144DetailFenceIsAttachedWholeOrNotAtAll(t *testing.T) {
	var b strings.Builder
	b.WriteString("- Recipe\n  intro\n  ```\n")
	for i := 0; i < vault.MaxDetailLines; i++ {
		b.WriteString("  line\n")
	}
	b.WriteString("  ```\n- After\n")
	parsed := vault.ParseFile("notes/a.md", "", []byte(b.String()))
	if got := parsed.Entries[0].DetailLines; len(got) != 1 || got[0] != "  intro" {
		t.Fatalf("a fence over the bound must be rolled back, got %q", got)
	}
	// Deleting the block leaves the fence intact, so "- After" stays outside
	// any fence and is still an entry.
	content, ok := parsed.WithoutEntry(parsed.Entries[0])
	if !ok {
		t.Fatal("WithoutEntry failed")
	}
	reparsed := vault.ParseFile("notes/a.md", "", []byte(content))
	if len(reparsed.Entries) != 1 || reparsed.Entries[0].FactText != "After" {
		t.Fatalf("entries after block delete = %+v\n%s", reparsed.Entries, content)
	}

	// A fence whose body leaves the indentation is not attached either.
	mixed := "- Recipe\n  ```\nunindented\n  ```\n"
	parsed = vault.ParseFile("notes/a.md", "", []byte(mixed))
	if len(parsed.Entries[0].DetailLines) != 0 {
		t.Fatalf("mixed fence attached: %q", parsed.Entries[0].DetailLines)
	}
}

func TestT144BlockEditsRemoveWholeBlock(t *testing.T) {
	content := "# T\r\n\r\n- One\r\n  step a\r\n  step b\r\n- Two\r\n"
	parsed := vault.ParseFile("notes/a.md", "", []byte(content))
	got, ok := parsed.WithoutEntry(parsed.Entries[0])
	if !ok || got != "# T\r\n\r\n- Two\r\n" {
		t.Fatalf("WithoutEntry = %q", got)
	}
	if block := vault.EntryBlock("- New", parsed.Entries[0]); block != "- New\n  step a\n  step b" {
		t.Fatalf("EntryBlock = %q", block)
	}
	replaced, _ := parsed.WithReplacedLine(parsed.Entries[0].LineNumber, "- Uno")
	if replaced != "# T\r\n\r\n- Uno\r\n  step a\r\n  step b\r\n- Two\r\n" {
		t.Fatalf("WithReplacedLine = %q", replaced)
	}
}

func TestT144MetadataEscapes(t *testing.T) {
	cases := []struct {
		line, key, value string
	}{
		{`- run it [source: agent://d/a/s; cmd: a \; b; added: 2026-10-01]`, "cmd", "a ; b"},
		{`- run it [source: agent://d/a/s; cmd: "a; b"; added: 2026-10-01]`, "cmd", "a; b"},
		{`- run it [url: "https://x.test/?a=1;b=2"; added: 2026-10-01]`, "url", "https://x.test/?a=1;b=2"},
		{`- run it [note: say \"hi\"; added: 2026-10-01]`, "note", `say \"hi\"`},
	}
	for _, tc := range cases {
		parsed := vault.ParseFile("notes/a.md", "", []byte(tc.line+"\n"))
		if parsed.Raw() != tc.line+"\n" {
			t.Fatalf("not lossless: %q", tc.line)
		}
		entry := parsed.Entries[0]
		if entry.FactText != "run it" || entry.AddedDate != "2026-10-01" {
			t.Fatalf("%q: fact=%q added=%q", tc.line, entry.FactText, entry.AddedDate)
		}
		found := false
		for _, field := range entry.Metadata {
			if field.Key == tc.key {
				found = true
				if field.Value != tc.value {
					t.Fatalf("%q: %s = %q, want %q", tc.line, tc.key, field.Value, tc.value)
				}
				if !strings.Contains(tc.line, field.Raw) {
					t.Fatalf("Raw %q is not an original segment", field.Raw)
				}
			}
		}
		if !found {
			t.Fatalf("%q: key %s missing from %+v", tc.line, tc.key, entry.Metadata)
		}
	}
	// An unbalanced quote keeps the old split, so the group stays parseable.
	parsed := vault.ParseFile("notes/a.md", "", []byte(`- x [note: 5" screen; added: 2026-10-01]`+"\n"))
	if parsed.Entries[0].AddedDate != "2026-10-01" || parsed.Entries[0].FactText != "x" {
		t.Fatalf("unbalanced quote: %+v", parsed.Entries[0])
	}
	// SplitFactAndMetadata keeps every metadata byte for a fact rewrite.
	line := `- old [cmd: "a; b"; added: 2026-10-01]`
	if got, _ := vault.ReplaceEntryFact(line, "new"); got != `- new [cmd: "a; b"; added: 2026-10-01]` {
		t.Fatalf("ReplaceEntryFact = %q", got)
	}
}

func TestT144EntryIDStableAcrossOtherEdits(t *testing.T) {
	before := "# T\n\n- Alpha [added: 2026-10-01]\n- Beta [added: 2026-10-02]\n"
	after := "# T\n\nintro paragraph\n\n- New first [added: 2026-10-03]\n- Alpha [added: 2026-10-01]\n- Beta changed [added: 2026-10-02]\n"
	one := vault.ParseFile("notes/a.md", "sha256:one", []byte(before))
	two := vault.ParseFile("notes/a.md", "sha256:two", []byte(after))
	alphaBefore, alphaAfter := one.Entries[0], two.Entries[1]
	if alphaBefore.LineNumber == alphaAfter.LineNumber {
		t.Fatal("test setup: the line number should move")
	}
	if alphaBefore.ID != alphaAfter.ID {
		t.Fatalf("untouched entry changed its id: %s -> %s", alphaBefore.ID, alphaAfter.ID)
	}
	if one.Entries[1].ID == two.Entries[2].ID {
		t.Fatal("a rewritten fact kept its id")
	}

	dup := vault.ParseFile("notes/a.md", "", []byte("- Same [added: 2026-10-01]\n- Same  [added: 2026-10-01]\n- Same [added: 2026-10-02]\n"))
	ids := map[string]bool{}
	for _, entry := range dup.Entries {
		ids[entry.ID] = true
	}
	if len(ids) != 3 {
		t.Fatalf("duplicate entries share ids: %+v", dup.Entries)
	}
	if dup.Entries[1].ID != vault.EntryID("notes/a.md", "Same", "2026-10-01", 1) {
		t.Fatal("second occurrence id is not the documented hash")
	}
}

func TestT144KindAndStatus(t *testing.T) {
	content := "- A [added: 2026-10-01; kind: event; status: hypothesis]\n" +
		"- B [kind: Event; status: maybe]\n" +
		"- C [kind: procedural; status: verified; owner: me]\n"
	parsed := vault.ParseFile("notes/a.md", "", []byte(content))
	if parsed.Raw() != content {
		t.Fatal("not lossless")
	}
	if e := parsed.Entries[0]; !e.IsEvent() || e.Status != vault.StatusHypothesis {
		t.Fatalf("A = kind %q status %q", e.Kind, e.Status)
	}
	if e := parsed.Entries[1]; e.Kind != "" || e.Status != "" || len(e.Metadata) != 2 {
		t.Fatalf("unknown values must stay raw only: %+v", e)
	}
	if e := parsed.Entries[2]; e.Kind != vault.KindProcedural || e.Status != vault.StatusVerified || len(e.Metadata) != 3 {
		t.Fatalf("C = %+v", e)
	}
	if _, _, err := vault.CheckKindStatus("eventual", ""); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if _, _, err := vault.CheckKindStatus("", "maybe"); err == nil {
		t.Fatal("an unknown status was accepted")
	}
	if kind, status, err := vault.CheckKindStatus(" event ", "verified"); err != nil || kind != "event" || status != "verified" {
		t.Fatalf("CheckKindStatus = %q %q %v", kind, status, err)
	}
}

func TestT144EventRemovals(t *testing.T) {
	event := "- Released v1 [added: 2026-10-01; kind: event]\n"
	before := map[string][]byte{"notes/a.md": []byte("# A\n" + event + "- other\n")}

	if got := vault.EventRemovals(before, map[string][]byte{"notes/a.md": []byte("# A\n- other\n")}); len(got) != 1 || !strings.Contains(got[0], "notes/a.md: Released v1") {
		t.Fatalf("delete not reported: %q", got)
	}
	rewritten := map[string][]byte{"notes/a.md": []byte("# A\n- Released v2 [added: 2026-10-01; kind: event]\n")}
	if got := vault.EventRemovals(before, rewritten); len(got) != 1 {
		t.Fatalf("rewrite not reported: %q", got)
	}
	unmarked := map[string][]byte{"notes/a.md": []byte("# A\n- Released v1 [added: 2026-10-01]\n")}
	if got := vault.EventRemovals(before, unmarked); len(got) != 1 {
		t.Fatalf("dropping kind: event not reported: %q", got)
	}
	moved := map[string][]byte{
		"notes/a.md":       []byte("# A\n- other\n"),
		"notes/archive.md": []byte("# Archive\n- Released  v1 [source: x; added: 2026-10-01; kind: event]\n"),
	}
	if got := vault.EventRemovals(before, moved); len(got) != 0 {
		t.Fatalf("a move was reported: %q", got)
	}
	if got := vault.EventRemovals(before, map[string][]byte{}); len(got) != 1 {
		t.Fatalf("file delete not reported: %q", got)
	}
	// Two identical events need two survivors.
	twice := map[string][]byte{"notes/a.md": []byte(event + event)}
	if got := vault.EventRemovals(twice, map[string][]byte{"notes/a.md": []byte(event)}); len(got) != 1 {
		t.Fatalf("duplicate removal not counted: %q", got)
	}
}

func TestT144HideExpiredHidesDetailLines(t *testing.T) {
	content := "# T\n- Old recipe [until: 2026-01-01]\n  secret step\n  second step\n- Live\n  live step\n"
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	hidden, count := vault.HideExpiredEntries("notes/a.md", "", []byte(content), now)
	if count != 1 || strings.Contains(hidden, "secret step") || strings.Contains(hidden, "second step") {
		t.Fatalf("expired details leaked (%d):\n%s", count, hidden)
	}
	want := "# T\n- " + vault.ExpiredFactPlaceholder + " [until: 2026-01-01]\n- Live\n  live step\n"
	if hidden != want {
		t.Fatalf("projection = %q, want %q", hidden, want)
	}
	// A block that ends the file without a newline keeps that ending.
	tail := "- Old [until: 2026-01-01]\n  step"
	hidden, _ = vault.HideExpiredEntries("notes/a.md", "", []byte(tail), now)
	if hidden != "- "+vault.ExpiredFactPlaceholder+" [until: 2026-01-01]" {
		t.Fatalf("tail projection = %q", hidden)
	}
}
