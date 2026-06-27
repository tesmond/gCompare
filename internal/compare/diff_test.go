package compare

import "testing"

func TestPatienceAnchorsUseLongestIncreasingUniqueMatches(t *testing.T) {
	left := testLines("z", "a", "b", "c", "d")
	right := testLines("a", "c", "b", "d")

	anchors := patienceAnchors(left, right, 0, len(left), 0, len(right))

	want := []patienceAnchor{
		{left: 1, right: 0},
		{left: 3, right: 1},
		{left: 4, right: 3},
	}
	if len(anchors) != len(want) {
		t.Fatalf("anchors = %#v, want %#v", anchors, want)
	}
	for index := range want {
		if anchors[index] != want[index] {
			t.Fatalf("anchor[%d] = %#v, want %#v", index, anchors[index], want[index])
		}
	}
}

func TestPatienceDiffFallsBackForRepeatedOnlyRegions(t *testing.T) {
	left := testLines("same", "x", "same")
	right := testLines("same", "y", "same")

	ops := patienceDiff(left, right)

	kinds := make([]string, 0, len(ops))
	for _, op := range ops {
		kinds = append(kinds, op.kind)
	}
	want := []string{"equal", "delete", "insert", "equal"}
	if len(kinds) != len(want) {
		t.Fatalf("ops = %#v, want kinds %#v", ops, want)
	}
	for index := range want {
		if kinds[index] != want[index] {
			t.Fatalf("kind[%d] = %s, want %s; ops = %#v", index, kinds[index], want[index], ops)
		}
	}
}

func TestBuildRowsMarksInsertedInlineText(t *testing.T) {
	rows := buildRows(
		testLines("procedure Delete(Index: Integer);"),
		testLines("procedure Delete(AIndex, ACount: Integer);"),
		false,
		false,
		"left.pas",
		"right.pas",
		"",
	).Rows

	if len(rows) != 1 {
		t.Fatalf("rows length = %d, want 1", len(rows))
	}
	wantRight := []LineTextSegment{
		{Text: "procedure Delete("},
		{Text: "A", IsDiffToken: true, Changed: true},
		{Text: "Index"},
		{Text: ", ACount", IsDiffToken: true, Changed: true},
		{Text: ": Integer);"},
	}
	assertSegments(t, "right", rows[0].RightSegments, wantRight)
	if rows[0].SemanticState != SemanticImportant {
		t.Fatalf("semantic state = %s, want %s", rows[0].SemanticState, SemanticImportant)
	}
}

func TestBuildRowsMarksDeletedInlineText(t *testing.T) {
	rows := buildRows(
		testLines("Name := Prefix + Suffix;"),
		testLines("Name := Suffix;"),
		false,
		false,
		"left.pas",
		"right.pas",
		"",
	).Rows

	if len(rows) != 1 {
		t.Fatalf("rows length = %d, want 1", len(rows))
	}
	wantLeft := []LineTextSegment{
		{Text: "Name := "},
		{Text: "Prefix + ", IsDiffToken: true, Changed: true},
		{Text: "Suffix;"},
	}
	assertSegments(t, "left", rows[0].LeftSegments, wantLeft)
}

func TestBuildRowsMarksWhitespaceAndCaseAsUnimportant(t *testing.T) {
	rows := buildRows(
		testLines("  Result := Value"),
		testLines("result:=value"),
		false,
		false,
		"left.pas",
		"right.pas",
		"",
	).Rows

	if len(rows) != 1 {
		t.Fatalf("rows length = %d, want 1", len(rows))
	}
	if rows[0].SemanticState != SemanticUnimportant {
		t.Fatalf("semantic state = %s, want %s", rows[0].SemanticState, SemanticUnimportant)
	}
	if rows[0].LeftSemantic != SemanticUnimportant || rows[0].RightSemantic != SemanticUnimportant {
		t.Fatalf("pane semantic states = %s/%s, want %s/%s", rows[0].LeftSemantic, rows[0].RightSemantic, SemanticUnimportant, SemanticUnimportant)
	}
}

func TestBuildRowsMarksGapPaneAsOrphanGap(t *testing.T) {
	rows := buildRows(
		testLines("left only"),
		testLines(),
		false,
		false,
		"left.pas",
		"right.pas",
		"",
	).Rows

	if len(rows) != 1 {
		t.Fatalf("rows length = %d, want 1", len(rows))
	}
	if rows[0].LeftSemantic != SemanticImportant || rows[0].RightSemantic != SemanticOrphanGap {
		t.Fatalf("pane semantic states = %s/%s, want %s/%s", rows[0].LeftSemantic, rows[0].RightSemantic, SemanticImportant, SemanticOrphanGap)
	}
}

func assertSegments(t *testing.T, name string, got []LineTextSegment, want []LineTextSegment) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s segments = %#v, want %#v", name, got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("%s segment[%d] = %#v, want %#v; all = %#v", name, index, got[index], want[index], got)
		}
	}
}

func testLines(values ...string) []textLine {
	lines := make([]textLine, 0, len(values))
	for _, value := range values {
		lines = append(lines, textLine{Text: value, Ending: "\n"})
	}
	return lines
}
