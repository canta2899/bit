package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandLineInterface tests the command line interface
// This is an integration test that runs the actual 'bit' command
func TestCommandLineInterface(t *testing.T) {
	// Skip if running in CI environment
	if os.Getenv("CI") != "" {
		t.Skip("Skipping integration test in CI environment")
	}

	// Get the path to the bit executable
	bitCmd, err := exec.LookPath("bit")
	if err != nil {
		// If bit is not in PATH, try to find it relative to test file
		testDir, err := os.Getwd()
		if err != nil {
			t.Fatalf("Failed to get working directory: %v", err)
		}

		// Try to find bit executable in standard locations
		bitCmd = filepath.Join(testDir, "..", "..", "bin", "bit")
		if _, err := os.Stat(bitCmd); os.IsNotExist(err) {
			// Try to build it
			buildCmd := exec.Command("go", "build", "-o", "bit")
			buildCmd.Dir = filepath.Join(testDir)
			if err := buildCmd.Run(); err != nil {
				t.Fatalf("Failed to build bit command: %v", err)
			}
			bitCmd = filepath.Join(testDir, "bit")
			defer os.Remove(bitCmd) // Clean up after test
		}
	}

	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "bit-cli-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to the temporary directory
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(origDir) // Change back to original directory

	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Test 'bit' without arguments (should print usage)
	cmd := exec.Command(bitCmd)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("Expected error when running 'bit' without arguments")
	}
	if !bytes.Contains(output, []byte("Usage:")) {
		t.Errorf("Expected usage information in output")
	}

	// Test 'bit init'
	cmd = exec.Command(bitCmd, "init")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run 'bit init': %v\nOutput: %s", err, output)
	}

	// Verify .bit directory was created
	if _, err := os.Stat(".bit"); os.IsNotExist(err) {
		t.Errorf(".bit directory not created after 'bit init'")
	}

	// Create test files
	testContent := "Initial test content"
	err = os.WriteFile("test.txt", []byte(testContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test 'bit save'
	cmd = exec.Command(bitCmd, "save", "Initial save")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run 'bit save': %v\nOutput: %s", err, output)
	}
	if !bytes.Contains(output, []byte("Saved state")) {
		t.Errorf("Expected success message from 'bit save'")
	}

	// Modify test file
	modifiedContent := "Modified test content"
	err = os.WriteFile("test.txt", []byte(modifiedContent), 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Create another test file
	err = os.WriteFile("another.txt", []byte("Another file"), 0644)
	if err != nil {
		t.Fatalf("Failed to create another test file: %v", err)
	}

	// Test 'bit save' again
	cmd = exec.Command(bitCmd, "save", "Second save")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run second 'bit save': %v\nOutput: %s", err, output)
	}

	// Test 'bit list'
	cmd = exec.Command(bitCmd, "list")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run 'bit list': %v\nOutput: %s", err, output)
	}

	outputStr := string(output)
	if !strings.Contains(outputStr, "Initial save") || !strings.Contains(outputStr, "Second save") {
		t.Errorf("Expected both saves to be listed in 'bit list' output")
	}

	// Extract hash from list output for testing checkout
	lines := strings.Split(outputStr, "\n")
	var hash string
	for _, line := range lines {
		if strings.Contains(line, "Initial save") {
			hash = strings.Fields(line)[0]
			break
		}
	}

	if hash == "" {
		t.Fatalf("Failed to extract hash from 'bit list' output")
	}

	// Test 'bit checkout'
	cmd = exec.Command(bitCmd, "checkout", hash)
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run 'bit checkout': %v\nOutput: %s", err, output)
	}

	// Verify test.txt was restored to initial content
	content, err := os.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("Failed to read test file after checkout: %v", err)
	}
	if string(content) != testContent {
		t.Errorf("File content not restored correctly after checkout")
	}

	// Verify another.txt was removed (it didn't exist in the first save)
	if _, err := os.Stat("another.txt"); !os.IsNotExist(err) {
		t.Errorf("Expected another.txt to be removed after checkout")
	}

	// Test 'bit now' (should checkout latest save)
	cmd = exec.Command(bitCmd, "now")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run 'bit now': %v\nOutput: %s", err, output)
	}

	// Verify test.txt was restored to modified content
	content, err = os.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("Failed to read test file after 'bit now': %v", err)
	}
	if string(content) != modifiedContent {
		t.Errorf("File content not restored correctly after 'bit now'")
	}

	// Verify another.txt exists again
	if _, err := os.Stat("another.txt"); os.IsNotExist(err) {
		t.Errorf("Expected another.txt to exist after 'bit now'")
	}

	// Test unknown command
	cmd = exec.Command(bitCmd, "unknown")
	output, err = cmd.CombinedOutput()
	if err == nil {
		t.Errorf("Expected error with unknown command")
	}
	if !bytes.Contains(output, []byte("Unknown command")) {
		t.Errorf("Expected 'Unknown command' message")
	}

	// Test 'bit debug' (just make sure it runs without error)
	cmd = exec.Command(bitCmd, "debug")
	_, err = cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Failed to run 'bit debug': %v", err)
	}
}
