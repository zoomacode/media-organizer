package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ExecuteOrganization moves files to their organized destinations
func ExecuteOrganization(albums []*Album, duplicates []*DuplicateGroup, config *Config, progressChan chan<- ScanProgress, cache *Cache) error {
	var moved, failed int
	totalFiles := 0

	// Count total files
	for _, album := range albums {
		totalFiles += len(album.Files)
	}
	for _, group := range duplicates {
		totalFiles += len(group.Files) - 1 // Exclude best duplicate
	}

	processed := 0

	// Move album files
	for _, album := range albums {
		// Create destination directory
		if err := os.MkdirAll(album.Destination, 0755); err != nil {
			return fmt.Errorf("create album dir %s: %w", album.Destination, err)
		}

		for _, file := range album.Files {
			destPath := filepath.Join(album.Destination, filepath.Base(file.Path))

			// Handle filename conflicts
			destPath = ensureUniqueFilename(destPath)

			// Move file
			if err := moveFile(file.Path, destPath); err != nil {
				fmt.Printf("  ✗ Failed to move %s: %v\n", file.Path, err)
				failed++
			} else {
				moved++

				// Update cache with new path (so duplicate detection works on next run)
				if cache != nil {
					// Update the file's path for cache update
					oldPath := file.Path
					file.Path = destPath
					if info, err := os.Stat(destPath); err == nil {
						cache.UpdatePath(oldPath, file, info.ModTime())
					}
				}
			}

			processed++
			if progressChan != nil {
				select {
				case progressChan <- ScanProgress{
					ProcessedFiles: processed,
					TotalFiles:     totalFiles,
					CurrentFile:    file.Path,
				}:
				default:
				}
			}
		}
	}

	// Move duplicates to trash
	if len(duplicates) > 0 {
		trashDir := config.DuplicatesTrash
		if err := os.MkdirAll(trashDir, 0755); err != nil {
			return fmt.Errorf("create trash dir: %w", err)
		}

		for _, group := range duplicates {
			for _, file := range group.Files {
				// Skip the best duplicate
				if file == group.Best {
					continue
				}

				// Preserve directory structure in trash
				relPath, _ := filepath.Rel(config.ScanPath, file.Path)
				trashPath := filepath.Join(trashDir, relPath)

				// Create parent directories
				if err := os.MkdirAll(filepath.Dir(trashPath), 0755); err != nil {
					fmt.Printf("  ✗ Failed to create trash dir for %s: %v\n", file.Path, err)
					failed++
					continue
				}

				// Move to trash
				if err := moveFile(file.Path, trashPath); err != nil {
					fmt.Printf("  ✗ Failed to trash %s: %v\n", file.Path, err)
					failed++
				} else {
					moved++
				}

				processed++
				if progressChan != nil {
					select {
					case progressChan <- ScanProgress{
						ProcessedFiles: processed,
						TotalFiles:     totalFiles,
						CurrentFile:    file.Path,
					}:
					default:
					}
				}
			}
		}
	}

	fmt.Printf("\nExecution complete: %d files moved, %d failed\n", moved, failed)
	return nil
}

// moveFile moves a file, with fallback to copy+delete if cross-device
func moveFile(src, dst string) error {
	// Try rename first (fast, atomic)
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// If rename fails (probably cross-device), copy then delete
	if err := copyFile(src, dst); err != nil {
		return fmt.Errorf("copy: %w", err)
	}

	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove source: %w", err)
	}

	return nil
}

// copyFile copies a file preserving permissions
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return dstFile.Sync()
}

// ensureUniqueFilename adds a counter if file exists
func ensureUniqueFilename(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := filepath.Base(path)
	name := base[:len(base)-len(ext)]

	for i := 1; ; i++ {
		newPath := filepath.Join(dir, fmt.Sprintf("%s_%d%s", name, i, ext))
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
			return newPath
		}
	}
}
