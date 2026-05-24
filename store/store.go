// Package store provides file-based memory operations with file locking and atomic writes.
package store

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

// obsLineRE validates observation format: "- YYYY-MM-DD [tags]: text"
var obsLineRE = regexp.MustCompile(`^-\s+\d{4}-\d{2}-\d{2}\s+\[.+\]:\s*.+$`)

// l0RE extracts L0 summary comments from memory file headers.
var l0RE = regexp.MustCompile(`<!--\s*L0:\s*(.+?)\s*-->`)

// scannedFile holds metadata from a single directory walk entry.
type scannedFile struct {
	relPath string
	absPath string
	size    int64
}

// SearchResult holds a single search match.
type SearchResult struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// StatsResult holds filesystem statistics over the memory root.
type StatsResult struct {
	Files int   `json:"files"`
	Lines int64 `json:"lines"`
	Size  int64 `json:"size"`
}

// MemoryStore provides file-based memory operations rooted at a directory.
type MemoryStore struct {
	root string
	mu   sync.RWMutex // coarse lock for walk-based operations
}

// New creates a MemoryStore rooted at the given absolute directory path.
func New(root string) (*MemoryStore, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("store: root must be absolute, got %q", root)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("store: cannot create root %q: %w", root, err)
	}
	return &MemoryStore{root: root}, nil
}

// absPath resolves a relative path under root, rejecting traversal attempts.
// Any path that contains ".." components or would escape the root is rejected.
func (s *MemoryStore) absPath(relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("store: empty path")
	}
	// Reject paths that contain ".." (traversal attempt)
	cleaned := filepath.Clean(relPath)
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("store: path traversal rejected: %q", relPath)
	}
	// Build absolute path and verify it stays within root
	abs := filepath.Join(s.root, cleaned)
	if !strings.HasPrefix(abs+string(filepath.Separator), s.root+string(filepath.Separator)) {
		return "", fmt.Errorf("store: path traversal rejected: %q", relPath)
	}
	return abs, nil
}

// Read returns the content of a file. Returns ("", nil) for non-existent files.
func (s *MemoryStore) Read(relPath string) (string, error) {
	// Handle special paths
	switch relPath {
	case "L0_INDEX":
		return s.L0Index()
	case "LIST":
		paths, err := s.List()
		if err != nil {
			return "", err
		}
		return strings.Join(paths, "\n"), nil
	}

	abs, err := s.absPath(relPath)
	if err != nil {
		return "", err
	}

	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("store: read %q: %w", relPath, err)
	}
	defer f.Close()

	if err := lockShared(f); err != nil {
		return "", fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	data, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("store: read %q: %w", relPath, err)
	}
	return string(data), nil
}

// Write atomically writes content to a file (temp → fsync → rename).
func (s *MemoryStore) Write(relPath, content string) error {
	abs, err := s.absPath(relPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", relPath, err)
	}
	return atomicWrite(abs, content)
}

// Append adds text to a file. Creates the file if it does not exist.
// For paths ending in "observations.md", each non-blank line must match
// the observation format "- YYYY-MM-DD [tags]: text".
func (s *MemoryStore) Append(relPath, text string) error {
	abs, err := s.absPath(relPath)
	if err != nil {
		return err
	}

	if isObsPath(relPath) {
		if err := validateObsLines(text); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", relPath, err)
	}

	f, err := os.OpenFile(abs, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return fmt.Errorf("store: open %q: %w", relPath, err)
	}
	defer f.Close()

	if err := lockExclusive(f); err != nil {
		return fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	// Check only the last byte to decide if a separator newline is needed,
	// avoiding a full file read just for separator detection.
	separator := ""
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("store: stat %q: %w", relPath, err)
	}
	if info.Size() > 0 {
		buf := make([]byte, 1)
		if _, err := f.ReadAt(buf, info.Size()-1); err != nil {
			return fmt.Errorf("store: read last byte %q: %w", relPath, err)
		}
		if buf[0] != '\n' && !strings.HasPrefix(text, "\n") {
			separator = "\n"
		}
	}

	trailing := ""
	if !strings.HasSuffix(text, "\n") {
		trailing = "\n"
	}

	// For files that need a separator injected, we must rewrite atomically.
	// For the common case (file ends with \n), we can append directly.
	if separator == "" && info.Size() > 0 {
		// Seek to end and append in place
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return fmt.Errorf("store: seek %q: %w", relPath, err)
		}
		if _, err := f.WriteString(text + trailing); err != nil {
			return fmt.Errorf("store: append %q: %w", relPath, err)
		}
		return f.Sync()
	}

	// File is empty or needs separator — read full content and rewrite atomically
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("store: seek %q: %w", relPath, err)
	}
	existing, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("store: read %q: %w", relPath, err)
	}
	newContent := string(existing) + separator + text + trailing
	return atomicWrite(abs, newContent)
}

// Patch performs a surgical string replacement in a file.
// oldText must appear exactly once; returns an error if it appears 0 or 2+ times.
func (s *MemoryStore) Patch(relPath, oldText, newText string) error {
	abs, err := s.absPath(relPath)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(abs, os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("store: open %q: %w", relPath, err)
	}
	defer f.Close()

	if err := lockExclusive(f); err != nil {
		return fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	data, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("store: read %q: %w", relPath, err)
	}

	content := string(data)
	count := strings.Count(content, oldText)
	switch {
	case count == 0:
		return fmt.Errorf("store: patch: oldText not found in %q", relPath)
	case count >= 2:
		return fmt.Errorf("store: patch: oldText appears %d times in %q (must appear exactly once)", count, relPath)
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	return atomicWrite(abs, newContent)
}

// scanFiles performs a single directory walk and returns metadata for all
// non-tmp files under the memory root. Callers use the result instead of
// doing independent WalkDir traversals.
func (s *MemoryStore) scanFiles() ([]scannedFile, error) {
	var files []scannedFile
	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(s.root, path)
		files = append(files, scannedFile{relPath: relPath, absPath: path, size: info.Size()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store: scan: %w", err)
	}
	return files, nil
}

// Search performs a case-insensitive substring search across all files.
func (s *MemoryStore) Search(query string) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}

	lowerQuery := strings.ToLower(query)
	var results []SearchResult

	for _, sf := range files {
		f, err := os.Open(sf.absPath)
		if err != nil {
			continue
		}

		_ = lockShared(f)
		data, err := io.ReadAll(f)
		unlock(f)
		f.Close()
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(strings.ToLower(line), lowerQuery) {
				results = append(results, SearchResult{
					Path: sf.relPath,
					Line: i + 1,
					Text: line,
				})
			}
		}
	}
	return results, nil
}

// Stats returns file count, total line count, and total size.
func (s *MemoryStore) Stats() (StatsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return StatsResult{}, fmt.Errorf("store: stats: %w", err)
	}

	var result StatsResult
	for _, sf := range files {
		result.Files++
		result.Size += sf.size

		data, err := os.ReadFile(sf.absPath)
		if err != nil {
			continue
		}
		result.Lines += int64(strings.Count(string(data), "\n"))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			result.Lines++
		}
	}
	return result, nil
}

// L0Index returns a concatenated string of L0 summary lines extracted from all files.
// Each line in the output has the format: "path: summary"
func (s *MemoryStore) L0Index() (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return "", fmt.Errorf("store: l0index: %w", err)
	}

	var lines []string
	for _, sf := range files {
		data, err := os.ReadFile(sf.absPath)
		if err != nil {
			continue
		}
		firstLine := strings.SplitN(string(data), "\n", 2)[0]
		if m := l0RE.FindStringSubmatch(firstLine); m != nil {
			lines = append(lines, sf.relPath+": "+m[1])
		}
	}
	return strings.Join(lines, "\n"), nil
}

// List returns all relative file paths under the memory root.
func (s *MemoryStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return nil, fmt.Errorf("store: list: %w", err)
	}

	paths := make([]string, len(files))
	for i, sf := range files {
		paths[i] = sf.relPath
	}
	return paths, nil
}

// isObsPath returns true if the path looks like an observations file.
func isObsPath(relPath string) bool {
	return strings.HasSuffix(relPath, "observations.md")
}

// validateObsLines checks that each non-blank line matches the observation format.
func validateObsLines(text string) error {
	var bad []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !obsLineRE.MatchString(trimmed) {
			bad = append(bad, trimmed)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("store: observation format error — each line must match `- YYYY-MM-DD [tags]: text`. Invalid lines:\n%s", strings.Join(bad, "\n"))
	}
	return nil
}

// atomicWrite writes content to path using a temp file + rename for atomicity.
func atomicWrite(abs, content string) error {
	dir := filepath.Dir(abs)
	tmp, err := os.CreateTemp(dir, ".tmp-memory-*")
	if err != nil {
		return fmt.Errorf("store: atomic write temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("store: atomic write data: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("store: atomic write sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("store: atomic write close: %w", err)
	}
	if err := os.Rename(tmpName, abs); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("store: atomic write rename: %w", err)
	}
	return nil
}

// lockShared acquires a shared (read) advisory lock on f.
func lockShared(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_SH)
}

// lockExclusive acquires an exclusive (write) advisory lock on f.
func lockExclusive(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}

// unlock releases the advisory lock on f.
func unlock(f *os.File) {
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
