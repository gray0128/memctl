package dream

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gray0128/memctl/internal/git"
	"github.com/gray0128/memctl/internal/memerr"
	"github.com/gray0128/memctl/internal/vault"
)

// This file implements the code-controlled, bounded multi-step worker of
// docs/11-github-actions-dreaming.md section 2.3. It runs inside the GitHub
// Actions virtual machine as `memctl dream --worker`:
//
//   - stage A reads the baseline commit, runs the static diagnosis and splits
//     the managed files into bounded batches (no model call);
//   - stage B sends each batch through one structured-output call with a
//     code-level Step Guard on every response (at most MaxStepRetries+1 tries);
//   - stage C aligns MEMORY.md with the resulting file set when batches changed
//     the index, and writes the result commit.
//
// The worker never decides to push a branch on its own: the caller owns the
// transport, so the same worker is exercised by tests through a plain Git
// repository and in Actions through the checkout it was started in.

// Worker limits, mirroring docs/11-github-actions-dreaming.md section 2.3.
const (
	MaxBatches          = 5
	MaxBatchInputBytes  = 64 << 10
	MaxStepRetries      = 1
	MaxTotalCalls       = 12
	SmallBatchFileCount = 5
	SmallBatchBytes     = 32 << 10
	WorkerBudget        = 8 * time.Minute
)

// Step output actions.
const (
	StepCreate = "create"
	StepUpdate = "update"
	StepDelete = "delete"
)

// DreamStepOutput is the structured shape every model response must satisfy.
// It is also the JSON Schema the providers receive, so the schema and the
// decoder cannot drift apart.
type DreamStepOutput struct {
	Operations []StepOperation `json:"operations"`
	Warnings   []string        `json:"warnings"`
}

// StepOperation is one file operation proposed by the model.
type StepOperation struct {
	Action    string `json:"action"`
	Path      string `json:"path"`
	Content   string `json:"content,omitempty"`
	Rationale string `json:"rationale"`
}

// stepSchemaJSON is the JSON Schema of DreamStepOutput. It is embedded in the
// prompt and, for schema-enforcing providers, sent as the enforced shape.
const stepSchemaJSON = `{
  "type": "object",
  "required": ["operations", "warnings"],
  "additionalProperties": false,
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["action", "path", "rationale"],
        "additionalProperties": false,
        "properties": {
          "action": {"type": "string", "enum": ["create", "update", "delete"]},
          "path": {"type": "string"},
          "content": {"type": "string"},
          "rationale": {"type": "string"}
        }
      }
    },
    "warnings": {"type": "array", "items": {"type": "string"}}
  }
}`

// StepSchema returns the schema document sent to providers.
func StepSchema() []byte { return []byte(stepSchemaJSON) }

// staticFinding is one deterministic candidate computed from the baseline. It
// is what makes the run "static rules identify candidates first, the model
// organizes second" rather than an unbounded agent loop.
type staticFinding struct {
	Code     string   `json:"code"`
	Path     string   `json:"path,omitempty"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
	Subjects []string `json:"subjects,omitempty"`
}

// batch is one unit of model work: its target files plus the static findings
// that concern them.
type batch struct {
	Paths    []string        `json:"paths"`
	Files    []batchFile     `json:"files"`
	Findings []staticFinding `json:"staticFindings"`
}

// batchFile is one file payload of a batch.
type batchFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// workerRequest is one user-message payload.
type workerRequest struct {
	Today string `json:"today"`
	Phase string `json:"phase"`
	// EntryFile is the current MEMORY.md content.
	EntryFile string `json:"entryFile"`
	// BatchFiles are the files of this batch.
	BatchFiles []batchFile `json:"batchFiles,omitempty"`
	// StaticFindings are the deterministic candidates that concern this batch.
	StaticFindings []staticFinding `json:"staticFindings"`
	// Correction carries the validation error of the previous attempt.
	Correction string `json:"correction,omitempty"`
	// Index summarizes the file set for stage C.
	Index []indexEntry `json:"index,omitempty"`
}

// indexEntry is the compact per-file summary used by stage C.
type indexEntry struct {
	Path      string   `json:"path"`
	Title     string   `json:"title"`
	Headings  []string `json:"headings"`
	LineCount int      `json:"lineCount"`
}

// worker runs the three stages against one checked-out baseline.
type worker struct {
	Provider Provider
	Now      func() time.Time
	// MaxBytes bounds one file write; it mirrors the managed text limit.
	MaxBytes int
	// Calls counts the model calls of this run.
	Calls int
	// introduced holds the link problems a previous step introduced; the guard
	// rejects the next step until it clears them.
	introduced map[string]bool
	// known holds the link problems the baseline already had. They are reported
	// as diagnosis candidates and never fail a step.
	known map[string]bool
}

// workerResult is the outcome of a worker run. Tree is the complete managed
// file set after the steps, so the caller can materialize it exactly and cannot
// mistake "unchanged" for "deleted".
type workerResult struct {
	Tree     map[string][]byte
	Warnings []string
	Steps    int
	Model    string
}

// run executes the three stages. Every failure is returned as an error; the
// caller converts it into a failed result commit so the local side learns the
// reason instead of waiting for a timeout.
func (w *worker) run(ctx context.Context, repoPath, baseHead string) (workerResult, error) {
	baselineTree, err := git.SourceFiles(ctx, repoPath, baseHead)
	if err != nil {
		return workerResult{}, memerr.Wrap(memerr.GitFailed, err, "cannot read the baseline tree")
	}
	// Protected paths (`.git`, `.github`, host configuration) are not memory
	// content. They are left exactly as the checkout has them and never become
	// part of a worker result.
	files := make(map[string][]byte, len(baselineTree))
	for path, content := range baselineTree {
		if protectedPath(path) {
			continue
		}
		files[path] = content
	}
	managed := &fileSet{files: files}
	w.known = baselineProblems(managed)
	w.introduced = map[string]bool{}

	findings := staticFindings(managed, w.timeNow())
	batches := planBatches(managed)
	result := workerResult{Warnings: []string{}}

	// Stage B: one structured call per batch with the Step Guard.
	for index, item := range batches {
		if index >= MaxBatches {
			break
		}
		output, err := w.runBatch(ctx, managed, item, findings)
		if err != nil {
			return workerResult{}, err
		}
		for _, warning := range output.Warnings {
			result.Warnings = append(result.Warnings, warning)
		}
		result.Steps++
	}

	// Stage C: align MEMORY.md when the batches changed the reachable set.
	if len(batches) > 1 {
		if err := w.alignIndex(ctx, managed, findings); err != nil {
			return workerResult{}, err
		}
		result.Steps++
	}

	result.Tree = managed.files
	result.Model = w.Provider.Model()
	return result, nil
}

// runBatch sends one batch and validates the response. A guard failure is
// retried once with the validation error as a correction message.
func (w *worker) runBatch(ctx context.Context, files *fileSet, item batch, findings []staticFinding) (DreamStepOutput, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxStepRetries; attempt++ {
		if w.Calls >= MaxTotalCalls {
			return DreamStepOutput{}, memerr.New(memerr.LimitExceeded,
				"the Dreaming run reached the %d model call limit", MaxTotalCalls)
		}
		payload := workerRequest{
			Today:          w.timeNow().UTC().Format("2006-01-02"),
			Phase:          "batch",
			EntryFile:      string(files.files[vault.EntryPointName]),
			BatchFiles:     batchFiles(files, item.Paths),
			StaticFindings: findings,
		}
		if lastErr != nil {
			payload.Correction = lastErr.Error()
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return DreamStepOutput{}, memerr.Wrap(memerr.InvalidArgument, err, "cannot encode the batch payload")
		}
		w.Calls++
		response, err := w.Provider.Complete(ctx, CompletionRequest{
			System: stageBSystemPrompt,
			User:   string(encoded),
			Schema: StepSchema(),
		})
		if err != nil {
			return DreamStepOutput{}, err
		}
		output, err := decodeStepOutput(response)
		if err != nil {
			lastErr = err
			continue
		}
		// Apply to a copy so a rejected step never partially mutates the tree.
		candidate := files.clone()
		if err := w.applyStep(candidate, output, item.Paths); err != nil {
			lastErr = err
			continue
		}
		files.replaceWith(candidate)
		return output, nil
	}
	return DreamStepOutput{}, memerr.Wrap(memerr.Unsupported, lastErr,
		"the model response did not satisfy the step contract after %d attempt(s)", MaxStepRetries+1)
}

// alignIndex asks for one final MEMORY.md update so every topic file stays
// reachable after the batches changed the file set.
func (w *worker) alignIndex(ctx context.Context, files *fileSet, findings []staticFinding) error {
	if w.Calls >= MaxTotalCalls {
		return memerr.New(memerr.LimitExceeded,
			"the Dreaming run reached the %d model call limit", MaxTotalCalls)
	}
	paths := files.sortedPaths()
	payload := workerRequest{
		Today:          w.timeNow().UTC().Format("2006-01-02"),
		Phase:          "index",
		EntryFile:      string(files.files[vault.EntryPointName]),
		StaticFindings: findings,
		Index:          summarizeIndex(files, paths),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return memerr.Wrap(memerr.InvalidArgument, err, "cannot encode the index payload")
	}
	w.Calls++
	response, err := w.Provider.Complete(ctx, CompletionRequest{
		System: stageCSystemPrompt,
		User:   string(encoded),
		Schema: StepSchema(),
	})
	if err != nil {
		return err
	}
	output, err := decodeStepOutput(response)
	if err != nil {
		return err
	}
	candidate := files.clone()
	// Stage C may only update the entry file; a topic-file operation here would
	// duplicate stage B with a smaller input.
	for _, operation := range output.Operations {
		if operation.Action != StepUpdate || operation.Path != vault.EntryPointName {
			return memerr.New(memerr.Unsupported,
				"the index step may only update %s, got %s %q", vault.EntryPointName, operation.Action, operation.Path)
		}
	}
	if err := w.applyStep(candidate, output, []string{vault.EntryPointName}); err != nil {
		return err
	}
	files.replaceWith(candidate)
	return nil
}

// decodeStepOutput parses one model response, tolerating a Markdown code fence
// because two of the four providers cannot enforce JSON natively.
func decodeStepOutput(response string) (DreamStepOutput, error) {
	text := stripCodeFence(response)
	var output DreamStepOutput
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return DreamStepOutput{}, memerr.Wrap(memerr.Unsupported, err,
			"the model response is not the required JSON object")
	}
	if output.Warnings == nil {
		output.Warnings = []string{}
	}
	return output, nil
}

// fencePattern matches one leading and trailing Markdown code fence.
var fencePattern = regexp.MustCompile("(?s)^\\s*```[a-zA-Z]*\\s*\n(.*?)\n?\\s*```\\s*$")

// stripCodeFence removes one surrounding Markdown fence. Any other text stays
// in place so the JSON decoder reports the real problem.
func stripCodeFence(text string) string {
	if match := fencePattern.FindStringSubmatch(text); match != nil {
		return match[1]
	}
	return strings.TrimSpace(text)
}

// fileSet is the in-memory memory tree the worker edits.
type fileSet struct {
	files map[string][]byte
}

func (f *fileSet) clone() *fileSet {
	copied := &fileSet{files: make(map[string][]byte, len(f.files))}
	for path, content := range f.files {
		copied.files[path] = append([]byte(nil), content...)
	}
	return copied
}

func (f *fileSet) replaceWith(other *fileSet) {
	f.files = other.files
}

func (f *fileSet) sortedPaths() []string {
	paths := make([]string, 0, len(f.files))
	for path := range f.files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// refuseProtected rejects a baseline that carries files the worker must never
// rewrite, so a workflow file can never be part of a Dreaming result. `.git`
// content is not a work-tree file at all, but `.github` is.
func (f *fileSet) refuseProtected() error {
	for path := range f.files {
		if protectedPath(path) {
			return memerr.New(memerr.Unsupported,
				"the baseline contains the protected path %q; Dreaming never rewrites Git or workflow configuration", path)
		}
	}
	return nil
}

// applyStep validates and applies one step output on this copy of the tree.
func (w *worker) applyStep(f *fileSet, output DreamStepOutput, batchPaths []string) error {
	inBatch := make(map[string]bool, len(batchPaths))
	for _, path := range batchPaths {
		inBatch[path] = true
	}
	for _, operation := range output.Operations {
		path, err := vault.ValidateRelPath(operation.Path)
		if err != nil {
			return err
		}
		if protectedPath(path) {
			return memerr.New(memerr.Unsupported,
				"the step modifies the protected path %q", path)
		}
		// A step may only rewrite or delete files of its own batch plus the
		// entry file, so one bad call cannot rewrite the whole repository. A
		// new topic file is allowed: splitting a batch into a new file is the
		// main reason the entry index step exists, and the path still passes
		// the range and protected-path checks above.
		if operation.Action != StepCreate && !inBatch[path] && path != vault.EntryPointName {
			return memerr.New(memerr.Unsupported,
				"the step modifies %q outside its batch", path)
		}
		switch operation.Action {
		case StepCreate, StepUpdate:
			if strings.TrimSpace(operation.Content) == "" {
				return memerr.New(memerr.Unsupported,
					"the step %s of %q has no content", operation.Action, path)
			}
			if _, exists := f.files[path]; exists && operation.Action == StepCreate {
				return memerr.New(memerr.Unsupported,
					"the step creates %q, which already exists", path)
			}
			if _, exists := f.files[path]; !exists && operation.Action == StepUpdate {
				return memerr.New(memerr.Unsupported,
					"the step updates %q, which does not exist", path)
			}
			f.files[path] = []byte(operation.Content)
		case StepDelete:
			if path == vault.EntryPointName {
				return memerr.New(memerr.Unsupported, "the step deletes the entry file")
			}
			if _, exists := f.files[path]; !exists {
				return memerr.New(memerr.Unsupported,
					"the step deletes %q, which does not exist", path)
			}
			delete(f.files, path)
		default:
			return memerr.New(memerr.Unsupported,
				"the step uses the unsupported action %q", operation.Action)
		}
	}
	return w.validateLinks(f)
}

// validateLinks re-parses every file and refuses a step that introduced a new
// dangling or ambiguous link. Format and link validity is the strongest check
// this code can make; it cannot prove the absence of semantic loss. A problem
// that the baseline already had stays a diagnosis candidate and never fails a
// step, because fixing every pre-existing dangling link is not this step's job.
func (w *worker) validateLinks(f *fileSet) error {
	current := problemSet(f)
	var introduced []string
	for key := range current {
		if w.known[key] || w.introduced[key] {
			continue
		}
		introduced = append(introduced, key)
	}
	if len(introduced) == 0 {
		w.introduced = map[string]bool{}
		return nil
	}
	sort.Strings(introduced)
	w.introduced = current
	return memerr.New(memerr.Unsupported,
		"the step introduced %d link problem(s): %s",
		len(introduced), strings.Join(introduced, "; "))
}

// problemSet collects every link problem of one file set as stable keys.
func problemSet(f *fileSet) map[string]bool {
	problems := map[string]bool{}
	index := vault.NewLinkIndex(f.sortedPaths())
	for _, path := range f.sortedPaths() {
		parsed := vault.ParseFile(path, "", f.files[path])
		_, diagnostics := parsed.ResolveLinks(index)
		for _, diagnostic := range diagnostics {
			problems[linkProblemKey(diagnostic)] = true
		}
	}
	return problems
}

// baselineProblems lists the link problems that existed before any model call.
func baselineProblems(files *fileSet) map[string]bool {
	return problemSet(files)
}

func linkProblemKey(diagnostic vault.LinkDiagnostic) string {
	return fmt.Sprintf("%s|%s|%d|%s", diagnostic.Code, diagnostic.SourceFile, diagnostic.LineNumber, diagnostic.RawTarget)
}

// staticFindings computes the deterministic candidates: bloat, orphans,
// dangling links, missing metadata, expired `until` values and duplicates.
func staticFindings(files *fileSet, now time.Time) []staticFinding {
	var findings []staticFinding
	paths := files.sortedPaths()
	index := vault.NewLinkIndex(paths)
	parsed := make(map[string]*vault.ParsedFile, len(paths))
	for _, path := range paths {
		value := vault.ParseFile(path, "", files.files[path])
		parsed[path] = &value
	}

	entry := parsed[vault.EntryPointName]
	if entry != nil {
		if len(entry.Lines) > EntryBloatLines {
			findings = append(findings, staticFinding{
				Code:    "ENTRY_BLOAT",
				Path:    vault.EntryPointName,
				Message: "MEMORY.md has more lines than the index threshold; group topics into linked files",
			})
		}
	}

	reachable := reachablePaths(parsed, index)
	for _, path := range paths {
		if path == vault.EntryPointName {
			continue
		}
		if !reachable[path] {
			findings = append(findings, staticFinding{
				Code:    "ORPHAN_FILE",
				Path:    path,
				Message: "the file is not reachable from MEMORY.md",
			})
		}
	}

	today := now.UTC().Format("2006-01-02")
	seen := map[string][]string{}
	for _, path := range paths {
		_, diagnostics := parsed[path].ResolveLinks(index)
		for _, diagnostic := range diagnostics {
			findings = append(findings, staticFinding{
				Code:    string(diagnostic.Code),
				Path:    path,
				Line:    diagnostic.LineNumber,
				Message: diagnostic.RawTarget,
			})
		}
		for _, entry := range parsed[path].Entries {
			if !hasMetadata(entry, "source") || !hasMetadata(entry, "added") {
				findings = append(findings, staticFinding{
					Code:    "MISSING_RECOMMENDED_METADATA",
					Path:    path,
					Line:    entry.LineNumber,
					Message: "the entry has no source or added metadata",
				})
			}
			if until, ok := metadataValue(entry, "until"); ok && until < today {
				message := "until " + until + " has passed"
				// Deletion is a separate explicit action that only becomes
				// allowed after the 30-day hidden period. Naming the class keeps
				// the model from deleting a freshly expired entry. See
				// docs/10-memory-lifecycle.md section 3.3.
				if vault.DeletionDue(until, now) {
					message += "; the hidden period is over, deleting it is allowed"
				} else {
					message += "; still inside the hidden period, keep it"
				}
				findings = append(findings, staticFinding{
					Code:    "EXPIRED_ENTRY",
					Path:    path,
					Line:    entry.LineNumber,
					Message: message,
				})
			}
			if key := normalizeFact(entry.FactText); key != "" {
				seen[key] = append(seen[key], path)
			}
		}
	}
	for key, locations := range seen {
		if len(locations) < 2 {
			continue
		}
		unique := uniqueStrings(locations)
		if len(unique) < 2 {
			continue
		}
		findings = append(findings, staticFinding{
			Code:     "DUPLICATE_ENTRY",
			Message:  "the same normalized fact appears in several files",
			Subjects: unique,
		})
		_ = key
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Code != findings[j].Code {
			return findings[i].Code < findings[j].Code
		}
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Line < findings[j].Line
	})
	return findings
}

// EntryBloatLines is the documented MEMORY.md line threshold.
const EntryBloatLines = 100

// planBatches splits the managed files into bounded batches. A small repository
// is one batch; otherwise files are grouped by their order in MEMORY.md and the
// batch input is bounded by MaxBatchInputBytes.
func planBatches(files *fileSet) []batch {
	paths := files.sortedPaths()
	total := 0
	for _, path := range paths {
		total += len(files.files[path])
	}
	if len(paths) <= SmallBatchFileCount && total <= SmallBatchBytes {
		return []batch{{Paths: paths}}
	}
	ordered := orderedPaths(files, paths)
	var batches []batch
	current := batch{}
	size := 0
	for _, path := range ordered {
		contentSize := len(files.files[path])
		if len(current.Paths) > 0 && size+contentSize > MaxBatchInputBytes {
			batches = append(batches, current)
			current = batch{}
			size = 0
			if len(batches) == MaxBatches {
				break
			}
		}
		current.Paths = append(current.Paths, path)
		size += contentSize
	}
	if len(current.Paths) > 0 && len(batches) < MaxBatches {
		batches = append(batches, current)
	}
	if len(batches) == 0 {
		batches = []batch{{Paths: paths}}
	}
	return batches
}

// batchFiles materializes the file payload of one batch.
func batchFiles(files *fileSet, paths []string) []batchFile {
	payload := make([]batchFile, 0, len(paths))
	for _, path := range paths {
		payload = append(payload, batchFile{Path: path, Content: string(files.files[path])})
	}
	return payload
}

// summarizeIndex builds the compact summary used by stage C.
func summarizeIndex(files *fileSet, paths []string) []indexEntry {
	entries := make([]indexEntry, 0, len(paths))
	for _, path := range paths {
		parsed := vault.ParseFile(path, "", files.files[path])
		entry := indexEntry{Path: path, LineCount: len(parsed.Lines)}
		for _, line := range parsed.Lines {
			text := strings.TrimSpace(line.Text)
			if strings.HasPrefix(text, "# ") && entry.Title == "" {
				entry.Title = strings.TrimSpace(strings.TrimPrefix(text, "# "))
				continue
			}
			if strings.HasPrefix(text, "#") {
				entry.Headings = append(entry.Headings, text)
			}
		}
		entries = append(entries, entry)
	}
	return entries
}

// orderedPaths returns the files in MEMORY.md link order, with the entry file
// first and every remaining file appended in sorted order.
func orderedPaths(files *fileSet, paths []string) []string {
	entry := vault.ParseFile(vault.EntryPointName, "", files.files[vault.EntryPointName])
	index := vault.NewLinkIndex(paths)
	var ordered []string
	seen := map[string]bool{}
	push := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		ordered = append(ordered, path)
	}
	push(vault.EntryPointName)
	for _, link := range entry.Links {
		resolution := index.Resolve(link.RawTarget)
		if resolution.ResolvedPath != "" {
			push(resolution.ResolvedPath)
		}
	}
	for _, path := range paths {
		push(path)
	}
	return ordered
}

// reachablePaths walks MEMORY.md links transitively over already-parsed files.
func reachablePaths(parsed map[string]*vault.ParsedFile, index *vault.LinkIndex) map[string]bool {
	reachable := map[string]bool{vault.EntryPointName: true}
	queue := []string{vault.EntryPointName}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, link := range parsed[current].Links {
			resolution := index.Resolve(link.RawTarget)
			if resolution.ResolvedPath == "" || reachable[resolution.ResolvedPath] {
				continue
			}
			if _, ok := parsed[resolution.ResolvedPath]; !ok {
				continue
			}
			reachable[resolution.ResolvedPath] = true
			queue = append(queue, resolution.ResolvedPath)
		}
	}
	return reachable
}

func hasMetadata(entry vault.MemoryEntry, key string) bool {
	_, ok := metadataValue(entry, key)
	return ok
}

func metadataValue(entry vault.MemoryEntry, key string) (string, bool) {
	for _, field := range entry.Metadata {
		if strings.EqualFold(field.Key, key) {
			return strings.TrimSpace(field.Value), true
		}
	}
	return "", false
}

// normalizeFact canonicalizes a fact for duplicate detection: case-folded,
// with whitespace and punctuation removed.
func normalizeFact(fact string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(fact) {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r > 127:
			if !strings.ContainsRune("，。、；：！？（）【】《》“”‘’—…", r) {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// timeNow reads the injectable clock of the worker.
func (w *worker) timeNow() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

// stageBSystemPrompt is the fixed contract sent with every batch. It embeds the
// three-tier lifecycle rules of docs/10-memory-lifecycle.md section 3.
const stageBSystemPrompt = `You reorganize one personal memory repository. The repository is a Git work tree:
MEMORY.md is the entry point and links to topic files with [[path]] links (the .md
suffix is optional in the link).

You receive a JSON object with: today, phase, entryFile, batchFiles and
staticFindings. Return a JSON object with exactly two fields: "operations" and
"warnings".

Rules by memory class:
- State-like memory (preferences, configuration, current status): merge entries
  that say the same thing. When two entries state the same fact and one has a
  newer "added" date, keep the newer fact in place and remove the outdated value.
  Never write a change log, never keep a history of superseded values. If two
  entries contradict each other and the dates or context do not decide which is
  newer, keep both verbatim and report the contradiction in "warnings".
- Event-like memory (decisions and reasons, release records, incident findings,
  architecture migrations, anything with a historical date): append-only and
  never rewritten or deleted. You may move such an entry into a topic file, but
  the original fact text and its source/added metadata must survive, and the
  topic file must stay reachable from MEMORY.md.
- Time-bound memory (entries carrying "until: YYYY-MM-DD"): an entry past its
  date is already hidden from reads, but it must stay in the file for a 30-day
  hidden period. Delete it only when its staticFindings entry says the hidden
  period is over; when the finding says it is still inside the period, keep the
  bullet byte for byte, including its "until" value. Never invent or extend an
  "until" value.

Format rules:
- Keep one bullet per entry on one line: "- <fact> [source: <tag>; added: YYYY-MM-DD]".
- Keep root-relative [[link]] syntax and do not invent facts or source tags.
- Only touch files in batchFiles plus MEMORY.md; never create or delete MEMORY.md.
- Assume "create" means the path does not exist and "update" means it does.

Operations: each item is {"action": "create"|"update"|"delete", "path": "<root-relative path>", "content": "<full new file text, required for create/update>", "rationale": "<short reason>"}.
Warnings: short human-readable strings for anything a human must review.

Return only the JSON object, with no prose and no Markdown fence.`

// stageCSystemPrompt is the fixed contract of the index alignment step.
const stageCSystemPrompt = `You align the entry index of a personal memory repository. You receive a JSON
object with today, phase, entryFile, staticFindings and index: one summary per
managed file (path, title, headings, lineCount).

Return a JSON object with "operations" and "warnings". You may emit at most one
operation: {"action": "update", "path": "MEMORY.md", "content": "<full new MEMORY.md text>", "rationale": "<reason>"}.
Nothing else is accepted.

Requirements:
- Every topic file in index must be reachable from MEMORY.md through a
  [[path]] link (the .md suffix is optional).
- Keep the MEMORY.md heading structure and keep every existing fact line.
- Do not invent facts. Do not delete a linked file. Keep one bullet per line.
Return only the JSON object, with no prose and no Markdown fence.`
