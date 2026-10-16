package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSessionRoundTrip(t *testing.T) {
	cache := OpenCache(t.TempDir(), "sha256:repo-a")
	record := SessionRecord{Agent: "agent", SessionID: "sid", SyncSucceeded: true, RemoteState: "synced"}
	if err := cache.SaveSession(record); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	loaded, ok, err := cache.LoadSession("agent", "sid")
	if err != nil || !ok {
		t.Fatalf("LoadSession ok=%v err=%v", ok, err)
	}
	if loaded.RepoID != "sha256:repo-a" || loaded.Agent != "agent" || loaded.SessionID != "sid" {
		t.Fatalf("loaded record = %+v", loaded)
	}
	if !loaded.SyncSucceeded || loaded.Injected {
		t.Fatalf("flags not preserved: %+v", loaded)
	}
	if loaded.UpdatedAt == "" {
		t.Fatal("UpdatedAt was not recorded")
	}

	if _, ok, _ := cache.LoadSession("agent", "other"); ok {
		t.Fatal("a different sid must not resolve to the same record")
	}
}

func TestSessionKeyDistinguishesTuples(t *testing.T) {
	a := SessionKey("repo", "agent", "sid")
	b := SessionKey("repo", "agent", "si/d")
	c := SessionKey("repo", "agent", "sid ")
	if a == b || a == c || b == c {
		t.Fatalf("session keys collided: %q %q %q", a, b, c)
	}
}

func TestSaveSessionRequiresIdentity(t *testing.T) {
	cache := OpenCache(t.TempDir(), "sha256:repo")
	if err := cache.SaveSession(SessionRecord{Agent: "agent"}); err == nil {
		t.Fatal("a record without a session id must be rejected")
	}
}

func TestPruneSessions(t *testing.T) {
	stateDir := t.TempDir()
	cache := OpenCache(stateDir, "sha256:repo")
	sessions := filepath.Join(stateDir, "repos", "repo", "cache", "sessions")

	fresh := SessionRecord{Agent: "a", SessionID: "fresh", UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	old := SessionRecord{Agent: "a", SessionID: "old", UpdatedAt: time.Now().Add(-SessionStateTTL - time.Hour).UTC().Format(time.RFC3339)}
	for _, record := range []SessionRecord{fresh, old} {
		if err := cache.SaveSession(record); err != nil {
			t.Fatalf("SaveSession %s: %v", record.SessionID, err)
		}
	}

	if err := cache.PruneSessions(time.Now()); err != nil {
		t.Fatalf("PruneSessions: %v", err)
	}
	if _, ok, _ := cache.LoadSession("a", "fresh"); !ok {
		t.Fatal("a fresh record was pruned")
	}
	if _, ok, _ := cache.LoadSession("a", "old"); ok {
		t.Fatal("an expired record was kept")
	}
	if _, err := os.Stat(filepath.Join(sessions, SessionKey("sha256:repo", "a", "old")+".json")); !os.IsNotExist(err) {
		t.Fatalf("expired session file still exists: %v", err)
	}
}
