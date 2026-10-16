package adapters

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// jsonKind is the node type of the lossless JSON reader.
type jsonKind uint8

const (
	jsonObject  jsonKind = '{'
	jsonArray   jsonKind = '['
	jsonString  jsonKind = 's'
	jsonLiteral jsonKind = 'l'
)

// jsonNode records the source span and structure of one JSON value. Spans let
// the merge rewrite only the containers it owns while leaving every other byte,
// including comments, unchanged.
type jsonNode struct {
	kind     jsonKind
	start    int
	end      int
	members  []jsonMember
	elements []*jsonNode
	// text is the decoded string value for jsonString and the literal token
	// for jsonLiteral.
	text string
}

type jsonMember struct {
	key      string
	keyStart int
	keyEnd   int
	value    *jsonNode
}

var jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type jsonScanner struct {
	src  string
	pos  int
	docs bool // config documents allow // and /* */ comments
}

// parseJSONDocument parses exactly one JSON value. Comments are accepted when
// config is true, which covers both JSON and JSONC configuration files.
func parseJSONDocument(src string, config bool) (*jsonNode, error) {
	scanner := &jsonScanner{src: src, docs: config}
	scanner.skipTrivia()
	node, err := scanner.parseValue()
	if err != nil {
		return nil, err
	}
	scanner.skipTrivia()
	if scanner.pos != len(src) {
		return nil, fmt.Errorf("unexpected trailing content at offset %d", scanner.pos)
	}
	return node, nil
}

func (s *jsonScanner) skipTrivia() {
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
			continue
		case '/':
			if !s.docs || s.pos+1 >= len(s.src) {
				return
			}
			switch s.src[s.pos+1] {
			case '/':
				s.pos += 2
				for s.pos < len(s.src) && s.src[s.pos] != '\n' {
					s.pos++
				}
				continue
			case '*':
				end := strings.Index(s.src[s.pos+2:], "*/")
				if end < 0 {
					s.pos = len(s.src)
					return
				}
				s.pos += 2 + end + 2
				continue
			}
		}
		return
	}
}

func (s *jsonScanner) parseValue() (*jsonNode, error) {
	if s.pos >= len(s.src) {
		return nil, fmt.Errorf("unexpected end of JSON input")
	}
	switch s.src[s.pos] {
	case '{':
		return s.parseObject()
	case '[':
		return s.parseArray()
	case '"':
		return s.parseString()
	default:
		return s.parseLiteral()
	}
}

func (s *jsonScanner) parseObject() (*jsonNode, error) {
	node := &jsonNode{kind: jsonObject, start: s.pos}
	s.pos++ // {
	s.skipTrivia()
	if s.pos < len(s.src) && s.src[s.pos] == '}' {
		s.pos++
		node.end = s.pos
		return node, nil
	}
	for {
		s.skipTrivia()
		if s.pos >= len(s.src) || s.src[s.pos] != '"' {
			return nil, fmt.Errorf("expected an object key at offset %d", s.pos)
		}
		keyStart := s.pos
		key, err := s.parseString()
		if err != nil {
			return nil, err
		}
		keyEnd := s.pos
		s.skipTrivia()
		if s.pos >= len(s.src) || s.src[s.pos] != ':' {
			return nil, fmt.Errorf("expected ':' at offset %d", s.pos)
		}
		s.pos++
		s.skipTrivia()
		value, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		node.members = append(node.members, jsonMember{key: key.text, keyStart: keyStart, keyEnd: keyEnd, value: value})
		s.skipTrivia()
		if s.pos >= len(s.src) {
			return nil, fmt.Errorf("unterminated object")
		}
		switch s.src[s.pos] {
		case ',':
			s.pos++
		case '}':
			s.pos++
			node.end = s.pos
			return node, nil
		default:
			return nil, fmt.Errorf("expected ',' or '}' at offset %d", s.pos)
		}
	}
}

func (s *jsonScanner) parseArray() (*jsonNode, error) {
	node := &jsonNode{kind: jsonArray, start: s.pos}
	s.pos++ // [
	s.skipTrivia()
	if s.pos < len(s.src) && s.src[s.pos] == ']' {
		s.pos++
		node.end = s.pos
		return node, nil
	}
	for {
		s.skipTrivia()
		element, err := s.parseValue()
		if err != nil {
			return nil, err
		}
		node.elements = append(node.elements, element)
		s.skipTrivia()
		if s.pos >= len(s.src) {
			return nil, fmt.Errorf("unterminated array")
		}
		switch s.src[s.pos] {
		case ',':
			s.pos++
		case ']':
			s.pos++
			node.end = s.pos
			return node, nil
		default:
			return nil, fmt.Errorf("expected ',' or ']' at offset %d", s.pos)
		}
	}
}

func (s *jsonScanner) parseString() (*jsonNode, error) {
	start := s.pos
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case '\\':
			s.pos += 2
			continue
		case '"':
			s.pos++
			raw := s.src[start:s.pos]
			var decoded string
			if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
				return nil, fmt.Errorf("invalid JSON string at offset %d", start)
			}
			return &jsonNode{kind: jsonString, start: start, end: s.pos, text: decoded}, nil
		default:
			s.pos++
		}
	}
	return nil, fmt.Errorf("unterminated JSON string at offset %d", start)
}

func (s *jsonScanner) parseLiteral() (*jsonNode, error) {
	start := s.pos
	for s.pos < len(s.src) {
		switch s.src[s.pos] {
		case ',', '}', ']', ' ', '\t', '\r', '\n':
			goto done
		default:
			s.pos++
		}
	}
done:
	raw := s.src[start:s.pos]
	switch raw {
	case "true", "false", "null":
	default:
		if !jsonNumberPattern.MatchString(raw) {
			return nil, fmt.Errorf("invalid JSON value %q at offset %d", raw, start)
		}
	}
	return &jsonNode{kind: jsonLiteral, start: start, end: s.pos, text: raw}, nil
}

// member returns the member with the given key.
func (n *jsonNode) member(key string) (jsonMember, bool) {
	if n == nil || n.kind != jsonObject {
		return jsonMember{}, false
	}
	for _, member := range n.members {
		if member.key == key {
			return member, true
		}
	}
	return jsonMember{}, false
}

// compactJSON renders the semantic value of a node without formatting. Two
// config entries are the same managed entry when their compact forms match.
func compactJSON(n *jsonNode) string {
	var builder strings.Builder
	writeCompactJSON(&builder, n)
	return builder.String()
}

func writeCompactJSON(builder *strings.Builder, n *jsonNode) {
	if n == nil {
		builder.WriteString("null")
		return
	}
	switch n.kind {
	case jsonObject:
		builder.WriteByte('{')
		for index, member := range n.members {
			if index > 0 {
				builder.WriteByte(',')
			}
			key, _ := json.Marshal(member.key)
			builder.Write(key)
			builder.WriteByte(':')
			writeCompactJSON(builder, member.value)
		}
		builder.WriteByte('}')
	case jsonArray:
		builder.WriteByte('[')
		for index, element := range n.elements {
			if index > 0 {
				builder.WriteByte(',')
			}
			writeCompactJSON(builder, element)
		}
		builder.WriteByte(']')
	case jsonString:
		encoded, _ := json.Marshal(n.text)
		builder.Write(encoded)
	default:
		builder.WriteString(n.text)
	}
}

// managedCommand reports whether a value contains a "command" string that
// invokes memctl. It is the stable identity of a memctl-managed JSON hook.
func managedCommand(n *jsonNode) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.kind {
	case jsonObject:
		for _, member := range n.members {
			if member.key == "command" && member.value.kind == jsonString && strings.Contains(member.value.text, "memctl") {
				return member.value.text, true
			}
			if command, ok := managedCommand(member.value); ok {
				return command, true
			}
		}
	case jsonArray:
		for _, element := range n.elements {
			if command, ok := managedCommand(element); ok {
				return command, true
			}
		}
	}
	return "", false
}

// linePrefix returns the whitespace between the start of the line containing at
// and at. ok is false when the prefix is not pure whitespace, which means the
// position is not anchored on its own line.
func linePrefix(src string, at int) (prefix string, ok bool) {
	start := strings.LastIndexByte(src[:at], '\n') + 1
	prefix = src[start:at]
	for index := 0; index < len(prefix); index++ {
		if prefix[index] != ' ' && prefix[index] != '\t' {
			return "", false
		}
	}
	return prefix, true
}

// indentUnit picks a one-level indent that matches the container's own indent
// style.
func indentUnit(closeIndent string) string {
	if strings.Contains(closeIndent, "\t") {
		return "\t"
	}
	return "  "
}

// containerIndent picks the indent for a newly inserted member or element: the
// indent of the first existing membership when it is anchored on its own line,
// otherwise one level deeper than the closing delimiter.
func containerIndent(src string, firstPos int, hasMembers bool, closeIndent string) string {
	if hasMembers {
		if prefix, ok := linePrefix(src, firstPos); ok {
			return prefix
		}
	}
	return closeIndent + indentUnit(closeIndent)
}

// gapHasComment reports whether the span between two tokens contains a
// comment. Merging across a comment would orphan it, so such a container is
// handled with a manual patch instead.
func gapHasComment(gap string) bool {
	return strings.Contains(gap, "//") || strings.Contains(gap, "/*")
}
