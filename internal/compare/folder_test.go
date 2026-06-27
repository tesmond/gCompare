package compare

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareFolderAlignsSortedRowsAndStatuses(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, left)
	mustMkdir(t, right)
	mustWrite(t, filepath.Join(left, "README.md"), "same\n")
	mustWrite(t, filepath.Join(right, "README.md"), "same\n")
	mustWrite(t, filepath.Join(left, "main.go"), "left\n")
	mustWrite(t, filepath.Join(right, "main.go"), "right\n")
	mustWrite(t, filepath.Join(left, "left.txt"), "only left\n")
	mustWrite(t, filepath.Join(right, "right.txt"), "only right\n")

	result, err := CompareFolder(left, right)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]ComparisonStatus{
		"README.md": StatusEqual,
		"left.txt":  StatusLeftOnly,
		"main.go":   StatusDifferent,
		"right.txt": StatusRightOnly,
	}
	if len(result.Rows) != len(want) {
		t.Fatalf("row count = %d, want %d", len(result.Rows), len(want))
	}
	lastName := ""
	for _, row := range result.Rows {
		if row.Name < lastName {
			t.Fatalf("rows are not sorted: %q before %q", row.Name, lastName)
		}
		lastName = row.Name
		if row.Status != want[row.Name] {
			t.Fatalf("%s status = %s, want %s", row.Name, row.Status, want[row.Name])
		}
		if (row.Name == "README.md" || row.Name == "main.go") && !row.CanCompareFiles {
			t.Fatalf("%s canCompareFiles = false, want true", row.Name)
		}
		if row.Name == "left.txt" && row.RightPath != filepath.Join(right, "left.txt") {
			t.Fatalf("left-only right path = %q, want destination path", row.RightPath)
		}
		if row.Name == "right.txt" && row.LeftPath != filepath.Join(left, "right.txt") {
			t.Fatalf("right-only left path = %q, want destination path", row.LeftPath)
		}
	}
}

func TestCompareFolderTypeMismatch(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, left)
	mustMkdir(t, right)
	mustWrite(t, filepath.Join(left, "thing"), "file\n")
	mustMkdir(t, filepath.Join(right, "thing"))

	result, err := CompareFolder(left, right)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0].Status; got != StatusTypeMismatch {
		t.Fatalf("status = %s, want %s", got, StatusTypeMismatch)
	}
}

func TestFolderSessionRefreshDetectsChangedSameNamedFile(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, left)
	mustMkdir(t, right)
	leftFile := filepath.Join(left, "same.txt")
	rightFile := filepath.Join(right, "same.txt")
	mustWrite(t, leftFile, "same\n")
	mustWrite(t, rightFile, "same\n")

	store := NewFolderSessionStore()
	result, err := store.Open("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0].Status; got != StatusEqual {
		t.Fatalf("initial status = %s, want %s", got, StatusEqual)
	}

	mustWrite(t, rightFile, "diff\n")
	result, err = store.Refresh("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0].Status; got != StatusDifferent {
		t.Fatalf("refreshed status = %s, want %s", got, StatusDifferent)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
