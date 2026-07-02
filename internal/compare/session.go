package compare

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxDisplayFileSize = 2 * 1024 * 1024

type fileSession struct {
	id           string
	leftPath     string
	rightPath    string
	left         []textLine
	right        []textLine
	leftDirty    bool
	rightDirty   bool
	leftModTime  time.Time
	rightModTime time.Time
	warning      string
}

type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]*fileSession
}

func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: map[string]*fileSession{}}
}

func (s *SessionStore) Open(tabID string, leftPath string, rightPath string) (FileComparisonResult, error) {
	if tabID == "" {
		return FileComparisonResult{}, fmt.Errorf("tab ID is required")
	}

	leftLines, leftInfo, leftWarning, err := readTextFile(leftPath)
	if err != nil {
		return FileComparisonResult{}, fmt.Errorf("left file: %w", err)
	}
	rightLines, rightInfo, rightWarning, err := readTextFile(rightPath)
	if err != nil {
		return FileComparisonResult{}, fmt.Errorf("right file: %w", err)
	}

	warning := joinWarnings(leftWarning, rightWarning)
	session := &fileSession{
		id:           tabID,
		leftPath:     leftPath,
		rightPath:    rightPath,
		left:         leftLines,
		right:        rightLines,
		leftModTime:  leftInfo.ModTime(),
		rightModTime: rightInfo.ModTime(),
		warning:      warning,
	}

	s.mu.Lock()
	s.sessions[tabID] = session
	s.mu.Unlock()

	return session.result(), nil
}

func (s *SessionStore) Refresh(tabID string) (FileComparisonResult, error) {
	session, err := s.get(tabID)
	if err != nil {
		return FileComparisonResult{}, err
	}
	if session.leftDirty || session.rightDirty {
		return session.result(), nil
	}
	return s.Open(tabID, session.leftPath, session.rightPath)
}

func (s *SessionStore) ApplyLinesLeftToRight(tabID string, startRow int, endRow int) (FileComparisonResult, error) {
	session, err := s.get(tabID)
	if err != nil {
		return FileComparisonResult{}, err
	}
	rows := session.result().Rows
	startRow, endRow, err = normalizeRange(startRow, endRow, len(rows))
	if err != nil {
		return FileComparisonResult{}, err
	}

	for i := endRow; i >= startRow; i-- {
		row := rows[i]
		switch {
		case row.LeftIndex != nil && row.RightIndex != nil:
			session.right[*row.RightIndex] = session.left[*row.LeftIndex]
		case row.LeftIndex != nil && row.RightIndex == nil:
			session.right = insertLine(session.right, row.RightInsertIndex, session.left[*row.LeftIndex])
		case row.LeftIndex == nil && row.RightIndex != nil:
			session.right = deleteLine(session.right, *row.RightIndex)
		}
	}
	session.rightDirty = true
	return session.result(), nil
}

func (s *SessionStore) ApplyLinesRightToLeft(tabID string, startRow int, endRow int) (FileComparisonResult, error) {
	session, err := s.get(tabID)
	if err != nil {
		return FileComparisonResult{}, err
	}
	rows := session.result().Rows
	startRow, endRow, err = normalizeRange(startRow, endRow, len(rows))
	if err != nil {
		return FileComparisonResult{}, err
	}

	for i := endRow; i >= startRow; i-- {
		row := rows[i]
		switch {
		case row.LeftIndex != nil && row.RightIndex != nil:
			session.left[*row.LeftIndex] = session.right[*row.RightIndex]
		case row.RightIndex != nil && row.LeftIndex == nil:
			session.left = insertLine(session.left, row.LeftInsertIndex, session.right[*row.RightIndex])
		case row.RightIndex == nil && row.LeftIndex != nil:
			session.left = deleteLine(session.left, *row.LeftIndex)
		}
	}
	session.leftDirty = true
	return session.result(), nil
}

func (s *SessionStore) Save(tabID string, side string) (FileComparisonResult, error) {
	session, err := s.get(tabID)
	if err != nil {
		return FileComparisonResult{}, err
	}

	switch side {
	case "left":
		if err := session.saveSide(session.leftPath, session.left, session.leftModTime); err != nil {
			return FileComparisonResult{}, err
		}
		info, err := os.Stat(session.leftPath)
		if err != nil {
			return FileComparisonResult{}, err
		}
		session.leftModTime = info.ModTime()
		session.leftDirty = false
	case "right":
		if err := session.saveSide(session.rightPath, session.right, session.rightModTime); err != nil {
			return FileComparisonResult{}, err
		}
		info, err := os.Stat(session.rightPath)
		if err != nil {
			return FileComparisonResult{}, err
		}
		session.rightModTime = info.ModTime()
		session.rightDirty = false
	case "both":
		if session.leftDirty {
			if _, err := s.Save(tabID, "left"); err != nil {
				return FileComparisonResult{}, err
			}
		}
		if session.rightDirty {
			if _, err := s.Save(tabID, "right"); err != nil {
				return FileComparisonResult{}, err
			}
		}
	default:
		return FileComparisonResult{}, fmt.Errorf("unknown save side: %s", side)
	}
	return session.result(), nil
}

func (s *SessionStore) ReplaceText(tabID string, side string, text string) (FileComparisonResult, error) {
	session, err := s.get(tabID)
	if err != nil {
		return FileComparisonResult{}, err
	}

	switch side {
	case "left":
		session.left = splitLines([]byte(text))
		session.leftDirty = true
	case "right":
		session.right = splitLines([]byte(text))
		session.rightDirty = true
	default:
		return FileComparisonResult{}, fmt.Errorf("unknown side: %s", side)
	}
	return session.result(), nil
}

func (s *SessionStore) Discard(tabID string) (FileComparisonResult, error) {
	session, err := s.get(tabID)
	if err != nil {
		return FileComparisonResult{}, err
	}
	return s.Open(tabID, session.leftPath, session.rightPath)
}

func (s *SessionStore) Close(tabID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tabID)
}

func (s *SessionStore) get(tabID string) (*fileSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[tabID]
	if !ok {
		return nil, fmt.Errorf("invalid tab/session: %s", tabID)
	}
	return session, nil
}

func (session *fileSession) result() FileComparisonResult {
	return buildRows(session.left, session.right, session.leftDirty, session.rightDirty, session.leftPath, session.rightPath, session.warning)
}

func (session *fileSession) saveSide(path string, lines []textLine, knownModTime time.Time) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("save target: %w", err)
	}
	if !info.ModTime().Equal(knownModTime) {
		return fmt.Errorf("file changed on disk since opening: %s", path)
	}
	return os.WriteFile(path, linesToBytes(lines), info.Mode().Perm())
}

func readTextFile(path string) ([]textLine, os.FileInfo, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, "", fmt.Errorf("not a regular file: %s", path)
	}
	if info.Size() > maxDisplayFileSize {
		return nil, nil, "", fmt.Errorf("file is too large to display comfortably: %s", path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, "", err
	}
	if isBinary(data) {
		return nil, nil, "", fmt.Errorf("binary file detected: %s", path)
	}
	if !utf8.Valid(data) {
		return nil, nil, "", fmt.Errorf("file is not valid UTF-8: %s", path)
	}

	warning := ""
	if info.Size() > maxDisplayFileSize/2 {
		warning = fmt.Sprintf("Large file loaded: %s", path)
	}
	return splitLines(data), info, warning, nil
}

func splitLines(data []byte) []textLine {
	if len(data) == 0 {
		return []textLine{}
	}
	lines := []textLine{}
	start := 0
	for i := 0; i < len(data); i++ {
		if data[i] != '\n' {
			continue
		}
		end := i
		ending := "\n"
		if i > start && data[i-1] == '\r' {
			end = i - 1
			ending = "\r\n"
		}
		lines = append(lines, textLine{Text: string(data[start:end]), Ending: ending})
		start = i + 1
	}
	if start < len(data) {
		lines = append(lines, textLine{Text: string(data[start:]), Ending: ""})
	}
	return lines
}

func linesToBytes(lines []textLine) []byte {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line.Text)
		builder.WriteString(line.Ending)
	}
	return []byte(builder.String())
}

func isBinary(data []byte) bool {
	sample := data
	if len(sample) > 8000 {
		sample = sample[:8000]
	}
	return bytes.IndexByte(sample, 0) >= 0
}

func joinWarnings(values ...string) string {
	warnings := []string{}
	for _, value := range values {
		if value != "" {
			warnings = append(warnings, value)
		}
	}
	return strings.Join(warnings, "\n")
}

func normalizeRange(startRow int, endRow int, length int) (int, int, error) {
	if length == 0 {
		return 0, 0, fmt.Errorf("no rows to select")
	}
	if startRow > endRow {
		startRow, endRow = endRow, startRow
	}
	if startRow < 0 || endRow >= length {
		return 0, 0, fmt.Errorf("selection out of range")
	}
	return startRow, endRow, nil
}

func insertLine(lines []textLine, index int, line textLine) []textLine {
	if index < 0 {
		index = 0
	}
	if index > len(lines) {
		index = len(lines)
	}
	lines = append(lines, textLine{})
	copy(lines[index+1:], lines[index:])
	lines[index] = line
	return lines
}

func deleteLine(lines []textLine, index int) []textLine {
	if index < 0 || index >= len(lines) {
		return lines
	}
	return append(lines[:index], lines[index+1:]...)
}
