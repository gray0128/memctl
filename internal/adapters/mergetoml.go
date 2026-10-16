package adapters

import (
	"errors"
	"regexp"
	"strings"
)

var (
	tomlTablePattern = regexp.MustCompile(`^\s*\[\s*([A-Za-z0-9_.\-"]+)\s*\]\s*(#.*)?$`)
	tomlKeyPattern   = regexp.MustCompile(`^\s*([A-Za-z0-9_.\-]+)\s*=\s*(.*)$`)
)

// mergeTOMLContent ensures a managed table key exists with the template value
// while preserving every other line and comment.
func mergeTOMLContent(src, template []byte) ([]byte, bool, error) {
	table, key, value, err := parseTOMLTemplate(string(template))
	if err != nil {
		return nil, false, errInvalidTemplate("toml", err)
	}

	lines := splitLinesKeep(string(src))
	headerIdx := -1
	for index := range lines {
		if name, ok := tomlTableName(lineBody(lines[index])); ok && name == table {
			headerIdx = index
			break
		}
	}
	if headerIdx < 0 {
		return []byte(appendTOMLBlock(string(src), string(template))), true, nil
	}

	end := tomlTableEnd(lines, headerIdx)
	for index := headerIdx + 1; index < end; index++ {
		body := lineBody(lines[index])
		match := tomlKeyPattern.FindStringSubmatch(body)
		if match == nil || match[1] != key {
			continue
		}
		if tomlValue(match[2]) == value {
			return src, false, nil
		}
		return nil, false, errManualMerge("the %s.%s key is already set to %s", table, key, tomlValue(match[2]))
	}

	newLine := key + " = " + value + "\n"
	out := append([]string{}, lines[:headerIdx+1]...)
	out = append(out, newLine)
	out = append(out, lines[headerIdx+1:]...)
	return []byte(strings.Join(out, "")), true, nil
}

// parseTOMLTemplate reads the single managed table and key from the template.
func parseTOMLTemplate(template string) (table, key, value string, err error) {
	lines := splitLinesKeep(template)
	tableIdx := -1
	for index := range lines {
		if name, ok := tomlTableName(lineBody(lines[index])); ok {
			table, tableIdx = name, index
			break
		}
	}
	if tableIdx < 0 {
		return "", "", "", errors.New("template has no table header")
	}
	for index := tableIdx + 1; index < len(lines); index++ {
		body := lineBody(lines[index])
		if strings.HasPrefix(strings.TrimSpace(body), "[") {
			break
		}
		match := tomlKeyPattern.FindStringSubmatch(body)
		if match == nil {
			continue
		}
		return table, match[1], tomlValue(match[2]), nil
	}
	return "", "", "", errors.New("template table has no key")
}

func tomlTableName(body string) (string, bool) {
	match := tomlTablePattern.FindStringSubmatch(body)
	if match == nil {
		return "", false
	}
	return unquoteYAMLScalar(match[1]), true
}

// tomlTableEnd returns the first line of the next table, or the end of file.
func tomlTableEnd(lines []string, headerIdx int) int {
	for index := headerIdx + 1; index < len(lines); index++ {
		if strings.HasPrefix(strings.TrimSpace(lineBody(lines[index])), "[") {
			return index
		}
	}
	return len(lines)
}

// tomlValue strips a trailing comment from a value.
func tomlValue(raw string) string {
	inSingle, inDouble := false, false
	for index := 0; index < len(raw); index++ {
		switch raw[index] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimSpace(raw[:index])
			}
		}
	}
	return strings.TrimSpace(raw)
}

func appendTOMLBlock(src, template string) string {
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
