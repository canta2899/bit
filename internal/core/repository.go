package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"bit/internal/util"
)

const (
	bitDir       = ".bit"
	savesDir     = ".bit/saves"
	objectsDir   = ".bit/objects"
	ignoreFile   = ".bitignore"
	metadataFile = ".bit/metadata.json"
)

type Save struct {
	Hash      string    `json:"hash"`
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
}

type Metadata struct {
	Saves []Save `json:"saves"`
}

// InitRepository initializes a new bit repository
func InitRepository() error {
	// Check if .bit directory already exists
	if _, err := os.Stat(bitDir); !os.IsNotExist(err) {
		return fmt.Errorf("repository already initialized")
	}

	// Create directory structure
	dirs := []string{bitDir, savesDir, objectsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Initialize empty metadata file
	metadata := Metadata{Saves: []Save{}}
	return saveMetadata(metadata)
}

// SaveState creates a snapshot of the current state with the given name
func SaveState(name string) (string, error) {
	// Check if repository is initialized
	if _, err := os.Stat(bitDir); os.IsNotExist(err) {
		return "", fmt.Errorf("repository not initialized, run 'bit init' first")
	}

	// Get list of files to save (already excludes ignored files except .bitignore)
	files, err := getFilesToSave()
	if err != nil {
		return "", fmt.Errorf("failed to get files to save: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no files to save")
	}

	// Create save hash
	timestamp := time.Now()
	hash := createSaveHash(name, timestamp, files)
	savePath := filepath.Join(savesDir, hash)

	// Create save directory
	if err := os.MkdirAll(savePath, 0755); err != nil {
		return "", fmt.Errorf("failed to create save directory: %w", err)
	}

	// Copy files to save directory
	for _, file := range files {
		targetPath := filepath.Join(objectsDir, hash+"_"+file)
		targetDir := filepath.Dir(targetPath)

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		if err := copyFile(file, targetPath); err != nil {
			return "", fmt.Errorf("failed to copy file %s: %w", file, err)
		}
	}

	// Update metadata
	save := Save{
		Hash:      hash,
		Name:      name,
		Timestamp: timestamp,
		Files:     files,
	}

	metadata, err := loadMetadata()
	if err != nil {
		return "", fmt.Errorf("failed to load metadata: %w", err)
	}

	metadata.Saves = append(metadata.Saves, save)
	if err := saveMetadata(metadata); err != nil {
		return "", fmt.Errorf("failed to save metadata: %w", err)
	}

	return hash, nil
}

// ListSaves returns a list of all saves
func ListSaves() ([]Save, error) {
	metadata, err := loadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	return metadata.Saves, nil
}

// Checkout restores the project to the state of the given save hash
func Checkout(hash string) error {
	// Check if repository is initialized
	if _, err := os.Stat(bitDir); os.IsNotExist(err) {
		return fmt.Errorf("repository not initialized, run 'bit init' first")
	}

	// Load metadata
	metadata, err := loadMetadata()
	if err != nil {
		return fmt.Errorf("failed to load metadata: %w", err)
	}

	// Find the save with the given hash
	var save *Save
	for i := range metadata.Saves {
		if metadata.Saves[i].Hash == hash {
			save = &metadata.Saves[i]
			break
		}
	}

	if save == nil {
		return fmt.Errorf("save with hash %s not found", hash)
	}

	// Store all current ignored files before any changes
	currentIgnoredFiles := make(map[string]string) // map of path -> content

	// First, get a list of all current files
	currentFiles, err := listAllFiles()
	if err != nil {
		return fmt.Errorf("failed to get current files: %w", err)
	}

	// First restore the .bitignore file if it exists in the save
	var hasIgnoreFile bool
	for _, file := range save.Files {
		if file == ignoreFile {
			hasIgnoreFile = true
			sourcePath := filepath.Join(objectsDir, hash+"_"+file)
			if err := copyFile(sourcePath, file); err != nil {
				return fmt.Errorf("failed to restore ignore file: %w", err)
			}
			break
		}
	}

	// If no .bitignore in save, but one exists now, keep it
	if !hasIgnoreFile {
		for _, file := range currentFiles {
			if file == ignoreFile {
				hasIgnoreFile = true
				break
			}
		}
	}

	// Load ignore patterns from the restored or existing .bitignore file
	ignoredPatterns, err := util.GetIgnorePatterns(ignoreFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load ignore patterns: %w", err)
	}

	// Read content of all current ignored files before we make any changes
	for _, file := range currentFiles {
		if util.IsBitDirectory(file) || file == ignoreFile {
			continue
		}

		if util.IsIgnored(file, ignoredPatterns) {
			// Read file content
			content, err := os.ReadFile(file)
			if err == nil {
				currentIgnoredFiles[file] = string(content)
			}
			// We intentionally ignore errors here as they might be symlinks or special files
		}
	}

	// Remove non-ignored files that aren't in the save
	for _, file := range currentFiles {
		if util.IsBitDirectory(file) || file == ignoreFile {
			continue
		}

		// Don't remove ignored files
		if util.IsIgnored(file, ignoredPatterns) {
			continue
		}

		// Check if file is in the save
		inSave := false
		for _, savedFile := range save.Files {
			if file == savedFile {
				inSave = true
				break
			}
		}

		// Remove file if not in save
		if !inSave {
			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to remove file %s: %w", file, err)
			}
		}
	}

	// Restore non-ignored files from the save
	for _, file := range save.Files {
		// Skip .bit directory
		if util.IsBitDirectory(file) {
			continue
		}

		// Skip restoring ignored files (except .bitignore which we already handled)
		if file != ignoreFile && util.IsIgnored(file, ignoredPatterns) {
			continue
		}

		sourcePath := filepath.Join(objectsDir, hash+"_"+file)
		targetDir := filepath.Dir(file)

		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		if err := copyFile(sourcePath, file); err != nil {
			return fmt.Errorf("failed to restore file %s: %w", file, err)
		}
	}

	// Restore all previously existing ignored files
	for file, content := range currentIgnoredFiles {
		// Create parent directories if needed
		targetDir := filepath.Dir(file)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		// Write file content
		if err := os.WriteFile(file, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to restore ignored file %s: %w", file, err)
		}
	}

	return nil
}

// Helper functions

func getFilesToSave() ([]string, error) {
	var files []string

	// Load ignore patterns from .bitignore
	ignoredPatterns, err := util.GetIgnorePatterns(ignoreFile)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load ignore patterns: %w", err)
	}

	// Walk through the current directory and add all files
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// Skip .bit directory completely
			if path == bitDir || filepath.HasPrefix(path, bitDir+"/") {
				return filepath.SkipDir
			}
			return nil
		}

		// Always include .bitignore file
		if path == ignoreFile {
			files = append(files, path)
			return nil
		}

		// Skip files matching ignore patterns
		if util.IsIgnored(path, ignoredPatterns) {
			// We intentionally skip ALL ignored files
			return nil
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return files, nil
}

func createSaveHash(name string, timestamp time.Time, files []string) string {
	h := sha256.New()
	h.Write([]byte(name))
	h.Write([]byte(timestamp.String()))
	for _, file := range files {
		h.Write([]byte(file))
	}
	return hex.EncodeToString(h.Sum(nil))[:12] // Use first 12 characters of hash for brevity
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func loadMetadata() (Metadata, error) {
	var metadata Metadata

	data, err := os.ReadFile(metadataFile)
	if os.IsNotExist(err) {
		return Metadata{Saves: []Save{}}, nil
	} else if err != nil {
		return metadata, err
	}

	err = json.Unmarshal(data, &metadata)
	return metadata, err
}

func saveMetadata(metadata Metadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(metadataFile, data, 0644)
}

// listAllFiles lists all files in the workspace (including ignored files)
func listAllFiles() ([]string, error) {
	var files []string

	// Walk through the current directory and add all files
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			// Skip .bit directory completely
			if path == bitDir || filepath.HasPrefix(path, bitDir+"/") {
				return filepath.SkipDir
			}
			return nil
		}

		files = append(files, path)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	return files, nil
}
