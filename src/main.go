package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Default to half of available CPUs (keeps laptop responsive)
	defaultWorkers := runtime.NumCPU() / 2
	if defaultWorkers < 1 {
		defaultWorkers = 1
	}

	// Define all flags
	var (
		reconfigure = flag.Bool("reconfigure", false, "Re-run setup wizard to change configuration")
		scanPath    = flag.String("path", "", "Path to scan for media files (overrides config)")
		libraryBase = flag.String("library", "", "Base path for organized library (overrides config)")
		dryRun      = flag.Bool("dry-run", true, "Dry run mode (no actual changes)")
		fileLimit   = flag.Int("limit", 0, "Limit number of files to process (0 = no limit)")
		workers     = flag.Int("workers", 0, "Number of parallel workers (overrides config)")
		pruneCache  = flag.Bool("prune-cache", false, "Prune deleted files from cache (auto if no --limit)")
		noTUI       = flag.Bool("no-tui", false, "Disable TUI, use simple CLI output")
		execute     = flag.Bool("execute", false, "Actually perform operations (disables dry-run)")
	)

	flag.Parse()

	// Load or create configuration
	var configFile *ConfigFile
	var err error

	if *reconfigure || !configExists() {
		// Run setup wizard
		configFile, err = runSetupWizard()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Load existing config
		configFile, err = loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading config from %s: %v\n", getConfigPath(), err)
			fmt.Println("Run with --reconfigure to set up again")
			os.Exit(1)
		}
	}

	// Create Config from file, with command-line overrides
	config := &Config{
		ScanPath:        configFile.ScanPath,
		LibraryBase:     configFile.LibraryBase,
		DuplicatesTrash: configFile.DuplicatesTrash,
		OllamaModel:     configFile.OllamaModel,
		DryRun:          *dryRun,
		Workers:         configFile.Workers,
		FileLimit:       *fileLimit,
		PruneCache:      *pruneCache,
	}

	// Command-line flags override config file
	if *scanPath != "" {
		config.ScanPath = *scanPath
	}
	if *libraryBase != "" {
		config.LibraryBase = *libraryBase
	}
	if *workers > 0 {
		config.Workers = *workers
	}

	if *execute {
		config.DryRun = false
	}

	// Run with or without TUI
	if *noTUI {
		runCLI(config)
	} else {
		runTUI(config)
	}
}

func runCLI(config *Config) {
	fmt.Println("Media Library Organizer")
	fmt.Println("======================")
	fmt.Println()

	// Configuration display
	fmt.Println("Configuration:")
	fmt.Printf("  Scan Path:    %s\n", config.ScanPath)
	fmt.Printf("  Library:      %s\n", config.LibraryBase)
	fmt.Printf("  Trash:        %s\n", config.DuplicatesTrash)
	fmt.Printf("  Ollama Model: %s\n", config.OllamaModel)
	fmt.Printf("  Workers:      %d\n", config.Workers)
	if config.FileLimit > 0 {
		fmt.Printf("  File Limit:   %d (testing mode)\n", config.FileLimit)
	}
	if config.PruneCache {
		fmt.Printf("  Cache Prune:  Enabled\n")
	}

	fmt.Println()
	if config.DryRun {
		fmt.Println("Mode: DRY RUN (no changes will be made)")
	} else {
		fmt.Println("Mode: EXECUTE (files will be moved)")
	}
	fmt.Println()

	// Open cache
	cache, err := OpenCache(config.LibraryBase)
	if err != nil {
		fmt.Printf("Warning: cache disabled: %v\n", err)
		cache = nil
	} else {
		defer cache.Close()
		total, withHash, withMetadata := cache.GetStats()
		fmt.Printf("Cache: %d files (%d with hashes, %d with metadata)\n", total, withHash, withMetadata)
	}
	fmt.Println()

	// Scan for media files
	fmt.Println("Scanning for media files...")
	files, err := ScanMediaFiles(config.ScanPath, config.FileLimit, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error scanning: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d media files\n", len(files))

	// Prune deleted files from cache (auto when scanning all files, or when --prune-cache flag set)
	if cache != nil && (config.FileLimit == 0 || config.PruneCache) {
		validPaths := make(map[string]bool)
		for _, f := range files {
			validPaths[f.Path] = true
		}
		pruned, err := cache.PruneDeleted(validPaths)
		if err == nil && pruned > 0 {
			fmt.Printf("  Pruned %d deleted files from cache\n", pruned)
		}
	}
	fmt.Printf("  Photos: %d\n", countByType(files, TypePhoto))
	fmt.Printf("  Videos: %d\n", countByType(files, TypeVideo))
	fmt.Printf("  Music:  %d\n", countByType(files, TypeMusic))
	newCount := countNewFiles(files)
	if cache != nil {
		fmt.Printf("  New files: %d (rest already in library)\n", newCount)
	}
	fmt.Println()

	// Extract metadata
	fmt.Println("Extracting metadata...")
	metadataProgress := make(chan ScanProgress, 10)
	go func() {
		for prog := range metadataProgress {
			if prog.TotalFiles > 0 {
				percent := float64(prog.ProcessedFiles) * 100 / float64(prog.TotalFiles)
				currentFile := truncateFilePath(prog.CurrentFile, 60)
				fmt.Printf("\r  Progress: [%-50s] %3.0f%% (%d/%d) %s",
					progressBar(percent),
					percent,
					prog.ProcessedFiles,
					prog.TotalFiles,
					currentFile)
			}
		}
		fmt.Printf("\r%s\r", strings.Repeat(" ", 150)) // Clear line
	}()

	metadataHits := ProcessMetadata(files, config.Workers, metadataProgress, cache)
	close(metadataProgress)

	if cache != nil {
		fmt.Printf("Done (%d from cache, %d processed)\n", metadataHits, len(files)-metadataHits)
	} else {
		fmt.Println("Done")
	}
	fmt.Println()

	// Calculate hashes
	fmt.Println("Calculating hashes for duplicate detection...")
	hashProgress := make(chan ScanProgress, 10)
	go func() {
		for prog := range hashProgress {
			if prog.TotalFiles > 0 {
				percent := float64(prog.ProcessedFiles) * 100 / float64(prog.TotalFiles)
				currentFile := truncateFilePath(prog.CurrentFile, 60)
				fmt.Printf("\r  Progress: [%-50s] %3.0f%% (%d/%d) %s",
					progressBar(percent),
					percent,
					prog.ProcessedFiles,
					prog.TotalFiles,
					currentFile)
			}
		}
		fmt.Printf("\r%s\r", strings.Repeat(" ", 150)) // Clear line
	}()

	hashHits := CalculateHashes(files, config.Workers, hashProgress, cache)
	close(hashProgress)

	if cache != nil {
		fmt.Printf("Done (%d from cache, %d calculated)\n", hashHits, len(files)-hashHits)
	} else {
		fmt.Println("Done")
	}
	fmt.Println()

	// Find duplicates
	fmt.Println("Finding duplicates...")
	duplicates := FindDuplicates(files)
	fmt.Printf("Found %d duplicate groups\n", len(duplicates))
	fmt.Println()

	// Organize into albums
	fmt.Println("Organizing into albums...")
	var albumCache *AlbumSuggestionCache
	if cache != nil {
		albumCache, _ = OpenAlbumSuggestionCache(cache)
	}
	albums, err := OrganizeIntoAlbums(files, config, nil, albumCache)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error organizing: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created %d albums\n", len(albums))
	fmt.Println()

	// Show summary
	if len(albums) == 0 {
		fmt.Println("No new files to organize! All files are already in the library.")
		return
	}

	totalFilesToMove := 0
	for _, album := range albums {
		totalFilesToMove += len(album.Files)
	}

	fmt.Println("Organization Plan:")
	fmt.Println("==================")
	fmt.Printf("Found %d new/moved files to organize into %d albums\n\n", totalFilesToMove, len(albums))
	for i, album := range albums {
		if i >= 10 {
			fmt.Printf("... and %d more albums\n", len(albums)-10)
			break
		}
		fmt.Printf("%s\n", album.Name)
		fmt.Printf("  → %s\n", album.Destination)
		fmt.Printf("  → %d files\n", len(album.Files))
		fmt.Println()
	}

	if config.DryRun {
		fmt.Println("This was a DRY RUN. Use --execute to actually organize files.")
	} else {
		// Execute the organization
		fmt.Println("\nExecuting organization...")
		execProgress := make(chan ScanProgress, 10)
		go func() {
			for prog := range execProgress {
				if prog.TotalFiles > 0 {
					percent := float64(prog.ProcessedFiles) * 100 / float64(prog.TotalFiles)
					currentFile := truncateFilePath(prog.CurrentFile, 60)
					fmt.Printf("\r  Progress: [%-50s] %3.0f%% (%d/%d) %s",
						progressBar(percent),
						percent,
						prog.ProcessedFiles,
						prog.TotalFiles,
						currentFile)
				}
			}
			fmt.Printf("\r%s\r", strings.Repeat(" ", 150)) // Clear line
		}()

		if err := ExecuteOrganization(albums, duplicates, config, execProgress, cache); err != nil {
			close(execProgress)
			fmt.Fprintf(os.Stderr, "Error executing: %v\n", err)
			os.Exit(1)
		}
		close(execProgress)
	}
}

func runTUI(config *Config) {
	p := tea.NewProgram(initialModel(config), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func countByType(files []*MediaFile, mediaType MediaType) int {
	count := 0
	for _, f := range files {
		if f.Type == mediaType {
			count++
		}
	}
	return count
}

func countNewFiles(files []*MediaFile) int {
	count := 0
	for _, f := range files {
		if f.IsNew {
			count++
		}
	}
	return count
}

// progressBar creates a text progress bar
func progressBar(percent float64) string {
	const width = 50
	filled := int(percent / 2) // 50 chars = 100%
	if filled > width {
		filled = width
	}
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "="
		} else if i == filled {
			bar += ">"
		} else {
			bar += " "
		}
	}
	return bar
}

// truncateFilePath shortens a file path for display
func truncateFilePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	// Show just the filename
	base := filepath.Base(path)
	if len(base) <= maxLen {
		return "..." + base
	}
	// Truncate filename too if needed
	return "..." + base[len(base)-maxLen+3:]
}
