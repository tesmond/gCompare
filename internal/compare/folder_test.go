package compare

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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
	wantOrder := []string{"left.txt", "main.go", "README.md", "right.txt"}
	if got := folderRowNames(result.Rows); !sameStrings(got, wantOrder) {
		t.Fatalf("row names = %v, want %v", got, wantOrder)
	}
	for _, row := range result.Rows {
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

func TestFolderSessionReturnsObviousStatusesWithoutPending(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, left)
	mustMkdir(t, right)
	mustWrite(t, filepath.Join(left, "left-only.txt"), "left\n")
	mustWrite(t, filepath.Join(left, "type-mismatch"), "file\n")
	mustMkdir(t, filepath.Join(right, "type-mismatch"))

	store := NewFolderSessionStore()
	result, err := store.Open("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}

	statuses := map[string]ComparisonStatus{}
	for _, row := range result.Rows {
		statuses[row.Name] = row.Status
	}
	if got := statuses["left-only.txt"]; got != StatusLeftOnly {
		t.Fatalf("left-only status = %s, want %s", got, StatusLeftOnly)
	}
	if got := statuses["type-mismatch"]; got != StatusTypeMismatch {
		t.Fatalf("type-mismatch status = %s, want %s", got, StatusTypeMismatch)
	}
}

func TestCompareFolderSortsFoldersBeforeFilesAlphabetically(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, left)
	mustMkdir(t, right)
	mustWrite(t, filepath.Join(left, "alpha.txt"), "same\n")
	mustWrite(t, filepath.Join(right, "alpha.txt"), "same\n")
	mustMkdir(t, filepath.Join(left, "beta"))
	mustMkdir(t, filepath.Join(right, "beta"))
	mustWrite(t, filepath.Join(left, "zeta.txt"), "same\n")
	mustWrite(t, filepath.Join(right, "zeta.txt"), "same\n")
	mustMkdir(t, filepath.Join(left, "omega"))
	mustMkdir(t, filepath.Join(right, "omega"))

	result, err := CompareFolder(left, right)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"beta", "omega", "alpha.txt", "zeta.txt"}
	if got := folderRowNames(result.Rows); !sameStrings(got, want) {
		t.Fatalf("row names = %v, want %v", got, want)
	}
	if !result.Rows[0].HasChildren || !result.Rows[1].HasChildren {
		t.Fatal("folder rows are not marked expandable")
	}
}

func TestCompareFolderMarksTopLevelFolderDifferentWhenNestedFileDiffers(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, filepath.Join(left, "src"))
	mustMkdir(t, filepath.Join(right, "src"))
	mustWrite(t, filepath.Join(left, "src", "main.go"), "left\n")
	mustWrite(t, filepath.Join(right, "src", "main.go"), "right\n")
	mustWrite(t, filepath.Join(left, "src", "left-only.txt"), "left only\n")

	result, err := CompareFolder(left, right)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(result.Rows))
	}
	if got := result.Rows[0].ID; got != "src" {
		t.Fatalf("row id = %q, want src", got)
	}
	if got := result.Rows[0].Status; got != StatusDifferent {
		t.Fatalf("src status = %s, want %s", got, StatusDifferent)
	}
}

func TestFolderSessionExpandInsertsIndentedFolderFirstChildren(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, filepath.Join(left, "src", "lib"))
	mustMkdir(t, filepath.Join(right, "src", "lib"))
	mustWrite(t, filepath.Join(left, "src", "alpha.go"), "same\n")
	mustWrite(t, filepath.Join(right, "src", "alpha.go"), "same\n")
	mustWrite(t, filepath.Join(left, "src", "zeta.go"), "same\n")
	mustWrite(t, filepath.Join(right, "src", "zeta.go"), "same\n")

	store := NewFolderSessionStore()
	result, err := store.Open("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != "src" {
		t.Fatalf("initial rows = %+v, want src only", result.Rows)
	}

	result, err = store.Expand("folder-tab", "src")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"src", "lib", "alpha.go", "zeta.go"}
	if got := folderRowNames(result.Rows); !sameStrings(got, want) {
		t.Fatalf("expanded row names = %v, want %v", got, want)
	}
	for _, row := range result.Rows[1:] {
		if row.ParentID != "src" {
			t.Fatalf("%s parent = %q, want src", row.Name, row.ParentID)
		}
		if row.Depth != 1 {
			t.Fatalf("%s depth = %d, want 1", row.Name, row.Depth)
		}
	}
	if !result.Rows[1].HasChildren {
		t.Fatal("nested folder row is not marked expandable")
	}

	mustWrite(t, filepath.Join(left, "src", "lib", "nested.go"), "same\n")
	mustWrite(t, filepath.Join(right, "src", "lib", "nested.go"), "same\n")
	result, err = store.Expand("folder-tab", "src/lib")
	if err != nil {
		t.Fatal(err)
	}
	want = []string{"src", "lib", "nested.go", "alpha.go", "zeta.go"}
	if got := folderRowNames(result.Rows); !sameStrings(got, want) {
		t.Fatalf("nested expanded row names = %v, want %v", got, want)
	}
	if result.Rows[2].ParentID != "src/lib" || result.Rows[2].Depth != 2 {
		t.Fatalf("nested.go parent/depth = %q/%d, want src/lib/2", result.Rows[2].ParentID, result.Rows[2].Depth)
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
	updates := make(chan FolderComparisonUpdate, 20)
	store.SetUpdateEmitter(func(update FolderComparisonUpdate) {
		updates <- update
	})
	result, err := store.Open("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0].Status; got != StatusPending {
		t.Fatalf("initial status = %s, want %s", got, StatusPending)
	}
	if got := waitForFolderStatus(t, updates, "same.txt"); got != StatusEqual {
		t.Fatalf("resolved status = %s, want %s", got, StatusEqual)
	}

	mustWrite(t, rightFile, "diff\n")
	result, err = store.Refresh("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForFolderStatus(t, updates, "same.txt"); got != StatusDifferent {
		t.Fatalf("refreshed status = %s, want %s", got, StatusDifferent)
	}
}

func TestFolderSessionReturnsTopLevelFoldersBeforeRecursiveComparison(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, filepath.Join(left, "src"))
	mustMkdir(t, filepath.Join(right, "src"))
	mustWrite(t, filepath.Join(left, "src", "main.go"), "left\n")
	mustWrite(t, filepath.Join(right, "src", "main.go"), "right\n")

	updates := make(chan FolderComparisonUpdate, 20)
	store := NewFolderSessionStore()
	store.SetUpdateEmitter(func(update FolderComparisonUpdate) {
		updates <- update
	})

	result, err := store.Open("folder-tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(result.Rows))
	}
	if got := result.Rows[0].Name; got != "src" {
		t.Fatalf("row name = %q, want src", got)
	}
	if got := result.Rows[0].Status; got != StatusPending {
		t.Fatalf("initial status = %s, want %s", got, StatusPending)
	}
	if got := waitForFolderStatus(t, updates, "src"); got != StatusDifferent {
		t.Fatalf("resolved folder status = %s, want %s", got, StatusDifferent)
	}
}

func TestFolderSessionExpandReusesVerifiedRecursiveResults(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, filepath.Join(left, "src"))
	mustMkdir(t, filepath.Join(right, "src"))
	mustWrite(t, filepath.Join(left, "src", "main.go"), "same\n")
	mustWrite(t, filepath.Join(right, "src", "main.go"), "same\n")

	updates := make(chan FolderComparisonUpdate, 20)
	store := NewFolderSessionStore()
	store.SetUpdateEmitter(func(update FolderComparisonUpdate) {
		updates <- update
	})

	if _, err := store.Open("folder-tab", left, right); err != nil {
		t.Fatal(err)
	}
	if got := waitForFolderStatus(t, updates, "src"); got != StatusEqual {
		t.Fatalf("src status = %s, want %s", got, StatusEqual)
	}

	result, err := store.Expand("folder-tab", "src")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 {
		t.Fatalf("row count = %d, want 2", len(result.Rows))
	}
	if got := result.Rows[1].Status; got != StatusEqual {
		t.Fatalf("cached child status = %s, want %s", got, StatusEqual)
	}
}

func TestFolderSessionRefreshNodeRebuildsOnlySelectedSubtree(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	mustMkdir(t, filepath.Join(left, "src"))
	mustMkdir(t, filepath.Join(right, "src"))
	mustMkdir(t, filepath.Join(left, "other"))
	mustMkdir(t, filepath.Join(right, "other"))
	mustWrite(t, filepath.Join(left, "src", "main.go"), "same\n")
	mustWrite(t, filepath.Join(right, "src", "main.go"), "same\n")
	mustWrite(t, filepath.Join(left, "other", "keep.txt"), "same\n")
	mustWrite(t, filepath.Join(right, "other", "keep.txt"), "same\n")

	updates := make(chan FolderComparisonUpdate, 40)
	store := NewFolderSessionStore()
	store.SetUpdateEmitter(func(update FolderComparisonUpdate) {
		updates <- update
	})
	if _, err := store.Open("folder-tab", left, right); err != nil {
		t.Fatal(err)
	}
	waitForFolderStatuses(t, updates, "src", "other")
	if _, err := store.Expand("folder-tab", "src"); err != nil {
		t.Fatal(err)
	}

	mustWrite(t, filepath.Join(right, "src", "new.go"), "new\n")
	result, err := store.RefreshNode("folder-tab", "src")
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Rows[0].Name; got != "other" {
		t.Fatalf("unrelated first row = %q, want other", got)
	}
	if got := result.Rows[0].Status; got != StatusEqual {
		t.Fatalf("unrelated status = %s, want %s", got, StatusEqual)
	}
	if got := waitForFolderStatus(t, updates, "src"); got != StatusDifferent {
		t.Fatalf("refreshed src status = %s, want %s", got, StatusDifferent)
	}
}

func waitForFolderStatus(t *testing.T, updates <-chan FolderComparisonUpdate, nodeID string) ComparisonStatus {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case update := <-updates:
			if update.NodeID == nodeID {
				return update.Status
			}
		case <-timeout:
			t.Fatalf("timed out waiting for update for %s", nodeID)
		}
	}
}

func waitForFolderStatuses(t *testing.T, updates <-chan FolderComparisonUpdate, nodeIDs ...string) {
	t.Helper()
	pending := map[string]bool{}
	for _, nodeID := range nodeIDs {
		pending[nodeID] = true
	}
	timeout := time.After(2 * time.Second)
	for len(pending) > 0 {
		select {
		case update := <-updates:
			delete(pending, update.NodeID)
		case <-timeout:
			t.Fatalf("timed out waiting for updates: %v", pending)
		}
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

func folderRowNames(rows []FolderComparisonRow) []string {
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

func sameStrings(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
