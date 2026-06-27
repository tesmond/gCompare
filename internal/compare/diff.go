package compare

import (
	"strings"
	"unicode"
)

const maxInlineDiffCells = 40000

type diffOp struct {
	kind  string
	left  int
	right int
}

func buildRows(left []textLine, right []textLine, leftDirty bool, rightDirty bool, leftPath string, rightPath string, warning string) FileComparisonResult {
	ops := patienceDiff(left, right)
	rows := pairRows(ops, left, right)
	return FileComparisonResult{
		LeftPath:   leftPath,
		RightPath:  rightPath,
		Rows:       rows,
		LeftDirty:  leftDirty,
		RightDirty: rightDirty,
		Warning:    warning,
	}
}

func patienceDiff(left []textLine, right []textLine) []diffOp {
	return patienceDiffRange(left, right, 0, len(left), 0, len(right))
}

func patienceDiffRange(left []textLine, right []textLine, leftStart int, leftEnd int, rightStart int, rightEnd int) []diffOp {
	ops := []diffOp{}
	for leftStart < leftEnd && rightStart < rightEnd && left[leftStart].Text == right[rightStart].Text {
		ops = append(ops, diffOp{kind: "equal", left: leftStart, right: rightStart})
		leftStart++
		rightStart++
	}

	suffix := []diffOp{}
	for leftStart < leftEnd && rightStart < rightEnd && left[leftEnd-1].Text == right[rightEnd-1].Text {
		leftEnd--
		rightEnd--
		suffix = append(suffix, diffOp{kind: "equal", left: leftEnd, right: rightEnd})
	}

	switch {
	case leftStart == leftEnd:
		for rightIndex := rightStart; rightIndex < rightEnd; rightIndex++ {
			ops = append(ops, diffOp{kind: "insert", left: leftStart, right: rightIndex})
		}
	case rightStart == rightEnd:
		for leftIndex := leftStart; leftIndex < leftEnd; leftIndex++ {
			ops = append(ops, diffOp{kind: "delete", left: leftIndex, right: rightStart})
		}
	default:
		anchors := patienceAnchors(left, right, leftStart, leftEnd, rightStart, rightEnd)
		if len(anchors) == 0 {
			ops = append(ops, lcsDiffRange(left, right, leftStart, leftEnd, rightStart, rightEnd)...)
		} else {
			prevLeft := leftStart
			prevRight := rightStart
			for _, anchor := range anchors {
				ops = append(ops, patienceDiffRange(left, right, prevLeft, anchor.left, prevRight, anchor.right)...)
				ops = append(ops, diffOp{kind: "equal", left: anchor.left, right: anchor.right})
				prevLeft = anchor.left + 1
				prevRight = anchor.right + 1
			}
			ops = append(ops, patienceDiffRange(left, right, prevLeft, leftEnd, prevRight, rightEnd)...)
		}
	}

	for i := len(suffix) - 1; i >= 0; i-- {
		ops = append(ops, suffix[i])
	}
	return ops
}

type patienceAnchor struct {
	left  int
	right int
}

type lineOccurrence struct {
	count int
	index int
}

func patienceAnchors(left []textLine, right []textLine, leftStart int, leftEnd int, rightStart int, rightEnd int) []patienceAnchor {
	leftOccurrences := map[string]lineOccurrence{}
	rightOccurrences := map[string]lineOccurrence{}

	for i := leftStart; i < leftEnd; i++ {
		key := left[i].Text
		occurrence := leftOccurrences[key]
		occurrence.count++
		occurrence.index = i
		leftOccurrences[key] = occurrence
	}
	for i := rightStart; i < rightEnd; i++ {
		key := right[i].Text
		occurrence := rightOccurrences[key]
		occurrence.count++
		occurrence.index = i
		rightOccurrences[key] = occurrence
	}

	candidates := []patienceAnchor{}
	for i := leftStart; i < leftEnd; i++ {
		key := left[i].Text
		leftOccurrence := leftOccurrences[key]
		rightOccurrence := rightOccurrences[key]
		if leftOccurrence.count == 1 && rightOccurrence.count == 1 {
			candidates = append(candidates, patienceAnchor{left: i, right: rightOccurrence.index})
		}
	}
	return longestIncreasingAnchors(candidates)
}

func longestIncreasingAnchors(candidates []patienceAnchor) []patienceAnchor {
	if len(candidates) <= 1 {
		return candidates
	}

	pileTops := []int{}
	previous := make([]int, len(candidates))
	for i := range previous {
		previous[i] = -1
	}

	for i, candidate := range candidates {
		pile := lowerBoundAnchor(candidates, pileTops, candidate.right)
		if pile > 0 {
			previous[i] = pileTops[pile-1]
		}
		if pile == len(pileTops) {
			pileTops = append(pileTops, i)
		} else {
			pileTops[pile] = i
		}
	}

	result := make([]patienceAnchor, len(pileTops))
	index := pileTops[len(pileTops)-1]
	for i := len(result) - 1; i >= 0; i-- {
		result[i] = candidates[index]
		index = previous[index]
	}
	return result
}

func lowerBoundAnchor(candidates []patienceAnchor, pileTops []int, rightIndex int) int {
	low := 0
	high := len(pileTops)
	for low < high {
		mid := low + (high-low)/2
		if candidates[pileTops[mid]].right < rightIndex {
			low = mid + 1
		} else {
			high = mid
		}
	}
	return low
}

func lcsDiffRange(left []textLine, right []textLine, leftStart int, leftEnd int, rightStart int, rightEnd int) []diffOp {
	n := leftEnd - leftStart
	m := rightEnd - rightStart
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if left[leftStart+i].Text == right[rightStart+j].Text {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	ops := []diffOp{}
	i, j := 0, 0
	for i < n && j < m {
		leftIndex := leftStart + i
		rightIndex := rightStart + j
		if left[leftIndex].Text == right[rightIndex].Text {
			ops = append(ops, diffOp{kind: "equal", left: leftIndex, right: rightIndex})
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			ops = append(ops, diffOp{kind: "delete", left: leftIndex, right: rightIndex})
			i++
		} else {
			ops = append(ops, diffOp{kind: "insert", left: leftIndex, right: rightIndex})
			j++
		}
	}
	for i < n {
		ops = append(ops, diffOp{kind: "delete", left: leftStart + i, right: rightStart + j})
		i++
	}
	for j < m {
		ops = append(ops, diffOp{kind: "insert", left: leftStart + i, right: rightStart + j})
		j++
	}
	return ops
}

func pairRows(ops []diffOp, left []textLine, right []textLine) []FileComparisonRow {
	rows := []FileComparisonRow{}
	rowIndex := 0
	for i := 0; i < len(ops); {
		op := ops[i]
		if op.kind == "equal" {
			rows = append(rows, makeFileRow(rowIndex, &op.left, &op.right, op.left, op.right, left, right, LineEqual))
			rowIndex++
			i++
			continue
		}

		deletes := []diffOp{}
		inserts := []diffOp{}
		for i < len(ops) && ops[i].kind != "equal" {
			if ops[i].kind == "delete" {
				deletes = append(deletes, ops[i])
			} else {
				inserts = append(inserts, ops[i])
			}
			i++
		}

		pairs := len(deletes)
		if len(inserts) < pairs {
			pairs = len(inserts)
		}
		for j := 0; j < pairs; j++ {
			leftIndex := deletes[j].left
			rightIndex := inserts[j].right
			rows = append(rows, makeFileRow(rowIndex, &leftIndex, &rightIndex, deletes[j].left, inserts[j].right, left, right, LineChanged))
			rowIndex++
		}
		for j := pairs; j < len(deletes); j++ {
			leftIndex := deletes[j].left
			rows = append(rows, makeFileRow(rowIndex, &leftIndex, nil, deletes[j].left, deletes[j].right, left, right, LineLeftOnly))
			rowIndex++
		}
		for j := pairs; j < len(inserts); j++ {
			rightIndex := inserts[j].right
			rows = append(rows, makeFileRow(rowIndex, nil, &rightIndex, inserts[j].left, inserts[j].right, left, right, LineRightOnly))
			rowIndex++
		}
	}
	return rows
}

func makeFileRow(rowIndex int, leftIndex *int, rightIndex *int, leftInsert int, rightInsert int, left []textLine, right []textLine, status LineComparisonStatus) FileComparisonRow {
	row := FileComparisonRow{
		RowIndex:         rowIndex,
		Status:           status,
		LeftIndex:        cloneInt(leftIndex),
		RightIndex:       cloneInt(rightIndex),
		LeftInsertIndex:  leftInsert,
		RightInsertIndex: rightInsert,
	}
	if leftIndex != nil {
		lineNumber := *leftIndex + 1
		row.LeftLineNumber = &lineNumber
		row.LeftText = left[*leftIndex].Text
	}
	if rightIndex != nil {
		lineNumber := *rightIndex + 1
		row.RightLineNumber = &lineNumber
		row.RightText = right[*rightIndex].Text
	}
	addInlineSegments(&row)
	addSemanticStates(&row)
	return row
}

func addInlineSegments(row *FileComparisonRow) {
	switch row.Status {
	case LineEqual:
		if row.LeftText != "" {
			row.LeftSegments = []LineTextSegment{lineTextSegment(row.LeftText, false)}
		}
		if row.RightText != "" {
			row.RightSegments = []LineTextSegment{lineTextSegment(row.RightText, false)}
		}
	case LineChanged:
		row.LeftSegments, row.RightSegments = inlineDiff(row.LeftText, row.RightText)
	case LineLeftOnly:
		if row.LeftText != "" {
			row.LeftSegments = []LineTextSegment{lineTextSegment(row.LeftText, true)}
		}
	case LineRightOnly:
		if row.RightText != "" {
			row.RightSegments = []LineTextSegment{lineTextSegment(row.RightText, true)}
		}
	}
}

func addSemanticStates(row *FileComparisonRow) {
	switch row.Status {
	case LineEqual:
		row.SemanticState = SemanticMatch
		row.LeftSemantic = SemanticMatch
		row.RightSemantic = SemanticMatch
	case LineChanged:
		if isUnimportantDifference(row.LeftText, row.RightText) {
			row.SemanticState = SemanticUnimportant
			row.LeftSemantic = SemanticUnimportant
			row.RightSemantic = SemanticUnimportant
		} else {
			row.SemanticState = SemanticImportant
			row.LeftSemantic = SemanticImportant
			row.RightSemantic = SemanticImportant
		}
	case LineLeftOnly:
		row.SemanticState = SemanticImportant
		row.LeftSemantic = SemanticImportant
		row.RightSemantic = SemanticOrphanGap
	case LineRightOnly:
		row.SemanticState = SemanticImportant
		row.LeftSemantic = SemanticOrphanGap
		row.RightSemantic = SemanticImportant
	default:
		row.SemanticState = SemanticImportant
		row.LeftSemantic = SemanticImportant
		row.RightSemantic = SemanticImportant
	}
}

func isUnimportantDifference(leftText string, rightText string) bool {
	if leftText == rightText {
		return false
	}
	if normalizeWhitespaceAndCase(leftText) == normalizeWhitespaceAndCase(rightText) {
		return true
	}
	return isCommentOnly(leftText) && isCommentOnly(rightText)
}

func normalizeWhitespaceAndCase(value string) string {
	var builder strings.Builder
	for _, char := range value {
		if unicode.IsSpace(char) {
			continue
		}
		builder.WriteRune(unicode.ToLower(char))
	}
	return builder.String()
}

func isCommentOnly(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	prefixes := []string{"//", "#", ";", "--", "{", "(*", "/*"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func inlineDiff(leftText string, rightText string) ([]LineTextSegment, []LineTextSegment) {
	leftRunes := []rune(leftText)
	rightRunes := []rune(rightText)

	prefix := commonPrefixRunes(leftRunes, rightRunes)
	leftRest := leftRunes[prefix:]
	rightRest := rightRunes[prefix:]
	suffix := commonSuffixRunes(leftRest, rightRest)

	leftSegments := []LineTextSegment{}
	rightSegments := []LineTextSegment{}
	if prefix > 0 {
		text := string(leftRunes[:prefix])
		leftSegments = append(leftSegments, lineTextSegment(text, false))
		rightSegments = append(rightSegments, lineTextSegment(text, false))
	}

	leftMiddle := leftRunes[prefix : len(leftRunes)-suffix]
	rightMiddle := rightRunes[prefix : len(rightRunes)-suffix]
	var leftMiddleSegments []LineTextSegment
	var rightMiddleSegments []LineTextSegment
	if len(leftMiddle)*len(rightMiddle) > maxInlineDiffCells {
		leftMiddleSegments = changedSegment(leftMiddle)
		rightMiddleSegments = changedSegment(rightMiddle)
	} else {
		leftMiddleSegments, rightMiddleSegments = lcsInlineDiff(leftMiddle, rightMiddle)
	}
	leftSegments = appendSegments(leftSegments, leftMiddleSegments...)
	rightSegments = appendSegments(rightSegments, rightMiddleSegments...)

	if suffix > 0 {
		leftSuffix := string(leftRunes[len(leftRunes)-suffix:])
		rightSuffix := string(rightRunes[len(rightRunes)-suffix:])
		leftSegments = appendSegments(leftSegments, lineTextSegment(leftSuffix, false))
		rightSegments = appendSegments(rightSegments, lineTextSegment(rightSuffix, false))
	}

	return leftSegments, rightSegments
}

func commonPrefixRunes(left []rune, right []rune) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if left[i] != right[i] {
			return i
		}
	}
	return limit
}

func commonSuffixRunes(left []rune, right []rune) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for i := 0; i < limit; i++ {
		if left[len(left)-1-i] != right[len(right)-1-i] {
			return i
		}
	}
	return limit
}

func lcsInlineDiff(left []rune, right []rune) ([]LineTextSegment, []LineTextSegment) {
	n := len(left)
	m := len(right)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if left[i] == right[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	leftSegments := []LineTextSegment{}
	rightSegments := []LineTextSegment{}
	i, j := 0, 0
	for i < n && j < m {
		if left[i] == right[j] {
			text := string(left[i])
			leftSegments = appendSegments(leftSegments, lineTextSegment(text, false))
			rightSegments = appendSegments(rightSegments, lineTextSegment(text, false))
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			leftSegments = appendSegments(leftSegments, lineTextSegment(string(left[i]), true))
			i++
		} else {
			rightSegments = appendSegments(rightSegments, lineTextSegment(string(right[j]), true))
			j++
		}
	}
	for i < n {
		leftSegments = appendSegments(leftSegments, lineTextSegment(string(left[i]), true))
		i++
	}
	for j < m {
		rightSegments = appendSegments(rightSegments, lineTextSegment(string(right[j]), true))
		j++
	}
	return leftSegments, rightSegments
}

func changedSegment(value []rune) []LineTextSegment {
	if len(value) == 0 {
		return nil
	}
	return []LineTextSegment{lineTextSegment(string(value), true)}
}

func appendSegments(segments []LineTextSegment, next ...LineTextSegment) []LineTextSegment {
	for _, segment := range next {
		if segment.Text == "" {
			continue
		}
		lastIndex := len(segments) - 1
		if lastIndex >= 0 && segments[lastIndex].IsDiffToken == segment.IsDiffToken {
			segments[lastIndex].Text += segment.Text
			continue
		}
		segments = append(segments, segment)
	}
	return segments
}

func lineTextSegment(text string, isDiffToken bool) LineTextSegment {
	return LineTextSegment{Text: text, IsDiffToken: isDiffToken, Changed: isDiffToken}
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
