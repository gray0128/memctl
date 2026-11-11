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

// UntilKey is the optional metadata key that marks a time-limited entry. It is
// not required, and an entry without it never expires. See
// docs/10-memory-lifecycle.md section 3.1.
const UntilKey = "until"

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

// Optional standard metadata keys of docs/18-agent-memory-review-and-optimization.md
// section 4.4.2. Both are open metadata: an absent or unknown value keeps the
// entry exactly as it was, and only the legal values below are recognized.
const (
	KindKey   = "kind"
	StatusKey = "status"

	KindState      = "state"
	KindEvent      = "event"
	KindProcedural = "procedural"

	StatusVerified   = "verified"
	StatusHypothesis = "hypothesis"
)

// Detail block bounds of docs/18 section 4.4.1. Indented lines beyond either
// bound stay ordinary unattributed lines; no byte is ever dropped.
const (
	MaxDetailLines = 20
	MaxDetailBytes = 2048
)

// ValidKind reports whether value is a recognized `kind` metadata value.
func ValidKind(value string) bool {
	switch value {
	case KindState, KindEvent, KindProcedural:
		return true
	}
	return false
}

// ValidStatus reports whether value is a recognized `status` metadata value.
func ValidStatus(value string) bool {
	switch value {
	case StatusVerified, StatusHypothesis:
		return true
	}
	return false
}

// MemoryEntry is one bullet line parsed losslessly. RawLine is the exact line
// text without its terminator; FactText is the trimmed fact text before the
// trailing metadata region. DetailLines are the indented lines directly after
// the bullet that belong to it (docs/18 section 4.4.1); they occupy the
// physical lines LineNumber+1 .. LastLine(). InlineLinks stays empty until
// ResolveLinks runs.
type MemoryEntry struct {
	ID           string
	FilePath     string
	FileRevision string
	LineNumber   int
	RawLine      string
	FactText     string
	DetailLines  []string
	Sources      []SourceRef
	Metadata     []MetadataField
	Heading      string
	AddedDate    string
	// UntilDate is the raw `until` metadata value when it is a legal
	// YYYY-MM-DD date; otherwise it stays empty and the entry never expires.
	UntilDate string
	// Kind and Status are the recognized `kind` and `status` metadata values,
	// or empty when the key is absent or carries an unknown value.
	Kind        string
	Status      string
	InlineLinks []WikiLink
}

// LastLine returns the physical line number of the last line of the entry's
// block: the bullet itself or its last detail line.
func (e MemoryEntry) LastLine() int {
	return e.LineNumber + len(e.DetailLines)
}

// IsEvent reports whether the entry is explicitly marked `kind: event`.
func (e MemoryEntry) IsEvent() bool {
	return e.Kind == KindEvent
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

// EntryID is the content-stable identity of an entry within one file
// (docs/18 section 4.4.1). It hashes the file path, the normalized fact text
// and the `added` date, so editing, inserting or deleting other lines of the
// file keeps the ID; rewriting the entry's own fact or `added` changes it.
// occurrence is the 0-based count of earlier entries of the same file with
// the same normalized fact and `added` date; it is mixed in only from the
// second such entry, so identical lines still get distinct IDs.
func EntryID(relPath, factText, addedDate string, occurrence int) string {
	hash := sha256.New()
	writeField(hash, []byte(relPath))
	writeField(hash, []byte(NormalizeFactText(factText)))
	writeField(hash, []byte(addedDate))
	if occurrence > 0 {
		writeField(hash, []byte(strconv.Itoa(occurrence)))
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil))
}

// NormalizeFactText is the comparison form of a fact: surrounding whitespace
// trimmed and inner whitespace runs collapsed to one space. It is the identity
// input of EntryID and of the `kind: event` guard.
func NormalizeFactText(fact string) string {
	return strings.Join(strings.Fields(fact), " ")
}

// ParseFile parses one managed text file without touching the filesystem.
// Entries are top-level `- ` bullets; trailing `[key: value; ...]` metadata
// groups are split into order-preserving fields, and `[[target]]` links are
// collected outside code fences and inline code. The indented lines directly
// after a bullet become its DetailLines; they keep their place in Lines, so
// Raw still reproduces every byte.
func ParseFile(relPath, fileRevision string, content []byte) ParsedFile {
	parsed := ParsedFile{RelPath: relPath, FileRevision: fileRevision}
	parsed.Lines = splitLines(string(content))

	heading := ""
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	occurrences := map[string]int{}

	// block tracks the entry that is collecting detail lines. A block that
	// ends inside a fence it opened drops that fence again, so a later block
	// edit never removes half of a fence and turns its rest into an opener.
	block := -1
	blockBytes := 0
	blockFence := false
	blockFenceStart := 0
	stopBlock := func() {
		if block >= 0 && blockFence {
			entry := &parsed.Entries[block]
			entry.DetailLines = entry.DetailLines[:blockFenceStart]
			if len(entry.DetailLines) == 0 {
				entry.DetailLines = nil
			}
		}
		block, blockBytes, blockFence = -1, 0, false
	}
	// attach adds one line to the open block, or ends the block when the
	// line would exceed a bound.
	attach := func(text string) bool {
		entry := &parsed.Entries[block]
		if len(entry.DetailLines)+1 > MaxDetailLines || blockBytes+len(text)+1 > MaxDetailBytes {
			stopBlock()
			return false
		}
		entry.DetailLines = append(entry.DetailLines, text)
		blockBytes += len(text) + 1
		return true
	}

	for _, line := range parsed.Lines {
		if inFence {
			closing := closesFence(line.Text, fenceChar, fenceLen)
			if block >= 0 {
				if blockFence && (strings.TrimSpace(line.Text) == "" || isDetailIndent(line.Text)) {
					if attach(line.Text) && closing {
						blockFence = false
					}
				} else {
					stopBlock()
				}
			}
			if closing {
				inFence = false
				fenceChar, fenceLen = 0, 0
			}
			continue
		}
		if block >= 0 {
			if _, _, isHeading := headingLevelAndText(line.Text); isHeading || !isDetailIndent(line.Text) || strings.TrimSpace(line.Text) == "" {
				stopBlock()
			}
		}
		if char, length, ok := opensFence(line.Text); ok {
			inFence = true
			fenceChar, fenceLen = char, length
			if block >= 0 {
				blockFenceStart = len(parsed.Entries[block].DetailLines)
				blockFence = true
				attach(line.Text)
			}
			continue
		}
		if text, ok := headingText(line.Text); ok {
			heading = text
		}
		parsed.Links = append(parsed.Links, scanLinks(relPath, line.Number, line.Text)...)
		if block >= 0 {
			attach(line.Text)
			continue
		}
		if body, ok := bulletBody(line.Text); ok {
			entry := parseEntry(relPath, fileRevision, line.Number, line.Text, body, heading)
			key := NormalizeFactText(entry.FactText) + "\x00" + entry.AddedDate
			entry.ID = EntryID(relPath, entry.FactText, entry.AddedDate, occurrences[key])
			occurrences[key]++
			parsed.Entries = append(parsed.Entries, entry)
			block = len(parsed.Entries) - 1
		}
	}
	return parsed
}

// isDetailIndent reports a line indented by at least two spaces or one tab,
// the indentation that attaches a line to the bullet above it.
func isDetailIndent(text string) bool {
	return strings.HasPrefix(text, "  ") || strings.HasPrefix(text, "\t")
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

// headingLevelAndText returns the ATX heading level (1..6) and trimmed title.
func headingLevelAndText(text string) (int, string, bool) {
	stripped := strings.TrimLeft(text, " ")
	if isIndentedCode(text, stripped) {
		return 0, "", false
	}
	count := 0
	for count < len(stripped) && stripped[count] == '#' {
		count++
	}
	if count == 0 || count > 6 {
		return 0, "", false
	}
	rest := stripped[count:]
	if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
		return 0, "", false
	}
	return count, strings.TrimSpace(rest), true
}

// headingText returns the text of an ATX heading line.
func headingText(text string) (string, bool) {
	_, title, ok := headingLevelAndText(text)
	return title, ok
}

// ParseHeadingSpec normalizes a caller-supplied heading specification such as
// "Decisions" or "## Decisions". It returns the trimmed heading title, the
// ATX hash prefix to use when creating a new heading (using defaultLevel 1..6
// when raw does not start with an ATX prefix), and the explicit ATX level when
// raw carried one (or 0 when raw was a plain title).
func ParseHeadingSpec(raw string, defaultLevel int) (title string, hashes string, explicitLevel int) {
	if defaultLevel < 1 || defaultLevel > 6 {
		defaultLevel = 2
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", 0
	}
	if level, parsedTitle, ok := headingLevelAndText(trimmed); ok {
		if parsedTitle == "" {
			return "", "", 0
		}
		return parsedTitle, strings.Repeat("#", level), level
	}
	return trimmed, strings.Repeat("#", defaultLevel), 0
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
		case UntilKey:
			if isDate(field.Value) {
				entry.UntilDate = field.Value
			}
		case KindKey:
			if ValidKind(field.Value) {
				entry.Kind = field.Value
			}
		case StatusKey:
			if ValidStatus(field.Value) {
				entry.Status = field.Value
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

// WithAppendedLineInSection inserts line at the end of the Markdown section
// matching heading (before the next heading of the same or higher level) and
// returns the updated file content and the 1-based line number of the inserted
// line. When heading is blank, it appends at the end of the file like
// WithAppendedLine. When no matching heading exists in the file, a new heading
// section is appended at the end of the file.
func (p ParsedFile) WithAppendedLineInSection(heading, line string) (string, int) {
	targetTitle, hashes, explicitLevel := ParseHeadingSpec(heading, 2)
	if targetTitle == "" {
		return p.WithAppendedLine(line)
	}

	type headingSpan struct {
		idx   int
		level int
		title string
	}
	var headings []headingSpan
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	for i, l := range p.Lines {
		if inFence {
			if closesFence(l.Text, fenceChar, fenceLen) {
				inFence = false
				fenceChar, fenceLen = 0, 0
			}
			continue
		}
		if char, length, ok := opensFence(l.Text); ok {
			inFence = true
			fenceChar, fenceLen = char, length
			continue
		}
		if level, title, ok := headingLevelAndText(l.Text); ok {
			headings = append(headings, headingSpan{idx: i, level: level, title: title})
		}
	}

	match := -1
	if explicitLevel > 0 {
		for i, h := range headings {
			if h.level == explicitLevel && h.title == targetTitle {
				match = i
				break
			}
		}
	}
	if match < 0 {
		for i, h := range headings {
			if h.title == targetTitle {
				match = i
				break
			}
		}
	}
	if match < 0 && explicitLevel > 0 {
		for i, h := range headings {
			if h.level == explicitLevel && strings.EqualFold(h.title, targetTitle) {
				match = i
				break
			}
		}
	}
	if match < 0 {
		for i, h := range headings {
			if strings.EqualFold(h.title, targetTitle) {
				match = i
				break
			}
		}
	}

	writeRange := func(b *strings.Builder, start, end int) {
		for i := start; i < end; i++ {
			b.WriteString(p.Lines[i].Text)
			if p.Lines[i].Ending != "" {
				b.WriteString(p.Lines[i].Ending)
			} else {
				b.WriteString("\n")
			}
		}
	}

	if match >= 0 {
		matchedIdx := headings[match].idx
		matchedLevel := headings[match].level
		boundaryLevel := matchedLevel
		if boundaryLevel == 1 {
			boundaryLevel = 2
		}
		endIdx := len(p.Lines)
		for _, h := range headings[match+1:] {
			if h.level <= boundaryLevel {
				endIdx = h.idx
				break
			}
		}

		lastNonBlank := matchedIdx
		for i := endIdx - 1; i > matchedIdx; i-- {
			if strings.TrimSpace(p.Lines[i].Text) != "" {
				lastNonBlank = i
				break
			}
		}

		var prefix strings.Builder
		if lastNonBlank > matchedIdx {
			writeRange(&prefix, 0, lastNonBlank+1)
		} else {
			writeRange(&prefix, 0, matchedIdx+1)
			if matchedIdx+1 < endIdx {
				prefix.WriteString("\n")
			}
		}
		prefix.WriteString(line)
		prefix.WriteString("\n")
		insertedLine := strings.Count(prefix.String(), "\n")

		if endIdx < len(p.Lines) {
			prefix.WriteString("\n")
			for i := endIdx; i < len(p.Lines); i++ {
				prefix.WriteString(p.Lines[i].Text)
				prefix.WriteString(p.Lines[i].Ending)
			}
		}
		return prefix.String(), insertedLine
	}

	lastNonBlank := -1
	for i := len(p.Lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(p.Lines[i].Text) != "" {
			lastNonBlank = i
			break
		}
	}

	var b strings.Builder
	if lastNonBlank >= 0 {
		writeRange(&b, 0, lastNonBlank+1)
		b.WriteString("\n")
	}
	b.WriteString(hashes)
	b.WriteString(" ")
	b.WriteString(targetTitle)
	b.WriteString("\n\n")
	b.WriteString(line)
	b.WriteString("\n")
	content := b.String()
	return content, strings.Count(content, "\n")
}

// WithoutLine returns the content with one physical line removed, including its
// terminator. It reports false when the line is not part of the file.
func (p ParsedFile) WithoutLine(lineNumber int) (string, bool) {
	return p.WithoutLines(lineNumber, lineNumber)
}

// WithoutLines returns the content with the physical lines first..last
// removed, including their terminators. It reports false when first is not
// part of the file.
func (p ParsedFile) WithoutLines(first, last int) (string, bool) {
	var b strings.Builder
	found := false
	for _, line := range p.Lines {
		if line.Number >= first && line.Number <= last {
			found = found || line.Number == first
			continue
		}
		b.WriteString(line.Text)
		b.WriteString(line.Ending)
	}
	return b.String(), found
}

// WithoutEntry returns the content with the entry's whole block removed: the
// bullet and its DetailLines, so no indented line is left behind.
func (p ParsedFile) WithoutEntry(entry MemoryEntry) (string, bool) {
	return p.WithoutLines(entry.LineNumber, entry.LastLine())
}

// EntryBlock returns the entry's bullet line with newLine in place of its raw
// line, followed by its DetailLines, joined by "\n". It is the text a block
// move appends to another file.
func EntryBlock(newLine string, entry MemoryEntry) string {
	if len(entry.DetailLines) == 0 {
		return newLine
	}
	return newLine + "\n" + strings.Join(entry.DetailLines, "\n")
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

// parseMetadataGroup splits one bracket group into `key: value` fields. A
// value may carry a literal `;` escaped as `\;` or inside a double-quoted
// value (docs/18 section 4.4.1); Value holds the unescaped text and Raw keeps
// the exact original segment. Unbalanced quotes fall back to splitting on
// every unescaped `;`, the behavior of a group without quoting.
func parseMetadataGroup(content string) ([]MetadataField, bool) {
	if content == "" || strings.ContainsAny(content, "[]") {
		return nil, false
	}
	segments, ok := splitMetadataSegments(content, true)
	if !ok {
		segments, _ = splitMetadataSegments(content, false)
	}
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
		fields = append(fields, MetadataField{Key: key, Value: unescapeMetadataValue(strings.TrimSpace(value)), Raw: segment})
	}
	return fields, true
}

// splitMetadataSegments splits on every `;` that is not escaped by a
// backslash and, when quotes is set, not inside a double-quoted run. It
// reports false when quotes is set and a quote is left open.
func splitMetadataSegments(content string, quotes bool) ([]string, bool) {
	if !strings.Contains(content, `\;`) && (!quotes || !strings.Contains(content, `"`)) {
		return strings.Split(content, ";"), true
	}
	var segments []string
	start := 0
	inQuote := false
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '\\':
			if i+1 < len(content) && (content[i+1] == ';' || (quotes && content[i+1] == '"')) {
				i++
			}
		case '"':
			if quotes {
				inQuote = !inQuote
			}
		case ';':
			if !inQuote {
				segments = append(segments, content[start:i])
				start = i + 1
			}
		}
	}
	if inQuote {
		return nil, false
	}
	return append(segments, content[start:]), true
}

// unescapeMetadataValue strips one pair of surrounding double quotes and
// turns `\;` and, inside quotes, `\"` back into the literal characters.
func unescapeMetadataValue(value string) string {
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		inner := value[1 : len(value)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		return strings.ReplaceAll(inner, `\;`, ";")
	}
	return strings.ReplaceAll(value, `\;`, ";")
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
	} else if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		ref.Device, ref.Agent = parts[0], parts[1]
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
