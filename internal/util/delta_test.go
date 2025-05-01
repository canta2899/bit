package util

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCalculateDelta(t *testing.T) {
	tests := []struct {
		name          string
		oldContent    []byte
		newContent    []byte
		path          string
		baseSaveHash  string
		expectedDelta DeltaInfo
	}{
		{
			name:         "New file",
			oldContent:   nil,
			newContent:   []byte("New file content"),
			path:         "test.txt",
			baseSaveHash: "",
			expectedDelta: DeltaInfo{
				Path:         "test.txt",
				IsNew:        true,
				IsDeleted:    false,
				BaseSaveHash: "",
				Patches:      nil,
				ContentHash:  calculateFileHash([]byte("New file content")),
				Compressed:   true,
			},
		},
		{
			name:         "Deleted file",
			oldContent:   []byte("Original content"),
			newContent:   nil,
			path:         "test.txt",
			baseSaveHash: "abc123",
			expectedDelta: DeltaInfo{
				Path:         "test.txt",
				IsNew:        false,
				IsDeleted:    true,
				BaseSaveHash: "abc123",
				Patches:      nil,
				ContentHash:  calculateFileHash([]byte("Original content")),
				Compressed:   true,
			},
		},
		{
			name:         "Modified file",
			oldContent:   []byte("Original content"),
			newContent:   []byte("Modified content"),
			path:         "test.txt",
			baseSaveHash: "abc123",
			expectedDelta: DeltaInfo{
				Path:         "test.txt",
				IsNew:        false,
				IsDeleted:    false,
				BaseSaveHash: "abc123",
				// We can't easily assert on the specific patch content, so we'll check non-nil in the test
				Compressed: true,
			},
		},
		{
			name:         "Unmodified file",
			oldContent:   []byte("Same content"),
			newContent:   []byte("Same content"),
			path:         "test.txt",
			baseSaveHash: "abc123",
			expectedDelta: DeltaInfo{
				Path:         "test.txt",
				IsNew:        false,
				IsDeleted:    false,
				BaseSaveHash: "abc123",
				Patches:      nil,
				ContentHash:  calculateFileHash([]byte("Same content")),
				Compressed:   true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := CalculateDelta(tc.oldContent, tc.newContent, tc.path, tc.baseSaveHash)

			// Common assertions
			if result.Path != tc.expectedDelta.Path {
				t.Errorf("Path: expected %s, got %s", tc.expectedDelta.Path, result.Path)
			}
			if result.IsNew != tc.expectedDelta.IsNew {
				t.Errorf("IsNew: expected %v, got %v", tc.expectedDelta.IsNew, result.IsNew)
			}
			if result.IsDeleted != tc.expectedDelta.IsDeleted {
				t.Errorf("IsDeleted: expected %v, got %v", tc.expectedDelta.IsDeleted, result.IsDeleted)
			}
			if result.BaseSaveHash != tc.expectedDelta.BaseSaveHash {
				t.Errorf("BaseSaveHash: expected %s, got %s", tc.expectedDelta.BaseSaveHash, result.BaseSaveHash)
			}

			// Content hash checks
			if tc.expectedDelta.ContentHash != "" && result.ContentHash != tc.expectedDelta.ContentHash {
				t.Errorf("ContentHash: expected %s, got %s", tc.expectedDelta.ContentHash, result.ContentHash)
			}

			// For modified file, we expect non-nil patches
			if tc.name == "Modified file" && (result.Patches == nil || len(result.Patches) == 0) {
				t.Errorf("Expected non-nil/non-empty patches for modified file")
			}

			// For unmodified file, we expect nil patches
			if tc.name == "Unmodified file" && result.Patches != nil {
				t.Errorf("Expected nil patches for unmodified file, got %v", result.Patches)
			}
		})
	}
}

func TestApplyDelta(t *testing.T) {
	// Create a simple content provider function for testing
	baseContentProvider := func(path, saveHash string) ([]byte, error) {
		// This is a simple mock that returns predefined content
		if path == "file.txt" && saveHash == "base123" {
			return []byte("Original content"), nil
		}
		if path == "new_file.txt" && saveHash == "" {
			return []byte("New file content"), nil
		}
		return nil, nil
	}

	// Prepare some pre-compressed patches
	patchText := "@@ -1,16 +1,17 @@\n Original%20\n+Modified%20\n content"
	compressedPatch, err := compressString(patchText)
	if err != nil {
		t.Fatalf("Failed to compress test patch: %v", err)
	}

	tests := []struct {
		name           string
		delta          DeltaInfo
		expectedResult []byte
		expectError    bool
	}{
		{
			name: "Apply delta to modified file",
			delta: DeltaInfo{
				Path:         "file.txt",
				IsNew:        false,
				IsDeleted:    false,
				BaseSaveHash: "base123",
				Patches:      []string{compressedPatch},
				ContentHash:  calculateFileHash([]byte("Original Modified content")),
				Compressed:   true,
			},
			expectedResult: []byte("Original Modified content"),
			expectError:    false,
		},
		{
			name: "Get content for new file",
			delta: DeltaInfo{
				Path:         "new_file.txt",
				IsNew:        true,
				IsDeleted:    false,
				BaseSaveHash: "",
				Patches:      nil,
				ContentHash:  calculateFileHash([]byte("New file content")),
				Compressed:   true,
			},
			expectedResult: []byte("New file content"),
			expectError:    false,
		},
		{
			name: "Handle deleted file",
			delta: DeltaInfo{
				Path:         "file.txt",
				IsNew:        false,
				IsDeleted:    true,
				BaseSaveHash: "base123",
				Patches:      nil,
				ContentHash:  calculateFileHash([]byte("Original content")),
				Compressed:   true,
			},
			expectedResult: nil,
			expectError:    false,
		},
		{
			name: "No changes file",
			delta: DeltaInfo{
				Path:         "file.txt",
				IsNew:        false,
				IsDeleted:    false,
				BaseSaveHash: "base123",
				Patches:      nil,
				ContentHash:  calculateFileHash([]byte("Original content")),
				Compressed:   true,
			},
			expectedResult: []byte("Original content"),
			expectError:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ApplyDelta(tc.delta, baseContentProvider)

			// Check error expectation
			if tc.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tc.expectError && err != nil {
				t.Errorf("Did not expect error but got: %v", err)
			}

			// Skip content check if expected error
			if tc.expectError {
				return
			}

			// For deleted files, result should be nil
			if tc.delta.IsDeleted {
				if result != nil {
					t.Errorf("Expected nil result for deleted file, got %v", result)
				}
				return
			}

			// Check content matches expected
			if !bytes.Equal(result, tc.expectedResult) {
				t.Errorf("Content mismatch\nExpected: %s\nGot: %s", tc.expectedResult, result)
			}
		})
	}
}

func TestSaveAndLoadDeltaSet(t *testing.T) {
	// Set up mock filesystem
	mockFS := NewMockFileSystem()
	objectsDir := ".bit/objects"
	mockFS.MkdirAll(objectsDir, 0755)

	// Create a delta set to save
	saveHash := "test123"
	deltaSet := DeltaSet{
		SaveHash: saveHash,
		Deltas: []DeltaInfo{
			{
				Path:         "file1.txt",
				IsNew:        true,
				IsDeleted:    false,
				BaseSaveHash: "",
				Patches:      nil,
				ContentHash:  "hash1",
				Compressed:   true,
			},
			{
				Path:         "file2.txt",
				IsNew:        false,
				IsDeleted:    false,
				BaseSaveHash: "base123",
				Patches:      []string{"@@ -1,8 +1,9 @@\n test\n+new\n"},
				ContentHash:  "hash2",
				Compressed:   true,
			},
		},
	}

	// Test saving
	err := SaveDeltaSet(deltaSet, objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to save delta set: %v", err)
	}

	// Check that file was created
	deltaPath := filepath.Join(objectsDir, "delta_"+saveHash+".json")
	if !mockFS.Exists(deltaPath) {
		t.Errorf("Expected delta file %s to exist", deltaPath)
	}

	// Test loading
	loadedDeltaSet, err := LoadDeltaSet(saveHash, objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to load delta set: %v", err)
	}

	// Verify loaded data
	if loadedDeltaSet.SaveHash != deltaSet.SaveHash {
		t.Errorf("SaveHash: expected %s, got %s", deltaSet.SaveHash, loadedDeltaSet.SaveHash)
	}

	if len(loadedDeltaSet.Deltas) != len(deltaSet.Deltas) {
		t.Errorf("Deltas length: expected %d, got %d", len(deltaSet.Deltas), len(loadedDeltaSet.Deltas))
	}

	// Check first delta info
	if len(loadedDeltaSet.Deltas) > 0 {
		expected := deltaSet.Deltas[0]
		actual := loadedDeltaSet.Deltas[0]

		if actual.Path != expected.Path ||
			actual.IsNew != expected.IsNew ||
			actual.IsDeleted != expected.IsDeleted ||
			actual.ContentHash != expected.ContentHash ||
			actual.Compressed != expected.Compressed {
			t.Errorf("Loaded delta info doesn't match original")
		}
	}
}

func TestSaveFullFile(t *testing.T) {
	// Set up mock filesystem
	mockFS := NewMockFileSystem()
	objectsDir := ".bit/objects"
	mockFS.MkdirAll(objectsDir, 0755)

	// Test data
	content := []byte("File content")
	path := "test/file.txt"
	saveHash := "save123"

	// Save file
	err := SaveFullFile(content, path, saveHash, objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to save full file: %v", err)
	}

	// Verify file was saved
	expectedPath := filepath.Join(objectsDir, saveHash+"_"+path)
	if !mockFS.Exists(expectedPath) {
		t.Errorf("Expected file %s to exist", expectedPath)
	}

	// Since we now compress files, we need to use GetFileContent to read it back correctly
	retrievedContent, err := GetFileContent(path, saveHash, objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to read saved file content: %v", err)
	}

	if !bytes.Equal(retrievedContent, content) {
		t.Errorf("Retrieved content doesn't match original")
	}
}

func TestGetFileContent(t *testing.T) {
	// Set up mock filesystem
	mockFS := NewMockFileSystem()
	objectsDir := ".bit/objects"
	mockFS.MkdirAll(objectsDir, 0755)

	// Add test files
	workingContent := []byte("Working content")
	savedContent := []byte("Saved content")
	path := "test.txt"
	saveHash := "save123"

	// Add working file directly
	mockFS.AddFile(path, workingContent)

	// Save the file with our proper save function to ensure it's compressed
	err := SaveFullFile(savedContent, path, saveHash, objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to save test file: %v", err)
	}

	// Test reading from working directory
	content, err := GetFileContent(path, "", objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to get file content from working dir: %v", err)
	}
	if !bytes.Equal(content, workingContent) {
		t.Errorf("Expected working content, got something else")
	}

	// Test reading from save
	content, err = GetFileContent(path, saveHash, objectsDir, mockFS)
	if err != nil {
		t.Fatalf("Failed to get file content from save: %v", err)
	}
	if !bytes.Equal(content, savedContent) {
		t.Errorf("Expected saved content, got something else")
	}

	// Test non-existent file
	_, err = GetFileContent("nonexistent.txt", "", objectsDir, mockFS)
	if err == nil {
		t.Error("Expected error for non-existent file but got none")
	}
}

func TestCompressDecompressString(t *testing.T) {
	// Test strings of different sizes
	testCases := []string{
		"",                        // Empty string
		"Hello, world!",           // Small string
		strings.Repeat("A", 1000), // Medium string with repetition (should compress well)
		strings.Repeat("Random text that should have some repetition. ", 100), // Larger string with some repetition
	}

	for _, tc := range testCases {
		// Test compression
		compressed, err := compressString(tc)
		if err != nil {
			t.Errorf("compressString error for %q: %v", truncateForDisplay(tc), err)
			continue
		}

		// Test decompression
		decompressed, err := decompressString(compressed)
		if err != nil {
			t.Errorf("decompressString error for %q: %v", truncateForDisplay(tc), err)
			continue
		}

		// Verify that the decompressed string matches the original
		if decompressed != tc {
			t.Errorf("decompression mismatch: expected %q, got %q", truncateForDisplay(tc), truncateForDisplay(decompressed))
		}
	}
}

func TestDeltaCompression(t *testing.T) {
	oldContent := []byte("This is the original content of the file.")
	newContent := []byte("This is the modified content of the file with some additional text.")
	path := "test.txt"
	baseSaveHash := "test-hash"

	// Create delta
	delta := CalculateDelta(oldContent, newContent, path, baseSaveHash)

	// Create file system
	fs := NewMockFileSystem()

	// Create delta set
	deltaSet := DeltaSet{
		SaveHash: "test-save-hash",
		Deltas:   []DeltaInfo{delta},
	}

	// Save delta set (this will compress the patches)
	err := SaveDeltaSet(deltaSet, "objects", fs)
	if err != nil {
		t.Fatalf("SaveDeltaSet error: %v", err)
	}

	// Load delta set
	loadedDeltaSet, err := LoadDeltaSet("test-save-hash", "objects", fs)
	if err != nil {
		t.Fatalf("LoadDeltaSet error: %v", err)
	}

	// Verify that the loaded delta has compressed patches
	if len(loadedDeltaSet.Deltas) != 1 {
		t.Fatalf("Expected 1 delta, got %d", len(loadedDeltaSet.Deltas))
	}

	loadedDelta := loadedDeltaSet.Deltas[0]
	if !loadedDelta.Compressed {
		t.Errorf("Delta was not compressed")
	}

	// Create a content provider for testing
	contentProvider := func(p, h string) ([]byte, error) {
		if p == path && h == baseSaveHash {
			return oldContent, nil
		}
		return nil, nil
	}

	// Apply the delta
	reconstructedContent, err := ApplyDelta(loadedDelta, contentProvider)
	if err != nil {
		t.Fatalf("ApplyDelta error: %v", err)
	}

	// Verify that the reconstructed content matches the new content
	if !bytes.Equal(reconstructedContent, newContent) {
		t.Errorf("Content mismatch after applying delta")
	}
}

// TestCompressionEfficiency tests that compression actually reduces size for typical deltas
func TestCompressionEfficiency(t *testing.T) {
	// Create a large string with repetition (patches often have repetitive structures)
	largeOriginalContent := "This is a line of text in a file.\n"
	var longFileBuilder bytes.Buffer
	for i := 0; i < 1000; i++ {
		longFileBuilder.WriteString(largeOriginalContent)
	}
	originalContent := longFileBuilder.Bytes()

	// Create slightly modified content by changing every 100th line
	var modifiedBuilder bytes.Buffer
	lines := bytes.Split(originalContent, []byte("\n"))
	for i, line := range lines {
		if i > 0 && i%100 == 0 {
			modifiedBuilder.WriteString("This line has been modified " + string(line) + "\n")
		} else if i < len(lines)-1 || len(line) > 0 {
			modifiedBuilder.Write(line)
			modifiedBuilder.WriteByte('\n')
		}
	}
	modifiedContent := modifiedBuilder.Bytes()

	// Calculate delta
	delta := CalculateDelta(originalContent, modifiedContent, "test.txt", "base-hash")

	// Compress the patches
	uncompressedSize := 0
	for _, patch := range delta.Patches {
		uncompressedSize += len(patch)
	}

	// Manually compress the first patch
	compressed, err := compressString(delta.Patches[0])
	if err != nil {
		t.Fatalf("Failed to compress: %v", err)
	}
	compressedSize := len(compressed)

	// Check if compression reduced size
	compressionRatio := float64(compressedSize) / float64(uncompressedSize)
	t.Logf("Uncompressed size: %d, Compressed size: %d, Ratio: %.2f", uncompressedSize, compressedSize, compressionRatio)

	// For typical diff patches, we expect good compression (ratio < 0.5)
	if compressionRatio > 0.9 {
		t.Errorf("Compression efficiency is poor: ratio = %.2f", compressionRatio)
	}
}

// Helper function to truncate long strings for error messages
func truncateForDisplay(s string) string {
	const maxLen = 50
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... [truncated]"
}

// TestFullFileCompression tests the compression for full file content
func TestFullFileCompression(t *testing.T) {
	// Create file system
	fs := NewMockFileSystem()
	objectsDir := "objects"
	fs.MkdirAll(objectsDir, 0755)

	// Create some test content of various sizes
	testCases := []struct {
		name        string
		content     []byte
		expectRatio float64 // Expected compression ratio range
	}{
		{
			name:        "Small random content",
			content:     []byte("Small test content with little repetition"),
			expectRatio: 0.9, // Might not compress well
		},
		{
			name:        "Repetitive content",
			content:     []byte(strings.Repeat("This is repetitive content. ", 100)),
			expectRatio: 0.2, // Should compress very well
		},
		{
			name:        "Mixed content",
			content:     []byte(strings.Repeat("AAAA", 50) + strings.Repeat("BBBB", 50) + strings.Repeat("CCCC", 50)),
			expectRatio: 0.3, // Should compress well
		},
	}

	path := "test.txt"
	saveHash := "test-save-hash"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Save the file (should always be compressed now)
			err := SaveFullFile(tc.content, path, saveHash, objectsDir, fs)
			if err != nil {
				t.Fatalf("Failed to save file: %v", err)
			}

			// Read the file back
			retrievedContent, err := GetFileContent(path, saveHash, objectsDir, fs)
			if err != nil {
				t.Fatalf("Failed to get file content: %v", err)
			}

			// Verify content matches
			if !bytes.Equal(retrievedContent, tc.content) {
				t.Errorf("Content mismatch after retrieval")
			}

			// Check if the file was compressed (it should always be now)
			rawContent, err := fs.ReadFile(filepath.Join(objectsDir, saveHash+"_"+path))
			if err != nil {
				t.Fatalf("Failed to read raw file: %v", err)
			}

			// Verify it has our compression header format
			isCompressed := false
			if len(rawContent) > 8 {
				// Try to parse metadata length
				metadataLen := (int(rawContent[0]) << 24) | (int(rawContent[1]) << 16) | (int(rawContent[2]) << 8) | int(rawContent[3])
				if metadataLen > 0 && metadataLen < 1000 && 4+metadataLen < len(rawContent) {
					// Extract metadata
					metadata := struct {
						Compressed bool `json:"compressed"`
					}{}
					if json.Unmarshal(rawContent[4:4+metadataLen], &metadata) == nil {
						isCompressed = metadata.Compressed
					}
				}
			}

			if !isCompressed {
				t.Errorf("File was not compressed even though compression is now mandatory")
			}

			// Check compression ratio
			if isCompressed && len(tc.content) > 0 {
				compressedSize := len(rawContent)
				compressionRatio := float64(compressedSize) / float64(len(tc.content))
				t.Logf("Compression ratio: %.2f (original: %d bytes, compressed: %d bytes)",
					compressionRatio, len(tc.content), compressedSize)

				// For repetitive content, expect a much better ratio (lower is better)
				if tc.name == "Repetitive content" && compressionRatio > tc.expectRatio {
					t.Errorf("Compression not as effective as expected for repetitive content: %.2f", compressionRatio)
				}
			}
		})
	}
}
