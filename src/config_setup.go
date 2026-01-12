package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile represents the YAML configuration
type ConfigFile struct {
	ScanPath        string `yaml:"scan_path"`
	LibraryBase     string `yaml:"library_base"`
	DuplicatesTrash string `yaml:"duplicates_trash"`
	OllamaModel     string `yaml:"ollama_model"`
	Workers         int    `yaml:"workers"`
}

// getConfigPath returns the path to the config file
func getConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".media-organizer.yaml"
	}
	return filepath.Join(home, ".media-organizer.yaml")
}

// configExists checks if config file exists
func configExists() bool {
	_, err := os.Stat(getConfigPath())
	return err == nil
}

// loadConfig loads configuration from YAML file
func loadConfig() (*ConfigFile, error) {
	configPath := getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// saveConfig saves configuration to YAML file
func saveConfig(cfg *ConfigFile) error {
	configPath := getConfigPath()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// runSetupWizard runs interactive setup and creates config file
func runSetupWizard() (*ConfigFile, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("╔════════════════════════════════════════════════════════════════╗")
	fmt.Println("║          Media Library Organizer - First Time Setup           ║")
	fmt.Println("╚════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Welcome! Let's set up your media library organizer.")
	fmt.Println("This configuration will be saved to:", getConfigPath())
	fmt.Println()

	cfg := &ConfigFile{}

	// Scan Path
	fmt.Println("1. Where are your media files located?")
	fmt.Println("   (This is the root directory containing photos, videos, music)")
	fmt.Print("   Path [/Volumes/TimeMachine]: ")
	scanPath, _ := reader.ReadString('\n')
	scanPath = strings.TrimSpace(scanPath)
	if scanPath == "" {
		scanPath = "/Volumes/TimeMachine"
	}
	cfg.ScanPath = scanPath

	// Library Base
	fmt.Println()
	fmt.Println("2. Where should the organized library be created?")
	fmt.Println("   (Files will be organized into subdirectories here)")
	defaultLibrary := filepath.Join(scanPath, "MediaLibrary")
	fmt.Printf("   Path [%s]: ", defaultLibrary)
	libraryBase, _ := reader.ReadString('\n')
	libraryBase = strings.TrimSpace(libraryBase)
	if libraryBase == "" {
		libraryBase = defaultLibrary
	}
	cfg.LibraryBase = libraryBase

	// Duplicates Trash
	fmt.Println()
	fmt.Println("3. Where should duplicate files be moved?")
	fmt.Println("   (You can review and delete these later)")
	defaultTrash := filepath.Join(scanPath, ".duplicates-trash")
	fmt.Printf("   Path [%s]: ", defaultTrash)
	trash, _ := reader.ReadString('\n')
	trash = strings.TrimSpace(trash)
	if trash == "" {
		trash = defaultTrash
	}
	cfg.DuplicatesTrash = trash

	// Ollama Model
	fmt.Println()
	fmt.Println("4. Which Ollama model for smart album naming?")
	fmt.Println("   (Requires Ollama running locally, or leave default)")
	fmt.Print("   Model [gemma2:2b]: ")
	model, _ := reader.ReadString('\n')
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gemma2:2b"
	}
	cfg.OllamaModel = model

	// Workers
	fmt.Println()
	fmt.Println("5. How many parallel workers?")
	fmt.Printf("   (Your system has %d CPUs, recommend %d for responsiveness)\n",
		runtime.NumCPU(), getDefaultWorkers())
	fmt.Printf("   Workers [%d]: ", getDefaultWorkers())
	workersStr, _ := reader.ReadString('\n')
	workersStr = strings.TrimSpace(workersStr)
	if workersStr == "" {
		cfg.Workers = getDefaultWorkers()
	} else {
		workers, err := strconv.Atoi(workersStr)
		if err != nil || workers < 1 {
			cfg.Workers = getDefaultWorkers()
		} else {
			cfg.Workers = workers
		}
	}

	// Summary
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("Configuration Summary:")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf("  Scan Path:        %s\n", cfg.ScanPath)
	fmt.Printf("  Library:          %s\n", cfg.LibraryBase)
	fmt.Printf("  Duplicates Trash: %s\n", cfg.DuplicatesTrash)
	fmt.Printf("  Ollama Model:     %s\n", cfg.OllamaModel)
	fmt.Printf("  Workers:          %d\n", cfg.Workers)
	fmt.Println()

	// Confirm
	fmt.Print("Save this configuration? [Y/n]: ")
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))
	if confirm == "n" || confirm == "no" {
		fmt.Println("\nSetup cancelled.")
		os.Exit(0)
	}

	// Save config
	if err := saveConfig(cfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Configuration saved to:", getConfigPath())
	fmt.Println()
	fmt.Println("You can edit this file manually or run with --reconfigure to change settings.")
	fmt.Println()

	return cfg, nil
}

// getDefaultWorkers returns recommended worker count
func getDefaultWorkers() int {
	cpus := runtime.NumCPU()
	workers := cpus / 2
	if workers < 1 {
		workers = 1
	}
	return workers
}
