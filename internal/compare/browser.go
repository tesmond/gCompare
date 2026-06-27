package compare

import (
	"os"
	"path/filepath"
	"sort"
)

type BrowserEntry struct {
	Name string    `json:"name"`
	Path string    `json:"path"`
	Type EntryType `json:"type"`
}

type DirectoryListing struct {
	Path    string         `json:"path"`
	Parent  string         `json:"parent"`
	Entries []BrowserEntry `json:"entries"`
}

type FilePreview struct {
	Path    string   `json:"path"`
	Parent  string   `json:"parent"`
	Lines   []string `json:"lines"`
	Warning string   `json:"warning,omitempty"`
}

func ListDirectory(path string) (DirectoryListing, error) {
	if path == "" {
		var err error
		path, err = os.UserHomeDir()
		if err != nil {
			return DirectoryListing{}, err
		}
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return DirectoryListing{}, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return DirectoryListing{}, err
	}
	if !info.IsDir() {
		absPath = filepath.Dir(absPath)
	}

	dirEntries, err := os.ReadDir(absPath)
	if err != nil {
		return DirectoryListing{}, err
	}

	entries := make([]BrowserEntry, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if entry.Name() == "" {
			continue
		}
		entries = append(entries, BrowserEntry{
			Name: entry.Name(),
			Path: filepath.Join(absPath, entry.Name()),
			Type: entryType(entry),
		})
	}

	sort.SliceStable(entries, func(i int, j int) bool {
		leftFolder := entries[i].Type == EntryFolder
		rightFolder := entries[j].Type == EntryFolder
		if leftFolder != rightFolder {
			return leftFolder
		}
		return entries[i].Name < entries[j].Name
	})

	parent := filepath.Dir(absPath)
	if parent == absPath {
		parent = ""
	}
	return DirectoryListing{Path: absPath, Parent: parent, Entries: entries}, nil
}

func PreviewFile(path string) (FilePreview, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return FilePreview{}, err
	}
	lines, _, warning, err := readTextFile(absPath)
	if err != nil {
		return FilePreview{}, err
	}
	previewLines := make([]string, 0, len(lines))
	for _, line := range lines {
		previewLines = append(previewLines, line.Text)
	}
	return FilePreview{
		Path:    absPath,
		Parent:  filepath.Dir(absPath),
		Lines:   previewLines,
		Warning: warning,
	}, nil
}
