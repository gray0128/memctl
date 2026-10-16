package adapters

import (
	"encoding/json"
	"strings"
)

// mergeJSONContent merges the template event map into a JSON or JSONC document.
// Only the managed event arrays are rewritten; all other bytes, including
// comments and user entries, are preserved.
func mergeJSONContent(src []byte, spec ArtifactSpec, template []byte) ([]byte, bool, error) {
	templateDoc, err := parseJSONDocument(string(template), true)
	if err != nil {
		return nil, false, errInvalidTemplate("json", err)
	}
	templateContainer, ok := navigate(templateDoc, spec.ContainerPath)
	if !ok || templateContainer.kind != jsonObject {
		return nil, false, errInvalidTemplate("json", nil)
	}

	doc, err := parseJSONDocument(string(src), true)
	if err != nil {
		return nil, false, errManualMerge("the file is not valid JSON or JSONC: %v", err)
	}
	if doc.kind != jsonObject {
		return nil, false, errManualMerge("the top level of the file is not a JSON object")
	}

	container, ok := navigate(doc, spec.ContainerPath)
	if !ok {
		if len(spec.ContainerPath) != 1 {
			return nil, false, errManualMerge("cannot locate the managed container")
		}
		raw := string(template)[templateContainer.start:templateContainer.end]
		updated, insertErr := insertJSONMember(string(src), doc, spec.ContainerPath[0], raw)
		if insertErr != nil {
			return nil, false, insertErr
		}
		return []byte(updated), true, nil
	}
	if container.kind != jsonObject {
		return nil, false, errManualMerge("the managed container is not a JSON object")
	}

	templateSource := string(template)
	current := string(src)
	for _, templateMember := range templateContainer.members {
		if templateMember.value.kind != jsonArray {
			return nil, false, errInvalidTemplate("json", nil)
		}
		templateElements := templateMember.value.elements
		if len(templateElements) == 0 {
			continue
		}
		templateRaw := make([]string, 0, len(templateElements))
		for _, element := range templateElements {
			templateRaw = append(templateRaw, templateSource[element.start:element.end])
		}

		existing, found := container.member(templateMember.key)
		if !found {
			raw := templateSource[templateMember.value.start:templateMember.value.end]
			updated, insertErr := insertJSONMember(current, container, templateMember.key, raw)
			if insertErr != nil {
				return nil, false, insertErr
			}
			current = updated
			doc, err = parseJSONDocument(current, true)
			if err != nil {
				return nil, false, errManualMerge("the merged document is unreadable: %v", err)
			}
			container, _ = navigate(doc, spec.ContainerPath)
			continue
		}
		if existing.value.kind != jsonArray {
			return nil, false, errManualMerge("event %q is not a JSON array", templateMember.key)
		}

		var kept []string
		var managedCompacts []string
		for _, element := range existing.value.elements {
			if _, managed := managedCommand(element); managed {
				managedCompacts = append(managedCompacts, compactJSON(element))
				continue
			}
			kept = append(kept, current[element.start:element.end])
		}

		if sameSequence(managedCompacts, compactEach(templateElements)) {
			continue
		}

		var updated string
		if len(managedCompacts) == 0 {
			updated, err = insertJSONElements(current, existing.value, templateRaw)
		} else {
			updated, err = rebuildJSONArray(current, existing.value, append(kept, templateRaw...))
		}
		if err != nil {
			return nil, false, err
		}
		current = updated
		doc, err = parseJSONDocument(current, true)
		if err != nil {
			return nil, false, errManualMerge("the merged document is unreadable: %v", err)
		}
		container, _ = navigate(doc, spec.ContainerPath)
	}

	if current == string(src) {
		return src, false, nil
	}
	return []byte(current), true, nil
}

// navigate walks a JSON object path. A missing member returns ok=false.
func navigate(node *jsonNode, path []string) (*jsonNode, bool) {
	current := node
	for _, key := range path {
		member, found := current.member(key)
		if !found {
			return nil, false
		}
		current = member.value
	}
	return current, true
}

func compactEach(nodes []*jsonNode) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, compactJSON(node))
	}
	return out
}

func sameSequence(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// insertJSONMember adds one member before the container's closing brace.
func insertJSONMember(src string, obj *jsonNode, key, valueText string) (string, error) {
	closeIdx := obj.end - 1
	lastEnd := obj.start + 1
	if len(obj.members) > 0 {
		lastEnd = obj.members[len(obj.members)-1].value.end
	}
	gap := src[lastEnd:closeIdx]
	if gapHasComment(gap) {
		return "", errManualMerge("a comment sits at the insertion point of %q", key)
	}
	closeIndent, _ := linePrefix(src, closeIdx)
	firstPos := obj.start + 1
	if len(obj.members) > 0 {
		firstPos = obj.members[0].keyStart
	}
	memberIndent := containerIndent(src, firstPos, len(obj.members) > 0, closeIndent)

	encodedKey, _ := json.Marshal(key)
	var builder strings.Builder
	if len(obj.members) > 0 || strings.Contains(gap, ",") {
		builder.WriteByte(',')
	}
	builder.WriteString("\n")
	builder.WriteString(memberIndent)
	builder.Write(encodedKey)
	builder.WriteString(": ")
	builder.WriteString(valueText)
	builder.WriteString("\n")
	builder.WriteString(closeIndent)
	return src[:lastEnd] + builder.String() + src[closeIdx:], nil
}

// insertJSONElements appends elements before the array's closing bracket while
// leaving every existing element byte-for-byte in place.
func insertJSONElements(src string, arr *jsonNode, items []string) (string, error) {
	closeIdx := arr.end - 1
	lastEnd := arr.start + 1
	if len(arr.elements) > 0 {
		lastEnd = arr.elements[len(arr.elements)-1].end
	}
	gap := src[lastEnd:closeIdx]
	if gapHasComment(gap) {
		return "", errManualMerge("a comment sits at the insertion point of the event array")
	}
	closeIndent, _ := linePrefix(src, closeIdx)
	firstPos := arr.start + 1
	if len(arr.elements) > 0 {
		firstPos = arr.elements[0].start
	}
	elementIndent := containerIndent(src, firstPos, len(arr.elements) > 0, closeIndent)
	hasTrailingComma := strings.Contains(gap, ",")

	var builder strings.Builder
	for index, item := range items {
		if index == 0 {
			if len(arr.elements) > 0 || hasTrailingComma {
				builder.WriteByte(',')
			}
		} else {
			builder.WriteByte(',')
		}
		builder.WriteString("\n")
		builder.WriteString(elementIndent)
		builder.WriteString(item)
	}
	builder.WriteString("\n")
	builder.WriteString(closeIndent)
	return src[:lastEnd] + builder.String() + src[closeIdx:], nil
}

// rebuildJSONArray rewrites a whole array from raw element texts. It is used
// only when managed entries must be replaced, and refuses to run when a
// comment sits between elements.
func rebuildJSONArray(src string, arr *jsonNode, items []string) (string, error) {
	previousEnd := arr.start + 1
	for _, element := range arr.elements {
		if gapHasComment(src[previousEnd:element.start]) {
			return "", errManualMerge("a comment sits between event entries")
		}
		previousEnd = element.end
	}
	closeIdx := arr.end - 1
	if gapHasComment(src[previousEnd:closeIdx]) {
		return "", errManualMerge("a comment sits at the end of the event array")
	}
	closeIndent, _ := linePrefix(src, closeIdx)
	firstPos := arr.start + 1
	if len(arr.elements) > 0 {
		firstPos = arr.elements[0].start
	}
	elementIndent := containerIndent(src, firstPos, len(arr.elements) > 0, closeIndent)

	var builder strings.Builder
	builder.WriteByte('[')
	for index, item := range items {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString("\n")
		builder.WriteString(elementIndent)
		builder.WriteString(item)
	}
	builder.WriteString("\n")
	builder.WriteString(closeIndent)
	builder.WriteByte(']')
	return src[:arr.start] + builder.String() + src[arr.end:], nil
}
