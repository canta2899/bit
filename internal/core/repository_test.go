package core

import (
	"bit/internal/util"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// mockFileSystemWithTestFiles extends MockFileSystem to expose test files for repository tests
type mockFileSystemWithTestFiles struct {
	*util.MockFileSystem
	testFiles []string
}

// NewMockFSWithTestFiles creates a new mock filesystem with test files tracking
func NewMockFSWithTestFiles() *mockFileSystemWithTestFiles {
	return &mockFileSystemWithTestFiles{
		MockFileSystem: util.NewMockFileSystem(),
		testFiles:      []string{},
	}
}

// AddTestFile adds a file to the mock filesystem and tracks it for testing
func (fs *mockFileSystemWithTestFiles) AddTestFile(path string, content []byte) {
	fs.MockFileSystem.AddFile(path, content)
	// Only add non-.bit files to the test files list
	if !util.IsBitDirectory(path) && path != ".bitignore" {
		fs.testFiles = append(fs.testFiles, path)
	}
}

// Walk overrides the standard Walk to expose test files directly when called
// from Repository.getFilesToSave
func (fs *mockFileSystemWithTestFiles) Walk(root string, walkFn filepath.WalkFunc) error {
	// If we're being called from getFilesToSave, return our test files
	if root == "." {
		// First create .bit directory if it doesn't exist
		if !fs.Exists(bitDir) {
			fs.MkdirAll(bitDir, 0755)
		}

		// Create fake file info for each file
		for _, path := range fs.testFiles {
			info := util.MockFileInfo{
				FileName:    filepath.Base(path),
				FileSize:    0,
				FileMode:    0644,
				FileModTime: time.Now(),
				FileIsDir:   false,
			}

			if err := walkFn(path, info, nil); err != nil {
				if err == filepath.SkipDir && path == bitDir {
					continue
				}
				return err
			}
		}

		// If .bitignore file exists, also return it
		if fs.Exists(".bitignore") {
			info := util.MockFileInfo{
				FileName:    ".bitignore",
				FileSize:    0,
				FileMode:    0644,
				FileModTime: time.Now(),
				FileIsDir:   false,
			}
			if err := walkFn(".bitignore", info, nil); err != nil {
				return err
			}
		}

		return nil
	}

	// Otherwise use the default implementation
	return fs.MockFileSystem.Walk(root, walkFn)
}

func TestInitRepository(t *testing.T) {
	// Create mock filesystem
	mockFS := util.NewMockFileSystem()
	repo := NewRepository(mockFS)

	// Test initialization
	err := repo.InitRepository()
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Verify .bit directory was created
	if !mockFS.Exists(bitDir) {
		t.Errorf("Expected %s directory to be created", bitDir)
	}

	// Verify objects directory was created
	if !mockFS.Exists(objectsDir) {
		t.Errorf("Expected %s directory to be created", objectsDir)
	}

	// Verify metadata file was created
	if !mockFS.Exists(metadataFile) {
		t.Errorf("Expected %s file to be created", metadataFile)
	}

	// Try to initialize again, should fail
	err = repo.InitRepository()
	if err == nil {
		t.Error("Expected error when initializing an already initialized repository")
	}
}

func TestSaveState(t *testing.T) {
	// Create mock filesystem with test files
	mockFS := NewMockFSWithTestFiles()
	repo := NewRepository(mockFS)

	// Initialize repository
	err := repo.InitRepository()
	if err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Create test files
	mockFS.AddTestFile("file1.txt", []byte("content of file1"))
	mockFS.AddTestFile("file2.txt", []byte("content of file2"))
	mockFS.AddTestFile("subdir/file3.txt", []byte("content in subdirectory"))

	// Add .bitignore file
	mockFS.AddFile(".bitignore", []byte(""))

	// Save state
	saveName := "Initial save"
	hash, err := repo.SaveState(saveName)
	if err != nil {
		t.Fatalf("Failed to save state: %v", err)
	}

	// Verify hash is not empty
	if hash == "" {
		t.Error("Expected non-empty hash")
	}

	// Verify metadata was updated
	metadata, err := repo.loadMetadata()
	if err != nil {
		t.Fatalf("Failed to load metadata: %v", err)
	}

	// Check save in metadata
	if len(metadata.Saves) != 1 {
		t.Errorf("Expected 1 save in metadata, got %d", len(metadata.Saves))
	}

	if metadata.Saves[0].Name != saveName {
		t.Errorf("Expected save name '%s', got '%s'", saveName, metadata.Saves[0].Name)
	}

	// Check that files were saved - note that .bitignore is also saved
	filesSaved := metadata.Saves[0].Files
	expectedFiles := []string{"file1.txt", "file2.txt", "subdir/file3.txt", ".bitignore"}

	if len(filesSaved) != len(expectedFiles) {
		t.Errorf("Expected %d files to be saved, got %d", len(expectedFiles), len(filesSaved))
		t.Errorf("Saved files: %v", filesSaved)
	} else {
		// Check each file is in the saved list
		for _, expectedFile := range expectedFiles {
			found := false
			for _, savedFile := range filesSaved {
				if savedFile == expectedFile {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("File %s was not saved", expectedFile)
			}
		}
	}
}

func TestSaveStateWithDeltas(t *testing.T) {
	// Create mock filesystem with test files
	mockFS := NewMockFSWithTestFiles()
	repo := NewRepository(mockFS)

	// Initialize repository
	if err := repo.InitRepository(); err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Create initial test file
	mockFS.AddTestFile("file.txt", []byte("Initial content"))

	// First save
	hash1, err := repo.SaveState("First save")
	if err != nil {
		t.Fatalf("Failed to create first save: %v", err)
	}

	// Modify file and create second save
	mockFS.AddTestFile("file.txt", []byte("Modified content"))
	mockFS.AddTestFile("file2.txt", []byte("New file content"))

	hash2, err := repo.SaveState("Second save")
	if err != nil {
		t.Fatalf("Failed to create second save: %v", err)
	}

	// Check that both hashes are different
	if hash1 == hash2 {
		t.Errorf("Expected different hashes for two saves, got %s for both", hash1)
	}

	// Load metadata and check save chain
	metadata, err := repo.loadMetadata()
	if err != nil {
		t.Fatalf("Failed to load metadata: %v", err)
	}

	if len(metadata.Saves) != 2 {
		t.Fatalf("Expected 2 saves in metadata, got %d", len(metadata.Saves))
	}

	// Check base save hash reference
	if metadata.Saves[1].BaseSaveHash != hash1 {
		t.Errorf("Expected second save to reference first save hash %s, got %s",
			hash1, metadata.Saves[1].BaseSaveHash)
	}

	// Check delta was created
	deltaPath := filepath.Join(objectsDir, "delta_"+hash2+".json")
	if !mockFS.Exists(deltaPath) {
		t.Errorf("Expected delta file %s to be created", deltaPath)
	}
}

func TestCheckout(t *testing.T) {
	// Create mock filesystem with test files
	mockFS := NewMockFSWithTestFiles()
	repo := NewRepository(mockFS)

	// Initialize repository
	if err := repo.InitRepository(); err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Setup initial state and save
	mockFS.AddTestFile("file1.txt", []byte("Initial content 1"))
	mockFS.AddTestFile("file2.txt", []byte("Initial content 2"))

	hash1, err := repo.SaveState("First state")
	if err != nil {
		t.Fatalf("Failed to save first state: %v", err)
	}

	// Modify files for second save
	mockFS.AddTestFile("file1.txt", []byte("Modified content 1"))
	mockFS.AddTestFile("file3.txt", []byte("New file content"))

	// Add ignored file
	mockFS.AddFile(".bitignore", []byte("ignored.txt"))
	mockFS.AddFile("ignored.txt", []byte("This file should be ignored"))

	hash2, err := repo.SaveState("Second state")
	if err != nil {
		t.Fatalf("Failed to save second state: %v", err)
	}

	// Checkout first save
	err = repo.Checkout(hash1)
	if err != nil {
		t.Fatalf("Failed to checkout first save: %v", err)
	}

	// Verify file contents match first save
	content1, err := mockFS.ReadFile("file1.txt")
	if err != nil {
		t.Fatalf("Failed to read file1.txt: %v", err)
	}
	if string(content1) != "Initial content 1" {
		t.Errorf("Expected file1.txt to contain 'Initial content 1', got '%s'", string(content1))
	}

	content2, err := mockFS.ReadFile("file2.txt")
	if err != nil {
		t.Fatalf("Failed to read file2.txt: %v", err)
	}
	if string(content2) != "Initial content 2" {
		t.Errorf("Expected file2.txt to contain 'Initial content 2', got '%s'", string(content2))
	}

	// file3.txt should not exist after checkout
	if mockFS.Exists("file3.txt") {
		t.Error("Expected file3.txt to be removed after checkout")
	}

	// ignored.txt should still exist
	if !mockFS.Exists("ignored.txt") {
		t.Error("Expected ignored.txt to still exist after checkout")
	}

	// Checkout back to second save
	err = repo.Checkout(hash2)
	if err != nil {
		t.Fatalf("Failed to checkout second save: %v", err)
	}

	// Verify file contents match second save
	content1, err = mockFS.ReadFile("file1.txt")
	if err != nil {
		t.Fatalf("Failed to read file1.txt: %v", err)
	}
	if string(content1) != "Modified content 1" {
		t.Errorf("Expected file1.txt to contain 'Modified content 1', got '%s'", string(content1))
	}

	// file3.txt should exist after checkout
	content3, err := mockFS.ReadFile("file3.txt")
	if err != nil {
		t.Fatalf("Failed to read file3.txt: %v", err)
	}
	if string(content3) != "New file content" {
		t.Errorf("Expected file3.txt to contain 'New file content', got '%s'", string(content3))
	}
}

func TestListSaves(t *testing.T) {
	// Create mock filesystem with test files
	mockFS := NewMockFSWithTestFiles()
	repo := NewRepository(mockFS)

	// Initialize repository
	if err := repo.InitRepository(); err != nil {
		t.Fatalf("Failed to initialize repository: %v", err)
	}

	// Create multiple saves
	saveNames := []string{"First save", "Second save", "Third save"}

	for i, name := range saveNames {
		// Add a different file for each save
		mockFS.AddTestFile(fmt.Sprintf("file%d.txt", i+1), []byte(fmt.Sprintf("Content %d", i+1)))

		// Create save
		_, err := repo.SaveState(name)
		if err != nil {
			t.Fatalf("Failed to create save '%s': %v", name, err)
		}
	}

	// List saves
	saves, err := repo.ListSaves()
	if err != nil {
		t.Fatalf("Failed to list saves: %v", err)
	}

	// Verify number of saves
	if len(saves) != len(saveNames) {
		t.Errorf("Expected %d saves, got %d", len(saveNames), len(saves))
	}

	// Verify save names
	for i, save := range saves {
		if save.Name != saveNames[i] {
			t.Errorf("Expected save name '%s', got '%s'", saveNames[i], save.Name)
		}
	}

	// Verify chronological order (most recent last)
	for i := 1; i < len(saves); i++ {
		if !saves[i-1].Timestamp.Before(saves[i].Timestamp) {
			t.Errorf("Expected saves to be ordered by timestamp")
		}
	}
}
