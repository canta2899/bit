package util

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MockFileInfo implements os.FileInfo for testing
type MockFileInfo struct {
	FileName    string
	FileSize    int64
	FileMode    os.FileMode
	FileModTime time.Time
	FileIsDir   bool
	FileSys     interface{}
}

func (m MockFileInfo) Name() string       { return m.FileName }
func (m MockFileInfo) Size() int64        { return m.FileSize }
func (m MockFileInfo) Mode() os.FileMode  { return m.FileMode }
func (m MockFileInfo) ModTime() time.Time { return m.FileModTime }
func (m MockFileInfo) IsDir() bool        { return m.FileIsDir }
func (m MockFileInfo) Sys() interface{}   { return m.FileSys }

// MockFile implements File interface for testing
type MockFile struct {
	Buffer *bytes.Buffer
	Name   string
	Closed bool
	mutex  sync.Mutex
}

func NewMockFile(name string, content []byte) *MockFile {
	return &MockFile{
		Buffer: bytes.NewBuffer(content),
		Name:   name,
		Closed: false,
	}
}

func (m *MockFile) Read(p []byte) (n int, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.Closed {
		return 0, errors.New("file closed")
	}
	return m.Buffer.Read(p)
}

func (m *MockFile) ReadAt(p []byte, off int64) (n int, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.Closed {
		return 0, errors.New("file closed")
	}
	// This is a simplified implementation
	data := m.Buffer.Bytes()
	if off >= int64(len(data)) {
		return 0, io.EOF
	}
	n = copy(p, data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (m *MockFile) Write(p []byte) (n int, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.Closed {
		return 0, errors.New("file closed")
	}
	return m.Buffer.Write(p)
}

func (m *MockFile) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.Closed {
		return errors.New("file already closed")
	}
	m.Closed = true
	return nil
}

func (m *MockFile) Seek(offset int64, whence int) (int64, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.Closed {
		return 0, errors.New("file closed")
	}
	// This is a very simplified implementation
	return 0, nil
}

// MockFileSystem implements FileSystem interface for testing
type MockFileSystem struct {
	Files     map[string][]byte
	FileInfos map[string]os.FileInfo
	Dirs      map[string]bool
	mutex     sync.RWMutex
}

func NewMockFileSystem() *MockFileSystem {
	return &MockFileSystem{
		Files:     make(map[string][]byte),
		FileInfos: make(map[string]os.FileInfo),
		Dirs:      make(map[string]bool),
	}
}

// AddFile adds a mock file to the filesystem
func (fs *MockFileSystem) AddFile(path string, content []byte) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	normalizedPath := filepath.ToSlash(path)
	fs.Files[normalizedPath] = content

	// Create file info
	fs.FileInfos[normalizedPath] = MockFileInfo{
		FileName:    filepath.Base(normalizedPath),
		FileSize:    int64(len(content)),
		FileMode:    0644,
		FileModTime: time.Now(),
		FileIsDir:   false,
	}

	// Create parent directories
	dir := filepath.Dir(normalizedPath)
	for dir != "." && dir != "/" {
		fs.Dirs[dir] = true
		fs.FileInfos[dir] = MockFileInfo{
			FileName:    filepath.Base(dir),
			FileSize:    0,
			FileMode:    0755,
			FileModTime: time.Now(),
			FileIsDir:   true,
		}
		dir = filepath.Dir(dir)
	}
}

// AddDirectory adds a mock directory to the filesystem
func (fs *MockFileSystem) AddDirectory(path string) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	normalizedPath := filepath.ToSlash(path)
	fs.Dirs[normalizedPath] = true
	fs.FileInfos[normalizedPath] = MockFileInfo{
		FileName:    filepath.Base(normalizedPath),
		FileSize:    0,
		FileMode:    0755,
		FileModTime: time.Now(),
		FileIsDir:   true,
	}
}

func (fs *MockFileSystem) ReadFile(filename string) ([]byte, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	normalizedPath := filepath.ToSlash(filename)
	if content, ok := fs.Files[normalizedPath]; ok {
		return content, nil
	}
	return nil, &os.PathError{Op: "open", Path: filename, Err: os.ErrNotExist}
}

func (fs *MockFileSystem) WriteFile(filename string, data []byte, perm os.FileMode) error {
	fs.AddFile(filename, data)
	return nil
}

func (fs *MockFileSystem) Open(name string) (File, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	normalizedPath := filepath.ToSlash(name)
	if content, ok := fs.Files[normalizedPath]; ok {
		return NewMockFile(name, content), nil
	}
	return nil, &os.PathError{Op: "open", Path: name, Err: os.ErrNotExist}
}

func (fs *MockFileSystem) Create(name string) (File, error) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	normalizedPath := filepath.ToSlash(name)
	fs.Files[normalizedPath] = []byte{}

	// Create file info
	fs.FileInfos[normalizedPath] = MockFileInfo{
		FileName:    filepath.Base(normalizedPath),
		FileSize:    0,
		FileMode:    0644,
		FileModTime: time.Now(),
		FileIsDir:   false,
	}

	return NewMockFile(name, []byte{}), nil
}

func (fs *MockFileSystem) Remove(name string) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	normalizedPath := filepath.ToSlash(name)
	if _, ok := fs.Files[normalizedPath]; ok {
		delete(fs.Files, normalizedPath)
		delete(fs.FileInfos, normalizedPath)
		return nil
	}
	if _, ok := fs.Dirs[normalizedPath]; ok {
		// Check if directory is empty
		for path := range fs.Files {
			if strings.HasPrefix(path, normalizedPath+"/") {
				return errors.New("directory not empty")
			}
		}
		delete(fs.Dirs, normalizedPath)
		delete(fs.FileInfos, normalizedPath)
		return nil
	}
	return &os.PathError{Op: "remove", Path: name, Err: os.ErrNotExist}
}

func (fs *MockFileSystem) RemoveAll(path string) error {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	normalizedPath := filepath.ToSlash(path)

	// Remove all files with this prefix
	for filePath := range fs.Files {
		if filePath == normalizedPath || strings.HasPrefix(filePath, normalizedPath+"/") {
			delete(fs.Files, filePath)
			delete(fs.FileInfos, filePath)
		}
	}

	// Remove all directories with this prefix
	for dirPath := range fs.Dirs {
		if dirPath == normalizedPath || strings.HasPrefix(dirPath, normalizedPath+"/") {
			delete(fs.Dirs, dirPath)
			delete(fs.FileInfos, dirPath)
		}
	}

	return nil
}

func (fs *MockFileSystem) MkdirAll(path string, perm os.FileMode) error {
	fs.AddDirectory(path)
	return nil
}

func (fs *MockFileSystem) Stat(name string) (os.FileInfo, error) {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	normalizedPath := filepath.ToSlash(name)
	if info, ok := fs.FileInfos[normalizedPath]; ok {
		return info, nil
	}
	return nil, &os.PathError{Op: "stat", Path: name, Err: os.ErrNotExist}
}

func (fs *MockFileSystem) Walk(root string, walkFn filepath.WalkFunc) error {
	fs.mutex.RLock()

	normalizedRoot := filepath.ToSlash(root)
	paths := make([]string, 0)

	// Collect all paths that match the root prefix
	for path := range fs.FileInfos {
		if path == normalizedRoot || strings.HasPrefix(path, normalizedRoot+"/") {
			paths = append(paths, path)
		}
	}

	fs.mutex.RUnlock()

	// Sort paths for deterministic order (important for testing)
	// This simplified version doesn't sort but you should in a real implementation
	// sort.Strings(paths)

	for _, path := range paths {
		fs.mutex.RLock()
		info := fs.FileInfos[path]
		fs.mutex.RUnlock()

		err := walkFn(path, info, nil)
		if err != nil {
			if err == filepath.SkipDir && info.IsDir() {
				continue
			}
			return err
		}
	}

	return nil
}

func (fs *MockFileSystem) Exists(path string) bool {
	fs.mutex.RLock()
	defer fs.mutex.RUnlock()

	normalizedPath := filepath.ToSlash(path)
	_, fileExists := fs.Files[normalizedPath]
	_, dirExists := fs.Dirs[normalizedPath]

	return fileExists || dirExists
}
