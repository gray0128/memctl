package adapters

import (
	"errors"
	"regexp"
	"strings"
)

var (
	yamlHooksKeyPattern   = regexp.MustCompile(`^hooks\s*:`)
	yamlHooksValuePattern = regexp.MustCompile(`^hooks\s*:\s*(.*)$`)
	yamlItemIDPattern     = regexp.MustCompile(`^\s*-\s*id\s*:\s*(.+?)\s*$`)
	yamlPlainIDPattern    = regexp.MustCompile(`^\s*id\s*:\s*(.+?)\s*$`)
)

type yamlItem struct {
	id     string
	indent string
	lines  []string
}

// mergeYAMLContent merges the template's hooks sequence into the target while
// leaving every line outside the hooks block, and every non-managed item,
// unchanged.
func mergeYAMLContent(src, template []byte) ([]byte, bool, error) {
	templateItems, err := yamlTemplateItems(string(template))
	if err != nil || len(templateItems) == 0 {
		return nil, false, errInvalidTemplate("yaml", err)
	}
	templateIDs := make(map[string]bool, len(templateItems))
	for _, item := range templateItems {
		templateIDs[item.id] = true
	}
	templateIndent := templateItems[0].indent

	lines := splitLinesKeep(string(src))
	hooksIdx := -1
	for index := range lines {
		if yamlHooksKeyPattern.MatchString(lineBody(lines[index])) {
			hooksIdx = index
			break
		}
	}
	if hooksIdx < 0 {
		return []byte(appendYAMLBlock(string(src), string(template))), true, nil
	}

	body := lineBody(lines[hooksIdx])
	inlineValue := ""
	if match := yamlHooksValuePattern.FindStringSubmatch(body); match != nil {
		inlineValue = strings.TrimSpace(match[1])
	}
	if inlineValue != "" && !strings.HasPrefix(inlineValue, "#") {
		return nil, false, errManualMerge("the hooks value is not a block sequence")
	}

	blockEnd := yamlBlockEnd(lines, hooksIdx)
	items, preamble, baseIndent, ok := splitYAMLItems(lines[hooksIdx+1 : blockEnd])
	if !ok || len(items) == 0 {
		return nil, false, errManualMerge("cannot read the hooks sequence")
	}

	var managedRaw [][]string
	kept := append([]string{}, preamble...)
	for _, item := range items {
		if item.id != "" && templateIDs[item.id] {
			managedRaw = append(managedRaw, item.lines)
			continue
		}
		kept = append(kept, item.lines...)
	}

	wanted := make([][]string, 0, len(templateItems))
	for _, item := range templateItems {
		wanted = append(wanted, reindentLines(item.lines, templateIndent, baseIndent))
	}
	if sameLineSequences(managedRaw, wanted) {
		return src, false, nil
	}

	out := append([]string{}, lines[:hooksIdx+1]...)
	out = append(out, kept...)
	for _, item := range wanted {
		out = append(out, item...)
	}
	out = append(out, lines[blockEnd:]...)
	return []byte(strings.Join(out, "")), true, nil
}

// yamlTemplateItems extracts the managed items of the embedded template.
func yamlTemplateItems(template string) ([]yamlItem, error) {
	lines := splitLinesKeep(template)
	hooksIdx := -1
	for index := range lines {
		if yamlHooksKeyPattern.MatchString(lineBody(lines[index])) {
			hooksIdx = index
			break
		}
	}
	if hooksIdx < 0 {
		return nil, errors.New("template has no hooks key")
	}
	items, _, _, ok := splitYAMLItems(lines[hooksIdx+1 : yamlBlockEnd(lines, hooksIdx)])
	if !ok {
		return nil, errors.New("template hooks block is not a sequence")
	}
	return items, nil
}

// yamlBlockEnd returns the first line after the hooks block: the next top-level
// key, or the end of the file.
func yamlBlockEnd(lines []string, hooksIdx int) int {
	for index := hooksIdx + 1; index < len(lines); index++ {
		body := lineBody(lines[index])
		trimmed := strings.TrimSpace(body)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if body[0] == ' ' || body[0] == '\t' || strings.HasPrefix(body, "-") {
			continue
		}
		return index
	}
	return len(lines)
}

// splitYAMLItems splits a sequence block into items. Preamble lines that belong
// to no item are returned separately.
func splitYAMLItems(block []string) (items []yamlItem, preamble []string, baseIndent string, ok bool) {
	first := -1
	for index, line := range block {
		if isYAMLItem(lineBody(line)) {
			first = index
			break
		}
	}
	if first < 0 {
		return nil, nil, "", false
	}
	baseIndent = leadingWhitespace(lineBody(block[first]))
	preamble = append([]string{}, block[:first]...)

	var current *yamlItem
	flush := func() {
		if current != nil {
			items = append(items, *current)
			current = nil
		}
	}
	for index := first; index < len(block); index++ {
		body := lineBody(block[index])
		if isYAMLItem(body) && leadingWhitespace(body) == baseIndent {
			flush()
			current = &yamlItem{id: yamlItemID(body), indent: baseIndent, lines: []string{block[index]}}
			continue
		}
		if current == nil {
			current = &yamlItem{indent: baseIndent, lines: []string{block[index]}}
			continue
		}
		if current.id == "" {
			if match := yamlPlainIDPattern.FindStringSubmatch(body); match != nil {
				current.id = unquoteYAMLScalar(match[1])
			}
		}
		current.lines = append(current.lines, block[index])
	}
	flush()
	return items, preamble, baseIndent, true
}

func isYAMLItem(body string) bool {
	trimmed := strings.TrimLeft(body, " \t")
	return trimmed == "-" || strings.HasPrefix(trimmed, "- ")
}

func yamlItemID(body string) string {
	if match := yamlItemIDPattern.FindStringSubmatch(body); match != nil {
		return unquoteYAMLScalar(match[1])
	}
	return ""
}

func unquoteYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func leadingWhitespace(body string) string {
	index := 0
	for index < len(body) && (body[index] == ' ' || body[index] == '\t') {
		index++
	}
	return body[:index]
}

// appendYAMLBlock appends the template sequence after the existing content.
func appendYAMLBlock(src, template string) string {
	if template != "" && !strings.HasSuffix(template, "\n") {
		template += "\n"
	}
	if src == "" {
		return template
	}
	out := src
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if !strings.HasSuffix(out, "\n\n") {
		out += "\n"
	}
	return out + template
}

// reindentLines replaces the leading whitespace of each line so template items
// adopt the target block's indentation.
func reindentLines(lines []string, from, to string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		body := lineBody(line)
		ending := line[len(body):]
		if body != "" && strings.HasPrefix(body, from) {
			body = to + body[len(from):]
		}
		out = append(out, body+ending)
	}
	return out
}

func sameLineSequences(a, b [][]string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if len(a[index]) != len(b[index]) {
			return false
		}
		for line := range a[index] {
			if a[index][line] != b[index][line] {
				return false
			}
		}
	}
	return true
}

// splitLinesKeep splits text into lines that retain their line endings. The
// final empty element produced by a trailing newline is kept so joining the
// slice reproduces the input exactly.
func splitLinesKeep(src string) []string {
	if src == "" {
		return nil
	}
	return strings.SplitAfter(src, "\n")
}

// lineBody returns the line without its trailing newline.
func lineBody(line string) string {
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
}
