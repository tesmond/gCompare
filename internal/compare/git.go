package compare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// GitRevisionKind identifies what a GitRevision refers to.
type GitRevisionKind string

const (
	// GitRevisionWorking is the file as it currently exists on disk, when it
	// differs from the last commit (or has never been committed).
	GitRevisionWorking GitRevisionKind = "working"
	// GitRevisionCommit is the file as recorded in a commit.
	GitRevisionCommit GitRevisionKind = "commit"
	// GitRevisionEmpty is the empty file that exists before the first commit
	// that introduced the file.
	GitRevisionEmpty GitRevisionKind = "empty"
)

const gitCommandTimeout = 20 * time.Second

// GitRevision is one version of a file in a repository's history.
type GitRevision struct {
	Kind      GitRevisionKind `json:"kind"`
	Hash      string          `json:"hash"`
	ShortHash string          `json:"shortHash"`
	// Path is the repository-relative path of the file at this revision. It
	// differs from GitFileHistory.RelativePath when the file was renamed.
	Path    string `json:"path"`
	Date    string `json:"date"`
	Author  string `json:"author"`
	Subject string `json:"subject"`
}

// GitFileHistory lists the revisions of a file, newest first. The list always
// ends with an empty revision so the oldest commit can be compared against an
// empty file.
type GitFileHistory struct {
	Path         string        `json:"path"`
	RepoRoot     string        `json:"repoRoot"`
	RelativePath string        `json:"relativePath"`
	Branch       string        `json:"branch"`
	Revisions    []GitRevision `json:"revisions"`
}

// LoadGitFileHistory finds the repository that contains path and lists every
// revision of the file, following renames.
func LoadGitFileHistory(path string) (GitFileHistory, error) {
	if path == "" {
		return GitFileHistory{}, fmt.Errorf("a file path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return GitFileHistory{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return GitFileHistory{}, err
	}
	if info.IsDir() {
		return GitFileHistory{}, fmt.Errorf("choose a file, not a folder: %s", absPath)
	}

	dir := filepath.Dir(absPath)
	rootOutput, err := runGit(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return GitFileHistory{}, fmt.Errorf("%s is not inside a git repository", absPath)
	}
	repoRoot := strings.TrimSpace(string(rootOutput))
	relativePath, err := repoRelativePath(repoRoot, absPath)
	if err != nil {
		return GitFileHistory{}, err
	}

	history := GitFileHistory{
		Path:         absPath,
		RepoRoot:     repoRoot,
		RelativePath: relativePath,
		Revisions:    []GitRevision{},
	}
	if branch, err := runGit(repoRoot, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		history.Branch = strings.TrimSpace(string(branch))
	}

	commits, err := gitFileCommits(repoRoot, relativePath)
	if err != nil {
		return GitFileHistory{}, err
	}

	if gitWorkingCopyDiffers(repoRoot, relativePath, commits) {
		subject := "Uncommitted changes"
		if len(commits) == 0 {
			subject = "New file, not yet committed"
		}
		history.Revisions = append(history.Revisions, GitRevision{
			Kind:      GitRevisionWorking,
			ShortHash: "Working copy",
			Path:      relativePath,
			Date:      info.ModTime().Format(time.RFC3339),
			Subject:   subject,
		})
	}
	history.Revisions = append(history.Revisions, commits...)
	history.Revisions = append(history.Revisions, GitRevision{
		Kind:      GitRevisionEmpty,
		ShortHash: "Empty",
		Path:      relativePath,
		Subject:   "Before the file was added",
	})
	return history, nil
}

// CompareGitRevisions compares two revisions of a file in a repository.
func CompareGitRevisions(repoRoot string, left GitRevision, right GitRevision) (FileComparisonResult, error) {
	leftLines, err := gitRevisionLines(repoRoot, left)
	if err != nil {
		return FileComparisonResult{}, fmt.Errorf("left revision: %w", err)
	}
	rightLines, err := gitRevisionLines(repoRoot, right)
	if err != nil {
		return FileComparisonResult{}, fmt.Errorf("right revision: %w", err)
	}
	return buildRows(leftLines, rightLines, false, false, gitRevisionLabel(left), gitRevisionLabel(right), ""), nil
}

func gitRevisionLabel(revision GitRevision) string {
	switch revision.Kind {
	case GitRevisionWorking:
		return revision.Path + " (working copy)"
	case GitRevisionEmpty:
		return revision.Path + " (empty)"
	default:
		return revision.Path + "@" + revision.ShortHash
	}
}

func gitRevisionLines(repoRoot string, revision GitRevision) ([]textLine, error) {
	switch revision.Kind {
	case GitRevisionEmpty:
		return []textLine{}, nil
	case GitRevisionWorking:
		path, err := safeRepoPath(repoRoot, revision.Path)
		if err != nil {
			return nil, err
		}
		lines, _, _, err := readTextFile(path)
		return lines, err
	case GitRevisionCommit:
		if revision.Hash == "" || strings.HasPrefix(revision.Hash, "-") {
			return nil, fmt.Errorf("invalid commit hash")
		}
		if _, err := safeRepoPath(repoRoot, revision.Path); err != nil {
			return nil, err
		}
		data, err := runGit(repoRoot, "show", revision.Hash+":"+filepath.ToSlash(revision.Path))
		if err != nil {
			return nil, err
		}
		if len(data) > maxDisplayFileSize {
			return nil, fmt.Errorf("file is too large to display comfortably at %s", revision.ShortHash)
		}
		if isBinary(data) {
			return nil, fmt.Errorf("binary file detected at %s", revision.ShortHash)
		}
		if !utf8.Valid(data) {
			return nil, fmt.Errorf("file is not valid UTF-8 at %s", revision.ShortHash)
		}
		return splitLines(data), nil
	default:
		return nil, fmt.Errorf("unknown revision kind: %q", revision.Kind)
	}
}

const gitLogFieldSeparator = "\x1f"
const gitLogRecordMarker = "\x1e"

func gitFileCommits(repoRoot string, relativePath string) ([]GitRevision, error) {
	format := gitLogRecordMarker + strings.Join([]string{"%H", "%h", "%aI", "%an", "%s"}, gitLogFieldSeparator)
	output, err := runGit(repoRoot, "log", "--follow", "--name-only", "--format="+format, "--", relativePath)
	if err != nil {
		// A repository with no commits yet has no history for any file.
		if _, headErr := runGit(repoRoot, "rev-parse", "--verify", "HEAD"); headErr != nil {
			return []GitRevision{}, nil
		}
		return nil, err
	}
	return parseGitLog(string(output), relativePath), nil
}

func parseGitLog(output string, fallbackPath string) []GitRevision {
	revisions := []GitRevision{}
	for _, record := range strings.Split(output, gitLogRecordMarker) {
		record = strings.Trim(record, "\n")
		if record == "" {
			continue
		}
		lines := strings.Split(record, "\n")
		fields := strings.Split(lines[0], gitLogFieldSeparator)
		if len(fields) < 5 {
			continue
		}
		path := fallbackPath
		for _, line := range lines[1:] {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				path = trimmed
				break
			}
		}
		revisions = append(revisions, GitRevision{
			Kind:      GitRevisionCommit,
			Hash:      fields[0],
			ShortHash: fields[1],
			Date:      fields[2],
			Author:    fields[3],
			Subject:   strings.Join(fields[4:], gitLogFieldSeparator),
			Path:      path,
		})
	}
	return revisions
}

func gitWorkingCopyDiffers(repoRoot string, relativePath string, commits []GitRevision) bool {
	if len(commits) == 0 {
		return true
	}
	// `git status` reports modified, staged, and untracked states in one call.
	output, err := runGit(repoRoot, "status", "--porcelain", "--", relativePath)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func repoRelativePath(repoRoot string, absPath string) (string, error) {
	root := repoRoot
	if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
		root = resolved
	}
	target := absPath
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		target = resolved
	}
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository %s", absPath, repoRoot)
	}
	return filepath.ToSlash(relative), nil
}

func safeRepoPath(repoRoot string, relativePath string) (string, error) {
	if relativePath == "" {
		return "", fmt.Errorf("revision path is required")
	}
	joined := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
	relative, err := filepath.Rel(repoRoot, joined)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("revision path is outside the repository: %s", relativePath)
	}
	return joined, nil
}

var gitExecutable = findGitExecutable

func findGitExecutable() (string, error) {
	if path, err := exec.LookPath("git"); err == nil {
		return path, nil
	}
	// GUI apps on macOS start with a minimal PATH, so check the usual places.
	for _, candidate := range []string{"/usr/bin/git", "/opt/homebrew/bin/git", "/usr/local/bin/git"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("git executable not found")
}

func runGit(dir string, args ...string) ([]byte, error) {
	executable, err := gitExecutable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, append([]string{"-c", "core.quotepath=off", "--no-pager"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("git %s timed out", args[0])
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", args[0], message)
	}
	return stdout.Bytes(), nil
}
