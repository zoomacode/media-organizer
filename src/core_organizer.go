package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OrganizeIntoAlbums groups media files into albums
func OrganizeIntoAlbums(files []*MediaFile, config *Config, progressChan chan<- string, albumCache *AlbumSuggestionCache) ([]*Album, error) {
	// Group by source directory and type
	byDirectory := make(map[string][]*MediaFile)

	for _, mf := range files {
		if mf.Type == TypeMusic {
			continue // Handle music separately
		}

		sourceDir := filepath.Dir(mf.Path)
		byDirectory[sourceDir] = append(byDirectory[sourceDir], mf)
	}

	var albums []*Album
	albumsByName := make(map[string]*Album)

	ollamaAvailable := CheckOllamaAvailable()
	if !ollamaAvailable && progressChan != nil {
		progressChan <- "Ollama not available, using folder names"
	}

	// Process each directory group
	for sourceDir, dirFiles := range byDirectory {
		if len(dirFiles) < 3 {
			continue // Skip directories with very few files
		}

		if progressChan != nil {
			progressChan <- fmt.Sprintf("Processing: %s (%d files)", sourceDir, len(dirFiles))
		}

		// Extract dates from files
		var dates []time.Time
		for _, mf := range dirFiles {
			if mf.DateTaken != nil {
				dates = append(dates, *mf.DateTaken)
			}
		}

		var medianDate *time.Time
		var yearMonth string

		if len(dates) > 0 {
			sort.Slice(dates, func(i, j int) bool {
				return dates[i].Before(dates[j])
			})
			median := dates[len(dates)/2]
			medianDate = &median
			yearMonth = median.Format("2006-01")
		} else {
			yearMonth = "Unknown Date"
		}

		// Suggest album name
		var albumName string
		if ollamaAvailable {
			samplePaths := make([]string, 0, 5)
			for i := 0; i < len(dirFiles) && i < 5; i++ {
				samplePaths = append(samplePaths, dirFiles[i].Path)
			}

			// Try cache first
			cached := false
			if albumCache != nil {
				if suggestion, ok := albumCache.Get(sourceDir, samplePaths); ok {
					albumName = suggestion
					cached = true
				}
			}

			// Call Ollama if not cached
			if !cached {
				suggested, err := SuggestAlbumName(config.OllamaModel, sourceDir, samplePaths)
				if err == nil && suggested != "" {
					albumName = suggested
					// Cache the suggestion
					if albumCache != nil {
						albumCache.Put(sourceDir, samplePaths, albumName)
					}
				} else {
					albumName = fallbackAlbumName(sourceDir, yearMonth)
				}
			}
		} else {
			albumName = fallbackAlbumName(sourceDir, yearMonth)
		}

		if progressChan != nil {
			progressChan <- fmt.Sprintf("  → Album: %s", albumName)
		}

		// Determine destination
		year := "Unknown"
		if medianDate != nil {
			year = fmt.Sprintf("%d", medianDate.Year())
		}

		var destDir string
		if dirFiles[0].Type == TypePhoto {
			destDir = filepath.Join(config.LibraryBase, "Photos", year, albumName)
		} else {
			destDir = filepath.Join(config.LibraryBase, "Videos", year, albumName)
		}

		// Merge into existing album if same name
		if existing, ok := albumsByName[albumName]; ok {
			existing.Files = append(existing.Files, dirFiles...)
			existing.SourceDirs = append(existing.SourceDirs, sourceDir)
		} else {
			album := &Album{
				Name:        albumName,
				Destination: destDir,
				Files:       dirFiles,
				SourceDirs:  []string{sourceDir},
				Date:        medianDate,
				Type:        dirFiles[0].Type,
			}
			albums = append(albums, album)
			albumsByName[albumName] = album
		}
	}

	// Handle music files
	musicAlbums := organizeMusicFiles(files, config)
	albums = append(albums, musicAlbums...)

	// Filter albums to only include those with new files
	albums = filterAlbumsWithNewFiles(albums)

	return albums, nil
}

// filterAlbumsWithNewFiles returns only albums that contain new files
func filterAlbumsWithNewFiles(albums []*Album) []*Album {
	var filtered []*Album
	for _, album := range albums {
		hasNewFiles := false
		var newFiles []*MediaFile
		for _, file := range album.Files {
			// Check if file is new OR if it needs to be moved (not already at destination)
			destPath := filepath.Join(album.Destination, filepath.Base(file.Path))
			if file.IsNew || file.Path != destPath {
				hasNewFiles = true
				newFiles = append(newFiles, file)
			}
		}
		if hasNewFiles {
			// Create a copy of the album with only new files
			filteredAlbum := &Album{
				Name:        album.Name,
				Destination: album.Destination,
				Files:       newFiles,
				SourceDirs:  album.SourceDirs,
				Date:        album.Date,
				Type:        album.Type,
			}
			filtered = append(filtered, filteredAlbum)
		}
	}
	return filtered
}

// fallbackAlbumName creates a fallback album name from directory
func fallbackAlbumName(sourceDir, yearMonth string) string {
	dirName := filepath.Base(sourceDir)

	// Clean up common patterns
	dirName = strings.ReplaceAll(dirName, "_____", "")
	dirName = strings.TrimSpace(dirName)

	if dirName == "" || dirName == "." {
		return fmt.Sprintf("%s Photos", yearMonth)
	}

	return fmt.Sprintf("%s %s", yearMonth, dirName)
}

// organizeMusicFiles organizes music files by artist/album
func organizeMusicFiles(files []*MediaFile, config *Config) []*Album {
	byAlbum := make(map[string][]*MediaFile)

	for _, mf := range files {
		if mf.Type != TypeMusic {
			continue
		}

		artist := mf.Artist
		if artist == "" {
			artist = "Unknown Artist"
		}

		album := mf.Album
		if album == "" {
			album = "Unknown Album"
		}

		key := fmt.Sprintf("%s - %s", artist, album)
		byAlbum[key] = append(byAlbum[key], mf)
	}

	var albums []*Album
	for name, files := range byAlbum {
		parts := strings.SplitN(name, " - ", 2)
		artist, albumName := parts[0], parts[1]

		destDir := filepath.Join(config.LibraryBase, "Music", artist, albumName)

		albums = append(albums, &Album{
			Name:        name,
			Destination: destDir,
			Files:       files,
			SourceDirs:  []string{"various"},
			Type:        TypeMusic,
		})
	}

	return albums
}
