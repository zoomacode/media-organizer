package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

const ollamaURL = "http://localhost:11434/api/generate"

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

// SuggestAlbumName uses Ollama to suggest an album name
func SuggestAlbumName(model, folderPath string, sampleFiles []string) (string, error) {
	// Extract folder names from path
	parts := strings.Split(folderPath, string(filepath.Separator))
	var relevantParts []string
	for _, part := range parts {
		if part != "" && !strings.HasPrefix(part, ".") &&
			part != "Volumes" && part != "TimeMachine" {
			relevantParts = append(relevantParts, part)
		}
	}

	// Take last 3 parts
	if len(relevantParts) > 3 {
		relevantParts = relevantParts[len(relevantParts)-3:]
	}

	// Get sample filenames
	var sampleNames []string
	for i, f := range sampleFiles {
		if i >= 5 {
			break
		}
		sampleNames = append(sampleNames, filepath.Base(f))
	}

	// Create prompt
	prompt := fmt.Sprintf(`Given these folder names from a photo/video path: %s

And these sample filenames: %s

Suggest a good album name in format: YYYY-MM Description (e.g., "2005-06 Cyprus Vacation" or "2021-10 Yellowstone Trip")

If you can't determine a date, use just the description (e.g., "Family Photos").

Reply with ONLY the album name, nothing else.`,
		strings.Join(relevantParts, " / "),
		strings.Join(sampleNames, ", "))

	// Call Ollama
	reqBody := ollamaRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(ollamaURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned status %d: %s", resp.StatusCode, string(body))
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", err
	}

	// Clean up response
	suggestion := strings.TrimSpace(ollamaResp.Response)
	suggestion = strings.Trim(suggestion, `"'`)

	// Remove common prefixes
	for _, prefix := range []string{"Album name: ", "Suggested album name: ", "I suggest: "} {
		suggestion = strings.TrimPrefix(suggestion, prefix)
	}

	return strings.TrimSpace(suggestion), nil
}

// CheckOllamaAvailable checks if Ollama is running
func CheckOllamaAvailable() bool {
	resp, err := http.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
