package vault

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
)

const (
	RelationOutgoing = "outgoing"
	RelationBacklink = "backlink"
	RelationBoth     = "both"
)

// RelatedFile describes a one-hop neighbor in the wiki-link graph together
// with its brief summary from MEMORY.md's ## Index section when available.
type RelatedFile struct {
	Path     string `json:"path"`
	Relation string `json:"relation"`
	Summary  string `json:"summary,omitempty"`
}

// RawLink is a `[[target]]` occurrence located during parsing, before it is
// resolved against the set of existing files. Start and End are the byte offsets
// of the target inside the physical line text (exclusive End, without the
// brackets), so a rename can rewrite only the target bytes.
type RawLink struct {
	SourceFile string
	LineNumber int
	RawTarget  string
	Start      int
	End        int
}

// WikiLink is a raw link resolved against the repository file set. ResolvedPath
// is only set when exactly one existing file matches; Exists reports whether
// any file matches at all, so an ambiguous link keeps Exists true and
// ResolvedPath empty.
type WikiLink struct {
	SourceFile   string
	LineNumber   int
	RawTarget    string
	Exists       bool
	ResolvedPath string
}

// LinkIssueCode classifies a link that did not resolve to one file.
type LinkIssueCode string

const (
	LinkIssueInvalid   LinkIssueCode = "INVALID_LINK"
	LinkIssueMissing   LinkIssueCode = "MISSING_LINK"
	LinkIssueAmbiguous LinkIssueCode = "AMBIGUOUS_LINK"
)

// LinkDiagnostic reports a link that could not be resolved to exactly one
// existing file. An ambiguous link is never silently pointed at a candidate.
type LinkDiagnostic struct {
	Code       LinkIssueCode
	SourceFile string
	LineNumber int
	RawTarget  string
	Candidates []string
	Message    string
}

// LinkIndex resolves root-relative [[target]] links against the managed file
// set. Markdown links omit the extension, so an extensionless target also
// matches the .md and .markdown files of the same name.
type LinkIndex struct {
	files map[string]struct{}
}

// NewLinkIndex builds a resolver from the repository's root-relative file
// paths.
func NewLinkIndex(relPaths []string) *LinkIndex {
	index := &LinkIndex{files: make(map[string]struct{}, len(relPaths))}
	for _, relPath := range relPaths {
		index.files[relPath] = struct{}{}
	}
	return index
}

// LinkResolution is the outcome of resolving one raw target.
type LinkResolution struct {
	ResolvedPath string
	Exists       bool
	Candidates   []string
	Invalid      bool
}

// Resolve resolves one raw target. Candidates lists every existing file the
// target matches, sorted; more than one means the link is ambiguous.
func (idx *LinkIndex) Resolve(rawTarget string) LinkResolution {
	clean, err := ValidateRelPath(rawTarget)
	if err != nil {
		return LinkResolution{Invalid: true}
	}
	var found []string
	for _, candidate := range candidatePaths(clean) {
		if _, ok := idx.files[candidate]; ok {
			found = append(found, candidate)
		}
	}
	sort.Strings(found)
	switch len(found) {
	case 0:
		return LinkResolution{}
	case 1:
		return LinkResolution{ResolvedPath: found[0], Exists: true, Candidates: found}
	default:
		return LinkResolution{Exists: true, Candidates: found}
	}
}

// ResolveLinks resolves every raw link in the file against index. It returns
// the ordered file-level links and the diagnostics for invalid, dead and
// ambiguous targets, and fills each entry's InlineLinks with the links on its
// line.
func (p *ParsedFile) ResolveLinks(index *LinkIndex) ([]WikiLink, []LinkDiagnostic) {
	links := make([]WikiLink, 0, len(p.Links))
	var diagnostics []LinkDiagnostic
	byLine := make(map[int][]WikiLink)
	for _, raw := range p.Links {
		resolution := index.Resolve(raw.RawTarget)
		link := WikiLink{
			SourceFile:   raw.SourceFile,
			LineNumber:   raw.LineNumber,
			RawTarget:    raw.RawTarget,
			Exists:       resolution.Exists,
			ResolvedPath: resolution.ResolvedPath,
		}
		links = append(links, link)
		byLine[raw.LineNumber] = append(byLine[raw.LineNumber], link)
		if diagnostic, ok := linkDiagnostic(raw, resolution); ok {
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	for i := range p.Entries {
		p.Entries[i].InlineLinks = byLine[p.Entries[i].LineNumber]
	}
	return links, diagnostics
}

// candidatePaths expands an extensionless target to the Markdown files it may
// name. A target with any extension is matched literally.
func candidatePaths(clean string) []string {
	if path.Ext(clean) != "" {
		return []string{clean}
	}
	return []string{clean, clean + ".md", clean + ".markdown"}
}

// LinkTargetFor returns the link text that replaces rawTarget when the file it
// resolves to is renamed to newPath. A Markdown link omits its .md or .markdown
// extension, so an extensionless raw target stays extensionless; any other
// target keeps the new path literally.
func LinkTargetFor(rawTarget, newPath string) string {
	if path.Ext(rawTarget) != "" {
		return newPath
	}
	switch path.Ext(newPath) {
	case ".md", ".markdown":
		return strings.TrimSuffix(newPath, path.Ext(newPath))
	default:
		return newPath
	}
}

// RewriteLinks rewrites every parsed link in file that resolves to oldPath so it
// points at newPath. Only the link target bytes change; untouched lines keep
// their original bytes. It returns the new content and whether anything
// changed. Links inside code fences or inline code are not parsed as links, so
// they are not rewritten.
func RewriteLinks(file ParsedFile, index *LinkIndex, oldPath, newPath string) (string, bool) {
	rangesByLine := map[int][][2]int{}
	changed := false
	for _, raw := range file.Links {
		if index.Resolve(raw.RawTarget).ResolvedPath != oldPath {
			continue
		}
		rangesByLine[raw.LineNumber] = append(rangesByLine[raw.LineNumber], [2]int{raw.Start, raw.End})
		changed = true
	}
	if !changed {
		return file.Raw(), false
	}

	var b strings.Builder
	for _, line := range file.Lines {
		ranges := rangesByLine[line.Number]
		if len(ranges) == 0 {
			b.WriteString(line.Text)
			b.WriteString(line.Ending)
			continue
		}
		text := line.Text
		// Replace from the end of the line backwards so earlier offsets stay
		// valid while the target bytes change length.
		for i := len(ranges) - 1; i >= 0; i-- {
			start, end := ranges[i][0], ranges[i][1]
			text = text[:start] + LinkTargetFor(text[start:end], newPath) + text[end:]
		}
		b.WriteString(text)
		b.WriteString(line.Ending)
	}
	return b.String(), true
}

func linkDiagnostic(raw RawLink, resolution LinkResolution) (LinkDiagnostic, bool) {
	diagnostic := LinkDiagnostic{
		SourceFile: raw.SourceFile,
		LineNumber: raw.LineNumber,
		RawTarget:  raw.RawTarget,
	}
	switch {
	case resolution.Invalid:
		diagnostic.Code = LinkIssueInvalid
		diagnostic.Message = fmt.Sprintf("link target %q is not a valid root-relative path", raw.RawTarget)
	case len(resolution.Candidates) > 1:
		diagnostic.Code = LinkIssueAmbiguous
		diagnostic.Candidates = resolution.Candidates
		diagnostic.Message = fmt.Sprintf("link target %q matches multiple files: %s", raw.RawTarget, strings.Join(resolution.Candidates, ", "))
	case resolution.ResolvedPath != "":
		return LinkDiagnostic{}, false
	default:
		diagnostic.Code = LinkIssueMissing
		diagnostic.Message = fmt.Sprintf("link target %q does not exist", raw.RawTarget)
	}
	return diagnostic, true
}

// ExtractIndexSummaries extracts the short summary text after `[[target]]` for
// each resolved wiki-link under MEMORY.md's `## Index` section (for example,
// `- [[notes/foo]] — 简述` maps `"notes/foo.md"` to `"简述"`).
func ExtractIndexSummaries(memoryParsed ParsedFile, index *LinkIndex) map[string]string {
	summaries := make(map[string]string)
	for _, entry := range memoryParsed.Entries {
		if !strings.EqualFold(strings.TrimSpace(entry.Heading), "Index") {
			continue
		}
		summary := extractEntryIndexSummary(entry.FactText)
		if summary == "" {
			continue
		}
		if len(entry.InlineLinks) > 0 {
			for _, link := range entry.InlineLinks {
				if link.ResolvedPath != "" && link.Exists && summaries[link.ResolvedPath] == "" {
					summaries[link.ResolvedPath] = summary
				}
			}
			continue
		}
		if index != nil {
			for _, raw := range scanLinks(memoryParsed.RelPath, entry.LineNumber, entry.FactText) {
				res := index.Resolve(raw.RawTarget)
				if res.ResolvedPath != "" && res.Exists && summaries[res.ResolvedPath] == "" {
					summaries[res.ResolvedPath] = summary
				}
			}
		}
	}
	return summaries
}

func extractEntryIndexSummary(factText string) string {
	idx := strings.LastIndex(factText, "]]")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(factText[idx+2:])
	rest = strings.TrimLeftFunc(rest, func(r rune) bool {
		return unicode.IsSpace(r) || r == '—' || r == '–' || r == '-' || r == ':'
	})
	return strings.TrimSpace(rest)
}

// ComputeRelatedFiles returns the deduplicated, path-sorted one-hop outgoing
// and backlink neighbors for targetPaths. Self-loops (neighbor == src) and,
// when excludeEntryPoint is true, MEMORY.md as a neighbor are omitted.
func ComputeRelatedFiles(
	targetPaths []string,
	outgoing map[string][]string,
	backlinks map[string][]string,
	summaries map[string]string,
	excludeEntryPoint bool,
) []RelatedFile {
	type relFlags struct {
		outgoing bool
		backlink bool
	}
	neighbors := make(map[string]relFlags)
	for _, src := range targetPaths {
		if src == "" {
			continue
		}
		for _, dst := range outgoing[src] {
			if dst == "" || dst == src {
				continue
			}
			if excludeEntryPoint && IsEntryPoint(dst) {
				continue
			}
			f := neighbors[dst]
			f.outgoing = true
			neighbors[dst] = f
		}
		for _, from := range backlinks[src] {
			if from == "" || from == src {
				continue
			}
			if excludeEntryPoint && IsEntryPoint(from) {
				continue
			}
			f := neighbors[from]
			f.backlink = true
			neighbors[from] = f
		}
	}
	if len(neighbors) == 0 {
		return nil
	}
	paths := make([]string, 0, len(neighbors))
	for p := range neighbors {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := make([]RelatedFile, 0, len(paths))
	for _, p := range paths {
		f := neighbors[p]
		rel := RelationOutgoing
		switch {
		case f.outgoing && f.backlink:
			rel = RelationBoth
		case f.backlink:
			rel = RelationBacklink
		}
		out = append(out, RelatedFile{
			Path:     p,
			Relation: rel,
			Summary:  summaries[p],
		})
	}
	return out
}
