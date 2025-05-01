package util

import (
	"bytes"
	"path/filepath"
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
				Patches:      []string{"@@ -1,16 +1,17 @@\n Original%20\n+Modified%20\n content"},
				ContentHash:  calculateFileHash([]byte("Original Modified content")),
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
			},
			{
				Path:         "file2.txt",
				IsNew:        false,
				IsDeleted:    false,
				BaseSaveHash: "base123",
				Patches:      []string{"@@ -1,8 +1,9 @@\n test\n+new\n"},
				ContentHash:  "hash2",
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
			actual.ContentHash != expected.ContentHash {
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

	// Verify content
	savedContent, err := mockFS.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	if !bytes.Equal(savedContent, content) {
		t.Errorf("Saved content doesn't match original")
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

	mockFS.AddFile(path, workingContent)
	mockFS.AddFile(filepath.Join(objectsDir, saveHash+"_"+path), savedContent)

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
