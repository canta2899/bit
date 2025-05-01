package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gobwas/glob"
)

func TestGetIgnorePatterns(t *testing.T) {
	// Create a temporary .bitignore file
	tempDir, err := os.MkdirTemp("", "ignore_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create .bitignore file with test patterns
	ignoreFile := filepath.Join(tempDir, ".bitignore")
	ignoreContent := `
# Comment line should be ignored
*.log
build/
node_modules/
  # Indented comment
  
# Empty line above should be ignored
/dist/
src/*.test.js
!important.log
`
	if err := os.WriteFile(ignoreFile, []byte(ignoreContent), 0644); err != nil {
		t.Fatalf("Failed to write ignore file: %v", err)
	}

	// Parse patterns
	patterns, err := GetIgnorePatterns(ignoreFile)
	if err != nil {
		t.Fatalf("GetIgnorePatterns failed: %v", err)
	}

	// Check number of patterns (excluding comments and empty lines)
	expectedPatterns := 6
	if len(patterns) != expectedPatterns {
		t.Errorf("Expected %d patterns, got %d", expectedPatterns, len(patterns))
	}

	// Test invalid file
	_, err = GetIgnorePatterns(filepath.Join(tempDir, "nonexistent"))
	if err == nil {
		t.Errorf("Expected error when reading non-existent file")
	}
}

func TestIsIgnored(t *testing.T) {
	// Create test patterns that match the actual implementation behavior
	// The implementation in ignore.go adds "**" to directory patterns and "**/" to file patterns without a slash
	patternStrings := []string{
		"**/*.log",           // Any .log file anywhere
		"**/build/**",        // Anything in build directory
		"**/node_modules/**", // node_modules directory
		"dist/**",            // dist directory at root
		"**/test/*.js",       // Any js files in a test directory
	}

	var patterns []glob.Glob
	for _, p := range patternStrings {
		g, err := glob.Compile(p)
		if err != nil {
			t.Fatalf("Failed to compile pattern %s: %v", p, err)
		}
		patterns = append(patterns, g)
	}

	tests := []struct {
		path     string
		expected bool
	}{
		{"file.log", true},             // *.log
		{"logs/error.log", true},       // *.log
		{"build/output", true},         // build/**
		{"build/nested/file", true},    // build/**
		{"node_modules/package", true}, // node_modules/**
		{"dist/bundle.js", true},       // dist/**
		{"src/dist/file", false},       // dist/** (only matches at root)
		{"src/test/test.js", true},     // **/test/*.js
		{"test/file.js", true},         // **/test/*.js
		{"regular.txt", false},         // Not matching any pattern
		{"src/component.js", false},    // Not matching any pattern
		{".bit/metadata.json", false},  // Not matching any pattern
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := IsIgnored(tc.path, patterns)
			if result != tc.expected {
				t.Errorf("IsIgnored(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}

func TestIsBitDirectory(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".bit", true},
		{".bit/", true},
		{".bit/objects", true},
		{".bit/metadata.json", true},
		{"not/.bit", false},
		{"mybit", false},
		{".bitmap", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			result := IsBitDirectory(tc.path)
			if result != tc.expected {
				t.Errorf("IsBitDirectory(%q) = %v, want %v", tc.path, result, tc.expected)
			}
		})
	}
}
