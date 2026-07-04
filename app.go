package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"gcompare/internal/compare"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx            context.Context
	sessions       *compare.SessionStore
	folderSessions *compare.FolderSessionStore
}

func NewApp() *App {
	return &App{
		sessions:       compare.NewSessionStore(),
		folderSessions: compare.NewFolderSessionStore(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.folderSessions.SetUpdateEmitter(func(update compare.FolderComparisonUpdate) {
		wailsRuntime.EventsEmit(ctx, "folder-comparison:update", update)
	})
}

func (a *App) ChooseDirectory() (string, error) {
	return wailsRuntime.OpenDirectoryDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Choose a folder",
	})
}

func (a *App) ChooseFile() (string, error) {
	return wailsRuntime.OpenFileDialog(a.ctx, wailsRuntime.OpenDialogOptions{
		Title: "Choose a file",
	})
}

func (a *App) ChooseSaveFile(defaultFilename string) (string, error) {
	return wailsRuntime.SaveFileDialog(a.ctx, wailsRuntime.SaveDialogOptions{
		Title:                "Save text",
		DefaultFilename:      defaultFilename,
		CanCreateDirectories: true,
	})
}

func (a *App) HomeDirectory() (string, error) {
	return os.UserHomeDir()
}

func (a *App) ListDirectory(path string) (compare.DirectoryListing, error) {
	return compare.ListDirectory(path)
}

func (a *App) PreviewFile(path string) (compare.FilePreview, error) {
	return compare.PreviewFile(path)
}

func (a *App) OpenFolderComparison(tabID string, leftPath string, rightPath string) (compare.FolderComparisonResult, error) {
	return a.folderSessions.Open(tabID, leftPath, rightPath)
}

func (a *App) RefreshFolderComparison(tabID string, leftPath string, rightPath string) (compare.FolderComparisonResult, error) {
	return a.folderSessions.Refresh(tabID, leftPath, rightPath)
}

func (a *App) ExpandFolderComparisonNode(tabID string, nodeID string) (compare.FolderComparisonResult, error) {
	return a.folderSessions.Expand(tabID, nodeID)
}

func (a *App) OpenFileComparison(tabID string, leftPath string, rightPath string) (compare.FileComparisonResult, error) {
	return a.sessions.Open(tabID, leftPath, rightPath)
}

func (a *App) CompareText(leftText string, rightText string) compare.FileComparisonResult {
	return compare.CompareText(leftText, rightText)
}

func (a *App) WriteTextFile(path string, text string) error {
	if path == "" {
		return fmt.Errorf("save path is required")
	}
	return os.WriteFile(path, []byte(text), 0o644)
}

func (a *App) RefreshFileComparison(tabID string) (compare.FileComparisonResult, error) {
	return a.sessions.Refresh(tabID)
}

func (a *App) UpdateFileComparisonText(tabID string, side string, text string) (compare.FileComparisonResult, error) {
	return a.sessions.ReplaceText(tabID, side, text)
}

func (a *App) ApplyLinesLeftToRight(tabID string, startRow int, endRow int) (compare.FileComparisonResult, error) {
	return a.sessions.ApplyLinesLeftToRight(tabID, startRow, endRow)
}

func (a *App) ApplyLinesRightToLeft(tabID string, startRow int, endRow int) (compare.FileComparisonResult, error) {
	return a.sessions.ApplyLinesRightToLeft(tabID, startRow, endRow)
}

func (a *App) SaveFileComparison(tabID string, side string) (compare.FileComparisonResult, error) {
	return a.sessions.Save(tabID, side)
}

func (a *App) DiscardFileChanges(tabID string) (compare.FileComparisonResult, error) {
	return a.sessions.Discard(tabID)
}

func (a *App) CloseFileComparison(tabID string) {
	a.sessions.Close(tabID)
}

func (a *App) CloseFolderComparison(tabID string) {
	a.folderSessions.Close(tabID)
}

func (a *App) CopyFileLeftToRight(leftPath string, rightPath string, overwrite bool) error {
	return compare.CopyFile(leftPath, rightPath, overwrite)
}

func (a *App) CopyFileRightToLeft(rightPath string, leftPath string, overwrite bool) error {
	return compare.CopyFile(rightPath, leftPath, overwrite)
}

func (a *App) RevealPath(path string) error {
	if path == "" {
		return fmt.Errorf("no path to reveal")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}
