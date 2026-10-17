package dream

import (
	"strconv"
	"strings"
)

// maxLCSCells bounds the dynamic-programming table of one file diff. Above it
// the diff falls back to a whole-file replacement, which is still a valid
// unified diff and keeps memory bounded. The ordered change list, not the
// rendered patch, is the authoritative change set.
const maxLCSCells = 1 << 20

// contextLines is the number of unchanged lines kept around each hunk.
const contextLines = 3

type diffOp struct {
	kind byte // ' ' context, '-' removal, '+' addition
	text string
}

// splitDiffLines splits text into physical lines and reports whether it ended
// with a newline. An empty text has no lines.
func splitDiffLines(text string) ([]string, bool) {
	if text == "" {
		return nil, true
	}
	trailing := strings.HasSuffix(text, "\n")
	body := text
	if trailing {
		body = body[:len(body)-1]
	}
	return strings.Split(body, "\n"), trailing
}

// unifiedDiff renders one file's change as a standard unified diff. It returns
// an empty string when the two sides are byte-identical.
func unifiedDiff(oldLabel, newLabel, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	oldLines, oldTrailing := splitDiffLines(oldText)
	newLines, newTrailing := splitDiffLines(newText)
	ops := diffLines(oldLines, newLines)
	if len(ops) == 0 {
		return ""
	}

	var changed []int
	for index, op := range ops {
		if op.kind != ' ' {
			changed = append(changed, index)
		}
	}
	if len(changed) == 0 {
		return ""
	}

	oldAt := make([]int, len(ops))
	newAt := make([]int, len(ops))
	oldNo, newNo := 1, 1
	lastOld, lastNew := -1, -1
	for index, op := range ops {
		oldAt[index] = oldNo
		newAt[index] = newNo
		if op.kind != '+' {
			oldNo++
			lastOld = index
		}
		if op.kind != '-' {
			newNo++
			lastNew = index
		}
	}

	var out strings.Builder
	out.WriteString("--- ")
	out.WriteString(oldLabel)
	out.WriteByte('\n')
	out.WriteString("+++ ")
	out.WriteString(newLabel)
	out.WriteByte('\n')

	groups := groupChanged(changed)
	for _, group := range groups {
		from := group[0] - contextLines
		if from < 0 {
			from = 0
		}
		to := group[1] + contextLines
		if to > len(ops)-1 {
			to = len(ops) - 1
		}
		oldCount, newCount := 0, 0
		for index := from; index <= to; index++ {
			if ops[index].kind != '+' {
				oldCount++
			}
			if ops[index].kind != '-' {
				newCount++
			}
		}
		oldStart, newStart := oldAt[from], newAt[from]
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		out.WriteString("@@ -")
		out.WriteString(strconv.Itoa(oldStart))
		out.WriteByte(',')
		out.WriteString(strconv.Itoa(oldCount))
		out.WriteString(" +")
		out.WriteString(strconv.Itoa(newStart))
		out.WriteByte(',')
		out.WriteString(strconv.Itoa(newCount))
		out.WriteString(" @@\n")
		for index := from; index <= to; index++ {
			out.WriteByte(ops[index].kind)
			out.WriteString(ops[index].text)
			out.WriteByte('\n')
			if !oldTrailing && ops[index].kind != '+' && index == lastOld {
				out.WriteString("\\ No newline at end of file\n")
			}
			if !newTrailing && ops[index].kind != '-' && index == lastNew {
				out.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return out.String()
}

// groupChanged groups changed op indices that are close enough to share a
// single hunk.
func groupChanged(changed []int) [][2]int {
	groups := make([][2]int, 0, len(changed))
	start, previous := changed[0], changed[0]
	for _, index := range changed[1:] {
		if index-previous > 2*contextLines {
			groups = append(groups, [2]int{start, previous})
			start = index
		}
		previous = index
	}
	return append(groups, [2]int{start, previous})
}

// diffLines computes an ordered context/removal/addition script. It uses an
// LCS table within a bounded budget and falls back to a whole-file replacement
// above it.
func diffLines(oldLines, newLines []string) []diffOp {
	n, m := len(oldLines), len(newLines)
	if n*m > maxLCSCells {
		ops := make([]diffOp, 0, n+m)
		for _, line := range oldLines {
			ops = append(ops, diffOp{'-', line})
		}
		for _, line := range newLines {
			ops = append(ops, diffOp{'+', line})
		}
		return ops
	}
	table := make([][]int32, n+1)
	for i := range table {
		table[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	ops := make([]diffOp, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case oldLines[i] == newLines[j]:
			ops = append(ops, diffOp{' ', oldLines[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, diffOp{'-', oldLines[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', newLines[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', oldLines[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', newLines[j]})
	}
	return ops
}
