// Package vault parses the memory format described in
// docs/02-domain-models-and-contracts.md section 1.2. Parsing is a pure,
// line-level, lossless operation: it keeps every original line and never reads
// or writes the repository beyond the bytes it is handed.
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// MetadataField is one order-preserving key/value pair from an entry's trailing
// metadata. Keys are open, so Raw keeps the exact original segment and unknown
// or repeated keys are never dropped or reordered.
type MetadataField struct {
	Key   string
	Value string
	Raw   string
}

// SourceRef is one parsed `source` value. Device, Agent and SessionID are only
// set for a legal agent:// source.
type SourceRef struct {
	Raw       string
	Scheme    string
	Device    string
	Agent     string
	SessionID string
}

// MemoryEntry is one bullet line parsed losslessly. RawLine is the exact line
// text without its terminator; FactText is the trimmed fact text before the
// trailing metadata region. InlineLinks stays empty until ResolveLinks runs.
type MemoryEntry struct {
	ID           string
	FilePath     string
	FileRevision string
	LineNumber   int
	RawLine      string
	FactText     string
	Sources      []SourceRef
	Metadata     []MetadataField
	Heading      string
	AddedDate    string
	InlineLinks  []WikiLink
}

// Line is one physical line kept with its original terminator so Raw reproduces
// the parsed input byte for byte.
type Line struct {
	Number int
	Text   string
	Ending string
}

// ParsedFile is the lossless structural parse of one managed text file.
type ParsedFile struct {
	RelPath      string
	FileRevision string
	Lines        []Line
	Entries      []MemoryEntry
	Links        []RawLink
}

// Raw reassembles the original bytes from the stored lines. It is the
// losslessness check: an edit changes only the targeted entries, and an
// untouched file round-trips unchanged.
func (p ParsedFile) Raw() string {
	total := 0
	for _, line := range p.Lines {
		total += len(line.Text) + len(line.Ending)
	}
	var b strings.Builder
	b.Grow(total)
	for _, line := range p.Lines {
		b.WriteString(line.Text)
		b.WriteString(line.Ending)
	}
	return b.String()
}

// EntryID is the documented identity of an entry within a specific file
// revision and line. It is not promised to survive edits; inserting a line is
// allowed to change it.
func EntryID(relPath, fileRevision string, lineNumber int) string {
	hash := sha256.New()
	writeField(hash, []byte(relPath))
	writeField(hash, []byte(fileRevision))
	writeField(hash, []byte(strconv.Itoa(lineNumber)))
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// ParseFile parses one managed text file without touching the filesystem.
// Entries are top-level `- ` bullets; trailing `[key: value; ...]` metadata
// groups are split into order-preserving fields, and `[[target]]` links are
// collected outside code fences and inline code.
func ParseFile(relPath, fileRevision string, content []byte) ParsedFile {
	parsed := ParsedFile{RelPath: relPath, FileRevision: fileRevision}
	parsed.Lines = splitLines(string(content))

	heading := ""
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0

	for _, line := range parsed.Lines {
		if inFence {
			if closesFence(line.Text, fenceChar, fenceLen) {
				inFence = false
				fenceChar, fenceLen = 0, 0
			}
			continue
		}
		if char, length, ok := opensFence(line.Text); ok {
			inFence = true
			fenceChar, fenceLen = char, length
			continue
		}
		if text, ok := headingText(line.Text); ok {
			heading = text
		}
		parsed.Links = append(parsed.Links, scanLinks(relPath, line.Number, line.Text)...)
		if body, ok := bulletBody(line.Text); ok {
			parsed.Entries = append(parsed.Entries, parseEntry(relPath, fileRevision, line.Number, line.Text, body, heading))
		}
	}
	return parsed
}

// Parse reads and parses one managed text file. It does not write to the
// repository.
func (r *Root) Parse(rel string) (ParsedFile, error) {
	file, err := r.ReadFile(rel)
	if err != nil {
		return ParsedFile{}, err
	}
	return ParseFile(file.RelPath, file.FileRevision, []byte(file.RawContent)), nil
}

func splitLines(content string) []Line {
	if content == "" {
		return nil
	}
	var lines []Line
	number := 1
	for len(content) > 0 {
		index := strings.IndexByte(content, '\n')
		if index < 0 {
			lines = append(lines, Line{Number: number, Text: content, Ending: ""})
			break
		}
		text := content[:index]
		ending := "\n"
		if strings.HasSuffix(text, "\r") {
			text = text[:len(text)-1]
			ending = "\r\n"
		}
		lines = append(lines, Line{Number: number, Text: text, Ending: ending})
		content = content[index+1:]
		number++
	}
	return lines
}

// opensFence reports an opening code fence: up to three leading spaces and at
// least three backticks or tildes.
func opensFence(text string) (char byte, length int, ok bool) {
	stripped := strings.TrimLeft(text, " ")
	if isIndentedCode(text, stripped) {
		return 0, 0, false
	}
	if len(stripped) < 3 || (stripped[0] != '`' && stripped[0] != '~') {
		return 0, 0, false
	}
	count := 0
	for count < len(stripped) && stripped[count] == stripped[0] {
		count++
	}
	if count < 3 {
		return 0, 0, false
	}
	return stripped[0], count, true
}

// closesFence reports a closing fence: the same character, at least as many as
// the opening run, and nothing else besides trailing spaces.
func closesFence(text string, char byte, length int) bool {
	stripped := strings.TrimLeft(text, " ")
	if isIndentedCode(text, stripped) {
		return false
	}
	count := 0
	for count < len(stripped) && stripped[count] == char {
		count++
	}
	if count < length {
		return false
	}
	return strings.TrimRight(stripped[count:], " \t") == ""
}

func isIndentedCode(original, stripped string) bool {
	return len(original)-len(stripped) > 3
}

// headingText returns the text of an ATX heading line.
func headingText(text string) (string, bool) {
	stripped := strings.TrimLeft(text, " ")
	if isIndentedCode(text, stripped) {
		return "", false
	}
	count := 0
	for count < len(stripped) && stripped[count] == '#' {
		count++
	}
	if count == 0 || count > 6 {
		return "", false
	}
	rest := stripped[count:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// bulletBody returns the text after the `- ` marker of a top-level entry.
// Indented lines, other bullet markers and unknown formats stay literal.
func bulletBody(text string) (string, bool) {
	if len(text) < 2 || text[0] != '-' {
		return "", false
	}
	if text[1] != ' ' && text[1] != '\t' {
		return "", false
	}
	return text[2:], true
}

func parseEntry(relPath, fileRevision string, lineNumber int, rawLine, body, heading string) MemoryEntry {
	fields, fact := splitTrailingMetadata(body)
	entry := MemoryEntry{
		ID:           EntryID(relPath, fileRevision, lineNumber),
		FilePath:     relPath,
		FileRevision: fileRevision,
		LineNumber:   lineNumber,
		RawLine:      rawLine,
		FactText:     strings.TrimSpace(fact),
		Heading:      heading,
		Metadata:     fields,
	}
	for _, field := range fields {
		switch field.Key {
		case "source":
			entry.Sources = append(entry.Sources, parseSource(field.Value))
		case "added":
			if isDate(field.Value) {
				entry.AddedDate = field.Value
			}
		}
	}
	return entry
}

// splitTrailingMetadata separates the optional trailing `[key: value; ...]`
// groups from the fact text. A trailing bracket group only counts as metadata
// when every `;`-separated segment is `key: value` with a plain key, so links,
// Markdown link text and other bracket text stay part of the fact.
func splitTrailingMetadata(body string) ([]MetadataField, string) {
	fields, fact, _ := splitEntryBody(body)
	return fields, fact
}

// SplitFactAndMetadata returns the trimmed fact text of one bullet body and the
// exact trailing metadata suffix, including the whitespace before the first
// metadata group. A body without metadata returns the fact and an empty suffix,
// so a caller can rewrite only the fact and keep every metadata byte.
func SplitFactAndMetadata(body string) (string, string) {
	_, fact, suffix := splitEntryBody(body)
	return strings.TrimSpace(fact), suffix
}

// splitEntryBody returns the parsed metadata fields, the fact prefix (trimmed
// on the right) and the exact suffix from the first metadata group to the end of
// body. The suffix includes the whitespace that separates the fact from the
// metadata so string concatenation reproduces the original bytes.
func splitEntryBody(body string) ([]MetadataField, string, string) {
	trimmed := strings.TrimRight(body, " \t")
	index := len(trimmed)
	var fields []MetadataField
	for index > 0 && trimmed[index-1] == ']' {
		open := strings.LastIndexByte(trimmed[:index-1], '[')
		if open < 0 {
			break
		}
		groupFields, ok := parseMetadataGroup(trimmed[open+1 : index-1])
		if !ok {
			break
		}
		fields = append(groupFields, fields...)
		index = open
		for index > 0 && (trimmed[index-1] == ' ' || trimmed[index-1] == '\t') {
			index--
		}
	}
	return fields, trimmed[:index], body[index:]
}

// ReplaceEntryFact rewrites the fact text of one bullet line and keeps its
// trailing metadata bytes untouched. It reports false for a non-bullet line.
func ReplaceEntryFact(rawLine, newFact string) (string, bool) {
	if len(rawLine) < 2 || rawLine[0] != '-' || (rawLine[1] != ' ' && rawLine[1] != '\t') {
		return rawLine, false
	}
	_, suffix := SplitFactAndMetadata(rawLine[2:])
	return rawLine[:2] + strings.TrimSpace(newFact) + suffix, true
}

// WithAppendedLine returns the content with one line appended after the final
// line and the 1-based number of the appended line.
func (p ParsedFile) WithAppendedLine(line string) (string, int) {
	content := p.Raw()
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"
	return content, strings.Count(content, "\n")
}

// WithoutLine returns the content with one physical line removed, including its
// terminator. It reports false when the line is not part of the file.
func (p ParsedFile) WithoutLine(lineNumber int) (string, bool) {
	var b strings.Builder
	found := false
	for _, line := range p.Lines {
		if line.Number == lineNumber {
			found = true
			continue
		}
		b.WriteString(line.Text)
		b.WriteString(line.Ending)
	}
	return b.String(), found
}

// WithReplacedLine returns the content with one physical line replaced by text.
// It reports false when the line is not part of the file.
func (p ParsedFile) WithReplacedLine(lineNumber int, text string) (string, bool) {
	var b strings.Builder
	found := false
	for _, line := range p.Lines {
		if line.Number == lineNumber {
			b.WriteString(text)
			b.WriteString(line.Ending)
			found = true
			continue
		}
		b.WriteString(line.Text)
		b.WriteString(line.Ending)
	}
	return b.String(), found
}

func parseMetadataGroup(content string) ([]MetadataField, bool) {
	if content == "" || strings.ContainsAny(content, "[]") {
		return nil, false
	}
	segments := strings.Split(content, ";")
	fields := make([]MetadataField, 0, len(segments))
	for _, segment := range segments {
		key, value, ok := strings.Cut(segment, ":")
		if !ok {
			return nil, false
		}
		key = strings.TrimSpace(key)
		if key == "" || !isMetadataKey(key) {
			return nil, false
		}
		fields = append(fields, MetadataField{Key: key, Value: strings.TrimSpace(value), Raw: segment})
	}
	return fields, true
}

func isMetadataKey(key string) bool {
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}

func isDate(value string) bool {
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}

func parseSource(raw string) SourceRef {
	ref := SourceRef{Raw: raw}
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || scheme == "" {
		return ref
	}
	ref.Scheme = strings.ToLower(scheme)
	if ref.Scheme != "agent" {
		return ref
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != "" {
		ref.Device, ref.Agent, ref.SessionID = parts[0], parts[1], parts[2]
	}
	return ref
}

// scanLinks collects `[[target]]` occurrences outside inline code spans.
func scanLinks(sourceFile string, lineNumber int, text string) []RawLink {
	var links []RawLink
	for i := 0; i < len(text); {
		if text[i] == '`' {
			i = skipCodeSpan(text, i)
			continue
		}
		if text[i] == '[' && i+1 < len(text) && text[i+1] == '[' {
			end := strings.Index(text[i+2:], "]]")
			if end < 0 {
				i++
				continue
			}
			links = append(links, RawLink{
				SourceFile: sourceFile,
				LineNumber: lineNumber,
				RawTarget:  text[i+2 : i+2+end],
				Start:      i + 2,
				End:        i + 2 + end,
			})
			i += 2 + end + 2
			continue
		}
		i++
	}
	return links
}

// skipCodeSpan returns the index just past a backtick code span. An unmatched
// opening run is literal, so only the run itself is skipped.
func skipCodeSpan(text string, start int) int {
	open := start
	for open < len(text) && text[open] == '`' {
		open++
	}
	length := open - start
	for search := open; search < len(text); {
		if text[search] != '`' {
			search++
			continue
		}
		run := search
		for run < len(text) && text[run] == '`' {
			run++
		}
		if run-search == length {
			return run
		}
		search = run
	}
	return open
}
