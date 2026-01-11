package main

import (
	"time"
)

// MediaType represents the type of media file
type MediaType int

const (
	TypePhoto MediaType = iota
	TypeVideo
	TypeMusic
	TypeUnknown
)

func (mt MediaType) String() string {
	return [...]string{"Photo", "Video", "Music", "Unknown"}[mt]
}

// MediaFile represents a media file with metadata
type MediaFile struct {
	Path         string
	Size         int64
	Hash         string
	Type         MediaType
	DateTaken    *time.Time
	CameraMake   string
	CameraModel  string
	Artist       string
	Album        string
	Title        string
	Width        int
	Height       int
}

// Album represents a collection of media files
type Album struct {
	Name        string
	Destination string
	Files       []*MediaFile
	SourceDirs  []string
	Date        *time.Time
	Type        MediaType
}

// DuplicateGroup represents a group of duplicate files
type DuplicateGroup struct {
	Hash  string
	Files []*MediaFile
	Best  *MediaFile
}

// ScanProgress tracks scanning progress
type ScanProgress struct {
	TotalFiles    int
	ProcessedFiles int
	PhotosFound   int
	VideosFound   int
	MusicFound    int
	CurrentFile   string
}

// Config holds application configuration
type Config struct {
	ScanPath        string
	LibraryBase     string
	DuplicatesTrash string
	OllamaModel     string
	DryRun          bool
	FileLimit       int
	Workers         int
	PruneCache      bool
}
