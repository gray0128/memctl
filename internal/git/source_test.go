package git

import (
	"strings"
	"testing"
)

// TestAppendTrailers covers the one trailer layout every commit-producing path
// uses: the audit view reads Agent, Session and Device from the body of
// `git log`, so the lines must be the last paragraph and must be omitted when
// the value is unknown.
func TestAppendTrailers(t *testing.T) {
	message := "memctl write\n\nMemctl-Transaction: tx-1"
	got := AppendTrailers(message, Source{Agent: "dsh", SessionID: "sid-1", Device: "laptop"}.Trailers()...)
	want := "memctl write\n\nMemctl-Transaction: tx-1\n\nAgent: dsh\nSession: sid-1\nDevice: laptop\n"
	if got != want {
		t.Fatalf("AppendTrailers = %q, want %q", got, want)
	}
	if got = AppendTrailers("Initialize memory repository", Source{}.Trailers()...); got != "Initialize memory repository" {
		t.Fatalf("an empty source changed the message: %q", got)
	}
	if got = AppendTrailers("x\n", ""); got != "x\n" {
		t.Fatalf("a blank trailer changed the message: %q", got)
	}
}

// TestParseLogReadsAppendedTrailers closes the loop between the writer and the
// reader: the audit view resolves the source from the Body parseLog returns.
func TestParseLogReadsAppendedTrailers(t *testing.T) {
	message := AppendTrailers("memctl write\n\nMemctl-Transaction: tx-1", Source{Agent: "dsh", Device: "laptop"}.Trailers()...)
	subject, body, _ := strings.Cut(message, "\n\n")
	record := "\x1e" + strings.Join([]string{"abc123", "", "2026-10-19T17:26:14+08:00", subject, body, "MEMORY.md"}, "\x1f")
	commits := parseLog(record)
	if len(commits) != 1 {
		t.Fatalf("parseLog returned %d commits", len(commits))
	}
	for _, want := range []string{"Agent: dsh", "Device: laptop"} {
		if !strings.Contains(commits[0].Body, want) {
			t.Fatalf("parsed body %q is missing %q", commits[0].Body, want)
		}
	}
	if commits[0].Body != strings.TrimRight(body, "\r\n") {
		t.Fatalf("parsed body = %q, want %q", commits[0].Body, body)
	}
}
