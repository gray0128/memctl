package vault_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

const f01Content = `# Memory: John

- John leads the product team [source: https://example.com/sessions/100]

## Index
- [[team_structure]]
- [[projects/payments]]

## Notes
- Fact without metadata
- Two sources [source: agent://laptop/claude/sess-1] [source: https://example.com/s/2]
- Unknown key [custom: value; added: 2026-09-03; source: https://example.com/s/3]
- Repeated source key [source: a; source: b]
- Invalid date [added: 2026-13-01]

` + "```text\n" + `- code bullet [[not/a/link]] [source: nope]
` + "```\n" + `- after fence [[projects/payments]]
- inline ` + "`[[not-a-link]]`" + ` and real [[team_structure]]
`

func entryOnLine(t *testing.T, parsed vault.ParsedFile, line int) vault.MemoryEntry {
	t.Helper()
	for _, entry := range parsed.Entries {
		if entry.LineNumber == line {
			return entry
		}
	}
	t.Fatalf("no entry parsed on line %d", line)
	return vault.MemoryEntry{}
}

// TestParseFileCoversF01 checks the upstream sample, an entry without
// metadata, repeated source keys, unknown keys and code fences, and that an
// untouched file round-trips byte for byte.
func TestParseFileCoversF01(t *testing.T) {
	parsed := vault.ParseFile("MEMORY.md", "sha256:base", []byte(f01Content))

	if got := parsed.Raw(); got != f01Content {
		t.Fatalf("Raw() is not byte-for-byte lossless:\n got %q\nwant %q", got, f01Content)
	}

	wantEntryLines := []int{3, 6, 7, 10, 11, 12, 13, 14, 19, 20}
	if len(parsed.Entries) != len(wantEntryLines) {
		t.Fatalf("parsed %d entries, want %d", len(parsed.Entries), len(wantEntryLines))
	}
	for i, line := range wantEntryLines {
		if parsed.Entries[i].LineNumber != line {
			t.Fatalf("entry %d is on line %d, want %d", i, parsed.Entries[i].LineNumber, line)
		}
	}

	// The raw link list excludes the code fence and the inline code span.
	wantLinks := [][2]any{
		{6, "team_structure"},
		{7, "projects/payments"},
		{19, "projects/payments"},
		{20, "team_structure"},
	}
	if len(parsed.Links) != len(wantLinks) {
		t.Fatalf("parsed %d raw links, want %d: %+v", len(parsed.Links), len(wantLinks), parsed.Links)
	}
	for i, want := range wantLinks {
		if parsed.Links[i].LineNumber != want[0] || parsed.Links[i].RawTarget != want[1] {
			t.Fatalf("link %d = line %d %q, want line %v %q",
				i, parsed.Links[i].LineNumber, parsed.Links[i].RawTarget, want[0], want[1])
		}
	}

	first := entryOnLine(t, parsed, 3)
	if first.FactText != "John leads the product team" {
		t.Fatalf("fact text = %q", first.FactText)
	}
	if first.Heading != "Memory: John" {
		t.Fatalf("heading = %q", first.Heading)
	}
	if len(first.Sources) != 1 || first.Sources[0].Scheme != "https" || first.Sources[0].Raw != "https://example.com/sessions/100" {
		t.Fatalf("sources = %+v", first.Sources)
	}
	if first.ID != vault.EntryID("MEMORY.md", "sha256:base", 3) {
		t.Fatalf("entry ID = %q", first.ID)
	}

	plain := entryOnLine(t, parsed, 10)
	if plain.FactText != "Fact without metadata" || len(plain.Sources) != 0 || len(plain.Metadata) != 0 {
		t.Fatalf("metadata-free entry = %+v", plain)
	}
	if plain.Heading != "Notes" {
		t.Fatalf("heading = %q", plain.Heading)
	}

	twoSources := entryOnLine(t, parsed, 11)
	if len(twoSources.Sources) != 2 {
		t.Fatalf("repeated source fields = %+v", twoSources.Sources)
	}
	if twoSources.Sources[0].Scheme != "agent" || twoSources.Sources[0].Device != "laptop" ||
		twoSources.Sources[0].Agent != "claude" || twoSources.Sources[0].SessionID != "sess-1" {
		t.Fatalf("agent source = %+v", twoSources.Sources[0])
	}
	if twoSources.Sources[1].Scheme != "https" {
		t.Fatalf("second source = %+v", twoSources.Sources[1])
	}

	unknown := entryOnLine(t, parsed, 12)
	if got := metadataKeys(unknown.Metadata); strings.Join(got, ",") != "custom,added,source" {
		t.Fatalf("unknown key order = %v", got)
	}
	if unknown.AddedDate != "2026-09-03" {
		t.Fatalf("added date = %q", unknown.AddedDate)
	}
	if len(unknown.Sources) != 1 {
		t.Fatalf("unknown-key sources = %+v", unknown.Sources)
	}

	repeated := entryOnLine(t, parsed, 13)
	if len(repeated.Sources) != 2 || repeated.Sources[0].Raw != "a" || repeated.Sources[1].Raw != "b" {
		t.Fatalf("duplicate source key = %+v", repeated.Sources)
	}

	invalidDate := entryOnLine(t, parsed, 14)
	if invalidDate.AddedDate != "" {
		t.Fatalf("invalid date parsed as %q", invalidDate.AddedDate)
	}
	if len(invalidDate.Metadata) != 1 || invalidDate.Metadata[0].Key != "added" {
		t.Fatalf("invalid-date metadata = %+v", invalidDate.Metadata)
	}

	webLink := entryOnLine(t, parsed, 20)
	if webLink.FactText != "inline `[[not-a-link]]` and real [[team_structure]]" {
		t.Fatalf("inline-code fact text = %q", webLink.FactText)
	}
}

func metadataKeys(fields []vault.MetadataField) []string {
	keys := make([]string, 0, len(fields))
	for _, field := range fields {
		keys = append(keys, field.Key)
	}
	return keys
}

// TestParseFileLosslessLineEndings covers empty, unterminated and CRLF input.
func TestParseFileLosslessLineEndings(t *testing.T) {
	cases := []string{
		"",
		"\n",
		"# Memory",
		"# Memory\n",
		"a\r\nb\r\n",
		"- fact\r\n- other",
		"no trailing newline",
	}
	for _, content := range cases {
		parsed := vault.ParseFile("MEMORY.md", "sha256:base", []byte(content))
		if got := parsed.Raw(); got != content {
			t.Fatalf("Raw() for %q = %q", content, got)
		}
	}
}

// TestParseFileAndResolveLinksCoversF02 checks root-relative link resolution,
// same-name ambiguity, dead and invalid links, and file kinds.
func TestParseFileAndResolveLinksCoversF02(t *testing.T) {
	root, dir := openTestRoot(t)

	files := map[string]string{
		"MEMORY.md": "# Memory\n\n## Index\n" +
			"- [[notes]]\n" +
			"- [[metrics/x.sql]]\n" +
			"- [[metrics/x]]\n" +
			"- [[project/payments]]\n" +
			"- [[missing.md]]\n" +
			"- [[../escape]]\n" +
			"- [[.git/config]]\n",
		"notes.md":            "# Notes\n",
		"notes.markdown":      "# Notes twice\n",
		"metrics/x.sql":       "SELECT 1;\n",
		"project/payments.md": "# Payments\n",
		"script.sh":           "#!/bin/sh\n",
		"binary.bin":          string([]byte{0x89, 'P', 'N', 'G', 0x00, 0x01}),
		"project/website.md":  "# Website\n",
	}
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	before, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	parsed, err := root.Parse("MEMORY.md")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("parsing modified the repository file")
	}

	index := vault.NewLinkIndex([]string{
		"MEMORY.md", "notes.md", "notes.markdown", "metrics/x.sql",
		"project/payments.md", "project/website.md", "script.sh", "binary.bin",
	})
	links, diagnostics := parsed.ResolveLinks(index)
	if len(links) != 7 {
		t.Fatalf("resolved %d links, want 7", len(links))
	}

	byTarget := map[string]vault.WikiLink{}
	for _, link := range links {
		byTarget[link.RawTarget] = link
	}

	if link := byTarget["notes"]; !link.Exists || link.ResolvedPath != "" {
		t.Fatalf("ambiguous link = %+v", link)
	}
	if link := byTarget["metrics/x.sql"]; !link.Exists || link.ResolvedPath != "metrics/x.sql" {
		t.Fatalf("exact link = %+v", link)
	}
	if link := byTarget["metrics/x"]; link.Exists || link.ResolvedPath != "" {
		t.Fatalf("extensionless SQL link must stay missing: %+v", link)
	}
	if link := byTarget["project/payments"]; !link.Exists || link.ResolvedPath != "project/payments.md" {
		t.Fatalf("markdown link = %+v", link)
	}
	if link := byTarget["missing.md"]; link.Exists || link.ResolvedPath != "" {
		t.Fatalf("missing link = %+v", link)
	}

	wantCodes := map[string]vault.LinkIssueCode{
		"notes":       vault.LinkIssueAmbiguous,
		"metrics/x":   vault.LinkIssueMissing,
		"missing.md":  vault.LinkIssueMissing,
		"../escape":   vault.LinkIssueInvalid,
		".git/config": vault.LinkIssueInvalid,
	}
	gotCodes := map[string]vault.LinkIssueCode{}
	for _, diagnostic := range diagnostics {
		gotCodes[diagnostic.RawTarget] = diagnostic.Code
	}
	for target, code := range wantCodes {
		if gotCodes[target] != code {
			t.Fatalf("diagnostic for %q = %q, want %q (all: %+v)", target, gotCodes[target], code, diagnostics)
		}
	}
	if len(diagnostics) != len(wantCodes) {
		t.Fatalf("diagnostic count = %d, want %d: %+v", len(diagnostics), len(wantCodes), diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == vault.LinkIssueAmbiguous {
			if strings.Join(diagnostic.Candidates, ",") != "notes.markdown,notes.md" {
				t.Fatalf("ambiguous candidates = %v", diagnostic.Candidates)
			}
		}
	}

	// Links on an entry line are attached to that entry after resolution.
	var indexed vault.MemoryEntry
	for _, entry := range parsed.Entries {
		if entry.FactText == "[[notes]]" {
			indexed = entry
		}
	}
	if len(indexed.InlineLinks) != 1 || indexed.InlineLinks[0].RawTarget != "notes" {
		t.Fatalf("entry inline links = %+v", indexed.InlineLinks)
	}

	kinds := []struct {
		path string
		kind vault.Kind
	}{
		{"MEMORY.md", vault.KindEntryPoint},
		{"project/payments.md", vault.KindMarkdown},
		{"metrics/x.sql", vault.KindSQL},
		{"script.sh", vault.KindScript},
		{"binary.bin", vault.KindOther},
	}
	for _, tc := range kinds {
		if got := vault.ClassifyKind(tc.path); got != tc.kind {
			t.Fatalf("ClassifyKind(%q) = %q, want %q", tc.path, got, tc.kind)
		}
	}

	// Binary assets stay out of the text parser.
	if _, err := root.ReadFile("binary.bin"); err == nil {
		t.Fatal("binary asset was read as text")
	} else if contractErr, ok := memerr.As(err); !ok || contractErr.Code != memerr.Unsupported {
		t.Fatalf("binary read error = %v, want UNSUPPORTED", err)
	}
}

// TestResolveLinksDistinguishesEntryIDs checks that the identity is bound to
// the file revision and line.
func TestResolveLinksDistinguishesEntryIDs(t *testing.T) {
	first := vault.EntryID("MEMORY.md", "sha256:one", 1)
	if first == vault.EntryID("MEMORY.md", "sha256:one", 2) {
		t.Fatal("entry ID does not depend on the line")
	}
	if first == vault.EntryID("MEMORY.md", "sha256:two", 1) {
		t.Fatal("entry ID does not depend on the file revision")
	}
	if first != vault.EntryID("MEMORY.md", "sha256:one", 1) {
		t.Fatal("entry ID is not deterministic")
	}
}
