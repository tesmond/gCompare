package compare

type EntryType string

const (
	EntryNone    EntryType = ""
	EntryFile    EntryType = "file"
	EntryFolder  EntryType = "folder"
	EntrySymlink EntryType = "symlink"
	EntryOther   EntryType = "other"
)

type ComparisonStatus string

const (
	StatusPending      ComparisonStatus = "pending"
	StatusEqual        ComparisonStatus = "equal"
	StatusDifferent    ComparisonStatus = "different"
	StatusLeftOnly     ComparisonStatus = "left_only"
	StatusRightOnly    ComparisonStatus = "right_only"
	StatusTypeMismatch ComparisonStatus = "type_mismatch"
	StatusError        ComparisonStatus = "error"
	StatusUnknown      ComparisonStatus = "unknown"
)

type LineComparisonStatus string

const (
	LineEqual     LineComparisonStatus = "equal"
	LineChanged   LineComparisonStatus = "changed"
	LineLeftOnly  LineComparisonStatus = "left_only"
	LineRightOnly LineComparisonStatus = "right_only"
	LineEmpty     LineComparisonStatus = "empty"
	LineError     LineComparisonStatus = "error"
)

type SemanticState string

const (
	SemanticMatch       SemanticState = "MATCH"
	SemanticUnimportant SemanticState = "UNIMPORTANT_DIFF"
	SemanticImportant   SemanticState = "IMPORTANT_DIFF"
	SemanticOrphanGap   SemanticState = "ORPHAN_GAP"
)

type FolderComparisonResult struct {
	LeftRoot  string                `json:"leftRoot"`
	RightRoot string                `json:"rightRoot"`
	Rows      []FolderComparisonRow `json:"rows"`
}

type FolderComparisonRow struct {
	ID              string           `json:"id"`
	ParentID        string           `json:"parentID,omitempty"`
	RowIndex        int              `json:"rowIndex"`
	Depth           int              `json:"depth"`
	HasChildren     bool             `json:"hasChildren"`
	Name            string           `json:"name"`
	LeftPath        string           `json:"leftPath"`
	RightPath       string           `json:"rightPath"`
	LeftExists      bool             `json:"leftExists"`
	RightExists     bool             `json:"rightExists"`
	LeftType        EntryType        `json:"leftType"`
	RightType       EntryType        `json:"rightType"`
	CanCompareFiles bool             `json:"canCompareFiles"`
	Status          ComparisonStatus `json:"status"`
	Error           string           `json:"error,omitempty"`
}

type FileComparisonResult struct {
	LeftPath   string              `json:"leftPath"`
	RightPath  string              `json:"rightPath"`
	Rows       []FileComparisonRow `json:"rows"`
	LeftDirty  bool                `json:"leftDirty"`
	RightDirty bool                `json:"rightDirty"`
	Error      string              `json:"error,omitempty"`
	Warning    string              `json:"warning,omitempty"`
}

type FileComparisonRow struct {
	RowIndex         int                  `json:"rowIndex"`
	LeftLineNumber   *int                 `json:"leftLineNumber,omitempty"`
	RightLineNumber  *int                 `json:"rightLineNumber,omitempty"`
	LeftText         string               `json:"leftText"`
	RightText        string               `json:"rightText"`
	LeftSegments     []LineTextSegment    `json:"leftSegments,omitempty"`
	RightSegments    []LineTextSegment    `json:"rightSegments,omitempty"`
	SemanticState    SemanticState        `json:"semanticState"`
	LeftSemantic     SemanticState        `json:"leftSemanticState"`
	RightSemantic    SemanticState        `json:"rightSemanticState"`
	Status           LineComparisonStatus `json:"status"`
	LeftIndex        *int                 `json:"leftIndex,omitempty"`
	RightIndex       *int                 `json:"rightIndex,omitempty"`
	LeftInsertIndex  int                  `json:"leftInsertIndex"`
	RightInsertIndex int                  `json:"rightInsertIndex"`
}

type LineTextSegment struct {
	Text        string `json:"text"`
	IsDiffToken bool   `json:"isDiffToken"`
	Changed     bool   `json:"changed"`
}

type textLine struct {
	Text   string
	Ending string
}
