package compare

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// CopyAction says what should happen to a single item when copying.
type CopyAction string

const (
	// CopyActionCopy: the destination is missing, or the source is newer.
	CopyActionCopy CopyAction = "copy"
	// CopyActionSkip: the destination already matches, nothing to do.
	CopyActionSkip CopyAction = "skip"
	// CopyActionConfirm: copying would overwrite a newer (or equally dated but
	// different) destination, so the user must confirm.
	CopyActionConfirm CopyAction = "confirm"
	// CopyActionError: the item can't be copied (see Reason).
	CopyActionError CopyAction = "error"
)

const (
	CopyReasonDestNewer = "dest_newer"
	CopyReasonSameTime  = "same_time"
)

// copyTimeTolerance absorbs the coarse timestamp resolution of some file
// systems (FAT stores modification times with 2 second granularity).
const copyTimeTolerance = 2 * time.Second

// CopyItem is one file, symlink or (empty/new) folder that a copy will touch.
type CopyItem struct {
	Source         string     `json:"source"`
	Dest           string     `json:"dest"`
	Name           string     `json:"name"`
	Type           EntryType  `json:"type"`
	Action         CopyAction `json:"action"`
	Reason         string     `json:"reason,omitempty"`
	SourceSize     int64      `json:"sourceSize"`
	SourceModified int64      `json:"sourceModified"`
	DestExists     bool       `json:"destExists"`
	DestSize       int64      `json:"destSize"`
	DestModified   int64      `json:"destModified"`
}

// PlanCopy expands src (a file, symlink or folder) into the list of items that
// copying it to dst involves, and classifies each one. Nothing is written.
func PlanCopy(src string, dst string) ([]CopyItem, error) {
	if src == "" || dst == "" {
		return nil, fmt.Errorf("source and destination are required")
	}
	if _, err := os.Lstat(src); err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if srcInfo, err := os.Stat(src); err == nil {
		if dstInfo, err := os.Stat(dst); err == nil && os.SameFile(srcInfo, dstInfo) {
			return nil, fmt.Errorf("source and destination are the same: %s", src)
		}
	}
	items := []CopyItem{}
	planCopyEntry(src, dst, filepath.Base(src), &items)
	return items, nil
}

func planCopyEntry(src string, dst string, name string, items *[]CopyItem) {
	item := CopyItem{Source: src, Dest: dst, Name: name}

	srcInfo, err := os.Lstat(src)
	if err != nil {
		item.Action = CopyActionError
		item.Reason = err.Error()
		*items = append(*items, item)
		return
	}
	item.Type = lstatEntryType(srcInfo)
	item.SourceSize = srcInfo.Size()
	item.SourceModified = srcInfo.ModTime().UnixMilli()

	dstInfo, dstErr := os.Lstat(dst)
	if dstErr != nil && !os.IsNotExist(dstErr) {
		item.Action = CopyActionError
		item.Reason = dstErr.Error()
		*items = append(*items, item)
		return
	}
	item.DestExists = dstErr == nil
	if item.DestExists {
		item.DestSize = dstInfo.Size()
		item.DestModified = dstInfo.ModTime().UnixMilli()
	}

	switch item.Type {
	case EntryFolder:
		if item.DestExists && !dstInfo.IsDir() {
			item.Action = CopyActionError
			item.Reason = "destination exists and is not a folder"
			*items = append(*items, item)
			return
		}
		if !item.DestExists {
			item.Action = CopyActionCopy
			*items = append(*items, item)
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			*items = append(*items, CopyItem{Source: src, Dest: dst, Name: name, Type: EntryFolder, Action: CopyActionError, Reason: err.Error()})
			return
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		for _, childName := range names {
			planCopyEntry(filepath.Join(src, childName), filepath.Join(dst, childName), filepath.Join(name, childName), items)
		}
		return
	case EntryFile, EntrySymlink:
		// handled below
	default:
		item.Action = CopyActionError
		item.Reason = "unsupported file type"
		*items = append(*items, item)
		return
	}

	if !item.DestExists {
		item.Action = CopyActionCopy
		*items = append(*items, item)
		return
	}
	if dstInfo.IsDir() {
		item.Action = CopyActionError
		item.Reason = "destination exists and is a folder"
		*items = append(*items, item)
		return
	}
	if item.Type == EntrySymlink {
		srcTarget, srcErr := os.Readlink(src)
		dstTarget, dstReadErr := os.Readlink(dst)
		if srcErr == nil && dstReadErr == nil && srcTarget == dstTarget {
			item.Action = CopyActionSkip
			*items = append(*items, item)
			return
		}
	}

	diff := srcInfo.ModTime().Sub(dstInfo.ModTime())
	switch {
	case diff > copyTimeTolerance:
		item.Action = CopyActionCopy
	case item.Type == EntryFile && sameContent(src, dst, srcInfo, dstInfo):
		// Nothing would change, so there is nothing to confirm.
		item.Action = CopyActionSkip
	case diff < -copyTimeTolerance:
		item.Action = CopyActionConfirm
		item.Reason = CopyReasonDestNewer
	default:
		item.Action = CopyActionConfirm
		item.Reason = CopyReasonSameTime
	}
	*items = append(*items, item)
}

func sameContent(src string, dst string, srcInfo os.FileInfo, dstInfo os.FileInfo) bool {
	if !srcInfo.Mode().IsRegular() || !dstInfo.Mode().IsRegular() || srcInfo.Size() != dstInfo.Size() {
		return false
	}
	equal, err := sameFileBytes(src, dst)
	return err == nil && equal
}

func lstatEntryType(info os.FileInfo) EntryType {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return EntrySymlink
	case mode.IsDir():
		return EntryFolder
	case mode.IsRegular():
		return EntryFile
	default:
		return EntryOther
	}
}

// CopyPath copies a single item (one file, symlink or folder entry) from src
// to dst, overwriting an existing file. Folders are only created, never
// descended into: PlanCopy lists their children as separate items.
func CopyPath(src string, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("read link: %w", err)
		}
		if existing, err := os.Lstat(dst); err == nil {
			if existing.IsDir() {
				return fmt.Errorf("destination is a folder: %s", dst)
			}
			if err := os.Remove(dst); err != nil {
				return fmt.Errorf("replace destination: %w", err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return fmt.Errorf("create destination folder: %w", err)
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		if existing, err := os.Lstat(dst); err == nil && !existing.IsDir() {
			return fmt.Errorf("destination exists and is not a folder: %s", dst)
		}
		return os.MkdirAll(dst, info.Mode().Perm()|0o700)
	default:
		return CopyFile(src, dst, true)
	}
}
