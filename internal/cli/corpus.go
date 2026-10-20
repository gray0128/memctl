package cli

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/doctor"
	"github.com/gray0128/memctl/internal/dream"
	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/hookio"
)

// This file wires the local session-corpus collector into the Hook commands of
// docs/12-session-corpus-dreaming.md section 2.2. Collection is best effort:
// nothing here may change a Hook's exit code, and every failure is silent by
// design.

// collectCorpus distills the current session log into the local digest cache.
// It runs on `sync` and `end`, with the transcript path coming from the hook
// payload's `transcript_path` or the explicit `--transcript` flag.
func collectCorpus(cfg config.Config, agent, sid, transcript, cwd string) {
	if !corpus.Enabled(os.Getenv) || transcript == "" {
		return
	}
	path := corpus.ResolveTranscript(transcript, cwd)
	if path == "" {
		return
	}
	if dir, err := os.Getwd(); err == nil && cwd == "" {
		cwd = dir
	}
	corpus.SetSuspiciousMatcher(func(text string) bool {
		return len(doctor.SuspiciousPhrases(text)) > 0
	})
	cache := corpus.OpenCache(cfg.StateDir, config.RepoID(cfg.RepoPath))
	// The TTL cleanup runs on every collection so an abandoned cache cannot
	// grow without bound. A consumed digest is kept while its proposal record
	// still exists; the proposal store is the authority for that.
	cache.Prune(time.Now(), corpusProposalExists(cfg))
	writer := corpus.Writer{Cache: cache}
	digest, ok := writer.Collect(corpus.RealTimeRequest{
		Agent:          agent,
		SID:            sid,
		TranscriptPath: path,
		Cwd:            cwd,
		Device:         hookio.ResolveDevice(os.Getenv),
	})
	if !ok || digest.WroteMemoryInSess {
		return
	}
	// The session's own commits tell the current agent to weigh this digest
	// lower: a session that already recorded memories rarely has more to
	// extract. The check is read-only and bounded, and a failure leaves the
	// flag false rather than claiming the session wrote nothing.
	if sessionWroteMemory(cfg, digest.SessionID) {
		digest.WroteMemoryInSess = true
		_ = cache.Save(digest)
	}
}

// sessionWroteMemory reports whether a commit reachable from HEAD carries a
// `Session: <sid>` trailer for this session. It reads a bounded page of the
// log and never writes the repository.
func sessionWroteMemory(cfg config.Config, sid string) bool {
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

// writeSessionScanLimit bounds how far back the trailer scan reads.
const writeSessionScanLimit = 50

// collectCorpusFromHook is the Hook form: the transcript path comes from the
// payload unless --transcript supplied one. The agent and session id use the
// documented resolution order so a payload-only invocation still records the
// right source tag.
func collectCorpusFromHook(cfg config.Config, agentFlag, sidFlag, explicit string, payload hookio.Input) {
	collectCorpusFromPayload(cfg,
		hookio.ResolveAgent(agentFlag, os.Getenv),
		hookio.ResolveSID(sidFlag, os.Getenv, payload),
		explicit, payload)
}

// collectCorpusFromPayload resolves the transcript path and starts one
// bounded collection.
func collectCorpusFromPayload(cfg config.Config, agent, sid, explicit string, payload hookio.Input) {
	transcript := explicit
	if transcript == "" {
		transcript = payload.TranscriptPath
	}
	cwd := ""
	if dir, err := os.Getwd(); err == nil {
		cwd = dir
	}
	collectCorpus(cfg, agent, sid, transcript, cwd)
}

// corpusProposalExists reports whether a proposal record still exists, so a
// consumed digest survives the TTL exactly as long as its proposal does.
func corpusProposalExists(cfg config.Config) func(string) bool {
	return func(id string) bool {
		store := dream.OpenStore(cfg.StateDir, config.RepoID(cfg.RepoPath))
		_, err := store.Load(id)
		return err == nil
	}
}
