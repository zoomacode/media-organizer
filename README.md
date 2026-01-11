# Media Library Organizer

Intelligent media library organizer that uses AI (Ollama) to suggest album names and organizes photos, videos, and music into a structured library.

## Quick Start

```bash
# Build the tool
go build -o media-organizer ./src

# Test on 100 files (safe - you can accept/reject the plan)
./media-organizer --path "/path/to/your/photos" --limit 100

# Review the plan in TUI, press 'y' to accept or 'n' to reject
# Files will be organized into MediaLibrary/ by date and album
```

**That's it!** The tool is safe by default - you always review before it moves files.

## Features

- **Smart Album Naming**: Uses Ollama local LLM to suggest meaningful album names from folder paths
- **EXIF Metadata**: Extracts dates, camera info, and other metadata
- **Duplicate Detection**: Finds and handles duplicate files intelligently
- **Parallel Processing**: Fast multi-threaded scanning and processing
- **TUI & CLI**: Beautiful terminal UI or simple CLI mode
- **Dry Run**: Preview changes before applying them

## Installation

Prerequisites:
- Go 1.24+
- Ollama running locally (optional, for smart album naming)

```bash
cd ~/Developer/media-organizer
go build -o media-organizer ./src
```

## Getting Started

### Workflow 1: Initial Organization of Old Archive

When you first organize an existing media archive (like old DVDs, external drives, or scattered folders):

**Step 1: Test with a small subset**
```bash
# Preview first 100 files to see how the tool works
./media-organizer --path "/Volumes/TimeMachine/Old DVD Archive" --limit 100
```
- Review the proposed organization in the TUI
- Check album names and structure
- Press `n` to reject if you want to adjust settings
- Press `y` to accept and organize those 100 files

**Step 2: Organize a larger section**
```bash
# Process a specific folder (e.g., photos from DVDs)
./media-organizer --path "/Volumes/TimeMachine/Old DVD Archive/Photos" --limit 1000
```
- Review the plan carefully
- Accept if it looks good
- The tool will move files to `MediaLibrary/`

**Step 3: Full archive organization**
```bash
# Scan and organize entire archive
./media-organizer --path "/Volumes/TimeMachine"
```
- This may take hours for large archives (but cache makes reruns fast!)
- All media files will be organized by date and type
- Duplicates automatically moved to `.duplicates-trash/`
- Review duplicates folder later and delete if satisfied

**Step 4: Verify and cleanup**
- Check your organized library at `/Volumes/TimeMachine/MediaLibrary/`
- Review `.duplicates-trash/` folder
- Run again to catch any missed files (uses cache, very fast!)

### Workflow 2: Adding New Photos Incrementally

After initial organization, when you add new photos from camera/phone:

**Step 1: Copy new photos to a staging area**
```bash
# Copy from camera/phone to a temp location
cp -r /Volumes/SD_CARD/DCIM/* /Volumes/TimeMachine/NewPhotos/2026-01/
```

**Step 2: Quick scan and organize**
```bash
# Organize just the new photos folder
./media-organizer --path "/Volumes/TimeMachine/NewPhotos/2026-01"
```
- Very fast! Cache only processes new files
- Detects if any are duplicates of existing photos
- Accept to move into organized library

**Step 3: Alternative - Scan everything (cache makes it fast!)**
```bash
# Scan entire library + new files (cache makes this fast!)
./media-organizer --path "/Volumes/TimeMachine"
```
- Cache hits on all existing files (instant!)
- Only new files are processed
- Great for finding files added anywhere in the tree

**Key Benefits:**
- **Cache makes reruns fast**: Already-processed files are instant
- **Automatic duplicate detection**: Won't create duplicates
- **Incremental workflow**: Can run daily/weekly without slowdown

### Common Scenarios

**Scenario: Multiple photo sources (camera, phone, old drives)**
```bash
# Organize each source separately, then scan all together
./media-organizer --path "/Volumes/TimeMachine/CameraPhotos"
./media-organizer --path "/Volumes/TimeMachine/PhoneBackup"
./media-organizer --path "/Volumes/TimeMachine/OldDVDs"

# Final scan to catch everything and deduplicate
./media-organizer --path "/Volumes/TimeMachine"
```

**Scenario: Found some duplicates, want to review**
- Duplicates are moved to `.duplicates-trash/` preserving folder structure
- Review manually, then `rm -rf .duplicates-trash/` when satisfied
- Or keep them as backup!

**Scenario: Made a mistake, want to undo**
- The tool moves files, doesn't copy
- If you rejected in TUI, nothing changed
- If you accepted and want to undo:
  - Check `.duplicates-trash/` for duplicates
  - Original file locations are lost after organization
  - Recommendation: **Test with --limit first!**

**Scenario: Want to organize but keep originals**
```bash
# Copy first, then organize the copy
cp -r /Volumes/OriginalArchive /Volumes/WorkingCopy
./media-organizer --path "/Volumes/WorkingCopy"
```

**Scenario: Large archive taking too long**
```bash
# Process in chunks with --limit
./media-organizer --path "/Volumes/Archive" --limit 5000

# Run again - cache makes it pick up where it left off
./media-organizer --path "/Volumes/Archive" --limit 5000

# Continue until done, then do full scan (fast with cache!)
./media-organizer --path "/Volumes/Archive"
```

**Scenario: Laptop freezing/too slow during processing**
```bash
# Reduce workers to 1 or 2 for minimal CPU usage
./media-organizer --path "/Volumes/Archive" --workers 2

# Or increase if you have a powerful machine and want speed
./media-organizer --path "/Volumes/Archive" --workers 16
```
Default is half of CPU cores - adjust based on your needs!

## Usage

### TUI Mode (Interactive)
```bash
./media-organizer --path "/Volumes/TimeMachine" --limit 1000
```

Beautiful terminal interface with:
- **Configuration display** (visible throughout processing)
- **Real-time progress bars** (percentage + file counts)
- **Current file display** (shows file being processed)
- **Cache statistics** (shows how many files cached)
- **Interactive album review** (navigate with ↑/↓ keys)
- **Accept/Reject plan** (y/a/enter to accept & execute, n/r to reject & quit)
- **Animated spinner** and phase indicators

### CLI Mode (Simple Output with Progress Bars)
```bash
./media-organizer --no-tui --path "/Volumes/TimeMachine/Old DVD Archive/Photo"
```

Shows:
- **Configuration** at startup (paths, workers, mode)
- **Real-time progress bars** with current file:
  - Metadata: `[==============>      ] 50% (25/50) ...DSC00053.JPG`
  - Hashing:  `[=====================>] 80% (40/50) ...PICT0012.JPG`
  - Execute:  `[========================>] 100% (250/250) ...file.jpg`
- **Cache statistics**: `Done (14 from cache, 36 processed)`

### Full Scan with Execution
```bash
./media-organizer --path "/Volumes/TimeMachine" --execute
```

## Command-Line Options

- `--path` - Path to scan for media files (default: `/Volumes/TimeMachine`)
- `--library` - Base path for organized library (default: `/Volumes/TimeMachine/MediaLibrary`)
- `--limit` - Limit number of files to process (0 = no limit, useful for testing)
- `--workers` - Number of parallel workers (default: half of CPU cores, keeps system responsive)
- `--dry-run` - Preview mode, no actual changes (default: true; in TUI you can still accept/reject)
- `--execute` - Actually perform the organization (in CLI mode; in TUI you can still reject)
- `--prune-cache` - Force pruning of deleted files from cache (auto when no --limit)
- `--no-tui` - Disable TUI, use simple CLI output

## Library Structure

The tool organizes media into:

```
MediaLibrary/
├── Photos/
│   ├── 2005/
│   │   ├── 2005-06 Cyprus Vacation/
│   │   └── Family Photos/
│   └── 2021/
│       └── 2021-10 Yellowstone Trip/
├── Videos/
│   └── 2020/
│       └── 2020-12 Christmas/
└── Music/
    └── Artist Name/
        └── Album Name/
```

## How It Works

1. **Scanning**: Walks directory tree and identifies media files (photos, videos, music)
2. **Metadata**: Extracts EXIF data (date taken, camera, location)
3. **Hashing**: Calculates MD5 hashes for duplicate detection
4. **Organizing**: Groups files by directory and date
5. **Album Naming**: Uses Ollama to suggest meaningful album names
6. **Review**: Shows organization plan (in TUI: navigate with ↑/↓, accept/reject; in CLI: displays preview)
7. **Execute**: Moves files to organized structure (TUI: after accepting; CLI: when using `--execute`)

## Ollama Integration

If Ollama is running locally (`http://localhost:11434`), the tool will use it to suggest album names.

Example: Folder `200508` → "2005-06 Cyprus Vacation"

If Ollama is not available, falls back to folder-based naming.

## Duplicate Handling

Duplicates are scored based on:
- File size (larger = better quality)
- Path organization (organized folders > Recovered)
- Metadata presence (EXIF data preferred)

Best duplicates are kept, others moved to `.duplicates-trash/`

## Caching

The tool uses SQLite to cache processed data for faster reruns:

- **File metadata cache**: Stores EXIF data, hashes, and metadata
- **Album suggestion cache**: Caches Ollama AI suggestions to avoid redundant API calls
- **Cache location**: `/Volumes/TimeMachine/MediaLibrary/.media-organizer-cache/cache.db`
- **Cache invalidation**: Automatic based on file modification time and size
- **Cache pruning**: Auto-removes deleted files when scanning without `--limit`
- **Write queue**: Single writer thread eliminates database contention (no more SQLITE_BUSY errors!)

Example performance:
```
First run:  0 from cache → Extracts all metadata, calculates all hashes
Second run: 152 from cache → Instant metadata, instant hashes!
```

### How Cache Works

**New files added**: Automatically detected and processed, then added to cache
**Files modified**: Detected by mod time/size change, re-processed and cache updated
**Files deleted**: Pruned from cache automatically on full scans (without `--limit`)
**Files moved to library**: Cache automatically updated with new path (critical for duplicate detection!)
**Testing with --limit**: Cache preserved (use `--prune-cache` flag to force pruning)

The cache dramatically speeds up reruns - hash calculation and Ollama API calls are expensive!

**Important**: When files are moved to the organized library, the cache is updated with the new paths. This means:
- ✅ Duplicate detection works even after organizing
- ✅ If you try to import the same photos again, they'll be detected as duplicates
- ✅ Subsequent scans of the library are instant (cache hits)

## Development

Project structure:

```
media-organizer/
├── src/                    # Source files (organized with prefixes)
│   ├── core_types.go      # Data structures (MediaFile, Album, Config, etc.)
│   ├── core_scanner.go    # File system scanning
│   ├── core_metadata.go   # EXIF/metadata extraction
│   ├── core_dedup.go      # Hash calculation and duplicate detection
│   ├── core_organizer.go  # Album grouping logic
│   ├── core_executor.go   # File moving and organization execution
│   ├── ai_ollama.go       # Ollama API integration for smart naming
│   ├── cache.go           # SQLite caching layer
│   ├── ui_tui.go          # Bubble Tea TUI implementation
│   └── main.go            # CLI entry point and flag parsing
├── go.mod
├── go.sum
└── README.md
```

All files are in `package main` for simplicity, but organized with prefixes for easy navigation.

## Examples

### Dry Run (Preview Only - Safe)
Test with limited files:
```bash
./media-organizer --no-tui --limit 100 --path "/Volumes/TimeMachine/Old DVD Archive"
```

Full library organization preview (TUI with accept/reject):
```bash
./media-organizer --path "/Volumes/TimeMachine"
```

### Actual Execution (Moves Files)
**Warning**: This will actually move files! Test with `--limit` first.

Test execution on small subset:
```bash
./media-organizer --no-tui --limit 50 --path "/path/to/test" --execute
```

Full execution (TUI will still show accept/reject):
```bash
./media-organizer --path "/Volumes/TimeMachine" --execute
```

CLI execution (auto-executes without prompting):
```bash
./media-organizer --no-tui --path "/Volumes/TimeMachine" --execute
```

What happens during execution:
- Files organized into albums → Moved to `MediaLibrary/Photos/YYYY/Album Name/`
- Duplicate files → Moved to `.duplicates-trash/` (preserves directory structure)
- Cache updated automatically
- Failed moves reported in summary
