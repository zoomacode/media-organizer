package main

import (
	"crypto/md5"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
)

// CalculateHashes calculates MD5 hashes for all files in parallel
func CalculateHashes(files []*MediaFile, workers int, progressChan chan<- ScanProgress, cache *Cache) int {
	var wg sync.WaitGroup
	fileChan := make(chan *MediaFile, len(files))
	processed := 0
	cacheHits := 0
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
						if cf, ok := cache.Get(mf.Path, mf.Size, info.ModTime()); ok && cf.Hash != "" {
							mf.Hash = cf.Hash
							cached = true
							mu.Lock()
							cacheHits++
							mu.Unlock()
						}
					}
				}

				// Calculate if not cached
				if !cached {
					hash, err := calculateFileHash(mf.Path)
					if err == nil {
						mf.Hash = hash

						// Store in cache (queued asynchronously)
						if cache != nil {
							if info, err := os.Stat(mf.Path); err == nil {
								cache.Put(mf, info.ModTime())
							}
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

// calculateFileHash calculates MD5 hash of a file
func calculateFileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return string(h.Sum(nil)), nil
}

// FindDuplicates groups files by hash and identifies duplicates
func FindDuplicates(files []*MediaFile) []*DuplicateGroup {
	byHash := make(map[string][]*MediaFile)

	for _, mf := range files {
		if mf.Hash == "" {
			continue
		}
		byHash[mf.Hash] = append(byHash[mf.Hash], mf)
	}

	var duplicates []*DuplicateGroup
	for hash, group := range byHash {
		if len(group) > 1 {
			best := chooseBestDuplicate(group)
			duplicates = append(duplicates, &DuplicateGroup{
				Hash:  hash,
				Files: group,
				Best:  best,
			})
		}
	}

	return duplicates
}

// chooseBestDuplicate selects the best version from duplicates
func chooseBestDuplicate(files []*MediaFile) *MediaFile {
	scored := make(map[*MediaFile]int)

	for _, mf := range files {
		score := 0

		// Prefer larger files (better quality)
		score += int(mf.Size / 1024) // KB

		// Prefer non-Recovered paths (+1000000)
		if !strings.Contains(mf.Path, "/Recovered/") {
			score += 1000000
		}

		// Prefer organized paths
		for _, pattern := range []string{"/Photo/", "/Pictures/", "/Video/", "/Music/"} {
			if strings.Contains(mf.Path, pattern) {
				score += 500000
				break
			}
		}

		// Penalize UNNAMED
		if strings.Contains(mf.Path, "/UNNAMED_") {
			score -= 500000
		}

		// Prefer files with more metadata
		if mf.CameraMake != "" {
			score += 10000
		}
		if mf.Album != "" {
			score += 10000
		}

		scored[mf] = score
	}

	// Sort by score
	sort.Slice(files, func(i, j int) bool {
		si, sj := scored[files[i]], scored[files[j]]
		if si != sj {
			return si > sj
		}
		// Tiebreaker: alphabetical
		return files[i].Path < files[j].Path
	})

	return files[0]
}
