package compare

import "testing"

func TestCompareTextBuildsRowsFromRawInput(t *testing.T) {
	result := CompareText("same\nleft only\nchanged left\n", "same\nchanged right\nright only\n")

	statuses := []LineComparisonStatus{}
	for _, row := range result.Rows {
		statuses = append(statuses, row.Status)
	}

	want := []LineComparisonStatus{LineEqual, LineChanged, LineChanged}
	if len(statuses) != len(want) {
		t.Fatalf("got %d rows, want %d: %#v", len(statuses), len(want), statuses)
	}
	for i := range want {
		if statuses[i] != want[i] {
			t.Fatalf("row %d status = %s, want %s", i, statuses[i], want[i])
		}
	}
	if result.LeftPath != "Pasted left text" || result.RightPath != "Pasted right text" {
		t.Fatalf("unexpected labels: %q / %q", result.LeftPath, result.RightPath)
	}
}
