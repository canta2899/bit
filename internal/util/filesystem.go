package util

import (
	"io"
	"os"
	"path/filepath"
)

// FileSystem interface abstracts filesystem operations for testing
type FileSystem interface {
	// Basic file operations
	ReadFile(filename string) ([]byte, error)
	WriteFile(filename string, data []byte, perm os.FileMode) error
	Open(name string) (File, error)
	Create(name string) (File, error)
	Remove(name string) error
	RemoveAll(path string) error
	MkdirAll(path string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)

	// Walk directory with callback function
	Walk(root string, walkFn filepath.WalkFunc) error

	// Check if file exists
	Exists(path string) bool
}

// File interface abstracts file operations
type File interface {
	io.Closer
	io.Reader
	io.ReaderAt
	io.Seeker
	io.Writer
}

// OsFileSystem is the production implementation of FileSystem using OS calls
type OsFileSystem struct{}

func NewOsFileSystem() FileSystem {
	return &OsFileSystem{}
}

// ReadFile reads the named file and returns its contents
func (fs *OsFileSystem) ReadFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}

// WriteFile writes data to the named file
func (fs *OsFileSystem) WriteFile(filename string, data []byte, perm os.FileMode) error {
	return os.WriteFile(filename, data, perm)
}

// Open opens the named file for reading
func (fs *OsFileSystem) Open(name string) (File, error) {
	return os.Open(name)
}

// Create creates or truncates the named file
func (fs *OsFileSystem) Create(name string) (File, error) {
	return os.Create(name)
}

// Remove removes the named file or directory
func (fs *OsFileSystem) Remove(name string) error {
	return os.Remove(name)
}

// RemoveAll removes path and any children it contains
func (fs *OsFileSystem) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// MkdirAll creates a directory and all parent directories if they don't exist
func (fs *OsFileSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// Stat returns file info
func (fs *OsFileSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// Walk walks the file tree rooted at root
func (fs *OsFileSystem) Walk(root string, walkFn filepath.WalkFunc) error {
	return filepath.Walk(root, walkFn)
}

// Exists checks if a file or directory exists
func (fs *OsFileSystem) Exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
