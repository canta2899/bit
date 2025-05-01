// Package util provides utility functions for the bit version control system
package util

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// CompressionConfig holds configuration options for delta compression
var CompressionConfig = struct {
	Enabled                bool
	MinSizeForCompression  int  // Minimum size in bytes before compressing (smaller patches don't benefit as much)
	CompressNewFileContent bool // Whether to also compress new file content when saved as full files
}{
	Enabled:                true,
	MinSizeForCompression:  1,    // Always compress regardless of size
	CompressNewFileContent: true, // Always compress new file content too
}

// DeltaInfo stores information about a file delta
type DeltaInfo struct {
	Path         string   `json:"path"`         // File path
	IsNew        bool     `json:"isNew"`        // Whether this is a new file
	IsDeleted    bool     `json:"isDeleted"`    // Whether the file was deleted
	BaseSaveHash string   `json:"baseSaveHash"` // Hash of the save this delta is based on (empty for full file)
	Patches      []string `json:"patches"`      // JSON representation of the patches
	ContentHash  string   `json:"contentHash"`  // Hash of the file content (for verification)
	Compressed   bool     `json:"compressed"`   // Whether the patches are compressed
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
			Compressed:   true, // Set to true by default
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
			Compressed:   true, // Set to true by default
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
		Compressed:   true, // Set to true by default
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

	// Handle compressed patches
	patchText := delta.Patches[0]
	if delta.Compressed {
		var err error
		patchText, err = decompressString(patchText)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress patches: %w", err)
		}
	}

	patches, err := dmp.PatchFromText(patchText)
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

// SaveDeltaSet stores a set of deltas to disk using the provided filesystem
func SaveDeltaSet(deltaSet DeltaSet, objectsDir string, fs FileSystem) error {
	// Create a new delta set with compressed patches
	compressedDeltaSet := DeltaSet{
		SaveHash: deltaSet.SaveHash,
		Deltas:   make([]DeltaInfo, len(deltaSet.Deltas)),
	}

	for i, delta := range deltaSet.Deltas {
		compressedDelta := delta

		// Compress the delta patches if they exist and the delta is marked for compression
		if delta.Compressed && delta.Patches != nil && len(delta.Patches) > 0 {
			// Compress the patch data
			compressed, err := compressString(delta.Patches[0])
			if err != nil {
				return fmt.Errorf("failed to compress delta for %s: %w", delta.Path, err)
			}
			compressedDelta.Patches = []string{compressed}
		}

		compressedDeltaSet.Deltas[i] = compressedDelta
	}

	// Create delta file path
	deltaPath := filepath.Join(objectsDir, "delta_"+deltaSet.SaveHash+".json")

	// Marshal to JSON
	data, err := json.MarshalIndent(compressedDeltaSet, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal delta set: %w", err)
	}

	// Write to file
	if err := fs.WriteFile(deltaPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write delta file: %w", err)
	}

	return nil
}

// LoadDeltaSet loads a set of deltas from disk using the provided filesystem
func LoadDeltaSet(saveHash, objectsDir string, fs FileSystem) (DeltaSet, error) {
	var deltaSet DeltaSet

	// Create delta file path
	deltaPath := filepath.Join(objectsDir, "delta_"+saveHash+".json")

	// Read file
	data, err := fs.ReadFile(deltaPath)
	if err != nil {
		return deltaSet, fmt.Errorf("failed to read delta file: %w", err)
	}

	// Unmarshal JSON
	if err := json.Unmarshal(data, &deltaSet); err != nil {
		return deltaSet, fmt.Errorf("failed to unmarshal delta set: %w", err)
	}

	return deltaSet, nil
}

// compressString compresses a string using gzip
func compressString(s string) (string, error) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write([]byte(s)); err != nil {
		return "", fmt.Errorf("failed to write to gzip writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip writer: %w", err)
	}
	return hex.EncodeToString(b.Bytes()), nil
}

// decompressString decompresses a hex-encoded gzipped string
func decompressString(s string) (string, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("failed to decode hex string: %w", err)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gz.Close()

	var b bytes.Buffer
	if _, err := io.Copy(&b, gz); err != nil {
		return "", fmt.Errorf("failed to read from gzip reader: %w", err)
	}

	return b.String(), nil
}

// calculateFileHash computes a SHA-256 hash of file content
func calculateFileHash(content []byte) string {
	h := sha256.New()
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

// CopyToFile copies content to a file, creating directories as needed using the provided filesystem
func CopyToFile(content []byte, targetPath string, fs FileSystem) error {
	// Create parent directories if needed
	targetDir := filepath.Dir(targetPath)
	if err := fs.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
	}

	// Write file
	return fs.WriteFile(targetPath, content, 0644)
}

// SaveFullFile saves a full copy of the file (for first version) using the provided filesystem
func SaveFullFile(content []byte, path, saveHash, objectsDir string, fs FileSystem) error {
	fullPath := filepath.Join(objectsDir, saveHash+"_"+path)

	// Always compress the content for storage
	// Create metadata indicating compression
	metadata := struct {
		Compressed  bool   `json:"compressed"`
		ContentHash string `json:"contentHash"`
	}{
		Compressed:  true,
		ContentHash: calculateFileHash(content),
	}

	// Compress the content
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write(content); err != nil {
		return fmt.Errorf("failed to compress file content: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("failed to close gzip writer: %w", err)
	}

	// Create combined content with metadata and compressed data
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal compression metadata: %w", err)
	}

	// Format: [metadata length (4 bytes)][metadata json][compressed content]
	metadataLen := len(metadataBytes)
	combinedContent := make([]byte, 4+metadataLen+b.Len())

	// Store metadata length
	combinedContent[0] = byte(metadataLen >> 24)
	combinedContent[1] = byte(metadataLen >> 16)
	combinedContent[2] = byte(metadataLen >> 8)
	combinedContent[3] = byte(metadataLen)

	// Copy metadata and compressed content
	copy(combinedContent[4:], metadataBytes)
	copy(combinedContent[4+metadataLen:], b.Bytes())

	return CopyToFile(combinedContent, fullPath, fs)
}

// GetFileContent retrieves file content either from working dir or saved object using the provided filesystem
func GetFileContent(path, saveHash, objectsDir string, fs FileSystem) ([]byte, error) {
	if saveHash == "" {
		// Read from working directory
		return fs.ReadFile(path)
	}

	// Read from objects directory
	filePath := filepath.Join(objectsDir, saveHash+"_"+path)
	content, err := fs.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Check if content is compressed (has metadata header)
	if len(content) > 8 { // Minimum size for metadata length + minimal JSON
		// Try to parse metadata length
		metadataLen := (int(content[0]) << 24) | (int(content[1]) << 16) | (int(content[2]) << 8) | int(content[3])

		// Validate metadata length
		if metadataLen > 0 && metadataLen < 1000 && 4+metadataLen < len(content) {
			// Extract and parse metadata
			metadata := struct {
				Compressed  bool   `json:"compressed"`
				ContentHash string `json:"contentHash"`
			}{}

			err := json.Unmarshal(content[4:4+metadataLen], &metadata)
			if err == nil && metadata.Compressed {
				// Content is compressed, decompress it
				compressedData := content[4+metadataLen:]
				gz, err := gzip.NewReader(bytes.NewReader(compressedData))
				if err != nil {
					return nil, fmt.Errorf("failed to create gzip reader: %w", err)
				}
				defer gz.Close()

				var b bytes.Buffer
				if _, err := io.Copy(&b, gz); err != nil {
					return nil, fmt.Errorf("failed to decompress content: %w", err)
				}

				decompressedContent := b.Bytes()

				// Verify content hash
				if calculateFileHash(decompressedContent) != metadata.ContentHash {
					return nil, fmt.Errorf("content hash mismatch after decompression")
				}

				return decompressedContent, nil
			}
		}
	}

	// Not compressed or invalid metadata, return as is
	return content, nil
}

// CalculateCompressionStats calculates and returns compression statistics for diagnostic purposes
func CalculateCompressionStats(deltaSet DeltaSet) (map[string]map[string]int, float64) {
	stats := make(map[string]map[string]int)
	var totalUncompressed, totalCompressed int

	for _, delta := range deltaSet.Deltas {
		if delta.Patches != nil && len(delta.Patches) > 0 {
			uncompressedSize := len(delta.Patches[0])
			totalUncompressed += uncompressedSize

			compressed, err := compressString(delta.Patches[0])
			if err == nil {
				compressedSize := len(compressed)
				totalCompressed += compressedSize

				stats[delta.Path] = map[string]int{
					"uncompressed": uncompressedSize,
					"compressed":   compressedSize,
					"saving":       uncompressedSize - compressedSize,
				}
			}
		}
	}

	var ratio float64
	if totalUncompressed > 0 {
		ratio = float64(totalCompressed) / float64(totalUncompressed)
	}

	return stats, ratio
}
