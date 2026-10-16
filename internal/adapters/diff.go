package adapters

import "strings"

// DiffLine is one line of a preview diff.
type DiffLine struct {
	Kind byte // ' ' unchanged, '-' removed, '+' added
	Text string
}

// maxDiffLines bounds the quadratic diff so a huge configuration file cannot
// exhaust memory. Above the bound the preview falls back to a full replacement.
const maxDiffLines = 800

// LineDiff returns a line-level diff of two configuration revisions.
func LineDiff(before, after string) []DiffLine {
	oldLines := splitDiffLines(before)
	newLines := splitDiffLines(after)
	if len(oldLines) > maxDiffLines || len(newLines) > maxDiffLines {
		out := make([]DiffLine, 0, len(oldLines)+len(newLines))
		for _, line := range oldLines {
			out = append(out, DiffLine{Kind: '-', Text: line})
		}
		for _, line := range newLines {
			out = append(out, DiffLine{Kind: '+', Text: line})
		}
		return out
	}

	table := make([][]int, len(oldLines)+1)
	for index := range table {
		table[index] = make([]int, len(newLines)+1)
	}
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}

	var out []DiffLine
	for i, j := 0, 0; i < len(oldLines) || j < len(newLines); {
		switch {
		case i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j]:
			out = append(out, DiffLine{Kind: ' ', Text: oldLines[i]})
			i++
			j++
		case j < len(newLines) && (i == len(oldLines) || table[i][j+1] >= table[i+1][j]):
			out = append(out, DiffLine{Kind: '+', Text: newLines[j]})
			j++
		default:
			out = append(out, DiffLine{Kind: '-', Text: oldLines[i]})
			i++
		}
	}
	return out
}

func splitDiffLines(text string) []string {
	if text == "" {
		return nil
	}
	trimmed := strings.TrimSuffix(text, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
