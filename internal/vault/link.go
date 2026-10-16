package vault

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// RawLink is a `[[target]]` occurrence located during parsing, before it is
// resolved against the set of existing files.
type RawLink struct {
	SourceFile string
	LineNumber int
	RawTarget  string
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
