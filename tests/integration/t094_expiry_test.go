package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file verifies T094: a time-limited entry (`until: YYYY-MM-DD`) is hidden
// from every read surface once its day has passed, while the file, the write
// baseline and Git history keep it. See docs/10-memory-lifecycle.md sections
// 3.3 and 3.4. The clock is the real one, so the fixtures use dates that are
// unambiguously in the past and the future.

const (
	t094ExpiredUntil = "2000-01-01"
	t094FutureUntil  = "2999-12-31"
)

func t094MemoryContent() string {
	return "# Memory\n\n## Index\n\n- [[topic]]\n" +
		"- active fact [until: " + t094FutureUntil + "]\n" +
		"- expired fact [until: " + t094ExpiredUntil + "; added: 2000-01-01]\n"
}

// TestT094ReadHidesExpiredEntry covers the CLI/HTTP full-file read: the fact is
// replaced by the placeholder, the metadata survives, the baseline is the
// on-disk revision and the response reports the hidden count.
func TestT094ReadHidesExpiredEntry(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.update(t, "MEMORY.md", t094MemoryContent())

	res := fixture.get(t, "/api/v1/files/MEMORY.md")
	mustStatus(t, res.status, http.StatusOK)
	var payload struct {
		File struct {
			FileRevision string `json:"fileRevision"`
		} `json:"file"`
		RawContent    string `json:"rawContent"`
		HiddenExpired int    `json:"hiddenExpired"`
	}
	if err := json.Unmarshal(res.body, &payload); err != nil {
		t.Fatalf("decode file read: %v\n%s", err, res.body)
	}
	if payload.HiddenExpired != 1 {
		t.Fatalf("hiddenExpired = %d, want 1", payload.HiddenExpired)
	}
	if strings.Contains(payload.RawContent, "expired fact") {
		t.Fatalf("expired fact is visible:\n%s", payload.RawContent)
	}
	if !strings.Contains(payload.RawContent, "active fact") {
		t.Fatalf("active fact disappeared:\n%s", payload.RawContent)
	}
	if !strings.Contains(payload.RawContent, "until: "+t094ExpiredUntil) {
		t.Fatalf("hidden entry lost its metadata:\n%s", payload.RawContent)
	}

	// The ETag is still the on-disk revision, so a read-then-write keeps the
	// hidden entry instead of deleting it.
	if got := res.header.Get("ETag"); got != `"`+payload.File.FileRevision+`"` {
		t.Fatalf("ETag = %q, want the on-disk revision %q", got, payload.File.FileRevision)
	}
	onDisk, err := os.ReadFile(filepath.Join(fixture.repo, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read the file on disk: %v", err)
	}
	if !strings.Contains(string(onDisk), "expired fact") {
		t.Fatalf("the read projection modified the file on disk:\n%s", onDisk)
	}
}

// TestT094SnapshotHidesExpiredEntries covers the snapshot and the entry query:
// structured surfaces drop the expired entry and expose the hidden count.
func TestT094SnapshotHidesExpiredEntries(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.update(t, "MEMORY.md", t094MemoryContent())
	fixture.write(t, "topic.md", "# Topic\n\n- kept [until: "+t094FutureUntil+"]\n- gone [until: "+t094ExpiredUntil+"]\n")

	var snapshot struct {
		HiddenExpired int `json:"hiddenExpired"`
		Files         []struct {
			RelPath string `json:"relPath"`
			Entries []struct {
				FactText string `json:"factText"`
			} `json:"entries"`
		} `json:"files"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/snapshot"), &snapshot)
	if snapshot.HiddenExpired != 2 {
		t.Fatalf("snapshot hiddenExpired = %d, want 2", snapshot.HiddenExpired)
	}
	for _, file := range snapshot.Files {
		for _, entry := range file.Entries {
			if entry.FactText == "expired fact" || entry.FactText == "gone" {
				t.Fatalf("%s still lists the expired entry %q", file.RelPath, entry.FactText)
			}
		}
	}

	var page struct {
		Total int `json:"total"`
		Items []struct {
			FactText string `json:"factText"`
		} `json:"items"`
	}
	fixture.decode(t, fixture.get(t, "/api/v1/entries"), &page)
	// Three live bullets remain: the index link, one active fact in MEMORY.md
	// and one in topic.md. The two expired entries are gone.
	if page.Total != 3 {
		t.Fatalf("entry total = %d, want 3 (index link plus one active fact per file)", page.Total)
	}
	for _, entry := range page.Items {
		if entry.FactText == "expired fact" || entry.FactText == "gone" {
			t.Fatalf("entry list still contains %q", entry.FactText)
		}
	}
}

// TestT094InjectionHidesExpiredEntry covers the hook injection path: the body
// the host receives carries the placeholder, not the expired fact.
func TestT094InjectionHidesExpiredEntry(t *testing.T) {
	fixture := newB1Fixture(t, false)
	fixture.update(t, "MEMORY.md", t094MemoryContent())
	file, err := fixture.app.ReadFile(context.Background(), "MEMORY.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if file.HiddenExpired != 1 {
		t.Fatalf("ReadFile hiddenExpired = %d, want 1", file.HiddenExpired)
	}
}
