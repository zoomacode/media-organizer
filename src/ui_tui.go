package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type phase int

const (
	phaseScanning phase = iota
	phaseMetadata
	phaseHashing
	phaseOrganizing
	phaseReview
	phaseExecuting
	phaseDone
)

type model struct {
	config      *Config
	currentPhase phase
	spinner      spinner.Model
	progress     progress.Model

	// Data
	files       []*MediaFile
	albums      []*Album
	duplicates  []*DuplicateGroup

	// Progress tracking
	scanProgress ScanProgress
	statusMsg    string

	// Cache
	cache      *Cache
	albumCache *AlbumSuggestionCache

	// Progress channels for async updates
	metadataProgress chan ScanProgress
	hashProgress     chan ScanProgress
	organizeProgress chan string

	// UI state
	selectedAlbum int
	scrollOffset  int
	width         int
	height        int

	// Error
	err error
}

type scanCompleteMsg struct {
	files []*MediaFile
}

type metadataCompleteMsg struct{}
type hashingCompleteMsg struct{}
type executionCompleteMsg struct {
	moved  int
	failed int
}

type albumsReadyMsg struct {
	albums []*Album
	duplicates []*DuplicateGroup
}

type progressMsg ScanProgress
type statusMsg string
type errMsg error

func initialModel(config *Config) model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithoutPercentage(), // Don't show built-in percentage
	)
	// Set a reasonable default width (will be updated when WindowSizeMsg arrives)
	p.Width = 60

	// Open cache
	cache, _ := OpenCache(config.LibraryBase)
	var albumCache *AlbumSuggestionCache
	if cache != nil {
		albumCache, _ = OpenAlbumSuggestionCache(cache)
	}

	return model{
		config:       config,
		spinner:      s,
		progress:     p,
		currentPhase: phaseScanning,
		cache:        cache,
		albumCache:   albumCache,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		scanFiles(m.config),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Account for: left margin (2) + suffix text like " 100% (9999/9999 files)" (~30)
		progressWidth := msg.Width - 35
		if progressWidth < 20 {
			progressWidth = 20 // Minimum width
		}
		m.progress.Width = progressWidth
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "y", "a", "enter":
			// Accept plan and execute
			if m.currentPhase == phaseReview {
				m.currentPhase = phaseExecuting
				m.statusMsg = "Moving files..."
				return m, executeOrganization(m.config, m.albums, m.duplicates, m.cache)
			}
			if m.currentPhase == phaseDone {
				return m, tea.Quit
			}

		case "n", "r":
			// Reject plan and quit
			if m.currentPhase == phaseReview {
				return m, tea.Quit
			}

		case "up", "k":
			if m.currentPhase == phaseReview && m.selectedAlbum > 0 {
				m.selectedAlbum--
				if m.selectedAlbum < m.scrollOffset {
					m.scrollOffset = m.selectedAlbum
				}
			}

		case "down", "j":
			if m.currentPhase == phaseReview && m.selectedAlbum < len(m.albums)-1 {
				m.selectedAlbum++
				maxVisible := m.height - 15
				if m.selectedAlbum >= m.scrollOffset+maxVisible {
					m.scrollOffset = m.selectedAlbum - maxVisible + 1
				}
			}
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case progressMsg:
		m.scanProgress = ScanProgress(msg)
		// Continue listening for more progress updates
		if m.currentPhase == phaseMetadata && m.metadataProgress != nil {
			return m, waitForProgress(m.metadataProgress)
		}
		if m.currentPhase == phaseHashing && m.hashProgress != nil {
			return m, waitForProgress(m.hashProgress)
		}
		return m, nil

	case statusMsg:
		m.statusMsg = string(msg)
		return m, nil

	case scanCompleteMsg:
		m.files = msg.files
		m.scanProgress.TotalFiles = 0     // Reset for next phase
		m.scanProgress.ProcessedFiles = 0
		m.scanProgress.CurrentFile = ""

		// Prune deleted files from cache (auto when scanning all files, or when --prune-cache flag set)
		if m.cache != nil && (m.config.FileLimit == 0 || m.config.PruneCache) {
			validPaths := make(map[string]bool)
			for _, f := range m.files {
				validPaths[f.Path] = true
			}
			m.cache.PruneDeleted(validPaths)
		}

		m.currentPhase = phaseMetadata
		m.statusMsg = fmt.Sprintf("Extracting metadata from %d files...", len(m.files))
		if m.cache != nil {
			_, _, withMetadata := m.cache.GetStats()
			m.statusMsg = fmt.Sprintf("Extracting metadata (%d cached)...", withMetadata)
		}

		// Create progress channel and start listening
		m.metadataProgress = make(chan ScanProgress, 100)
		return m, tea.Batch(
			processMetadata(m.config, m.files, m.cache, m.metadataProgress),
			waitForProgress(m.metadataProgress),
		)

	case metadataCompleteMsg:
		m.currentPhase = phaseHashing
		m.scanProgress.TotalFiles = 0     // Reset for next phase
		m.scanProgress.ProcessedFiles = 0
		m.scanProgress.CurrentFile = ""
		m.statusMsg = fmt.Sprintf("Calculating hashes for %d files...", len(m.files))
		if m.cache != nil {
			_, withHash, _ := m.cache.GetStats()
			m.statusMsg = fmt.Sprintf("Calculating hashes (%d cached)...", withHash)
		}

		// Create progress channel and start listening
		m.hashProgress = make(chan ScanProgress, 100)
		return m, tea.Batch(
			calculateHashes(m.config, m.files, m.cache, m.hashProgress),
			waitForProgress(m.hashProgress),
		)

	case hashingCompleteMsg:
		m.currentPhase = phaseOrganizing
		m.statusMsg = "Organizing into albums..."
		return m, organizeFiles(m.config, m.files, m.albumCache)

	case albumsReadyMsg:
		m.albums = msg.albums
		m.duplicates = msg.duplicates
		m.currentPhase = phaseReview
		m.statusMsg = "Review organization plan"
		return m, nil

	case executionCompleteMsg:
		m.currentPhase = phaseDone
		m.statusMsg = fmt.Sprintf("Complete! %d files moved, %d failed", msg.moved, msg.failed)
		return m, nil

	case errMsg:
		m.err = error(msg)
		return m, nil
	}

	return m, nil
}

func (m model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress q to quit", m.err)
	}

	var b strings.Builder

	// Top margin
	b.WriteString("\n")

	// Header
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		MarginLeft(2)

	b.WriteString(titleStyle.Render("Media Library Organizer"))
	b.WriteString("\n\n")

	// Configuration (shown during all processing phases)
	if m.currentPhase != phaseReview && m.currentPhase != phaseDone {
		configStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			MarginLeft(2)
		modeStr := map[bool]string{true: "DRY RUN", false: "EXECUTE"}[m.config.DryRun]
		limitStr := ""
		if m.config.FileLimit > 0 {
			limitStr = fmt.Sprintf(" | Limit: %d", m.config.FileLimit)
		}
		b.WriteString(configStyle.Render(fmt.Sprintf(
			"%s → %s | Workers: %d | %s%s",
			truncatePath(m.config.ScanPath, 25),
			truncatePath(m.config.LibraryBase, 25),
			m.config.Workers,
			modeStr,
			limitStr,
		)))
		b.WriteString("\n\n")
	}

	// Phase indicator
	b.WriteString("  ") // Left margin
	phases := []string{"Scanning", "Metadata", "Hashing", "Organizing", "Review", "Executing", "Done"}
	for i, phase := range phases {
		if i > 0 {
			b.WriteString(" → ")
		}
		if int(m.currentPhase) == i {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render(phase))
		} else if int(m.currentPhase) > i {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("✓"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(phase))
		}
	}
	b.WriteString("\n\n")

	// Content based on phase
	switch m.currentPhase {
	case phaseScanning, phaseMetadata, phaseHashing, phaseOrganizing, phaseExecuting:
		b.WriteString(fmt.Sprintf("  %s %s\n\n", m.spinner.View(), m.statusMsg))

		// Show progress bar if we have total files
		if m.scanProgress.TotalFiles > 0 {
			percent := float64(m.scanProgress.ProcessedFiles) / float64(m.scanProgress.TotalFiles)
			percentDisplay := int(percent * 100)

			b.WriteString("  ") // Left margin
			b.WriteString(m.progress.ViewAs(percent))
			b.WriteString(fmt.Sprintf(" %d%% (%d/%d files)\n\n",
				percentDisplay,
				m.scanProgress.ProcessedFiles,
				m.scanProgress.TotalFiles))
		} else if len(m.files) > 0 {
			// Show total files count during processing phases
			b.WriteString(fmt.Sprintf("  Processing %d files...\n\n", len(m.files)))
		}

		// Show found files during scanning
		if m.currentPhase == phaseScanning && (m.scanProgress.PhotosFound > 0 || m.scanProgress.VideosFound > 0 || m.scanProgress.MusicFound > 0) {
			b.WriteString(fmt.Sprintf("  Found: %d photos • %d videos • %d music\n",
				m.scanProgress.PhotosFound,
				m.scanProgress.VideosFound,
				m.scanProgress.MusicFound))
		}

		// Show current file being processed
		if m.scanProgress.CurrentFile != "" {
			maxLen := m.width - 20
			if maxLen < 40 {
				maxLen = 40
			}
			currentFile := truncatePath(m.scanProgress.CurrentFile, maxLen)
			fileStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Italic(true).
				MarginLeft(2)
			b.WriteString(fmt.Sprintf("\n%s", fileStyle.Render(currentFile)))
		}

	case phaseReview:
		b.WriteString(m.renderReview())

	case phaseDone:
		doneStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true).
			MarginLeft(2)
		b.WriteString(doneStyle.Render("✓ " + m.statusMsg))
		b.WriteString("\n\n")
	}

	// Footer
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginLeft(2)
	switch m.currentPhase {
	case phaseReview:
		b.WriteString(helpStyle.Render("↑/↓: navigate • y/a/enter: accept & execute • n/r: reject & quit • q: quit"))
	case phaseDone:
		b.WriteString(helpStyle.Render("enter: quit • q: quit"))
	default:
		b.WriteString(helpStyle.Render("q: quit"))
	}

	// Bottom margin
	b.WriteString("\n")

	return b.String()
}

func (m model) renderReview() string {
	var b strings.Builder

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(0, 1).
		MarginLeft(2)

	// Summary
	b.WriteString(boxStyle.Render(fmt.Sprintf(
		"Total: %d files • Photos: %d • Videos: %d • Music: %d\nAlbums: %d • Duplicates: %d groups",
		len(m.files),
		countByType(m.files, TypePhoto),
		countByType(m.files, TypeVideo),
		countByType(m.files, TypeMusic),
		len(m.albums),
		len(m.duplicates),
	)))
	b.WriteString("\n\n")

	// Albums list
	albumsHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		MarginLeft(2)
	b.WriteString(albumsHeaderStyle.Render("Albums:"))
	b.WriteString("\n\n")

	maxVisible := m.height - 15
	start := m.scrollOffset
	end := start + maxVisible
	if end > len(m.albums) {
		end = len(m.albums)
	}

	for i := start; i < end; i++ {
		album := m.albums[i]

		var line string
		if i == m.selectedAlbum {
			selectedStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230")).
				MarginLeft(2)
			line = selectedStyle.Render(fmt.Sprintf("► %s (%d files)", album.Name, len(album.Files)))
		} else {
			line = fmt.Sprintf("    %s (%d files)", album.Name, len(album.Files))
		}

		b.WriteString(line)
		b.WriteString("\n")

		if i == m.selectedAlbum {
			destStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				MarginLeft(2)
			dest := destStyle.Render(fmt.Sprintf("    → %s", album.Destination))
			b.WriteString(dest)
			b.WriteString("\n")
		}
	}

	if len(m.albums) > maxVisible {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			MarginLeft(2)
		b.WriteString(moreStyle.Render(fmt.Sprintf("\n... %d more albums ...", len(m.albums)-end)))
	}

	return b.String()
}

// Commands
func scanFiles(config *Config) tea.Cmd {
	return func() tea.Msg {
		files, err := ScanMediaFiles(config.ScanPath, config.FileLimit, nil)
		if err != nil {
			return errMsg(err)
		}
		return scanCompleteMsg{files: files}
	}
}

func processMetadata(config *Config, files []*MediaFile, cache *Cache, progressChan chan ScanProgress) tea.Cmd {
	return func() tea.Msg {
		// Start processing in background
		go func() {
			ProcessMetadata(files, config.Workers, progressChan, cache)
			close(progressChan)
		}()

		// Wait for completion (indicated by closed channel)
		for range progressChan {
		}

		return metadataCompleteMsg{}
	}
}

// waitForProgress polls the progress channel and sends updates
func waitForProgress(progressChan <-chan ScanProgress) tea.Cmd {
	return func() tea.Msg {
		prog, ok := <-progressChan
		if !ok {
			// Channel closed, processing done
			return nil
		}
		return progressMsg(prog)
	}
}

func calculateHashes(config *Config, files []*MediaFile, cache *Cache, progressChan chan ScanProgress) tea.Cmd {
	return func() tea.Msg {
		// Start processing in background
		go func() {
			CalculateHashes(files, config.Workers, progressChan, cache)
			close(progressChan)
		}()

		// Wait for completion
		for range progressChan {
		}

		return hashingCompleteMsg{}
	}
}

func organizeFiles(config *Config, files []*MediaFile, albumCache *AlbumSuggestionCache) tea.Cmd {
	return func() tea.Msg {
		albums, _ := OrganizeIntoAlbums(files, config, nil, albumCache)
		duplicates := FindDuplicates(files)
		return albumsReadyMsg{albums: albums, duplicates: duplicates}
	}
}

func executeOrganization(config *Config, albums []*Album, duplicates []*DuplicateGroup, cache *Cache) tea.Cmd {
	return func() tea.Msg {
		// Execute without progress channel for TUI (uses spinner instead)
		err := ExecuteOrganization(albums, duplicates, config, nil, cache)

		// Count moved/failed from error or assume success
		totalFiles := 0
		for _, album := range albums {
			totalFiles += len(album.Files)
		}
		for _, group := range duplicates {
			totalFiles += len(group.Files) - 1
		}

		if err != nil {
			return executionCompleteMsg{moved: 0, failed: totalFiles}
		}
		return executionCompleteMsg{moved: totalFiles, failed: 0}
	}
}

// truncatePath shortens a file path for display
func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}

	// Try to show end of path with ...
	if maxLen > 10 {
		return "..." + path[len(path)-maxLen+3:]
	}

	return path[:maxLen]
}
