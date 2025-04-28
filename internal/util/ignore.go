package util

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/gobwas/glob"
)

// GetIgnorePatterns loads ignore patterns from .bitignore file
func GetIgnorePatterns(ignoreFile string) ([]glob.Glob, error) {
	file, err := os.Open(ignoreFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var patterns []glob.Glob
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Convert the pattern to a glob pattern
		pattern := line

		// Handle directory patterns (ending with /)
		if strings.HasSuffix(pattern, "/") {
			pattern = pattern + "**"
		}

		// Handle file patterns
		if !strings.Contains(pattern, "/") {
			// *.log should match both test.log and subfolder/test.log
			pattern = "**/" + pattern
		}

		// Compile the pattern
		compiledPattern, err := glob.Compile(pattern)
		if err != nil {
			return nil, err
		}
		patterns = append(patterns, compiledPattern)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return patterns, nil
}

// IsIgnored checks if a file path matches any of the ignore patterns
func IsIgnored(path string, patterns []glob.Glob) bool {
	// Normalize path to use forward slashes
	normalizedPath := filepath.ToSlash(path)

	// Also try with a leading ./ as some patterns might be specified that way
	altPath := "./" + normalizedPath

	for _, pattern := range patterns {
		if pattern.Match(normalizedPath) || pattern.Match(altPath) {
			return true
		}
	}

	return false
}

// IsBitDirectory checks if a path is inside the .bit directory
func IsBitDirectory(path string) bool {
	return path == ".bit" || strings.HasPrefix(path, ".bit/")
}
