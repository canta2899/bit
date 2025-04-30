// Package util provides utility functions for the bit version control system
package util

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// DeltaInfo stores information about a file delta
type DeltaInfo struct {
	Path         string   `json:"path"`         // File path
	IsNew        bool     `json:"isNew"`        // Whether this is a new file
	IsDeleted    bool     `json:"isDeleted"`    // Whether the file was deleted
	BaseSaveHash string   `json:"baseSaveHash"` // Hash of the save this delta is based on (empty for full file)
	Patches      []string `json:"patches"`      // JSON representation of the patches
	ContentHash  string   `json:"contentHash"`  // Hash of the file content (for verification)
}

// DeltaSet represents a collection of deltas for a single save
type DeltaSet struct {
	SaveHash string      `json:"saveHash"` // Hash of the save this delta set belongs to
	Deltas   []DeltaInfo `json:"deltas"`   // List of deltas
}

// CalculateDelta computes the delta between two versions of a file
func CalculateDelta(oldContent, newContent []byte, path string, baseSaveHash string) DeltaInfo {
	// If old content is nil, this is a new file
	if oldContent == nil {
		return DeltaInfo{
			Path:         path,
			IsNew:        true,
			IsDeleted:    false,
			BaseSaveHash: "",
			Patches:      nil,
			ContentHash:  calculateFileHash(newContent),
		}
	}

	// If new content is nil, this is a deleted file
	if newContent == nil {
		return DeltaInfo{
			Path:         path,
			IsNew:        false,
			IsDeleted:    true,
			BaseSaveHash: baseSaveHash,
			Patches:      nil,
			ContentHash:  calculateFileHash(oldContent),
		}
	}

	// Calculate patches
	dmp := diffmatchpatch.New()
	patches := dmp.PatchMake(string(oldContent), string(newContent))
	patchesText := dmp.PatchToText(patches)

	// Split patch text by newlines to store as array
	patchesArray := []string{patchesText}
	if len(patchesText) == 0 {
		patchesArray = nil // No changes, file is identical
	}

	return DeltaInfo{
		Path:         path,
		IsNew:        false,
		IsDeleted:    false,
		BaseSaveHash: baseSaveHash,
		Patches:      patchesArray,
		ContentHash:  calculateFileHash(newContent),
	}
}

// ApplyDelta applies a delta to reconstruct a file
func ApplyDelta(delta DeltaInfo, baseContentProvider func(path, saveHash string) ([]byte, error)) ([]byte, error) {
	// Handle new file
	if delta.IsNew {
		// For new files, we need to get the full content from the save
		return baseContentProvider(delta.Path, delta.BaseSaveHash)
	}

	// Handle deleted file
	if delta.IsDeleted {
		return nil, nil
	}

	// Handle no changes
	if delta.Patches == nil || len(delta.Patches) == 0 {
		// File exists but has no changes, get base version
		return baseContentProvider(delta.Path, delta.BaseSaveHash)
	}

	// Get base content
	baseContent, err := baseContentProvider(delta.Path, delta.BaseSaveHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get base content: %w", err)
	}

	// Apply patches
	dmp := diffmatchpatch.New()
	patches, err := dmp.PatchFromText(delta.Patches[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse patches: %w", err)
	}

	newContent, _ := dmp.PatchApply(patches, string(baseContent))
	resultContent := []byte(newContent)

	// Verify content hash
	if calculateFileHash(resultContent) != delta.ContentHash {
		return nil, fmt.Errorf("content hash mismatch after applying delta")
	}

	return resultContent, nil
}

// SaveDeltaSet stores a set of deltas to disk
func SaveDeltaSet(deltaSet DeltaSet, objectsDir string) error {
	// Create delta file path
	deltaPath := filepath.Join(objectsDir, "delta_"+deltaSet.SaveHash+".json")

	// Marshal to JSON
	data, err := json.MarshalIndent(deltaSet, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal delta set: %w", err)
	}

	// Write to file
	if err := os.WriteFile(deltaPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write delta file: %w", err)
	}

	return nil
}

// LoadDeltaSet loads a set of deltas from disk
func LoadDeltaSet(saveHash, objectsDir string) (DeltaSet, error) {
	var deltaSet DeltaSet

	// Create delta file path
	deltaPath := filepath.Join(objectsDir, "delta_"+saveHash+".json")

	// Read file
	data, err := os.ReadFile(deltaPath)
	if err != nil {
		return deltaSet, fmt.Errorf("failed to read delta file: %w", err)
	}

	// Unmarshal JSON
	if err := json.Unmarshal(data, &deltaSet); err != nil {
		return deltaSet, fmt.Errorf("failed to unmarshal delta set: %w", err)
	}

	return deltaSet, nil
}

// calculateFileHash computes a SHA-256 hash of file content
func calculateFileHash(content []byte) string {
	h := sha256.New()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// CopyToFile copies content to a file, creating directories as needed
func CopyToFile(content []byte, targetPath string) error {
	// Create parent directories if needed
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
	}

	// Write file
	return os.WriteFile(targetPath, content, 0644)
}

// SaveFullFile saves a full copy of the file (for first version)
func SaveFullFile(content []byte, path, saveHash, objectsDir string) error {
	fullPath := filepath.Join(objectsDir, saveHash+"_"+path)
	return CopyToFile(content, fullPath)
}

// GetFileContent retrieves file content either from working dir or saved object
func GetFileContent(path, saveHash, objectsDir string) ([]byte, error) {
	if saveHash == "" {
		// Read from working directory
		return os.ReadFile(path)
	}

	// Read from objects directory
	return os.ReadFile(filepath.Join(objectsDir, saveHash+"_"+path))
}
