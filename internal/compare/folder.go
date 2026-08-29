package compare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

const recursiveFolderEntryLimit = 10000
const fileCompareChunkSize = 1024 * 1024

var fileCompareBufferPool = sync.Pool{
	New: func() any {
		return make([]byte, fileCompareChunkSize)
	},
}

type FolderComparisonUpdate struct {
	TabID    string           `json:"tabID"`
	NodeID   string           `json:"nodeID"`
	Revision uint64           `json:"revision"`
	Status   ComparisonStatus `json:"status"`
	Error    string           `json:"error,omitempty"`
}

type FolderSessionStore struct {
	mu        sync.Mutex
	sessions  map[string]*folderSession
	revisions map[string]uint64
	emit      func(FolderComparisonUpdate)
}

type folderSession struct {
	mu        sync.Mutex
	cacheMu   sync.RWMutex
	id        string
	revision  uint64
	leftRoot  string
	rightRoot string
	nodes     []*folderNode
	byID      map[string]*folderNode
	ctx       context.Context
	cancel    context.CancelFunc
	emit      func(FolderComparisonUpdate)
	cache     map[string]cachedFolderStatus
}

type folderNode struct {
	row            FolderComparisonRow
	childrenLoaded bool
	completed      bool
}

type folderEntry struct {
	name   string
	path   string
	exists bool
	typ    EntryType
	err    error
}

type folderCompareOutcome struct {
	node   *folderNode
	status ComparisonStatus
	err    string
}

type cachedFolderStatus struct {
	status ComparisonStatus
	err    string
}

func NewFolderSessionStore() *FolderSessionStore {
	return &FolderSessionStore{
		sessions:  map[string]*folderSession{},
		revisions: map[string]uint64{},
	}
}

func (s *FolderSessionStore) SetUpdateEmitter(emit func(FolderComparisonUpdate)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emit = emit
}

func (s *FolderSessionStore) Open(tabID string, leftPath string, rightPath string) (FolderComparisonResult, error) {
	if tabID == "" {
		return FolderComparisonResult{}, fmt.Errorf("tab ID is required")
	}
	s.mu.Lock()
	if previous := s.sessions[tabID]; previous != nil {
		previous.stop()
	}
	s.revisions[tabID]++
	revision := s.revisions[tabID]
	emit := s.emit
	s.mu.Unlock()

	session, result, err := newFolderSession(tabID, revision, leftPath, rightPath, emit)
	if err != nil {
		return FolderComparisonResult{}, err
	}

	s.mu.Lock()
	s.sessions[tabID] = session
	s.mu.Unlock()
	session.start()
	return result, nil
}

func (s *FolderSessionStore) Refresh(tabID string, leftPath string, rightPath string) (FolderComparisonResult, error) {
	return s.Open(tabID, leftPath, rightPath)
}

func (s *FolderSessionStore) Expand(tabID string, nodeID string) (FolderComparisonResult, error) {
	s.mu.Lock()
	session := s.sessions[tabID]
	s.mu.Unlock()
	if session == nil {
		return FolderComparisonResult{}, fmt.Errorf("folder comparison session not found: %s", tabID)
	}
	return session.expand(nodeID)
}

func (s *FolderSessionStore) RefreshNode(tabID string, nodeID string) (FolderComparisonResult, error) {
	s.mu.Lock()
	session := s.sessions[tabID]
	s.mu.Unlock()
	if session == nil {
		return FolderComparisonResult{}, fmt.Errorf("folder comparison session not found: %s", tabID)
	}
	return session.refreshNode(nodeID)
}

func (s *FolderSessionStore) Close(tabID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[tabID]; session != nil {
		session.stop()
	}
	delete(s.sessions, tabID)
}

func CompareFolder(leftPath string, rightPath string) (FolderComparisonResult, error) {
	session, _, err := newFolderSession("", 0, leftPath, rightPath, nil)
	if err != nil {
		return FolderComparisonResult{}, err
	}
	session.run(context.Background(), session.nodes)
	return session.result(), nil
}

func newFolderSession(tabID string, revision uint64, leftPath string, rightPath string, emit func(FolderComparisonUpdate)) (*folderSession, FolderComparisonResult, error) {
	leftInfo, err := os.Stat(leftPath)
	if err != nil {
		return nil, FolderComparisonResult{}, fmt.Errorf("left folder: %w", err)
	}
	if !leftInfo.IsDir() {
		return nil, FolderComparisonResult{}, fmt.Errorf("left path is not a folder: %s", leftPath)
	}

	rightInfo, err := os.Stat(rightPath)
	if err != nil {
		return nil, FolderComparisonResult{}, fmt.Errorf("right folder: %w", err)
	}
	if !rightInfo.IsDir() {
		return nil, FolderComparisonResult{}, fmt.Errorf("right path is not a folder: %s", rightPath)
	}

	session := &folderSession{
		id:        tabID,
		revision:  revision,
		leftRoot:  leftPath,
		rightRoot: rightPath,
		byID:      map[string]*folderNode{},
		emit:      emit,
		cache:     map[string]cachedFolderStatus{},
	}
	children, err := session.buildTopLevelChildren(leftPath, rightPath)
	if err != nil {
		return nil, FolderComparisonResult{}, err
	}
	for _, child := range children {
		session.appendNode(child)
	}
	return session, session.initialResult(), nil
}

func (session *folderSession) initialResult() FolderComparisonResult {
	return FolderComparisonResult{
		LeftRoot:  session.leftRoot,
		RightRoot: session.rightRoot,
		Revision:  session.revision,
		Rows:      session.rows(),
	}
}

func (session *folderSession) result() FolderComparisonResult {
	session.mu.Lock()
	defer session.mu.Unlock()
	return FolderComparisonResult{
		LeftRoot:  session.leftRoot,
		RightRoot: session.rightRoot,
		Revision:  session.revision,
		Rows:      session.rowsLocked(),
	}
}

func (session *folderSession) rows() []FolderComparisonRow {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.rowsLocked()
}

func (session *folderSession) rowsLocked() []FolderComparisonRow {
	rows := make([]FolderComparisonRow, 0, len(session.nodes))
	for index, node := range session.nodes {
		row := node.row
		row.RowIndex = index
		rows = append(rows, row)
	}
	return rows
}

func (session *folderSession) appendNode(node *folderNode) {
	node.row.RowIndex = len(session.nodes)
	session.nodes = append(session.nodes, node)
	session.byID[node.row.ID] = node
}

func (session *folderSession) buildTopLevelChildren(leftDir string, rightDir string) ([]*folderNode, error) {
	return session.buildChildNodes(nil, leftDir, rightDir, "", 0, true, true)
}

func (session *folderSession) buildChildNodes(parent *folderNode, leftDir string, rightDir string, parentRel string, childDepth int, leftExists bool, rightExists bool) ([]*folderNode, error) {
	leftEntries, err := readFolderEntries(leftDir, leftExists)
	if err != nil {
		return nil, fmt.Errorf("read left folder: %w", err)
	}
	rightEntries, err := readFolderEntries(rightDir, rightExists)
	if err != nil {
		return nil, fmt.Errorf("read right folder: %w", err)
	}

	namesSeen := make(map[string]bool, len(leftEntries)+len(rightEntries))
	for name := range leftEntries {
		namesSeen[name] = true
	}
	for name := range rightEntries {
		namesSeen[name] = true
	}
	names := make([]string, 0, len(namesSeen))
	for name := range namesSeen {
		names = append(names, name)
	}
	sortFolderEntryNames(names, leftEntries, rightEntries)

	nodes := make([]*folderNode, 0, len(names))
	for _, name := range names {
		left := leftEntries[name]
		right := rightEntries[name]
		if !left.exists {
			left = missingFolderEntry(name, filepath.Join(leftDir, name))
		}
		if !right.exists {
			right = missingFolderEntry(name, filepath.Join(rightDir, name))
		}
		rel := name
		if parentRel != "" {
			rel = filepath.Join(parentRel, name)
		}
		node := session.newNode(parent, left, right, rel, childDepth)
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (session *folderSession) newNode(parent *folderNode, left folderEntry, right folderEntry, rel string, depth int) *folderNode {
	parentID := ""
	if parent != nil {
		parentID = parent.row.ID
	}
	row := FolderComparisonRow{
		ID:              filepath.ToSlash(rel),
		ParentID:        parentID,
		Depth:           depth,
		HasChildren:     folderEntryIsExpandable(left) || folderEntryIsExpandable(right),
		Name:            left.name,
		LeftPath:        left.path,
		RightPath:       right.path,
		LeftExists:      left.exists,
		RightExists:     right.exists,
		LeftType:        left.typ,
		RightType:       right.typ,
		CanCompareFiles: left.exists && right.exists && left.typ == EntryFile && right.typ == EntryFile,
		Status:          StatusPending,
	}
	if row.Name == "" {
		row.Name = right.name
	}
	completed := false
	if left.err != nil {
		row.Status = StatusError
		row.Error = fmt.Sprintf("left: %s", left.err)
		completed = true
	}
	if right.err != nil {
		row.Status = StatusError
		row.Error = fmt.Sprintf("right: %s", right.err)
		completed = true
	}
	if !completed {
		switch {
		case !row.LeftExists:
			row.Status = StatusRightOnly
			completed = true
		case !row.RightExists:
			row.Status = StatusLeftOnly
			completed = true
		case row.LeftType != row.RightType:
			row.Status = StatusTypeMismatch
			completed = true
		}
	}
	if !completed {
		if cached, ok := session.cachedStatus(row.ID); ok {
			row.Status = cached.status
			row.Error = cached.err
			completed = true
		}
	}
	return &folderNode{row: row, completed: completed}
}

func sortFolderEntryNames(names []string, leftEntries map[string]folderEntry, rightEntries map[string]folderEntry) {
	sort.Slice(names, func(i int, j int) bool {
		leftRank := folderEntrySortRank(leftEntries[names[i]], rightEntries[names[i]])
		rightRank := folderEntrySortRank(leftEntries[names[j]], rightEntries[names[j]])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftName := strings.ToLower(names[i])
		rightName := strings.ToLower(names[j])
		if leftName != rightName {
			return leftName < rightName
		}
		return names[i] < names[j]
	})
}

func folderEntrySortRank(left folderEntry, right folderEntry) int {
	if left.typ == EntryFolder || right.typ == EntryFolder {
		return 0
	}
	if left.typ == EntryFile || right.typ == EntryFile {
		return 1
	}
	if left.typ == EntrySymlink || right.typ == EntrySymlink {
		return 2
	}
	return 3
}

func folderEntryIsExpandable(entry folderEntry) bool {
	return entry.exists && entry.typ == EntryFolder
}

func readFolderEntries(dir string, exists bool) (map[string]folderEntry, error) {
	entries := map[string]folderEntry{}
	if !exists {
		return entries, nil
	}
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range dirEntries {
		path := filepath.Join(dir, entry.Name())
		typ := entryType(entry)
		entries[entry.Name()] = folderEntry{
			name:   entry.Name(),
			path:   path,
			exists: true,
			typ:    typ,
		}
	}
	return entries, nil
}

func missingFolderEntry(name string, path string) folderEntry {
	return folderEntry{name: name, path: path, typ: EntryNone}
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

func (session *folderSession) start() {
	ctx, cancel := context.WithCancel(context.Background())
	session.ctx = ctx
	session.cancel = cancel
	session.startComparing(session.nodes)
}

func (session *folderSession) stop() {
	if session.cancel != nil {
		session.cancel()
	}
}

func (session *folderSession) startComparing(nodes []*folderNode) {
	ctx := session.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	compareNodes := append([]*folderNode(nil), nodes...)
	go session.run(ctx, compareNodes)
}

func (session *folderSession) run(ctx context.Context, nodes []*folderNode) {
	if len(nodes) == 0 {
		return
	}
	workerCount := runtime.NumCPU()
	if workerCount < 2 {
		workerCount = 2
	}
	if workerCount > 8 {
		workerCount = 8
	}

	jobs := make(chan *folderNode)
	outcomes := make(chan folderCompareOutcome)
	var wg sync.WaitGroup
	for worker := 0; worker < workerCount; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for node := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				outcome := session.compareFolderNode(node)
				select {
				case outcomes <- outcome:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, node := range nodes {
			if node.completed {
				continue
			}
			select {
			case jobs <- node:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		wg.Wait()
		close(outcomes)
	}()

	for outcome := range outcomes {
		session.applyOutcome(outcome)
	}
}

func (session *folderSession) expand(nodeID string) (FolderComparisonResult, error) {
	session.mu.Lock()
	node := session.byID[nodeID]
	if node == nil {
		session.mu.Unlock()
		return FolderComparisonResult{}, fmt.Errorf("folder node not found: %s", nodeID)
	}
	if !node.row.HasChildren {
		result := FolderComparisonResult{LeftRoot: session.leftRoot, RightRoot: session.rightRoot, Revision: session.revision, Rows: session.rowsLocked()}
		session.mu.Unlock()
		return result, nil
	}
	if node.childrenLoaded {
		result := FolderComparisonResult{LeftRoot: session.leftRoot, RightRoot: session.rightRoot, Revision: session.revision, Rows: session.rowsLocked()}
		session.mu.Unlock()
		return result, nil
	}
	leftExists := node.row.LeftExists && node.row.LeftType == EntryFolder
	rightExists := node.row.RightExists && node.row.RightType == EntryFolder
	leftDir := node.row.LeftPath
	rightDir := node.row.RightPath
	parentRel := filepath.FromSlash(node.row.ID)
	childDepth := node.row.Depth + 1
	session.mu.Unlock()

	children, err := session.buildChildNodes(node, leftDir, rightDir, parentRel, childDepth, leftExists, rightExists)
	if err != nil {
		return FolderComparisonResult{}, err
	}

	session.mu.Lock()
	if node.childrenLoaded {
		result := FolderComparisonResult{LeftRoot: session.leftRoot, RightRoot: session.rightRoot, Revision: session.revision, Rows: session.rowsLocked()}
		session.mu.Unlock()
		return result, nil
	}
	insertAt := session.descendantEndIndexLocked(node) + 1
	for index, child := range children {
		session.byID[child.row.ID] = child
		session.nodes = append(session.nodes, nil)
		copy(session.nodes[insertAt+index+1:], session.nodes[insertAt+index:])
		session.nodes[insertAt+index] = child
	}
	node.childrenLoaded = true
	rows := session.rowsLocked()
	session.mu.Unlock()

	session.startComparing(children)
	return FolderComparisonResult{LeftRoot: session.leftRoot, RightRoot: session.rightRoot, Revision: session.revision, Rows: rows}, nil
}

func (session *folderSession) refreshNode(nodeID string) (FolderComparisonResult, error) {
	session.mu.Lock()
	node := session.byID[nodeID]
	if node == nil {
		session.mu.Unlock()
		return FolderComparisonResult{}, fmt.Errorf("folder node not found: %s", nodeID)
	}
	if !folderEntryIsExpandable(folderEntry{exists: node.row.LeftExists, typ: node.row.LeftType}) &&
		!folderEntryIsExpandable(folderEntry{exists: node.row.RightExists, typ: node.row.RightType}) {
		session.mu.Unlock()
		return FolderComparisonResult{}, fmt.Errorf("folder node is not refreshable: %s", nodeID)
	}
	for _, current := range session.nodes {
		if !current.completed {
			session.mu.Unlock()
			return FolderComparisonResult{}, fmt.Errorf("wait for the current folder comparison to finish before refreshing")
		}
	}

	loaded := map[string]bool{}
	for _, current := range session.nodes {
		if current.childrenLoaded && (current == node || strings.HasPrefix(current.row.ID, nodeID+"/")) {
			loaded[current.row.ID] = true
		}
	}
	leftExists := node.row.LeftExists && node.row.LeftType == EntryFolder
	rightExists := node.row.RightExists && node.row.RightType == EntryFolder
	leftDir := node.row.LeftPath
	rightDir := node.row.RightPath
	parentRel := filepath.FromSlash(node.row.ID)
	childDepth := node.row.Depth + 1

	kept := session.nodes[:0]
	for _, current := range session.nodes {
		if current != node && strings.HasPrefix(current.row.ID, nodeID+"/") {
			delete(session.byID, current.row.ID)
			continue
		}
		kept = append(kept, current)
	}
	session.nodes = kept
	node.childrenLoaded = false
	session.resetNodeStatusLocked(node)
	session.mu.Unlock()

	session.invalidateCachedSubtree(nodeID)

	children := []*folderNode{}
	if loaded[nodeID] {
		var err error
		children, err = session.buildLoadedDescendants(node, leftDir, rightDir, parentRel, childDepth, leftExists, rightExists, loaded)
		if err != nil {
			return FolderComparisonResult{}, err
		}
	}

	session.mu.Lock()
	insertAt := session.descendantEndIndexLocked(node) + 1
	for index, child := range children {
		session.byID[child.row.ID] = child
		session.nodes = append(session.nodes, nil)
		copy(session.nodes[insertAt+index+1:], session.nodes[insertAt+index:])
		session.nodes[insertAt+index] = child
	}
	node.childrenLoaded = loaded[nodeID]
	rows := session.rowsLocked()
	session.mu.Unlock()

	toCompare := make([]*folderNode, 0, len(children)+1)
	toCompare = append(toCompare, node)
	toCompare = append(toCompare, children...)
	session.startComparing(toCompare)
	return FolderComparisonResult{
		LeftRoot:  session.leftRoot,
		RightRoot: session.rightRoot,
		Revision:  session.revision,
		Rows:      rows,
	}, nil
}

func (session *folderSession) buildLoadedDescendants(parent *folderNode, leftDir string, rightDir string, parentRel string, childDepth int, leftExists bool, rightExists bool, loaded map[string]bool) ([]*folderNode, error) {
	children, err := session.buildChildNodes(parent, leftDir, rightDir, parentRel, childDepth, leftExists, rightExists)
	if err != nil {
		return nil, err
	}
	result := make([]*folderNode, 0, len(children))
	for _, child := range children {
		result = append(result, child)
		if !loaded[child.row.ID] || !child.row.HasChildren {
			continue
		}
		child.childrenLoaded = true
		nested, err := session.buildLoadedDescendants(
			child,
			child.row.LeftPath,
			child.row.RightPath,
			filepath.FromSlash(child.row.ID),
			child.row.Depth+1,
			child.row.LeftExists && child.row.LeftType == EntryFolder,
			child.row.RightExists && child.row.RightType == EntryFolder,
			loaded,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, nested...)
	}
	return result, nil
}

func (session *folderSession) resetNodeStatusLocked(node *folderNode) {
	node.row.Error = ""
	node.row.Status = StatusPending
	node.completed = false
	switch {
	case !node.row.LeftExists:
		node.row.Status = StatusRightOnly
		node.completed = true
	case !node.row.RightExists:
		node.row.Status = StatusLeftOnly
		node.completed = true
	case node.row.LeftType != node.row.RightType:
		node.row.Status = StatusTypeMismatch
		node.completed = true
	}
}

func (session *folderSession) invalidateCachedSubtree(nodeID string) {
	session.cacheMu.Lock()
	for cachedID := range session.cache {
		if cachedID == nodeID || strings.HasPrefix(cachedID, nodeID+"/") {
			delete(session.cache, cachedID)
		}
	}
	session.cacheMu.Unlock()
}

func (session *folderSession) descendantEndIndexLocked(node *folderNode) int {
	index := -1
	for currentIndex, current := range session.nodes {
		if current == node {
			index = currentIndex
			break
		}
	}
	if index == -1 {
		return len(session.nodes) - 1
	}
	for next := index + 1; next < len(session.nodes); next++ {
		if session.nodes[next].row.Depth <= node.row.Depth {
			break
		}
		index = next
	}
	return index
}

func (session *folderSession) compareFolderNode(node *folderNode) folderCompareOutcome {
	row := node.row
	status, errText := session.compareFolderNodeStatus(row)
	session.storeCachedStatus(row.ID, status, errText)
	return folderCompareOutcome{node: node, status: status, err: errText}
}

func (session *folderSession) compareFolderNodeStatus(row FolderComparisonRow) (ComparisonStatus, string) {
	if row.Status == StatusError {
		return StatusError, row.Error
	}
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
		equal, err := filesEqual(row.LeftPath, row.RightPath)
		if err != nil {
			return StatusError, err.Error()
		}
		if equal {
			return StatusEqual, ""
		}
		return StatusDifferent, ""
	case EntryFolder:
		equal, err := session.foldersEqualRecursive(row.LeftPath, row.RightPath, row.ID, 0)
		if err != nil {
			return StatusError, err.Error()
		}
		if equal {
			return StatusEqual, ""
		}
		return StatusDifferent, ""
	case EntrySymlink:
		equal, err := symlinksEqual(row.LeftPath, row.RightPath)
		if err != nil {
			return StatusError, err.Error()
		}
		if equal {
			return StatusEqual, ""
		}
		return StatusDifferent, ""
	case EntryOther:
		return StatusUnknown, "unsupported entry type"
	default:
		return StatusUnknown, ""
	}
}

func (session *folderSession) cachedStatus(nodeID string) (cachedFolderStatus, bool) {
	session.cacheMu.RLock()
	defer session.cacheMu.RUnlock()
	result, ok := session.cache[nodeID]
	return result, ok
}

func (session *folderSession) storeCachedStatus(nodeID string, status ComparisonStatus, errText string) {
	if nodeID == "" || status == StatusPending {
		return
	}
	session.cacheMu.Lock()
	session.cache[nodeID] = cachedFolderStatus{status: status, err: errText}
	session.cacheMu.Unlock()
}

func (session *folderSession) applyOutcome(outcome folderCompareOutcome) {
	session.mu.Lock()
	updates := session.applyNodeStatusLocked(outcome.node, outcome.status, outcome.err)
	session.mu.Unlock()
	session.emitUpdates(updates)
}

func (session *folderSession) applyNodeStatusLocked(node *folderNode, status ComparisonStatus, errText string) []FolderComparisonUpdate {
	if node.completed {
		return nil
	}
	node.completed = true
	return session.setNodeStatusLocked(node, status, errText)
}

func (session *folderSession) setNodeStatusLocked(node *folderNode, status ComparisonStatus, errText string) []FolderComparisonUpdate {
	if status == StatusPending {
		return nil
	}
	if node.row.Status == status && node.row.Error == errText {
		return nil
	}
	node.row.Status = status
	node.row.Error = errText
	return []FolderComparisonUpdate{{
		TabID:    session.id,
		NodeID:   node.row.ID,
		Revision: session.revision,
		Status:   status,
		Error:    errText,
	}}
}

func (session *folderSession) emitUpdates(updates []FolderComparisonUpdate) {
	if session.emit == nil {
		return
	}
	for _, update := range updates {
		if update.TabID == "" {
			continue
		}
		session.emit(update)
	}
}

func symlinksEqual(leftPath string, rightPath string) (bool, error) {
	leftTarget, err := os.Readlink(leftPath)
	if err != nil {
		return false, err
	}
	rightTarget, err := os.Readlink(rightPath)
	if err != nil {
		return false, err
	}
	return leftTarget == rightTarget, nil
}

func (session *folderSession) foldersEqualRecursive(leftPath string, rightPath string, nodeID string, seen int) (bool, error) {
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

	leftByName := make(map[string]os.DirEntry, len(leftEntries))
	for _, entry := range leftEntries {
		leftByName[entry.Name()] = entry
	}
	for _, rightEntry := range rightEntries {
		childID := path.Join(nodeID, rightEntry.Name())
		leftEntry, ok := leftByName[rightEntry.Name()]
		if !ok {
			session.storeCachedStatus(childID, StatusRightOnly, "")
			return false, nil
		}
		leftType := entryType(leftEntry)
		rightType := entryType(rightEntry)
		if leftType != rightType {
			session.storeCachedStatus(childID, StatusTypeMismatch, "")
			return false, nil
		}
		leftChild := filepath.Join(leftPath, rightEntry.Name())
		rightChild := filepath.Join(rightPath, rightEntry.Name())
		equal, err := session.entriesEqual(leftChild, rightChild, childID, leftType, seen+len(leftEntries)+len(rightEntries))
		status := StatusEqual
		errText := ""
		if !equal {
			status = StatusDifferent
		}
		if err != nil {
			status = StatusError
			errText = err.Error()
		}
		session.storeCachedStatus(childID, status, errText)
		if err != nil || !equal {
			return equal, err
		}
	}
	return true, nil
}

func (session *folderSession) entriesEqual(leftPath string, rightPath string, nodeID string, typ EntryType, seen int) (bool, error) {
	switch typ {
	case EntryFile:
		return filesEqual(leftPath, rightPath)
	case EntryFolder:
		return session.foldersEqualRecursive(leftPath, rightPath, nodeID, seen)
	case EntrySymlink:
		return symlinksEqual(leftPath, rightPath)
	default:
		return false, nil
	}
}

func filesEqual(leftPath string, rightPath string) (bool, error) {
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
	return sameFileBytes(leftPath, rightPath)
}

func sameFileBytes(leftPath string, rightPath string) (bool, error) {
	left, err := os.Open(leftPath)
	if err != nil {
		return false, err
	}
	defer left.Close()
	right, err := os.Open(rightPath)
	if err != nil {
		return false, err
	}
	defer right.Close()

	leftBuffer := fileCompareBufferPool.Get().([]byte)
	rightBuffer := fileCompareBufferPool.Get().([]byte)
	defer fileCompareBufferPool.Put(leftBuffer)
	defer fileCompareBufferPool.Put(rightBuffer)
	for {
		leftN, leftErr := io.ReadFull(left, leftBuffer)
		rightN, rightErr := io.ReadFull(right, rightBuffer)
		if leftN != rightN || !bytes.Equal(leftBuffer[:leftN], rightBuffer[:rightN]) {
			return false, nil
		}
		if leftErr == io.EOF || leftErr == io.ErrUnexpectedEOF {
			return rightErr == io.EOF || rightErr == io.ErrUnexpectedEOF, nil
		}
		if leftErr != nil {
			return false, leftErr
		}
		if rightErr != nil {
			return false, rightErr
		}
	}
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
