package corpus

import (
	"sort"
	"testing"
	"time"
)

// TestEveryParsedHostIsLocatedAndSwept keeps the three per-host tables of this
// package in step: every host with a parser must also be found by
// LocateSession, which `dream --prepare` uses to refresh the calling session,
// and by the backfill sweep. A host added to parsers without a store lookup
// fails here instead of silently losing its current session, which is how OMP
// drifted. Delta is the only exception: it has no log file and is collected
// from `delta thread watch --json` through CollectDelta.
func TestEveryParsedHostIsLocatedAndSwept(t *testing.T) {
	writers := map[string]func(t *testing.T, home, cwd, sid, user string) string{
		"claude-code": writeClaudeSession,
		"codex":       writeCodexSession,
		"pi":          writePiSession,
		"omp":         writeOmpSession,
		"dsh":         writeDSHSession,
		"antigravity": writeAntigravitySession,
		"codebuddy":   writeCodeBuddySession,
		"workbuddy":   writeWorkBuddySession,
	}
	agents := make([]string, 0, len(parsers))
	for agent := range parsers {
		agents = append(agents, agent)
	}
	sort.Strings(agents)

	cwd := "/work/memctl"
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, agent := range agents {
		if agent == "delta" {
			continue
		}
		t.Run(agent, func(t *testing.T) {
			write, ok := writers[agent]
			if !ok {
				t.Fatalf("host %q has a parser but no store fixture; add its LocateSession and backfill lookup, then a fixture here", agent)
			}
			home := t.TempDir()
			sid := agent + "-session"
			path := write(t, home, cwd, sid, "记住：发布只推标签")
			if got := LocateSession(agent, sid, cwd, home); got != path {
				t.Fatalf("LocateSession(%q) = %q, want %q", agent, got, path)
			}
			cache := OpenCache(t.TempDir(), "sha256:repo")
			result := Backfill(cache, BackfillRequest{Cwd: cwd, Device: "testhost", Home: home, Now: func() time.Time { return now }})
			if result.Collected != 1 {
				t.Fatalf("backfill collected %d sessions, want 1 (%+v)", result.Collected, result)
			}
			if _, ok := cache.Load(agent, sid); !ok {
				t.Fatalf("backfill did not collect the %s session under its own agent name", agent)
			}
		})
	}
}
