package compare

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenFileComparisonAlignsChangedAndMissingLines(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left.txt")
	right := filepath.Join(root, "right.txt")
	mustWrite(t, left, "one\ntwo\nthree\n")
	mustWrite(t, right, "one\nTWO\nfour\nthree\n")

	store := NewSessionStore()
	result, err := store.Open("tab", left, right)
	if err != nil {
		t.Fatal(err)
	}

	statuses := []LineComparisonStatus{}
	for _, row := range result.Rows {
		statuses = append(statuses, row.Status)
	}
	want := []LineComparisonStatus{LineEqual, LineChanged, LineRightOnly, LineEqual}
	if len(statuses) != len(want) {
		t.Fatalf("statuses = %#v, want %#v", statuses, want)
	}
	for index := range want {
		if statuses[index] != want[index] {
			t.Fatalf("status[%d] = %s, want %s", index, statuses[index], want[index])
		}
	}
}

func TestReplaceTextMarksSideDirtyAndRebuildsDiff(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left.txt")
	right := filepath.Join(root, "right.txt")
	mustWrite(t, left, "one\n")
	mustWrite(t, right, "one\n")

	store := NewSessionStore()
	if _, err := store.Open("tab", left, right); err != nil {
		t.Fatal(err)
	}

	result, err := store.ReplaceText("tab", "left", "one\ntwo\n")
	if err != nil {
		t.Fatal(err)
	}
	if !result.LeftDirty || result.RightDirty {
		t.Fatalf("dirty flags = left %v right %v", result.LeftDirty, result.RightDirty)
	}
	if got := result.Rows[len(result.Rows)-1].Status; got != LineLeftOnly {
		t.Fatalf("last row status = %s, want %s", got, LineLeftOnly)
	}
}

func TestApplyLinesLeftToRightEditsInMemoryAndMarksDirty(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left.txt")
	right := filepath.Join(root, "right.txt")
	mustWrite(t, left, "one\ntwo\nthree\n")
	mustWrite(t, right, "one\nTWO\nthree\n")

	store := NewSessionStore()
	result, err := store.Open("tab", left, right)
	if err != nil {
		t.Fatal(err)
	}
	if result.Rows[1].Status != LineChanged {
		t.Fatalf("row 1 status = %s, want %s", result.Rows[1].Status, LineChanged)
	}

	result, err = store.ApplyLinesLeftToRight("tab", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !result.RightDirty {
		t.Fatal("right side was not marked dirty")
	}
	for _, row := range result.Rows {
		if row.Status != LineEqual {
			t.Fatalf("row after copy = %#v, want all equal", row)
		}
	}

	onDisk, err := os.ReadFile(right)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "one\nTWO\nthree\n" {
		t.Fatalf("copy wrote to disk before save: %q", string(onDisk))
	}
}

func TestSaveWritesDirtySide(t *testing.T) {
	root := t.TempDir()
	left := filepath.Join(root, "left.txt")
	right := filepath.Join(root, "right.txt")
	mustWrite(t, left, "one\ntwo\n")
	mustWrite(t, right, "one\nTWO\n")

	store := NewSessionStore()
	if _, err := store.Open("tab", left, right); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyLinesLeftToRight("tab", 1, 1); err != nil {
		t.Fatal(err)
	}
	result, err := store.Save("tab", "right")
	if err != nil {
		t.Fatal(err)
	}
	if result.RightDirty {
		t.Fatal("right side is still dirty after save")
	}
	onDisk, err := os.ReadFile(right)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != "one\ntwo\n" {
		t.Fatalf("saved content = %q", string(onDisk))
	}
}
