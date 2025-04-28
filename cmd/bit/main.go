package main

import (
	"fmt"
	"os"
	"strings"

	"bit/internal/core"
	"bit/internal/util"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "init":
		handleInit()
	case "save":
		handleSave()
	case "list":
		handleList()
	case "checkout":
		handleCheckout()
	case "now":
		handleNow()
	case "debug":
		handleDebug()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: bit <command> [options]")
	fmt.Println("Commands:")
	fmt.Println("  init                Initialize a .bit repository")
	fmt.Println("  save <name>         Save the current state with the given name")
	fmt.Println("  list                List all saved states")
	fmt.Println("  checkout <hash>     Restore files to the state of the given hash")
	fmt.Println("  now                 Restore files to the latest saved state")
}

func handleInit() {
	err := core.InitRepository()
	if err != nil {
		fmt.Printf("Error initializing repository: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Initialized empty bit repository in .bit/")
}

func handleSave() {
	if len(os.Args) < 3 {
		fmt.Println("Error: Save name required")
		fmt.Println("Usage: bit save <name>")
		os.Exit(1)
	}
	name := strings.Join(os.Args[2:], " ")
	hash, err := core.SaveState(name)
	if err != nil {
		fmt.Printf("Error saving state: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Saved state '%s' with hash %s\n", name, hash)
}

func handleList() {
	saves, err := core.ListSaves()
	if err != nil {
		fmt.Printf("Error listing saves: %v\n", err)
		os.Exit(1)
	}

	if len(saves) == 0 {
		fmt.Println("No saves found")
		return
	}

	fmt.Println("Saves:")
	for _, save := range saves {
		fmt.Printf("  %s  %s\n", save.Hash, save.Name)
	}
}

func handleCheckout() {
	if len(os.Args) < 3 {
		fmt.Println("Error: Save hash required")
		fmt.Println("Usage: bit checkout <hash>")
		os.Exit(1)
	}

	hash := os.Args[2]
	err := core.Checkout(hash)
	if err != nil {
		fmt.Printf("Error checking out save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully checked out save with hash %s\n", hash)
}

func handleNow() {
	saves, err := core.ListSaves()
	if err != nil {
		fmt.Printf("Error listing saves: %v\n", err)
		os.Exit(1)
	}

	if len(saves) == 0 {
		fmt.Println("No saves found")
		return
	}

	// Get the latest save (last in the list)
	latestSave := saves[len(saves)-1]
	err = core.Checkout(latestSave.Hash)
	if err != nil {
		fmt.Printf("Error checking out latest save: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Successfully checked out latest save '%s' with hash %s\n", latestSave.Name, latestSave.Hash)
}

func handleDebug() {
	// Test ignore patterns
	patterns, err := util.GetIgnorePatterns(".bitignore")
	if err != nil {
		fmt.Printf("Error loading ignore patterns: %v\n", err)
		return
	}

	// Test a few paths
	paths := []string{
		"test.log",
		"subfolder/test.log",
		"random.txt",
		".bitignore",
	}

	fmt.Println("Testing ignore patterns:")
	for _, path := range paths {
		ignored := util.IsIgnored(path, patterns)
		fmt.Printf("  %s: %v\n", path, ignored)
	}
}
