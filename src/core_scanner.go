package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	photoExtensions = map[string]bool{
		".jpg": true, ".jpeg": true, ".jpe": true, ".png": true,
		".tiff": true, ".tif": true, ".heic": true, ".heif": true,
		".raw": true, ".cr2": true, ".nef": true, ".arw": true,
	}

	videoExtensions = map[string]bool{
		".mp4": true, ".mov": true, ".avi": true, ".wmv": true,
		".mkv": true, ".m4v": true, ".mpg": true, ".mpeg": true,
		".flv": true, ".3gp": true, ".mts": true, ".m2ts": true,
	}

	musicExtensions = map[string]bool{
		".mp3": true, ".m4a": true, ".flac": true, ".wav": true,
		".aac": true, ".ogg": true, ".wma": true, ".alac": true,
	}

	excludePatterns = []string{
		"/.Trash/", "/.Thumbnails/", "/Thumbnails/",
		"/.deleted_media/", "/.duplicates-trash/",
		"/System/", "/Library/", "/Applications/",
		"/.config/", "/retropie/", "/OFFICE/",
		"/Template/", "/Software/", "/Windows/",
		"/Program Files/",
	}
)

// detectMediaType detects the type of media file from extension
func detectMediaType(path string) MediaType {
	ext := strings.ToLower(filepath.Ext(path))

	if photoExtensions[ext] {
		return TypePhoto
	}
	if videoExtensions[ext] {
		return TypeVideo
	}
	if musicExtensions[ext] {
		return TypeMusic
	}
	return TypeUnknown
}

// shouldExclude checks if a path should be excluded
func shouldExclude(path string) bool {
	for _, pattern := range excludePatterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}

// ScanMediaFiles scans directory for media files using parallel workers
func ScanMediaFiles(basePath string, limit int, progressChan chan<- ScanProgress) ([]*MediaFile, error) {
	var (
		files  []*MediaFile
		mu     sync.Mutex
		count  int
		photos int
		videos int
		music  int
	)

	// Walk directory and collect paths
	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		if info.IsDir() {
			if shouldExclude(path) {
				return filepath.SkipDir
			}
			return nil
		}

		// Check if it's a media file
		mediaType := detectMediaType(path)
		if mediaType == TypeUnknown {
			return nil
		}

		if shouldExclude(path) {
			return nil
		}

		// Apply limit
		mu.Lock()
		if limit > 0 && count >= limit {
			mu.Unlock()
			return filepath.SkipDir
		}
		count++
		mu.Unlock()

		// Create MediaFile
		mf := &MediaFile{
			Path: path,
			Size: info.Size(),
			Type: mediaType,
		}

		mu.Lock()
		files = append(files, mf)
		switch mediaType {
		case TypePhoto:
			photos++
		case TypeVideo:
			videos++
		case TypeMusic:
			music++
		}

		// Send progress update
		if progressChan != nil {
			select {
			case progressChan <- ScanProgress{
				TotalFiles:     count,
				ProcessedFiles: count,
				PhotosFound:    photos,
				VideosFound:    videos,
				MusicFound:     music,
				CurrentFile:    path,
			}:
			default:
			}
		}
		mu.Unlock()

		return nil
	})

	if err != nil {
		return nil, err
	}

	return files, nil
}

// ProcessMetadata extracts metadata from files in parallel
func ProcessMetadata(files []*MediaFile, workers int, progressChan chan<- ScanProgress, cache *Cache) int {
	var wg sync.WaitGroup
	fileChan := make(chan *MediaFile, len(files))
	cacheHits := 0
	processed := 0
	var mu sync.Mutex

	// Start worker pool
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for mf := range fileChan {
				// Try cache first
				cached := false
				if cache != nil {
					info, err := os.Stat(mf.Path)
					if err == nil {
						if cf, ok := cache.Get(mf.Path, mf.Size, info.ModTime()); ok {
							// Use cached metadata
							mf.DateTaken = cf.DateTaken
							mf.CameraMake = cf.CameraMake
							mf.CameraModel = cf.CameraModel
							mf.Artist = cf.Artist
							mf.Album = cf.Album
							mf.Title = cf.Title
							mf.Width = cf.Width
							mf.Height = cf.Height
							mf.IsNew = false // File was in cache
							cached = true
							mu.Lock()
							cacheHits++
							mu.Unlock()
						}
					}
				}

				// Extract if not cached
				if !cached {
					mf.IsNew = true // New file, not in cache
					extractMetadata(mf)

					// Store in cache (queued asynchronously)
					if cache != nil {
						if info, err := os.Stat(mf.Path); err == nil {
							cache.Put(mf, info.ModTime())
						}
					}
				}

				mu.Lock()
				processed++
				if progressChan != nil {
					select {
					case progressChan <- ScanProgress{
						ProcessedFiles: processed,
						TotalFiles:     len(files),
						CurrentFile:    mf.Path,
					}:
					default:
					}
				}
				mu.Unlock()
			}
		}()
	}

	// Send files to workers
	for _, mf := range files {
		fileChan <- mf
	}
	close(fileChan)

	wg.Wait()
	return cacheHits
}
