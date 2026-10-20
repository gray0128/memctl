package app

import (
	"context"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/git"
)

// This file provides the local Dreaming hooks the corpus path needs from the
// application service: an already-mined session is flagged on the bundle, so the
// current agent can weigh a digest whose session already recorded memories
// lower. The scan is read-only and bounded.

// writeSessionScanLimit bounds how far back the trailer scan reads.
const writeSessionScanLimit = 50

// sessionWroteMemory reports whether a commit reachable from HEAD carries a
// `Session: <sid>` trailer for this session. A failure reports false rather than
// claiming the session wrote nothing.
func (a *App) sessionWroteMemory(sid string) bool {
	return SessionWroteMemoryFor(a.cfg, sid)
}

// SessionWroteMemoryFor is the config-bound form of the trailer scan, used by
// the Hook collection path that does not build an App.
func SessionWroteMemoryFor(cfg config.Config, sid string) bool {
	if sid == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	commits, err := git.Log(ctx, cfg.RepoPath, writeSessionScanLimit, "")
	if err != nil {
		return false
	}
	for _, commit := range commits {
		if strings.Contains(commit.Body, "Session: "+sid) {
			return true
		}
	}
	return false
}
