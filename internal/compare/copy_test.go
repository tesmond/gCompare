package compare

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeAt(t *testing.T, path string, text string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func actionFor(items []CopyItem, name string) CopyItem {
	for _, item := range items {
		if item.Name == name {
			return item
		}
	}
	return CopyItem{}
}

func TestPlanCopyClassifiesFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	old := now.Add(-time.Hour)

	writeAt(t, filepath.Join(dir, "l", "d", "new.txt"), "new", now)
	writeAt(t, filepath.Join(dir, "l", "d", "newer-src.txt"), "src", now)
	writeAt(t, filepath.Join(dir, "r", "d", "newer-src.txt"), "dst", old)
	writeAt(t, filepath.Join(dir, "l", "d", "newer-dst.txt"), "src", old)
	writeAt(t, filepath.Join(dir, "r", "d", "newer-dst.txt"), "dst", now)
	writeAt(t, filepath.Join(dir, "l", "d", "same-bytes.txt"), "same", old)
	writeAt(t, filepath.Join(dir, "r", "d", "same-bytes.txt"), "same", now)
	writeAt(t, filepath.Join(dir, "l", "d", "same-time.txt"), "aaa", now)
	writeAt(t, filepath.Join(dir, "r", "d", "same-time.txt"), "bbb", now)
	if err := os.MkdirAll(filepath.Join(dir, "l", "d", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	items, err := PlanCopy(filepath.Join(dir, "l", "d"), filepath.Join(dir, "r", "d"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]CopyAction{
		"d/new.txt":        CopyActionCopy,
		"d/newer-src.txt":  CopyActionCopy,
		"d/newer-dst.txt":  CopyActionConfirm,
		"d/same-bytes.txt": CopyActionSkip,
		"d/same-time.txt":  CopyActionConfirm,
		"d/empty":          CopyActionCopy,
	}
	for name, action := range want {
		if got := actionFor(items, name); got.Action != action {
			t.Errorf("%s: got %q want %q", name, got.Action, action)
		}
	}
	if got := actionFor(items, "d/newer-dst.txt"); got.Reason != CopyReasonDestNewer {
		t.Errorf("reason = %q", got.Reason)
	}
	if got := actionFor(items, "d/same-time.txt"); got.Reason != CopyReasonSameTime {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestCopyPathPreservesTimeAndCreatesFolders(t *testing.T) {
	dir := t.TempDir()
	mod := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	writeAt(t, filepath.Join(dir, "l", "a", "b.txt"), "hello", mod)
	if err := CopyPath(filepath.Join(dir, "l", "a"), filepath.Join(dir, "r", "a")); err != nil {
		t.Fatal(err)
	}
	if err := CopyPath(filepath.Join(dir, "l", "a", "b.txt"), filepath.Join(dir, "r", "a", "b.txt")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "r", "a", "b.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(mod) {
		t.Errorf("mod time %v, want %v", info.ModTime(), mod)
	}
	// Re-planning now finds nothing to do.
	items, _ := PlanCopy(filepath.Join(dir, "l", "a"), filepath.Join(dir, "r", "a"))
	if got := actionFor(items, "a/b.txt"); got.Action != CopyActionSkip {
		t.Errorf("after copy action = %q", got.Action)
	}
}

func TestPlanCopyTypeMismatchAndSamePath(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeAt(t, filepath.Join(dir, "l", "x"), "file", now)
	writeAt(t, filepath.Join(dir, "r", "x", "inner.txt"), "in", now)
	items, err := PlanCopy(filepath.Join(dir, "l", "x"), filepath.Join(dir, "r", "x"))
	if err != nil {
		t.Fatal(err)
	}
	if got := actionFor(items, "x"); got.Action != CopyActionError {
		t.Errorf("file over folder: %q", got.Action)
	}
	if _, err := PlanCopy(filepath.Join(dir, "l", "x"), filepath.Join(dir, "l", "x")); err == nil {
		t.Error("expected same-path error")
	}
}
