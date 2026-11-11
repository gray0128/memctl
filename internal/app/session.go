package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gray0128/memctl/internal/assets"
	"github.com/gray0128/memctl/internal/config"
	"github.com/gray0128/memctl/internal/corpus"
	"github.com/gray0128/memctl/internal/git"
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

// Documented session injection budget and folding thresholds from
// docs/18-agent-memory-review-and-optimization.md section 4.2.3.
const (
	MaxInjectedLines       = 200
	MaxInjectedBytes       = 25 * 1024
	MaxUnrelatedIndexLines = 20
	MemoryTruncatedNotice  = "[agent-memory] MEMORY.md truncated at 200 lines / 25 KiB; run 'memctl dream --prepare' to move details into notes/."
)

// userInstructionsLimit bounds the optional user instruction file.
const userInstructionsLimit = 256 << 10

// SessionOptions controls one start/context/pull invocation.
type SessionOptions struct {
	Agent     string
	SessionID string
	// Cwd is the working directory from the Hook payload; when empty,
	// PrepareSession falls back to os.Getwd() for current-project memory lookup.
	Cwd string
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
	a.buildSessionBody(ctx, opts, out)
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
// tag, embedded instructions, optional user instructions, the budgeted entry
// file and any preloaded current-project memory dashboard.
func (a *App) buildSessionBody(ctx context.Context, opts SessionOptions, out *SessionOutcome) {
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

	forceTruncated := false
	file, err := a.ReadFile(ctx, vault.EntryPointName)
	if err != nil {
		if code, ok := memerr.As(err); ok && code.Code == memerr.Busy {
			file, err = a.readEntryWithoutLock()
		}
	}
	if err != nil {
		if code, ok := memerr.As(err); ok && code.Code == memerr.LimitExceeded {
			prefix, prefixErr := a.readOversizedEntryPrefix()
			if prefixErr == nil {
				file = prefix
				err = nil
				forceTruncated = true
			}
		}
	}
	if err != nil {
		fmt.Fprintf(&b, "[agent-memory] cannot read %s: %v\n", vault.EntryPointName, err)
		out.Body = b.String()
		return
	}

	cwdBase := a.resolveSessionCwdBase(ctx, opts.Cwd)
	managedPaths, _ := a.listManagedPaths(ctx)
	linkIdx := vault.NewLinkIndex(managedPaths)

	renderedMemory, usedBytes, linkedProjectPaths := renderInjectedMemory(file.RawContent, cwdBase, linkIdx, forceTruncated)

	fmt.Fprintf(&b, "----- %s -----\n", filepath.Join(out.RepoPath, vault.EntryPointName))
	if renderedMemory != "" {
		b.WriteString(strings.TrimRight(renderedMemory, "\n"))
		b.WriteString("\n")
	}

	if cwdBase != "" && usedBytes < MaxInjectedBytes {
		projectSection := a.buildProjectMemorySection(ctx, cwdBase, managedPaths, linkedProjectPaths, MaxInjectedBytes-usedBytes)
		if projectSection != "" {
			b.WriteString(projectSection)
		}
	}

	out.Body = b.String()
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// resolveSessionCwdBase resolves the current project's normalized root basename
// from opts.Cwd or os.Getwd(), returning an empty string when the working
// directory is a host configuration directory, the memory repository itself,
// the user's home directory, or cannot be determined.
func (a *App) resolveSessionCwdBase(ctx context.Context, rawCwd string) string {
	cwd := strings.TrimSpace(rawCwd)
	if cwd != "" {
		if corpus.IsHostConfigDir(cwd) {
			return ""
		}
	} else {
		dir, err := os.Getwd()
		if err != nil || corpus.IsHostConfigDir(dir) {
			return ""
		}
		cwd = dir
	}
	projectRoot := corpus.ResolveProjectRoot(ctx, git.Run, cwd)
	if projectRoot == "" || corpus.IsHostConfigDir(projectRoot) {
		return ""
	}
	if config.Canonical(projectRoot) == config.Canonical(a.cfg.RepoPath) {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if config.Canonical(projectRoot) == config.Canonical(home) {
			return ""
		}
	}
	base := corpus.CwdBase(projectRoot)
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return base
}

type memoryLine struct {
	text string
	// tier 1: top title, `## Working preferences`, `## Index` heading & current-project index lines
	// tier 2: other non-index sections
	// tier 3: unrelated index lines & omitted-index summary line
	tier int
}

// renderInjectedMemory folds unrelated `## Index` lines beyond
// MaxUnrelatedIndexLines and truncates the result at MaxInjectedLines /
// MaxInjectedBytes while prioritizing `## Working preferences` and
// current-project index lines.
func renderInjectedMemory(rawContent, cwdBase string, linkIdx *vault.LinkIndex, forceTruncated bool) (string, int, []string) {
	trimmed := strings.TrimRight(rawContent, "\n")
	if trimmed == "" {
		if forceTruncated {
			return MemoryTruncatedNotice, len(MemoryTruncatedNotice), nil
		}
		return "", 0, nil
	}
	rawLines := strings.Split(trimmed, "\n")

	var folded []memoryLine
	var linkedProjectPaths []string
	currentSection := ""
	unrelatedIndexCount := 0
	omittedUnrelatedCount := 0

	flushOmittedIndex := func() {
		if omittedUnrelatedCount <= 0 {
			return
		}
		summary := fmt.Sprintf("[+%d other project topics omitted; query with 'memctl entries --q <keyword>']", omittedUnrelatedCount)
		omittedUnrelatedCount = 0
		if len(folded) > 0 && strings.TrimSpace(folded[len(folded)-1].text) == "" {
			last := folded[len(folded)-1]
			folded[len(folded)-1] = memoryLine{text: summary, tier: 3}
			folded = append(folded, last)
			return
		}
		folded = append(folded, memoryLine{text: summary, tier: 3})
	}

	for _, line := range rawLines {
		cleanLine := strings.TrimSpace(line)
		if strings.HasPrefix(cleanLine, "# ") {
			if currentSection == "## index" {
				flushOmittedIndex()
			}
			currentSection = strings.ToLower(cleanLine)
			folded = append(folded, memoryLine{text: line, tier: 1})
			continue
		}
		if strings.HasPrefix(cleanLine, "## ") {
			if currentSection == "## index" {
				flushOmittedIndex()
			}
			currentSection = strings.ToLower(cleanLine)
			tier := 2
			if currentSection == "## working preferences" || currentSection == "## index" {
				tier = 1
			}
			folded = append(folded, memoryLine{text: line, tier: tier})
			continue
		}

		switch currentSection {
		case "## working preferences":
			folded = append(folded, memoryLine{text: line, tier: 1})
		case "## index":
			if strings.HasPrefix(cleanLine, "- ") || strings.HasPrefix(cleanLine, "* ") {
				related, targets := matchIndexLine(line, cwdBase, linkIdx)
				if related {
					linkedProjectPaths = append(linkedProjectPaths, targets...)
					folded = append(folded, memoryLine{text: line, tier: 1})
				} else {
					unrelatedIndexCount++
					if unrelatedIndexCount <= MaxUnrelatedIndexLines {
						folded = append(folded, memoryLine{text: line, tier: 3})
					} else {
						omittedUnrelatedCount++
					}
				}
			} else {
				folded = append(folded, memoryLine{text: line, tier: 2})
			}
		default:
			folded = append(folded, memoryLine{text: line, tier: 2})
		}
	}
	if currentSection == "## index" {
		flushOmittedIndex()
	}

	totalLines := len(folded)
	totalBytes := 0
	for i, item := range folded {
		if i > 0 {
			totalBytes++
		}
		totalBytes += len(item.text)
	}

	if !forceTruncated && totalLines <= MaxInjectedLines && totalBytes <= MaxInjectedBytes {
		out := make([]string, len(folded))
		for i, item := range folded {
			out[i] = item.text
		}
		rendered := strings.Join(out, "\n")
		return rendered, len(rendered), linkedProjectPaths
	}

	byteBudget := MaxInjectedBytes - len(MemoryTruncatedNotice) - 1
	if byteBudget < 0 {
		byteBudget = 0
	}
	lineBudget := MaxInjectedLines

	keep := make([]bool, len(folded))
	keptLines := 0
	keptBytes := 0

	for tier := 1; tier <= 3; tier++ {
		for i, item := range folded {
			if item.tier != tier {
				continue
			}
			cost := len(item.text) + 1
			if keptLines+1 <= lineBudget && keptBytes+cost <= byteBudget {
				keep[i] = true
				keptLines++
				keptBytes += cost
			}
		}
	}

	var outLines []string
	for i, item := range folded {
		if keep[i] {
			outLines = append(outLines, item.text)
		}
	}
	outLines = append(outLines, MemoryTruncatedNotice)
	rendered := strings.Join(outLines, "\n")
	return rendered, len(rendered), linkedProjectPaths
}

// matchIndexLine reports whether one bullet line under `## Index` belongs to
// cwdBase (via `[project: <cwdBase>]` metadata or a `[[...]]` link target
// matching `notes/<cwdBase>.md` / `notes/<cwdBase>-*.md`) and returns any
// resolved target paths referenced by the line.
func matchIndexLine(line, cwdBase string, linkIdx *vault.LinkIndex) (bool, []string) {
	if cwdBase == "" {
		return false, nil
	}
	hasProjectTag := lineHasProjectMeta(line, cwdBase)
	rawTargets := extractWikiLinkTargets(line)
	var resolvedTargets []string
	matchedByLink := false

	for _, rawTarget := range rawTargets {
		resolved := resolveNoteTarget(rawTarget, linkIdx)
		if resolved != "" {
			resolvedTargets = append(resolvedTargets, resolved)
		}
		if isProjectNotePath(resolved, cwdBase) || isProjectNotePath(rawTarget, cwdBase) {
			matchedByLink = true
		}
	}
	if hasProjectTag || matchedByLink {
		return true, resolvedTargets
	}
	return false, nil
}

func lineHasProjectMeta(line, cwdBase string) bool {
	if cwdBase == "" {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != '[' {
			continue
		}
		if i+1 < len(line) && line[i+1] == '[' {
			if end := strings.Index(line[i+2:], "]]"); end >= 0 {
				i += 2 + end + 1
				continue
			}
		}
		end := strings.IndexByte(line[i+1:], ']')
		if end < 0 {
			break
		}
		block := line[i+1 : i+1+end]
		for _, field := range strings.Split(block, ";") {
			key, val, ok := strings.Cut(field, ":")
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(key), "project") && strings.EqualFold(strings.TrimSpace(val), cwdBase) {
				return true
			}
		}
		i += end + 1
	}
	return false
}

func extractWikiLinkTargets(line string) []string {
	var targets []string
	rest := line
	for {
		start := strings.Index(rest, "[[")
		if start < 0 {
			break
		}
		rest = rest[start+2:]
		end := strings.Index(rest, "]]")
		if end < 0 {
			break
		}
		if target := strings.TrimSpace(rest[:end]); target != "" {
			targets = append(targets, target)
		}
		rest = rest[end+2:]
	}
	return targets
}

func resolveNoteTarget(rawTarget string, linkIdx *vault.LinkIndex) string {
	target := strings.TrimSpace(rawTarget)
	if target == "" {
		return ""
	}
	if linkIdx != nil {
		if res := linkIdx.Resolve(target); res.ResolvedPath != "" {
			return res.ResolvedPath
		}
	}
	clean := strings.TrimPrefix(filepath.ToSlash(target), "./")
	if !strings.Contains(clean, "/") {
		clean = "notes/" + clean
	}
	lower := strings.ToLower(clean)
	if !strings.HasSuffix(lower, ".md") && !strings.HasSuffix(lower, ".markdown") {
		clean += ".md"
	}
	if validated, err := vault.ValidateRelPath(clean); err == nil {
		return validated
	}
	return ""
}

func isProjectNotePath(relPath, cwdBase string) bool {
	if cwdBase == "" || relPath == "" {
		return false
	}
	clean := strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(relPath)), "./")
	lowerPath := strings.ToLower(clean)
	lowerBase := strings.ToLower(strings.TrimSpace(cwdBase))
	if lowerBase == "" || !strings.HasPrefix(lowerPath, "notes/") {
		return false
	}
	var stem string
	switch {
	case strings.HasSuffix(lowerPath, ".md"):
		stem = strings.TrimSuffix(strings.TrimPrefix(lowerPath, "notes/"), ".md")
	case strings.HasSuffix(lowerPath, ".markdown"):
		stem = strings.TrimSuffix(strings.TrimPrefix(lowerPath, "notes/"), ".markdown")
	default:
		return false
	}
	return stem == lowerBase || strings.HasPrefix(stem, lowerBase+"-")
}

type projectMemoryItem struct {
	relPath    string
	lineNumber int
	addedDate  string
	rawLine    string
}

func (a *App) buildProjectMemorySection(ctx context.Context, cwdBase string, managedPaths, linkedProjectPaths []string, remainingBytes int) string {
	if cwdBase == "" || remainingBytes <= 0 {
		return ""
	}
	seen := map[string]bool{}
	var targetFiles []string
	for _, rel := range managedPaths {
		if isProjectNotePath(rel, cwdBase) && !seen[rel] {
			seen[rel] = true
			targetFiles = append(targetFiles, rel)
		}
	}
	for _, rel := range linkedProjectPaths {
		clean, err := vault.ValidateRelPath(rel)
		if err == nil && clean != vault.EntryPointName && !seen[clean] {
			seen[clean] = true
			targetFiles = append(targetFiles, clean)
		}
	}
	sort.Strings(targetFiles)
	if len(targetFiles) == 0 {
		return ""
	}

	now := a.clock()
	var items []projectMemoryItem
	for _, relPath := range targetFiles {
		file, err := a.ReadFile(ctx, relPath)
		if err != nil {
			continue
		}
		parsed := vault.ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent))
		for _, entry := range parsed.Entries {
			if vault.EntryExpired(entry.UntilDate, now) || entry.FactText == vault.ExpiredFactPlaceholder {
				continue
			}
			items = append(items, projectMemoryItem{
				relPath:    relPath,
				lineNumber: entry.LineNumber,
				addedDate:  entry.AddedDate,
				rawLine:    entry.RawLine,
			})
		}
	}
	if len(items) == 0 {
		return ""
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].addedDate != items[j].addedDate {
			return items[i].addedDate > items[j].addedDate
		}
		if items[i].relPath != items[j].relPath {
			return items[i].relPath < items[j].relPath
		}
		return items[i].lineNumber < items[j].lineNumber
	})

	header := fmt.Sprintf("----- CURRENT PROJECT MEMORY (%s) -----\n", cwdBase)
	if len(header) >= remainingBytes {
		return ""
	}

	var b strings.Builder
	b.WriteString(header)
	count := 0
	for _, item := range items {
		body := strings.TrimPrefix(strings.TrimSpace(item.rawLine), "- ")
		line := fmt.Sprintf("- [%s] %s\n", item.relPath, body)
		if b.Len()+len(line) > remainingBytes {
			break
		}
		b.WriteString(line)
		count++
	}
	if count == 0 {
		return ""
	}
	return b.String()
}

// readEntryWithoutLock reads the context entry file for injection when an
// unborn repository has no commits yet while the lock is held.
func (a *App) readEntryWithoutLock() (vault.FileRead, error) {
	root, err := vault.OpenRoot(a.cfg.RepoPath)
	if err != nil {
		return vault.FileRead{}, err
	}
	defer root.Close()
	file, err := root.ReadFile(vault.EntryPointName)
	return a.projectRead(vault.EntryPointName, file, err)
}

// readOversizedEntryPrefix reads a bounded prefix of MEMORY.md when the on-disk
// file exceeds MaxTextFileBytes, so `start` and `context` can still inject the
// valid top lines and append the documented truncation notice instead of
// failing the hook.
func (a *App) readOversizedEntryPrefix() (vault.FileRead, error) {
	path := filepath.Join(a.cfg.RepoPath, vault.EntryPointName)
	f, err := os.Open(path)
	if err != nil {
		return vault.FileRead{}, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, int64(MaxInjectedBytes*2)))
	if err != nil {
		return vault.FileRead{}, err
	}
	for len(buf) > 0 && !utf8.Valid(buf) {
		buf = buf[:len(buf)-1]
	}
	raw := vault.FileRead{
		RelPath:      vault.EntryPointName,
		FileRevision: vault.FileRevision("100644", buf),
		Kind:         vault.KindEntryPoint,
		SizeBytes:    int64(len(buf)),
		Editable:     true,
		RawContent:   string(buf),
	}
	return a.projectRead(vault.EntryPointName, raw, nil)
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
