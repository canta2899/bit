package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOsFileSystem(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "filesystem_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create file system
	fs := NewOsFileSystem()

	// Test file operations
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := []byte("test content")

	// Test WriteFile and ReadFile
	err = fs.WriteFile(testFile, testContent, 0644)
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	content, err := fs.ReadFile(testFile)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(content) != string(testContent) {
		t.Errorf("ReadFile content mismatch: expected %q, got %q", testContent, content)
	}

	// Test Exists
	if !fs.Exists(testFile) {
		t.Errorf("Exists returned false for existing file")
	}

	if fs.Exists(filepath.Join(tmpDir, "nonexistent.txt")) {
		t.Errorf("Exists returned true for non-existent file")
	}

	// Test Stat
	info, err := fs.Stat(testFile)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("Stat name mismatch: expected %q, got %q", "test.txt", info.Name())
	}

	if info.Size() != int64(len(testContent)) {
		t.Errorf("Stat size mismatch: expected %d, got %d", len(testContent), info.Size())
	}

	// Test MkdirAll
	testDir := filepath.Join(tmpDir, "nested/dir/structure")
	err = fs.MkdirAll(testDir, 0755)
	if err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	if !fs.Exists(testDir) {
		t.Errorf("MkdirAll did not create directory")
	}

	// Test Open
	file, err := fs.Open(testFile)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	// Read some content from the file
	buffer := make([]byte, len(testContent))
	n, err := file.Read(buffer)
	if err != nil {
		t.Fatalf("Read from opened file failed: %v", err)
	}

	if n != len(testContent) || string(buffer) != string(testContent) {
		t.Errorf("File content mismatch after Open")
	}

	// Test Create
	newFile := filepath.Join(tmpDir, "new.txt")
	created, err := fs.Create(newFile)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer created.Close()

	newContent := []byte("new content")
	_, err = created.Write(newContent)
	if err != nil {
		t.Fatalf("Write to created file failed: %v", err)
	}

	// Close the file to ensure content is flushed
	created.Close()

	// Verify content
	readContent, err := fs.ReadFile(newFile)
	if err != nil {
		t.Fatalf("ReadFile failed for created file: %v", err)
	}

	if string(readContent) != string(newContent) {
		t.Errorf("Content mismatch for created file: expected %q, got %q", newContent, readContent)
	}

	// Test Remove
	err = fs.Remove(newFile)
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	if fs.Exists(newFile) {
		t.Errorf("File still exists after Remove")
	}

	// Test RemoveAll
	nestedFile := filepath.Join(testDir, "nested.txt")
	err = fs.WriteFile(nestedFile, []byte("nested content"), 0644)
	if err != nil {
		t.Fatalf("WriteFile failed for nested file: %v", err)
	}

	err = fs.RemoveAll(filepath.Join(tmpDir, "nested"))
	if err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}

	if fs.Exists(filepath.Join(tmpDir, "nested")) {
		t.Errorf("Directory still exists after RemoveAll")
	}

	// Test Walk
	// Create a few files for walking
	dirs := []string{
		filepath.Join(tmpDir, "walk/dir1"),
		filepath.Join(tmpDir, "walk/dir2"),
	}

	files := []string{
		filepath.Join(tmpDir, "walk/file1.txt"),
		filepath.Join(tmpDir, "walk/dir1/file2.txt"),
		filepath.Join(tmpDir, "walk/dir2/file3.txt"),
	}

	for _, dir := range dirs {
		err = fs.MkdirAll(dir, 0755)
		if err != nil {
			t.Fatalf("MkdirAll failed for walk test: %v", err)
		}
	}

	for _, file := range files {
		err = fs.WriteFile(file, []byte(filepath.Base(file)), 0644)
		if err != nil {
			t.Fatalf("WriteFile failed for walk test: %v", err)
		}
	}

	// Perform walk
	var visited []string
	err = fs.Walk(filepath.Join(tmpDir, "walk"), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, filepath.Base(path))
		return nil
	})

	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	// Check that we visited all files
	expectedItems := []string{"walk", "dir1", "dir2", "file1.txt", "file2.txt", "file3.txt"}
	if len(visited) != len(expectedItems) {
		t.Errorf("Walk didn't visit expected number of items: expected %d, got %d", len(expectedItems), len(visited))
	}

	// This is a basic test of Walk - we're just ensuring it runs without errors
	// A more comprehensive test would check the exact paths visited
}
