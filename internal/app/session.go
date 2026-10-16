package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/assets"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/hookio"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/state"
	"github.com/gray0128/memctl/internal/syncer"
	"github.com/gray0128/memctl/internal/vault"
)

// Legacy Hook sync states used in the context header, per
// docs/02-domain-models-and-contracts.md section 2.2. They summarize the
// structured remote state for the session banner and never replace it.
const (
	SessionSyncOK     = "ok"
	SessionSyncLocal  = "local"
	SessionSyncCached = "cached"
	SessionSyncFailed = "failed"
	SessionSyncBusy   = "busy"
	SessionSyncNoRepo = "norepo"
)

// userInstructionsLimit bounds the optional user instruction file.
const userInstructionsLimit = 256 << 10

// SessionOptions controls one start/context/pull invocation.
type SessionOptions struct {
	Agent     string
	SessionID string
	// Once suppresses the context body when this session already injected it.
	Once bool
	// NoBody skips assembling the context body entirely. pull sets it because
	// its stdout must stay empty.
	NoBody bool
	// SkipSyncWhenSynced attempts the remote task only while the session has
	// not yet synchronized successfully. context and pull set it; start does
	// not.
	SkipSyncWhenSynced bool
	// Interactive allows Git and SSH native prompts. Hook callers pass false.
	Interactive bool
	// NonBlockingLock takes the repository lock without waiting so a busy
	// repository reports busy instead of blocking the host.
	NonBlockingLock bool
	// Budget bounds the remote task. Zero leaves the default budget to the
	// synchronization service.
	Budget time.Duration
}

// SessionOutcome is the assembled session context plus the facts the CLI needs
// to decide an exit code. Repository and sync failures are reported here, not
// as errors, so a hook can always finish.
type SessionOutcome struct {
	RepoPath    string
	RepoID      string
	Device      string
	SourceTag   string
	StableSID   bool
	SyncState   string
	RemoteState string
	ReasonCode  string
	Body        string
	Inject      bool
	// HookFailed reports a repository or sync failure that an automatic hook
	// must not surface as a non-zero exit code.
	HookFailed bool
}

// PrepareSession reads the session cache, runs the conditional remote task and
// assembles the injection body. It does not write stdout and does not mark the
// session injected; the caller does that after a successful write.
func (a *App) PrepareSession(ctx context.Context, opts SessionOptions) (SessionOutcome, error) {
	out := SessionOutcome{
		RepoPath: a.cfg.RepoPath,
		RepoID:   config.RepoID(a.cfg.RepoPath),
		Device:   hookio.ResolveDevice(os.Getenv),
	}
	out.StableSID = opts.SessionID != ""
	if out.StableSID {
		out.SourceTag = hookio.Tag(out.Device, opts.Agent, opts.SessionID)
	}

	cache := state.OpenCache(a.cfg.StateDir, out.RepoID)
	_ = cache.PruneSessions(time.Now())

	var record state.SessionRecord
	var haveRecord bool
	if out.StableSID {
		var err error
		record, haveRecord, err = cache.LoadSession(opts.Agent, opts.SessionID)
		if err != nil {
			return out, err
		}
	}

	if _, err := a.requireRepo(ctx); err != nil {
		out.SyncState = SessionSyncNoRepo
		out.HookFailed = true
	} else if opts.SkipSyncWhenSynced && haveRecord && record.SyncSucceeded {
		out.SyncState = SessionSyncCached
	} else {
		a.runSessionSync(ctx, opts, &out)
	}

	a.buildSessionBodyIfNeeded(ctx, opts, haveRecord, record, &out)

	// Only write the cache when the round changed something: a sync attempt or
	// the first successful injection. A deduplicated round stays read-only,
	// which keeps the hook path free of a per-invocation fsync.
	needsSave := out.StableSID &&
		(out.SyncState != SessionSyncCached || (out.Inject && !(haveRecord && record.Injected)))
	if needsSave {
		a.saveSessionRecord(cache, record, opts, out)
	}
	return out, nil
}

// buildSessionBodyIfNeeded skips assembling the body when --once is set and
// this session already injected it, so a deduplicated round does not read the
// entry file at all.
func (a *App) buildSessionBodyIfNeeded(ctx context.Context, opts SessionOptions, haveRecord bool, record state.SessionRecord, out *SessionOutcome) {
	out.Inject = true
	if opts.NoBody || (opts.Once && haveRecord && record.Injected) {
		out.Inject = false
		return
	}
	a.buildSessionBody(ctx, out)
}

// MarkSessionInjected records that the context body reached stdout. It is
// best-effort: the cache is rebuildable and must never block the host.
func (a *App) MarkSessionInjected(agent, sid string) {
	if sid == "" {
		return
	}
	cache := state.OpenCache(a.cfg.StateDir, config.RepoID(a.cfg.RepoPath))
	record, ok, err := cache.LoadSession(agent, sid)
	if err != nil {
		return
	}
	if !ok {
		record = state.SessionRecord{Agent: agent, SessionID: sid}
	} else if record.Injected {
		// The invocation-state flag is already set; do not rewrite the cache
		// on every non-once round.
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	record.Injected = true
	record.LastInjectedAt = now
	record.UpdatedAt = now
	_ = cache.SaveSession(record)
}

func (a *App) runSessionSync(ctx context.Context, opts SessionOptions, out *SessionOutcome) {
	if opts.Budget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Budget)
		defer cancel()
	}
	result, err := a.SyncWithOptions(ctx, SyncOptions{
		Interactive: opts.Interactive,
		Background:  opts.NonBlockingLock,
	})
	out.RemoteState = result.RemoteState

	switch {
	case err != nil:
		out.HookFailed = true
		if code, ok := memerr.As(err); ok {
			out.ReasonCode = string(code.Code)
			if code.Code == memerr.Busy {
				out.SyncState = SessionSyncBusy
				return
			}
		}
		out.SyncState = SessionSyncFailed
	case result.RemoteState == syncer.StateSynced:
		out.SyncState = SessionSyncOK
	case result.RemoteState == syncer.StateLocalOnly:
		out.SyncState = SessionSyncLocal
	default:
		out.SyncState = SessionSyncFailed
		out.HookFailed = true
	}
}

func (a *App) saveSessionRecord(cache *state.Cache, record state.SessionRecord, opts SessionOptions, out SessionOutcome) {
	now := time.Now().UTC().Format(time.RFC3339)
	record.Agent = opts.Agent
	record.SessionID = opts.SessionID
	if out.SyncState != SessionSyncCached {
		record.LastAttemptAt = now
		record.RemoteState = out.RemoteState
		record.ReasonCode = out.ReasonCode
		record.SyncSucceeded = !out.HookFailed
		if record.SyncSucceeded {
			record.LastSyncAt = now
		}
	}
	record.UpdatedAt = now
	_ = cache.SaveSession(record)
}

// buildSessionBody assembles the text delivered to the host: banner, source
// tag, embedded instructions, optional user instructions and the entry file.
func (a *App) buildSessionBody(ctx context.Context, out *SessionOutcome) {
	var b strings.Builder
	fmt.Fprintf(&b, "[agent-memory] repo=%s sync=%s device=%s\n", out.RepoPath, out.SyncState, out.Device)
	if out.StableSID {
		fmt.Fprintf(&b, "[agent-memory] source tag: %s\n", out.SourceTag)
	} else {
		b.WriteString("[agent-memory] source tag: unavailable (no stable session id; pass --sid or set AGENT_MEMORY_SID)\n")
	}
	if out.SyncState == SessionSyncFailed || out.SyncState == SessionSyncBusy {
		fmt.Fprintf(&b, "[agent-memory] sync detail: remote=%s reason=%s; local content stays available and the next hook retries\n",
			valueOrUnknown(out.RemoteState), valueOrUnknown(out.ReasonCode))
	}

	b.WriteString("----- INSTRUCTIONS -----\n")
	b.WriteString(strings.TrimRight(assets.Instructions, "\n"))
	b.WriteString("\n")

	if extra, ok := readUserInstructions(a.cfg.StateDir); ok {
		fmt.Fprintf(&b, "----- %s (user instructions) -----\n", filepath.Join(a.cfg.StateDir, "INSTRUCTIONS.md"))
		b.WriteString(strings.TrimRight(extra, "\n"))
		b.WriteString("\n")
	}

	if out.SyncState == SessionSyncNoRepo {
		fmt.Fprintf(&b, "[agent-memory] %s is not a memory repository; run `memctl init` first.\n", out.RepoPath)
		out.Body = b.String()
		return
	}

	file, err := a.ReadFile(ctx, vault.EntryPointName)
	if err != nil {
		if code, ok := memerr.As(err); ok && code.Code == memerr.Busy {
			// The lock is held by another memctl process. The host still gets
			// the most recent complete entry file; the banner already reports
			// sync=busy. A conflict or a pending transaction is not a lock
			// contention and keeps refusing the read.
			file, err = a.readEntryWithoutLock()
		}
	}
	if err != nil {
		if code, ok := memerr.As(err); ok && code.Code == memerr.LimitExceeded {
			fmt.Fprintf(&b, "[agent-memory] %s exceeds the %d byte injection limit and was not included in full. Use `memctl read %s` for a bounded read.\n",
				vault.EntryPointName, vault.MaxTextFileBytes, vault.EntryPointName)
		} else {
			fmt.Fprintf(&b, "[agent-memory] cannot read %s: %v\n", vault.EntryPointName, err)
		}
		out.Body = b.String()
		return
	}

	fmt.Fprintf(&b, "----- %s -----\n", filepath.Join(out.RepoPath, vault.EntryPointName))
	b.WriteString(strings.TrimRight(file.RawContent, "\n"))
	b.WriteString("\n")
	out.Body = b.String()
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// readEntryWithoutLock reads the context entry file for injection while the
// repository lock is held elsewhere. The hook path must keep the local content
// available in that case (docs/03-workflows-and-dataflows.md section 3), and
// the busy sync state in the banner marks the content as the last readable
// snapshot rather than a verified current revision.
func (a *App) readEntryWithoutLock() (vault.FileRead, error) {
	root, err := vault.OpenRoot(a.cfg.RepoPath)
	if err != nil {
		return vault.FileRead{}, err
	}
	defer root.Close()
	return root.ReadFile(vault.EntryPointName)
}

// readUserInstructions returns the optional $STATE/INSTRUCTIONS.md, bounded and
// without following a symbolic link. A missing or unreadable file is not an
// error: the embedded instructions still apply.
func readUserInstructions(stateDir string) (string, bool) {
	path := filepath.Join(stateDir, "INSTRUCTIONS.md")
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > userInstructionsLimit {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > userInstructionsLimit {
		return "", false
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", false
	}
	return string(data), true
}
