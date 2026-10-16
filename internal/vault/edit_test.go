package vault

import (
	"strings"
	"testing"
)

// TestRewriteLinksHandlesMultipleTargetsPerLine checks that precise byte-range
// rewriting stays correct when one line carries several links and a replacement
// changes the target length.
func TestRewriteLinksHandlesMultipleTargetsPerLine(t *testing.T) {
	content := "# Index\n\n- see [[beta]] and [[notes/beta]] plus `[[beta]]`\n- dead [[missing]]\n"
	parsed := ParseFile("MEMORY.md", "sha256:rev", []byte(content))
	index := NewLinkIndex([]string{"MEMORY.md", "beta.md", "notes/beta.md"})

	rewritten, changed := RewriteLinks(parsed, index, "beta.md", "archive/renamed.md")
	if !changed {
		t.Fatal("rewrite reported no change")
	}
	if !strings.Contains(rewritten, "- see [[archive/renamed]] and [[notes/beta]] plus `[[beta]]`") {
		t.Fatalf("rewritten line = %q", rewritten)
	}
	if !strings.Contains(rewritten, "[[missing]]") {
		t.Fatalf("dead link changed: %q", rewritten)
	}
	// An untouched file round-trips byte for byte.
	unchanged, changed := RewriteLinks(parsed, index, "nowhere.md", "x.md")
	if changed || unchanged != content {
		t.Fatalf("no-op rewrite changed the file: %q", unchanged)
	}
}

// TestRewriteLinksKeepsExtension checks the Markdown extension rule: an
// extensionless target stays extensionless, an explicit target keeps the path.
func TestRewriteLinksKeepsExtension(t *testing.T) {
	parsed := ParseFile("a.md", "rev", []byte("- [[b]]\n- [[b.md]]\n"))
	index := NewLinkIndex([]string{"a.md", "b.md"})

	rewritten, _ := RewriteLinks(parsed, index, "b.md", "deep/c.md")
	if !strings.Contains(rewritten, "- [[deep/c]]") || !strings.Contains(rewritten, "- [[deep/c.md]]") {
		t.Fatalf("rewritten content = %q", rewritten)
	}
}

// TestReplaceEntryFactKeepsMetadata checks a fact edit preserves every
// trailing metadata byte and order.
func TestReplaceEntryFactKeepsMetadata(t *testing.T) {
	raw := "- old fact [source: agent://dev/claude/s1; added: 2026-01-02]"
	got, ok := ReplaceEntryFact(raw, "new fact")
	if !ok {
		t.Fatal("bullet was not recognized")
	}
	if got != "- new fact [source: agent://dev/claude/s1; added: 2026-01-02]" {
		t.Fatalf("replaced line = %q", got)
	}
	withoutMeta, ok := ReplaceEntryFact("- plain", "changed")
	if !ok || withoutMeta != "- changed" {
		t.Fatalf("metadata-free replacement = %q", withoutMeta)
	}
	if _, ok := ReplaceEntryFact("# heading", "x"); ok {
		t.Fatal("a heading must not be treated as a bullet")
	}
}

// TestParsedFileLineEdits checks the line-level helpers used by the entry
// routes.
func TestParsedFileLineEdits(t *testing.T) {
	content := "# Title\n\n- one\n- two\n"
	parsed := ParseFile("a.md", "rev", []byte(content))

	removed, ok := parsed.WithoutLine(4)
	if !ok || removed != "# Title\n\n- one\n" {
		t.Fatalf("WithoutLine = %q ok=%v", removed, ok)
	}
	replaced, ok := parsed.WithReplacedLine(3, "- one changed")
	if !ok || replaced != "# Title\n\n- one changed\n- two\n" {
		t.Fatalf("WithReplacedLine = %q ok=%v", replaced, ok)
	}
	appended, line := parsed.WithAppendedLine("- three")
	if line != 5 || appended != content+"- three\n" {
		t.Fatalf("WithAppendedLine = %q line=%d", appended, line)
	}
	// A file without a final newline gains one before the appended line.
	noTrailing := ParseFile("b.md", "rev", []byte("- one"))
	appended, line = noTrailing.WithAppendedLine("- two")
	if line != 2 || appended != "- one\n- two\n" {
		t.Fatalf("append to an unterminated file = %q line=%d", appended, line)
	}
}
