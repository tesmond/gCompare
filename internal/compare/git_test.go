package compare

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitTest(t, dir, "init", "-q")
	gitTest(t, dir, "config", "user.email", "test@example.com")
	gitTest(t, dir, "config", "user.name", "Test")
	gitTest(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func gitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func commitFile(t *testing.T, dir string, name string, content string, message string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, dir, "add", name)
	gitTest(t, dir, "commit", "-q", "-m", message)
}

func revisionKinds(history GitFileHistory) []GitRevisionKind {
	kinds := []GitRevisionKind{}
	for _, revision := range history.Revisions {
		kinds = append(kinds, revision.Kind)
	}
	return kinds
}

func TestGitFileHistoryListsCommitsNewestFirstWithTrailingEmpty(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "notes.txt", "one\n", "first")
	commitFile(t, dir, "notes.txt", "one\ntwo\n", "second")
	commitFile(t, dir, "other.txt", "x\n", "unrelated")
	commitFile(t, dir, "notes.txt", "one\ntwo\nthree\n", "third")

	history, err := LoadGitFileHistory(filepath.Join(dir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if history.RelativePath != "notes.txt" {
		t.Fatalf("relative path = %q", history.RelativePath)
	}
	if got := revisionKinds(history); len(got) != 4 || got[0] != GitRevisionCommit || got[3] != GitRevisionEmpty {
		t.Fatalf("unexpected revision kinds: %v", got)
	}
	subjects := []string{history.Revisions[0].Subject, history.Revisions[1].Subject, history.Revisions[2].Subject}
	if strings.Join(subjects, ",") != "third,second,first" {
		t.Fatalf("unexpected subjects: %v", subjects)
	}
	if history.Revisions[0].Date == "" || history.Revisions[0].ShortHash == "" {
		t.Fatalf("missing commit metadata: %+v", history.Revisions[0])
	}

	result, err := CompareGitRevisions(history.RepoRoot, history.Revisions[1], history.Revisions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 3 || result.Rows[2].Status != LineRightOnly {
		t.Fatalf("unexpected rows: %+v", result.Rows)
	}
}

func TestGitFileHistoryIncludesWorkingCopyWhenModified(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "one\n", "first")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nlocal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	history, err := LoadGitFileHistory(filepath.Join(dir, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := revisionKinds(history)
	if len(got) != 3 || got[0] != GitRevisionWorking || got[1] != GitRevisionCommit || got[2] != GitRevisionEmpty {
		t.Fatalf("unexpected revision kinds: %v", got)
	}
	result, err := CompareGitRevisions(history.RepoRoot, history.Revisions[1], history.Revisions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Rows[1].RightText != "local" {
		t.Fatalf("unexpected rows: %+v", result.Rows)
	}
}

func TestGitFileHistoryForUntrackedFileComparesAgainstEmpty(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "a.txt", "one\n", "first")
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("brand\nnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	history, err := LoadGitFileHistory(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := revisionKinds(history)
	if len(got) != 2 || got[0] != GitRevisionWorking || got[1] != GitRevisionEmpty {
		t.Fatalf("unexpected revision kinds: %v", got)
	}
	result, err := CompareGitRevisions(history.RepoRoot, history.Revisions[1], history.Revisions[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 2 || result.Rows[0].Status != LineRightOnly {
		t.Fatalf("unexpected rows: %+v", result.Rows)
	}
}

func TestGitFileHistoryFollowsRenames(t *testing.T) {
	dir := initTestRepo(t)
	commitFile(t, dir, "old.txt", "alpha\nbeta\ngamma\ndelta\n", "add old")
	gitTest(t, dir, "mv", "old.txt", "new.txt")
	gitTest(t, dir, "commit", "-q", "-m", "rename")

	history, err := LoadGitFileHistory(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(history.Revisions) != 3 {
		t.Fatalf("expected rename to be followed, got %+v", history.Revisions)
	}
	if history.Revisions[1].Path != "old.txt" {
		t.Fatalf("expected old path for first commit, got %q", history.Revisions[1].Path)
	}
	if _, err := CompareGitRevisions(history.RepoRoot, history.Revisions[1], history.Revisions[0]); err != nil {
		t.Fatal(err)
	}
}

func TestGitFileHistoryRejectsFilesOutsideRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "loose.txt")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGitFileHistory(path); err == nil || !strings.Contains(err.Error(), "not inside a git repository") {
		t.Fatalf("expected repository error, got %v", err)
	}
}

func TestCompareGitRevisionsRejectsEscapingPaths(t *testing.T) {
	dir := initTestRepo(t)
	_, err := CompareGitRevisions(dir, GitRevision{Kind: GitRevisionWorking, Path: "../secret"}, GitRevision{Kind: GitRevisionEmpty})
	if err == nil {
		t.Fatal("expected path escape to be rejected")
	}
}
