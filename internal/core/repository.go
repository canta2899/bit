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
	deltaMode    = true // Use delta-based storage when true
	// Maximum number of deltas in a chain before storing a full file
	// Set to 0 to disable and rely purely on deltas
	maxDeltaChainLength = 10
)

type Save struct {
	Hash      string    `json:"hash"`
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Files     []string  `json:"files"`
	// If this is a delta save, this references the base save
	BaseSaveHash string `json:"baseSaveHash,omitempty"`
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
	dirs := []string{bitDir, objectsDir}
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

	// Load existing metadata to find the previous save
	metadata, err := loadMetadata()
	if err != nil {
		return "", fmt.Errorf("failed to load metadata: %w", err)
	}

	// Initialize delta storage values
	var baseSaveHash string
	var baseSave *Save

	// Find the most recent save to use as a base for deltas
	if deltaMode && len(metadata.Saves) > 0 {
		baseSave = &metadata.Saves[len(metadata.Saves)-1]
		baseSaveHash = baseSave.Hash
	}

	if deltaMode {
		// Use delta-based storage
		err = saveFilesAsDelta(files, hash, baseSave)
		if err != nil {
			return "", fmt.Errorf("failed to save files as delta: %w", err)
		}
	} else {
		// Use traditional full-file storage
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
	}

	// Update metadata
	save := Save{
		Hash:         hash,
		Name:         name,
		Timestamp:    timestamp,
		Files:        files,
		BaseSaveHash: baseSaveHash,
	}

	metadata.Saves = append(metadata.Saves, save)
	if err := saveMetadata(metadata); err != nil {
		return "", fmt.Errorf("failed to save metadata: %w", err)
	}

	return hash, nil
}

// saveFilesAsDelta saves files using delta-based storage
func saveFilesAsDelta(files []string, saveHash string, baseSave *Save) error {
	var deltas []util.DeltaInfo
	var baseFileMap map[string]bool
	deltaCounts := make(map[string]int) // Track delta chain length for each file

	// Create a map of files in the base save for quick lookup
	if baseSave != nil {
		baseFileMap = make(map[string]bool, len(baseSave.Files))
		for _, file := range baseSave.Files {
			baseFileMap[file] = true
		}

		// Calculate delta chain lengths from metadata
		metadata, err := loadMetadata()
		if err == nil {
			// Build a map of save hash to index for quick lookup
			saveMap := make(map[string]int, len(metadata.Saves))
			for i, save := range metadata.Saves {
				saveMap[save.Hash] = i
			}

			// For each file, traverse the delta chain to count its length
			for _, file := range files {
				currentHash := baseSave.Hash
				count := 0

				// Follow delta chain back until we find a save with a full file
				for currentHash != "" {
					saveIndex, found := saveMap[currentHash]
					if !found {
						break
					}

					save := metadata.Saves[saveIndex]

					// Check if this save has a full file content stored
					fullPath := filepath.Join(objectsDir, currentHash+"_"+file)
					if _, err := os.Stat(fullPath); err == nil {
						// Full file found, chain ends here
						break
					}

					// Move to base save and increment count
					currentHash = save.BaseSaveHash
					count++
				}

				deltaCounts[file] = count
			}
		}
	}

	// Process each file in the current state
	for _, file := range files {
		// Read current file content
		currentContent, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", file, err)
		}

		// Check if this file exists in the base save
		if baseSave != nil && baseFileMap[file] {
			// Try to read base content directly or from delta chain
			baseContent, err := getFileContentFromSave(file, baseSave.Hash)
			if err != nil {
				return fmt.Errorf("failed to read base file %s: %w", file, err)
			}

			// Calculate delta between base and current
			delta := util.CalculateDelta(baseContent, currentContent, file, baseSave.Hash)
			deltas = append(deltas, delta)

			// Store full file only if:
			// 1. The delta chain length exceeds our maximum limit (if configured)
			// 2. There are actual changes (delta.Patches is not nil/empty)
			if maxDeltaChainLength > 0 &&
				delta.Patches != nil &&
				len(delta.Patches) > 0 &&
				deltaCounts[file] >= maxDeltaChainLength {
				// Store full file to avoid excessive delta chain length
				err = util.SaveFullFile(currentContent, file, saveHash, objectsDir)
				if err != nil {
					return fmt.Errorf("failed to save full file %s: %w", file, err)
				}
			}
		} else {
			// This is a new file, store full content
			delta := util.CalculateDelta(nil, currentContent, file, "")
			deltas = append(deltas, delta)

			// Always store full content for new files
			err = util.SaveFullFile(currentContent, file, saveHash, objectsDir)
			if err != nil {
				return fmt.Errorf("failed to save full file %s: %w", file, err)
			}
		}
	}

	// Check for deleted files (files in base save but not in current save)
	if baseSave != nil {
		currentFileMap := make(map[string]bool, len(files))
		for _, file := range files {
			currentFileMap[file] = true
		}

		for _, file := range baseSave.Files {
			if !currentFileMap[file] {
				// Get base content
				baseContent, err := getFileContentFromSave(file, baseSave.Hash)
				if err != nil {
					return fmt.Errorf("failed to read base file %s: %w", file, err)
				}

				// Add a deletion delta
				delta := util.CalculateDelta(baseContent, nil, file, baseSave.Hash)
				deltas = append(deltas, delta)
			}
		}
	}

	// Save delta set
	deltaSet := util.DeltaSet{
		SaveHash: saveHash,
		Deltas:   deltas,
	}

	return util.SaveDeltaSet(deltaSet, objectsDir)
}

// getFileContentFromSave retrieves file content from a specific save
func getFileContentFromSave(file, saveHash string) ([]byte, error) {
	if saveHash == "" {
		return nil, fmt.Errorf("invalid save hash")
	}

	// Check if the file exists as full content first
	fullPath := filepath.Join(objectsDir, saveHash+"_"+file)
	if content, err := os.ReadFile(fullPath); err == nil {
		return content, nil
	}

	// If not found as full content, check for delta
	metadata, err := loadMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Find the save to get its base save
	var save *Save
	for i := range metadata.Saves {
		if metadata.Saves[i].Hash == saveHash {
			save = &metadata.Saves[i]
			break
		}
	}

	if save == nil {
		return nil, fmt.Errorf("save with hash %s not found", saveHash)
	}

	// Load delta set
	deltaSet, err := util.LoadDeltaSet(saveHash, objectsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load delta set: %w", err)
	}

	// Find delta for this file
	var fileDelta *util.DeltaInfo
	for i := range deltaSet.Deltas {
		if deltaSet.Deltas[i].Path == file {
			fileDelta = &deltaSet.Deltas[i]
			break
		}
	}

	if fileDelta == nil {
		return nil, fmt.Errorf("delta for file %s not found in save %s", file, saveHash)
	}

	// Apply delta using recursive content provider
	return util.ApplyDelta(*fileDelta, getFileContentFromSave)
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

			// Get the content of the .bitignore file from save
			ignoreContent, err := getFileContentFromSave(file, hash)
			if err != nil {
				return fmt.Errorf("failed to get ignore file content: %w", err)
			}

			// Write the .bitignore file
			if err := os.WriteFile(file, ignoreContent, 0644); err != nil {
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

		// Get file content from save (either directly or by applying deltas)
		content, err := getFileContentFromSave(file, hash)
		if err != nil {
			return fmt.Errorf("failed to get content for file %s: %w", file, err)
		}

		// Create parent directories if needed
		targetDir := filepath.Dir(file)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
		}

		// Write the file
		if err := os.WriteFile(file, content, 0644); err != nil {
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
