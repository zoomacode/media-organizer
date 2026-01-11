package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

type cacheWriteRequest struct {
	mf      *MediaFile
	modTime time.Time
}

type Cache struct {
	db         *sql.DB
	writeChan  chan cacheWriteRequest
	writerDone sync.WaitGroup
}

type CachedFile struct {
	Path        string
	Size        int64
	ModTime     int64
	Hash        string
	DateTaken   *time.Time
	CameraMake  string
	CameraModel string
	Artist      string
	Album       string
	Title       string
	Width       int
	Height      int
	ProcessedAt int64
}

// OpenCache opens or creates the cache database
func OpenCache(libraryBase string) (*Cache, error) {
	cacheDir := filepath.Join(libraryBase, ".media-organizer-cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	dbPath := filepath.Join(cacheDir, "cache.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open cache db: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Set busy timeout to 5 seconds (retry instead of failing immediately)
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// Create table if not exists
	schema := `
	CREATE TABLE IF NOT EXISTS files (
		path TEXT PRIMARY KEY,
		size INTEGER NOT NULL,
		mod_time INTEGER NOT NULL,
		hash TEXT,
		date_taken INTEGER,
		camera_make TEXT,
		camera_model TEXT,
		artist TEXT,
		album TEXT,
		title TEXT,
		width INTEGER,
		height INTEGER,
		processed_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_hash ON files(hash) WHERE hash IS NOT NULL;
	CREATE INDEX IF NOT EXISTS idx_mod_time ON files(mod_time);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	// Create cache with write queue
	cache := &Cache{
		db:        db,
		writeChan: make(chan cacheWriteRequest, 1000), // Buffer for 1000 pending writes
	}

	// Start single writer goroutine to serialize all writes
	cache.writerDone.Add(1)
	go cache.writerLoop()

	return cache, nil
}

// writerLoop handles all database writes in a single thread
func (c *Cache) writerLoop() {
	defer c.writerDone.Done()

	for req := range c.writeChan {
		c.writeToDatabase(req.mf, req.modTime)
	}
}

// Close closes the cache database
func (c *Cache) Close() error {
	// Close write channel and wait for pending writes
	if c.writeChan != nil {
		close(c.writeChan)
		c.writerDone.Wait()
	}

	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// Get retrieves cached file data if valid
func (c *Cache) Get(path string, size int64, modTime time.Time) (*CachedFile, bool) {
	var cf CachedFile
	var dateTakenUnix sql.NullInt64

	err := c.db.QueryRow(`
		SELECT path, size, mod_time, hash, date_taken, camera_make, camera_model,
		       artist, album, title, width, height, processed_at
		FROM files
		WHERE path = ? AND size = ? AND mod_time = ?
	`, path, size, modTime.Unix()).Scan(
		&cf.Path, &cf.Size, &cf.ModTime, &cf.Hash, &dateTakenUnix,
		&cf.CameraMake, &cf.CameraModel, &cf.Artist, &cf.Album, &cf.Title,
		&cf.Width, &cf.Height, &cf.ProcessedAt,
	)

	if err == sql.ErrNoRows {
		return nil, false
	}
	if err != nil {
		return nil, false
	}

	// Convert unix timestamp to time.Time
	if dateTakenUnix.Valid {
		dt := time.Unix(dateTakenUnix.Int64, 0)
		cf.DateTaken = &dt
	}

	return &cf, true
}

// Put queues file data for writing to cache (non-blocking)
func (c *Cache) Put(mf *MediaFile, modTime time.Time) error {
	// Send to write queue (non-blocking if buffer full)
	select {
	case c.writeChan <- cacheWriteRequest{mf: mf, modTime: modTime}:
		return nil
	default:
		// Channel full, skip this write (better than blocking)
		return fmt.Errorf("cache write queue full")
	}
}

// writeToDatabase performs the actual database write (called by writer goroutine)
func (c *Cache) writeToDatabase(mf *MediaFile, modTime time.Time) {
	var dateTakenUnix sql.NullInt64
	if mf.DateTaken != nil {
		dateTakenUnix.Valid = true
		dateTakenUnix.Int64 = mf.DateTaken.Unix()
	}

	_, err := c.db.Exec(`
		INSERT OR REPLACE INTO files
		(path, size, mod_time, hash, date_taken, camera_make, camera_model,
		 artist, album, title, width, height, processed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, mf.Path, mf.Size, modTime.Unix(), mf.Hash, dateTakenUnix,
		mf.CameraMake, mf.CameraModel, mf.Artist, mf.Album, mf.Title,
		mf.Width, mf.Height, time.Now().Unix())

	if err != nil {
		// Log error but don't crash - cache is best-effort
		fmt.Printf("Warning: cache write failed for %s: %v\n", mf.Path, err)
	}
}

// UpdatePath updates cache entry when a file is moved (for duplicate detection)
func (c *Cache) UpdatePath(oldPath string, mf *MediaFile, modTime time.Time) {
	// Delete old cache entry
	c.db.Exec("DELETE FROM files WHERE path = ?", oldPath)

	// Queue write for new path (async)
	c.Put(mf, modTime)
}

// GetStats returns cache statistics
func (c *Cache) GetStats() (total, withHash, withMetadata int64) {
	c.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&total)
	c.db.QueryRow("SELECT COUNT(*) FROM files WHERE hash IS NOT NULL AND hash != ''").Scan(&withHash)
	c.db.QueryRow("SELECT COUNT(*) FROM files WHERE camera_make IS NOT NULL AND camera_make != ''").Scan(&withMetadata)
	return
}

// PruneDeleted removes entries for files that no longer exist
func (c *Cache) PruneDeleted(validPaths map[string]bool) (int64, error) {
	// Get all paths from cache
	rows, err := c.db.Query("SELECT path FROM files")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var toDelete []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			continue
		}
		if !validPaths[path] {
			toDelete = append(toDelete, path)
		}
	}

	// Delete in batches
	if len(toDelete) == 0 {
		return 0, nil
	}

	tx, err := c.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare("DELETE FROM files WHERE path = ?")
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	for _, path := range toDelete {
		if _, err := stmt.Exec(path); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return int64(len(toDelete)), nil
}

// AlbumSuggestionCache stores Ollama suggestions
type AlbumSuggestionCache struct {
	db *sql.DB
}

// OpenAlbumSuggestionCache opens the album suggestion cache
func OpenAlbumSuggestionCache(cache *Cache) (*AlbumSuggestionCache, error) {
	// Create table for album suggestions
	schema := `
	CREATE TABLE IF NOT EXISTS album_suggestions (
		folder_path TEXT PRIMARY KEY,
		sample_files TEXT NOT NULL,
		suggestion TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	`

	if _, err := cache.db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create album suggestion schema: %w", err)
	}

	return &AlbumSuggestionCache{db: cache.db}, nil
}

// Get retrieves cached album suggestion
func (a *AlbumSuggestionCache) Get(folderPath string, sampleFiles []string) (string, bool) {
	var suggestion string
	var cachedSamples string

	err := a.db.QueryRow(`
		SELECT sample_files, suggestion
		FROM album_suggestions
		WHERE folder_path = ?
	`, folderPath).Scan(&cachedSamples, &suggestion)

	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}

	// Verify sample files match (simple check)
	currentSamples, _ := json.Marshal(sampleFiles)
	if cachedSamples != string(currentSamples) {
		return "", false
	}

	return suggestion, true
}

// Put stores album suggestion
func (a *AlbumSuggestionCache) Put(folderPath string, sampleFiles []string, suggestion string) error {
	samplesJSON, _ := json.Marshal(sampleFiles)

	_, err := a.db.Exec(`
		INSERT OR REPLACE INTO album_suggestions
		(folder_path, sample_files, suggestion, created_at)
		VALUES (?, ?, ?, ?)
	`, folderPath, string(samplesJSON), suggestion, time.Now().Unix())

	return err
}
