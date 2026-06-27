package compare

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

const recursiveFolderEntryLimit = 10000

type FolderSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*folderSession
}

type folderSession struct {
	id        string
	leftRoot  string
	rightRoot string
	cache     map[string]folderRowCache
}

type folderRowCache struct {
	row      FolderComparisonRow
	leftSig  folderEntrySignature
	rightSig folderEntrySignature
}

type folderCompareContext struct {
	hashes sync.Map
}

type folderEntrySignature struct {
	Exists  bool
	Type    EntryType
	Size    int64
	ModTime int64
	Mode    os.FileMode
	Digest  [32]byte
	Error   string
}

type folderEntrySnapshot struct {
	name   string
	path   string
	exists bool
	typ    EntryType
	sig    folderEntrySignature
	err    error
}

type namedFolderRow struct {
	name     string
	row      FolderComparisonRow
	leftSig  folderEntrySignature
	rightSig folderEntrySignature
}

func NewFolderSessionStore() *FolderSessionStore {
	return &FolderSessionStore{sessions: map[string]*folderSession{}}
}

func (s *FolderSessionStore) Open(tabID string, leftPath string, rightPath string) (FolderComparisonResult, error) {
	if tabID == "" {
		return FolderComparisonResult{}, fmt.Errorf("tab ID is required")
	}
	session := &folderSession{
		id:        tabID,
		leftRoot:  leftPath,
		rightRoot: rightPath,
		cache:     map[string]folderRowCache{},
	}
	result, err := session.refresh()
	if err != nil {
		return FolderComparisonResult{}, err
	}
	s.mu.Lock()
	s.sessions[tabID] = session
	s.mu.Unlock()
	return result, nil
}

func (s *FolderSessionStore) Refresh(tabID string, leftPath string, rightPath string) (FolderComparisonResult, error) {
	s.mu.Lock()
	session, ok := s.sessions[tabID]
	s.mu.Unlock()
	if !ok || session.leftRoot != leftPath || session.rightRoot != rightPath {
		return s.Open(tabID, leftPath, rightPath)
	}
	return session.refresh()
}

func (s *FolderSessionStore) Close(tabID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tabID)
}

func CompareFolder(leftPath string, rightPath string) (FolderComparisonResult, error) {
	session := &folderSession{
		leftRoot:  leftPath,
		rightRoot: rightPath,
		cache:     map[string]folderRowCache{},
	}
	return session.refresh()
}

func (session *folderSession) refresh() (FolderComparisonResult, error) {
	leftPath := session.leftRoot
	rightPath := session.rightRoot
	leftInfo, err := os.Stat(leftPath)
	if err != nil {
		return FolderComparisonResult{}, fmt.Errorf("left folder: %w", err)
	}
	if !leftInfo.IsDir() {
		return FolderComparisonResult{}, fmt.Errorf("left path is not a folder: %s", leftPath)
	}

	rightInfo, err := os.Stat(rightPath)
	if err != nil {
		return FolderComparisonResult{}, fmt.Errorf("right folder: %w", err)
	}
	if !rightInfo.IsDir() {
		return FolderComparisonResult{}, fmt.Errorf("right path is not a folder: %s", rightPath)
	}

	leftEntries, err := snapshotFolderEntries(leftPath)
	if err != nil {
		return FolderComparisonResult{}, fmt.Errorf("read left folder: %w", err)
	}
	rightEntries, err := snapshotFolderEntries(rightPath)
	if err != nil {
		return FolderComparisonResult{}, fmt.Errorf("read right folder: %w", err)
	}

	leftMap := map[string]folderEntrySnapshot{}
	rightMap := map[string]folderEntrySnapshot{}
	namesSeen := map[string]bool{}
	for _, entry := range leftEntries {
		leftMap[entry.name] = entry
		namesSeen[entry.name] = true
	}
	for _, entry := range rightEntries {
		rightMap[entry.name] = entry
		namesSeen[entry.name] = true
	}

	names := make([]string, 0, len(namesSeen))
	for name := range namesSeen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := leftMap[name]; !ok {
			leftMap[name] = folderEntrySnapshot{name: name, path: filepath.Join(leftPath, name)}
		}
		if _, ok := rightMap[name]; !ok {
			rightMap[name] = folderEntrySnapshot{name: name, path: filepath.Join(rightPath, name)}
		}
	}

	rows, nextCache := compareFolderRows(names, leftMap, rightMap, session.cache)
	session.cache = nextCache

	return FolderComparisonResult{
		LeftRoot:  leftPath,
		RightRoot: rightPath,
		Rows:      rows,
	}, nil
}

func snapshotFolderEntries(root string) ([]folderEntrySnapshot, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	snapshots := make([]folderEntrySnapshot, 0, len(entries))
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		typ := entryType(entry)
		sig, sigErr := signatureForPath(path, typ)
		snapshots = append(snapshots, folderEntrySnapshot{
			name:   entry.Name(),
			path:   path,
			exists: true,
			typ:    typ,
			sig:    sig,
			err:    sigErr,
		})
	}
	return snapshots, nil
}

func compareFolderRows(names []string, leftMap map[string]folderEntrySnapshot, rightMap map[string]folderEntrySnapshot, previous map[string]folderRowCache) ([]FolderComparisonRow, map[string]folderRowCache) {
	workerCount := runtime.NumCPU()
	if workerCount < 2 {
		workerCount = 2
	}
	if workerCount > 8 {
		workerCount = 8
	}

	jobs := make(chan string)
	results := make(chan namedFolderRow, len(names))
	ctx := &folderCompareContext{}
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for name := range jobs {
				results <- compareNamedFolderRow(ctx, name, leftMap[name], rightMap[name], previous[name])
			}
		}()
	}

	go func() {
		for _, name := range names {
			jobs <- name
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	byName := map[string]namedFolderRow{}
	for result := range results {
		byName[result.name] = result
	}

	rows := make([]FolderComparisonRow, 0, len(names))
	nextCache := make(map[string]folderRowCache, len(names))
	for _, name := range names {
		result := byName[name]
		rows = append(rows, result.row)
		nextCache[name] = folderRowCache{
			row:      result.row,
			leftSig:  result.leftSig,
			rightSig: result.rightSig,
		}
	}
	return rows, nextCache
}

func compareNamedFolderRow(ctx *folderCompareContext, name string, left folderEntrySnapshot, right folderEntrySnapshot, previous folderRowCache) namedFolderRow {
	row := FolderComparisonRow{
		Name:        name,
		LeftPath:    left.path,
		RightPath:   right.path,
		LeftExists:  left.exists,
		RightExists: right.exists,
	}

	if left.exists {
		row.LeftType = left.typ
	}
	if right.exists {
		row.RightType = right.typ
	}
	row.CanCompareFiles = row.LeftExists && row.RightExists && row.LeftType == EntryFile && row.RightType == EntryFile

	if left.err != nil {
		row.Status = StatusError
		row.Error = fmt.Sprintf("left: %s", left.err)
		return namedFolderRow{name: name, row: row, leftSig: left.sig, rightSig: right.sig}
	}
	if right.err != nil {
		row.Status = StatusError
		row.Error = fmt.Sprintf("right: %s", right.err)
		return namedFolderRow{name: name, row: row, leftSig: left.sig, rightSig: right.sig}
	}

	if signaturesReusable(previous, left.sig, right.sig) {
		row.Status = previous.row.Status
		row.Error = previous.row.Error
		return namedFolderRow{name: name, row: row, leftSig: left.sig, rightSig: right.sig}
	}

	row.Status, row.Error = compareFolderRow(ctx, row)
	return namedFolderRow{name: name, row: row, leftSig: left.sig, rightSig: right.sig}
}

func signaturesReusable(previous folderRowCache, left folderEntrySignature, right folderEntrySignature) bool {
	return previous.row.Name != "" && previous.leftSig == left && previous.rightSig == right
}

func compareFolderRow(ctx *folderCompareContext, row FolderComparisonRow) (ComparisonStatus, string) {
	if !row.LeftExists {
		return StatusRightOnly, ""
	}
	if !row.RightExists {
		return StatusLeftOnly, ""
	}
	if row.LeftType != row.RightType {
		return StatusTypeMismatch, ""
	}

	switch row.LeftType {
	case EntryFile:
		equal, err := ctx.filesEqual(row.LeftPath, row.RightPath)
		if err != nil {
			return StatusError, err.Error()
		}
		if equal {
			return StatusEqual, ""
		}
		return StatusDifferent, ""
	case EntryFolder:
		equal, err := ctx.foldersEqual(row.LeftPath, row.RightPath, 0)
		if err != nil {
			return StatusError, err.Error()
		}
		if equal {
			return StatusEqual, ""
		}
		return StatusDifferent, ""
	case EntrySymlink, EntryOther:
		return StatusUnknown, "unsupported entry type"
	default:
		return StatusUnknown, ""
	}
}

func entryType(entry os.DirEntry) EntryType {
	mode := entry.Type()
	if mode&os.ModeSymlink != 0 {
		return EntrySymlink
	}
	if entry.IsDir() {
		return EntryFolder
	}
	if mode.IsRegular() || mode == 0 {
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			return EntryFile
		}
	}
	return EntryOther
}

func (ctx *folderCompareContext) filesEqual(leftPath string, rightPath string) (bool, error) {
	leftInfo, err := os.Stat(leftPath)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(rightPath)
	if err != nil {
		return false, err
	}
	if !leftInfo.Mode().IsRegular() || !rightInfo.Mode().IsRegular() {
		return false, errors.New("unsupported non-regular file")
	}
	if leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}

	leftHash, err := ctx.fileHash(leftPath, leftInfo)
	if err != nil {
		return false, err
	}
	rightHash, err := ctx.fileHash(rightPath, rightInfo)
	if err != nil {
		return false, err
	}
	return leftHash == rightHash, nil
}

func filesEqual(leftPath string, rightPath string) (bool, error) {
	return (&folderCompareContext{}).filesEqual(leftPath, rightPath)
}

func (ctx *folderCompareContext) fileHash(path string, info os.FileInfo) ([32]byte, error) {
	key := fileHashKey(path, info)
	if value, ok := ctx.hashes.Load(key); ok {
		return value.([32]byte), nil
	}
	hash, err := fileHash(path)
	if err != nil {
		return [32]byte{}, err
	}
	ctx.hashes.Store(key, hash)
	return hash, nil
}

func fileHashKey(path string, info os.FileInfo) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%d", path, info.Size(), info.ModTime().UnixNano(), info.Mode())
}

func fileHash(path string) ([32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [32]byte{}, err
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func (ctx *folderCompareContext) foldersEqual(leftPath string, rightPath string, seen int) (bool, error) {
	if seen > recursiveFolderEntryLimit {
		return false, fmt.Errorf("folder comparison exceeded %d entries", recursiveFolderEntryLimit)
	}

	leftEntries, err := os.ReadDir(leftPath)
	if err != nil {
		return false, err
	}
	rightEntries, err := os.ReadDir(rightPath)
	if err != nil {
		return false, err
	}
	if len(leftEntries) != len(rightEntries) {
		return false, nil
	}

	leftMap := map[string]os.DirEntry{}
	for _, entry := range leftEntries {
		leftMap[entry.Name()] = entry
	}
	for _, rightEntry := range rightEntries {
		leftEntry, ok := leftMap[rightEntry.Name()]
		if !ok {
			return false, nil
		}
		leftChild := filepath.Join(leftPath, rightEntry.Name())
		rightChild := filepath.Join(rightPath, rightEntry.Name())
		leftType := entryType(leftEntry)
		rightType := entryType(rightEntry)
		if leftType != rightType {
			return false, nil
		}
		switch leftType {
		case EntryFile:
			equal, err := ctx.filesEqual(leftChild, rightChild)
			if err != nil || !equal {
				return equal, err
			}
		case EntryFolder:
			equal, err := ctx.foldersEqual(leftChild, rightChild, seen+len(leftEntries)+len(rightEntries))
			if err != nil || !equal {
				return equal, err
			}
		default:
			return false, nil
		}
	}
	return true, nil
}

func foldersEqual(leftPath string, rightPath string, seen int) (bool, error) {
	return (&folderCompareContext{}).foldersEqual(leftPath, rightPath, seen)
}

func signatureForPath(path string, typ EntryType) (folderEntrySignature, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return folderEntrySignature{Exists: true, Type: typ, Error: err.Error()}, err
	}
	signature := folderEntrySignature{
		Exists:  true,
		Type:    typ,
		Size:    info.Size(),
		ModTime: info.ModTime().UnixNano(),
		Mode:    info.Mode(),
	}
	if typ != EntryFolder {
		return signature, nil
	}
	digest, err := folderMetadataDigest(path, 0)
	if err != nil {
		signature.Error = err.Error()
		return signature, err
	}
	signature.Digest = digest
	return signature, nil
}

func folderMetadataDigest(root string, seen int) ([32]byte, error) {
	if seen > recursiveFolderEntryLimit {
		return [32]byte{}, fmt.Errorf("folder signature exceeded %d entries", recursiveFolderEntryLimit)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return [32]byte{}, err
	}
	hash := sha256.New()
	for _, entry := range entries {
		path := filepath.Join(root, entry.Name())
		typ := entryType(entry)
		info, err := os.Lstat(path)
		if err != nil {
			return [32]byte{}, err
		}
		fmt.Fprintf(hash, "%s\x00%s\x00%d\x00%d\x00%d\n", entry.Name(), typ, info.Size(), info.ModTime().UnixNano(), info.Mode())
		if typ == EntryFolder {
			childDigest, err := folderMetadataDigest(path, seen+len(entries))
			if err != nil {
				return [32]byte{}, err
			}
			hash.Write(childDigest[:])
		}
	}
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result, nil
}

func CopyFile(src string, dst string, overwrite bool) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("source file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file: %s", src)
	}
	if _, err := os.Stat(dst); err == nil && !overwrite {
		return fmt.Errorf("destination exists: %s", dst)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("destination file: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("create destination folder: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("open destination: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy file: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}
	return os.Chmod(dst, info.Mode().Perm())
}
