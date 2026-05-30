// Package store provides file-based memory operations with file locking and atomic writes.
package store

import (
	"bufio"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
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
	modTime time.Time
}

// SearchResult holds a single search match.
type SearchResult struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// OpenActionItem holds one unchecked action item from an action-items.md file.
type OpenActionItem struct {
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Text     string `json:"text"`
	Raw      string `json:"raw"`
	Due      string `json:"due,omitempty"`
	Priority string `json:"priority,omitempty"`
	Added    string `json:"added,omitempty"`
}

// OutlineEntry holds a markdown heading found in a memory file.
type OutlineEntry struct {
	Line  int    `json:"line"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// FileStats holds per-file statistics for a single memory file.
type FileStats struct {
	Path     string `json:"path"`
	Lines    int64  `json:"lines"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"` // RFC3339 timestamp
}

// StatsResult holds filesystem statistics over the memory root.
type StatsResult struct {
	Files   int         `json:"files"`
	Lines   int64       `json:"lines"`
	Size    int64       `json:"size"`
	PerFile []FileStats `json:"per_file"`
}

// GlacierEntry describes one archived glacier file with its YAML frontmatter
// metadata. Tags is always a non-nil slice in JSON output.
type GlacierEntry struct {
	Path      string   `json:"path"`
	Domain    string   `json:"domain,omitempty"`
	Type      string   `json:"type,omitempty"`
	Tags      []string `json:"tags"`
	DateRange string   `json:"date_range,omitempty"`
	Entries   int      `json:"entries,omitempty"`
	Summary   string   `json:"summary,omitempty"`
}

// EntityFormatViolation flags an entity whose block exceeds the 3-line compact
// format. Lines counts non-blank, non-comment lines in the block including the
// `### Name` heading. HasDetailFile is true when the block references a
// detail file via a [[wiki:...]] link — signalling the long form is
// intentional and the body line could be compressed to a one-liner pointer.
type EntityFormatViolation struct {
	Path          string `json:"path"`
	Domain        string `json:"domain,omitempty"`
	Name          string `json:"name"`
	Lines         int    `json:"lines"`
	Issue         string `json:"issue"`
	HasDetailFile bool   `json:"has_detail_file"`
}

// EntityGlacierCandidate flags an entity whose `status: inactive` or whose
// `last:` date is older than the glacier threshold (180 days).
type EntityGlacierCandidate struct {
	Path    string `json:"path"`
	Domain  string `json:"domain,omitempty"`
	Name    string `json:"name"`
	Status  string `json:"status,omitempty"`
	Last    string `json:"last,omitempty"`
	AgeDays int    `json:"age_days,omitempty"`
}

// EntityMissingMetadata flags an entity missing one or more required
// metadata fields (currently: status, last).
type EntityMissingMetadata struct {
	Path    string   `json:"path"`
	Domain  string   `json:"domain,omitempty"`
	Name    string   `json:"name"`
	Missing []string `json:"missing"`
}

// EntityTemporalViolation flags a `(until YYYY-MM)` marker whose date has
// passed and which has not already been struck through (`~~...~~`).
type EntityTemporalViolation struct {
	Path   string `json:"path"`
	Domain string `json:"domain,omitempty"`
	Name   string `json:"name"`
	Line   int    `json:"line"`
	Text   string `json:"text"`
	Needs  string `json:"needs"`
}

// EntityAuditResult is the four-bucket envelope returned by EntityAudit.
// All slices are non-nil so JSON output never contains `null`.
type EntityAuditResult struct {
	FormatViolations   []EntityFormatViolation   `json:"format_violations"`
	GlacierCandidates  []EntityGlacierCandidate  `json:"glacier_candidates"`
	MissingMetadata    []EntityMissingMetadata   `json:"missing_metadata"`
	TemporalViolations []EntityTemporalViolation `json:"temporal_violations"`
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
func (s *MemoryStore) Read(relPath, section string, start, end int) (string, error) {
	// Handle special paths
	switch relPath {
	case "L0_INDEX":
		return s.L0Index("")
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
	content := string(data)
	if section != "" {
		return extractSection(relPath, content, section)
	}
	if start > 0 || end > 0 {
		return extractLineRange(content, start, end), nil
	}
	return content, nil
}

func extractSection(relPath, content, section string) (string, error) {
	lines := splitLines(content)
	target := normalizeHeading(section)
	for i, line := range lines {
		if !strings.EqualFold(strings.TrimSpace(line), target) {
			continue
		}

		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(strings.TrimSpace(lines[j]), "##") {
				end = j
				break
			}
		}
		return strings.Join(lines[i:end], "\n"), nil
	}
	return "", fmt.Errorf("store: section not found in %q: %s", relPath, section)
}

func normalizeHeading(section string) string {
	trimmed := strings.TrimSpace(section)
	if strings.HasPrefix(trimmed, "##") {
		return trimmed
	}
	return "## " + trimmed
}

func extractLineRange(content string, start, end int) string {
	lines := splitLines(content)
	if len(lines) == 0 {
		return ""
	}
	if start == 0 {
		start = 1
	}
	if end == 0 {
		end = len(lines)
	}
	start = clamp(start, 1, len(lines))
	end = clamp(end, 1, len(lines))
	if start > end {
		return ""
	}
	return strings.Join(lines[start-1:end], "\n")
}

func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
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
	return s.AppendSection(relPath, "", text)
}

// AppendSection adds text to a file, optionally targeting a markdown
// section heading. When section is non-empty, text is inserted at the end
// of the named section (before the next heading at the same-or-shallower
// level, or EOF). The section argument matches a markdown heading line
// such as "## Open" or just "Open" (any "#"-prefix level is accepted).
// Returns an error if the section is not found in an existing file —
// callers must create the heading first rather than silently land
// content at EOF (which historically dropped items under unintended
// trailing sections like "## Completed").
//
// When section is empty, behavior is identical to Append at EOF.
// When the file does not exist and section is non-empty, returns an error.
func (s *MemoryStore) AppendSection(relPath, section, text string) error {
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

	// Section-targeted append: full read, locate heading, rewrite atomically.
	if section != "" {
		return s.appendUnderSection(abs, relPath, section, text)
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

// headingRE matches a markdown ATX heading line and captures the level
// (count of leading '#') and the heading text.
var headingRE = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*$`)

// appendUnderSection reads the file, locates a heading whose text (or
// full "## Text" form) matches section, and inserts text at the end of
// that section — immediately before the next heading at the same or
// shallower level, or EOF. Atomic rewrite under exclusive lock.
func (s *MemoryStore) appendUnderSection(abs, relPath, section, text string) error {
	f, err := os.OpenFile(abs, os.O_RDWR, 0o644)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("store: append section %q: file %q does not exist (create it first)", section, relPath)
		}
		return fmt.Errorf("store: open %q: %w", relPath, err)
	}
	defer f.Close()

	if err := lockExclusive(f); err != nil {
		return fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	existing, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("store: read %q: %w", relPath, err)
	}

	// Normalize the requested section: strip leading '#' and whitespace.
	wantTitle := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(section), "#"))
	if wantTitle == "" {
		return fmt.Errorf("store: append section: empty section name")
	}

	lines := strings.Split(string(existing), "\n")
	startIdx := -1
	startLevel := 0
	for i, ln := range lines {
		m := headingRE.FindStringSubmatch(ln)
		if m == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(m[2]), wantTitle) {
			startIdx = i
			startLevel = len(m[1])
			break
		}
	}
	if startIdx < 0 {
		return fmt.Errorf("store: append section %q: heading not found in %q", section, relPath)
	}

	// Find end of section: first heading at level <= startLevel after startIdx.
	endIdx := len(lines)
	for j := startIdx + 1; j < len(lines); j++ {
		m := headingRE.FindStringSubmatch(lines[j])
		if m == nil {
			continue
		}
		if len(m[1]) <= startLevel {
			endIdx = j
			break
		}
	}

	// Trim trailing blank lines inside the section so insertion doesn't
	// drift further and further from the heading on repeated appends.
	insertAt := endIdx
	for insertAt > startIdx+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}

	// Build the insertion block. Ensure it's separated from the prior
	// content by exactly one blank line, and from the next section by
	// exactly one blank line (when there is a next section).
	block := strings.TrimRight(text, "\n")
	newLines := make([]string, 0, len(lines)+4)
	newLines = append(newLines, lines[:insertAt]...)
	// Separator from prior content (skip if immediately after the heading
	// with no body yet — keep one blank line for readability).
	if insertAt == startIdx+1 {
		newLines = append(newLines, "")
	}
	newLines = append(newLines, strings.Split(block, "\n")...)
	if endIdx < len(lines) {
		newLines = append(newLines, "")
		newLines = append(newLines, lines[endIdx:]...)
	} else {
		// At EOF: preserve a trailing newline element if the original had one.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			newLines = append(newLines, "")
		}
	}

	newContent := strings.Join(newLines, "\n")
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
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

// Outline returns level-2 and level-3 markdown headings from a file.
func (s *MemoryStore) Outline(relPath string) ([]OutlineEntry, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(abs)
	if err != nil {
		return nil, fmt.Errorf("store: outline %q: %w", relPath, err)
	}
	defer f.Close()

	if err := lockShared(f); err != nil {
		return nil, fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	var entries []OutlineEntry
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "### "):
			entries = append(entries, OutlineEntry{
				Line:  lineNo,
				Text:  strings.TrimPrefix(line, "### "),
				Level: 3,
			})
		case strings.HasPrefix(line, "## "):
			entries = append(entries, OutlineEntry{
				Line:  lineNo,
				Text:  strings.TrimPrefix(line, "## "),
				Level: 2,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("store: outline %q: %w", relPath, err)
	}
	return entries, nil
}

// Move renames a file within the memory root.
func (s *MemoryStore) Move(fromPath, toPath string) error {
	absFrom, err := s.absPath(fromPath)
	if err != nil {
		return err
	}
	absTo, err := s.absPath(toPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(absTo); err == nil {
		return fmt.Errorf("store: move destination exists: %q", toPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("store: stat %q: %w", toPath, err)
	}
	if err := os.MkdirAll(filepath.Dir(absTo), 0o755); err != nil {
		return fmt.Errorf("store: mkdir %q: %w", toPath, err)
	}
	if err := os.Rename(absFrom, absTo); err != nil {
		return fmt.Errorf("store: move %q to %q: %w", fromPath, toPath, err)
	}
	return nil
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
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		relPath, _ := filepath.Rel(s.root, path)
		files = append(files, scannedFile{relPath: relPath, absPath: path, size: info.Size(), modTime: info.ModTime()})
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

// ActionTarget is one resolved action-items file the store should scan.
// The Domain field is the canonical domain id supplied by the caller
// (typically the domain controller); the store no longer infers it from
// the path's leaf basename.
type ActionTarget struct {
	Domain string
	Path   string // relative path under root, POSIX-style
}

// OpenActions scans the supplied action-items targets and returns unchecked
// markdown tasks from each. Missing files are skipped silently; malformed
// scans surface as errors. Item.Domain is taken from the target, not from
// the leaf directory name.
func (s *MemoryStore) OpenActions(targets []ActionTarget) ([]OpenActionItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Sort by path for deterministic output regardless of target order.
	sorted := make([]ActionTarget, len(targets))
	copy(sorted, targets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	items := []OpenActionItem{}
	for _, t := range sorted {
		abs, err := s.absPath(t.Path)
		if err != nil {
			return nil, fmt.Errorf("store: open actions: %w", err)
		}
		f, err := os.Open(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("store: open actions: open %q: %w", t.Path, err)
		}

		_ = lockShared(f)
		scanner := bufio.NewScanner(f)
		// Allow up to 1MiB per line so a giant embedded URL or pasted blob
		// doesn't silently truncate the file scan with bufio.ErrTooLong.
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		lineNo := 0
		inComment := false
		inFence := false
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			if skipActionLine(trimmed, &inComment, &inFence) {
				continue
			}
			if !strings.HasPrefix(trimmed, "- [ ] ") {
				continue
			}
			if item, ok := parseOpenActionItem(t.Domain, t.Path, lineNo, trimmed); ok {
				items = append(items, item)
			}
		}
		scanErr := scanner.Err()
		unlock(f)
		f.Close()
		if scanErr != nil {
			return nil, fmt.Errorf("store: open actions: scan %q: %w", t.Path, scanErr)
		}
	}
	return items, nil
}

// Stats returns file count, total line count, total size, and per-file
// breakdown. If prefix is non-empty, only files whose relative path begins with
// prefix (after normalizing trailing slashes) are included; totals reflect only
// the matched subset. Per-file entries are sorted by path for deterministic
// output.
func (s *MemoryStore) Stats(prefix string) (StatsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return StatsResult{}, fmt.Errorf("store: stats: %w", err)
	}

	// Normalize prefix: strip leading/trailing slashes for forgiving matching.
	prefix = strings.Trim(prefix, "/")

	var result StatsResult
	for _, sf := range files {
		if prefix != "" {
			// Match against the file's path or any of its parent dirs.
			if sf.relPath != prefix &&
				!strings.HasPrefix(sf.relPath, prefix+"/") {
				continue
			}
		}
		result.Files++
		result.Size += sf.size

		data, readErr := os.ReadFile(sf.absPath)
		if readErr != nil {
			continue
		}
		lines := int64(strings.Count(string(data), "\n"))
		if len(data) > 0 && data[len(data)-1] != '\n' {
			lines++
		}
		result.Lines += lines

		result.PerFile = append(result.PerFile, FileStats{
			Path:     sf.relPath,
			Lines:    lines,
			Size:     sf.size,
			Modified: sf.modTime.UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(result.PerFile, func(i, j int) bool {
		return result.PerFile[i].Path < result.PerFile[j].Path
	})
	// Ensure non-nil JSON output even when empty.
	if result.PerFile == nil {
		result.PerFile = []FileStats{}
	}
	return result, nil
}

// L0Index returns a concatenated string of L0 summary lines extracted from all files.
// Each line in the output has the format: "path: summary"
func (s *MemoryStore) L0Index(domain string) (string, error) {
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
	if domain != "" {
		prefix := strings.TrimSuffix(domain, "/") + "/"
		var filtered []string
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) {
				filtered = append(filtered, line)
			}
		}
		lines = filtered
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

// isActionItemsPath is no longer used internally — action-items files come
// from the domain controller now. Kept commented out as a marker for the
// refactor; the function and its tests were removed in feat/domain-controller.

func skipActionLine(trimmed string, inComment, inFence *bool) bool {
	if *inComment {
		if strings.Contains(trimmed, "-->") {
			*inComment = false
		}
		return true
	}
	if strings.HasPrefix(trimmed, "<!--") {
		if !strings.Contains(trimmed, "-->") {
			*inComment = true
		}
		return true
	}
	if *inFence {
		if isFenceLine(trimmed) {
			*inFence = false
		}
		return true
	}
	if isFenceLine(trimmed) {
		*inFence = true
		return true
	}
	return false
}

func isFenceLine(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func parseOpenActionItem(domain, path string, line int, raw string) (OpenActionItem, bool) {
	body := strings.TrimSpace(strings.TrimPrefix(raw, "- [ ] "))
	if body == "" {
		return OpenActionItem{}, false
	}
	parts := strings.Split(body, "|")
	item := OpenActionItem{
		Domain: domain,
		Path:   path,
		Line:   line,
		Text:   strings.TrimSpace(parts[0]),
		Raw:    raw,
	}
	if item.Text == "" {
		return OpenActionItem{}, false
	}
	for _, part := range parts[1:] {
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "due":
			item.Due = strings.TrimSpace(value)
		case "pri", "priority":
			item.Priority = strings.TrimSpace(value)
		case "added":
			item.Added = strings.TrimSpace(value)
		}
	}
	return item, true
}

// Observation is one parsed entry from an observations.md file:
// Observation, obsParseRE, RecentObservationsForFile —
// removed. observations.go owns the canonical Observation parser
// (ObservationEntry + parseObservationLine), and domain_summary now uses it
// directly. RecentObservationsForFile shipped in PR #8 as a single-file
// helper before observations.go landed; superseded.

// addedDateRE — defined in housekeeping.go (same package). Reused here.

// CountActions scans an action-items.md file at relPath and returns
// (openCount, completedSinceCount). openCount counts every "- [ ]" line.
// completedSinceCount counts "- [x]" lines whose metadata includes
// "added:YYYY-MM-DD" with date >= sinceDate. Completed items without an
// added: tag are not counted (no date → cannot place in a since window).
// When sinceDate is "", every completed item counts.
// Missing file → (0, 0, nil). Skips fenced code and HTML comments.
func (s *MemoryStore) CountActions(relPath, sinceDate string) (int, int, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return 0, 0, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("store: count actions: %w", err)
	}
	defer f.Close()
	if err := lockShared(f); err != nil {
		return 0, 0, fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var open, completed int
	inComment, inFence := false, false
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if skipActionLine(trimmed, &inComment, &inFence) {
			continue
		}
		switch {
		case strings.HasPrefix(trimmed, "- [ ] "):
			open++
		case strings.HasPrefix(trimmed, "- [x] "), strings.HasPrefix(trimmed, "- [X] "):
			if sinceDate == "" {
				completed++
				continue
			}
			m := addedDateRE.FindStringSubmatch(trimmed)
			if m != nil && m[1] >= sinceDate {
				completed++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("store: count actions scan %q: %w", relPath, err)
	}
	return open, completed, nil
}

// FileModTime returns the mtime of relPath. Missing file → (zero, nil).
func (s *MemoryStore) FileModTime(relPath string) (time.Time, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return time.Time{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("store: mtime %q: %w", relPath, err)
	}
	return info.ModTime(), nil
}

// FileExists reports whether relPath exists as a regular file under root.
func (s *MemoryStore) FileExists(relPath string) (bool, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("store: exists %q: %w", relPath, err)
	}
	return !info.IsDir(), nil
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

// GlacierIndex walks glacier/**/*.md, parses YAML frontmatter, and returns
// one entry per file sorted by path. Files without parseable frontmatter still
// appear in the index (with only Path set) so the caller can spot orphans.
// Returns an empty slice (not nil) when the glacier directory is absent.
func (s *MemoryStore) GlacierIndex() ([]GlacierEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	glacierRoot := filepath.Join(s.root, "glacier")
	info, err := os.Stat(glacierRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []GlacierEntry{}, nil
		}
		return nil, fmt.Errorf("store: glacier index: %w", err)
	}
	if !info.IsDir() {
		return []GlacierEntry{}, nil
	}

	entries := []GlacierEntry{}
	walkErr := filepath.WalkDir(glacierRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}
		relPath, _ := filepath.Rel(s.root, path)
		relPath = filepath.ToSlash(relPath)
		entry := GlacierEntry{Path: relPath, Tags: []string{}}

		f, openErr := os.Open(path)
		if openErr != nil {
			entries = append(entries, entry)
			return nil
		}
		_ = lockShared(f)
		data, readErr := io.ReadAll(f)
		unlock(f)
		f.Close()
		if readErr != nil {
			entries = append(entries, entry)
			return nil
		}

		fm, ok := extractFrontmatter(data)
		if ok {
			var parsed struct {
				Domain    string   `yaml:"domain"`
				Type      string   `yaml:"type"`
				Tags      []string `yaml:"tags"`
				DateRange string   `yaml:"date_range"`
				Entries   int      `yaml:"entries"`
				Summary   string   `yaml:"summary"`
			}
			if yaml.Unmarshal(fm, &parsed) == nil {
				entry.Domain = parsed.Domain
				entry.Type = parsed.Type
				if parsed.Tags != nil {
					entry.Tags = parsed.Tags
				}
				entry.DateRange = parsed.DateRange
				entry.Entries = parsed.Entries
				entry.Summary = parsed.Summary
			}
		}
		entries = append(entries, entry)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("store: glacier index: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// extractFrontmatter pulls the YAML block delimited by leading "---" / "---"
// from a markdown file body. Returns (nil, false) if no frontmatter is present.
// A leading HTML comment line (e.g. "<!-- L0: ... -->") is skipped so files
// that prepend an L0 hint to their frontmatter still parse.
func extractFrontmatter(data []byte) ([]byte, bool) {
	text := string(data)
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i]), "<!--") &&
		strings.HasSuffix(strings.TrimSpace(lines[i]), "-->") {
		i++
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return nil, false
	}
	start := i + 1
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "---" {
			return []byte(strings.Join(lines[start:j], "\n")), true
		}
	}
	return nil, false
}

// entityHeadingRE matches an entity header line: `### Name` (with optional
// trailing parenthetical, e.g. `### Microsoft (employer)`). The captured
// group is the cleaned name without the parenthetical.
var entityHeadingRE = regexp.MustCompile(`^###\s+(.+?)\s*$`)

// entityUntilRE finds `(until YYYY-MM)` markers in a line. The capture is the
// YYYY-MM string. Already-struck-through markers (`~~(until YYYY-MM)~~`) are
// matched too; the caller checks for surrounding `~~` to decide whether to
// flag.
var entityUntilRE = regexp.MustCompile(`\(until\s+(\d{4}-\d{2})\)`)

// entityDetailLinkRE matches a [[wiki:...]] link anywhere in a block, used
// to flag long-form entries that already point at a detail file.
var entityDetailLinkRE = regexp.MustCompile(`\[\[wiki:[^\]]+\]\]`)

// EntityAudit scans the supplied entities.md targets and returns four buckets
// of violations: format (blocks exceeding the 3-line compact shape),
// glacier candidates (status:inactive or last:>180d), missing metadata, and
// temporal markers `(until YYYY-MM)` whose dates have passed and which have
// not been struck through. Missing files are skipped silently. The result's
// slices are all non-nil so callers can safely range without nil checks.
//
// "now" is parameterized so tests can pin a deterministic clock.
func (s *MemoryStore) EntityAudit(targets []ActionTarget, now time.Time) (EntityAuditResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := EntityAuditResult{
		FormatViolations:   []EntityFormatViolation{},
		GlacierCandidates:  []EntityGlacierCandidate{},
		MissingMetadata:    []EntityMissingMetadata{},
		TemporalViolations: []EntityTemporalViolation{},
	}

	sorted := make([]ActionTarget, len(targets))
	copy(sorted, targets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	for _, t := range sorted {
		abs, err := s.absPath(t.Path)
		if err != nil {
			return EntityAuditResult{}, fmt.Errorf("store: entity audit: %w", err)
		}
		f, err := os.Open(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return EntityAuditResult{}, fmt.Errorf("store: entity audit: open %q: %w", t.Path, err)
		}
		_ = lockShared(f)
		data, readErr := io.ReadAll(f)
		unlock(f)
		f.Close()
		if readErr != nil {
			return EntityAuditResult{}, fmt.Errorf("store: entity audit: read %q: %w", t.Path, readErr)
		}
		auditOneEntitiesFile(&res, t.Domain, t.Path, string(data), now)
	}
	return res, nil
}

// auditOneEntitiesFile parses one entities.md body and appends violations to
// res. The format is "### Name" headers separated by blank lines, each block
// containing one body line per fact plus optional `status:` / `last:`
// metadata. We treat everything from a `### ` line until the next `### `
// (or EOF) as one block.
func auditOneEntitiesFile(res *EntityAuditResult, domainID, path, body string, now time.Time) {
	lines := strings.Split(body, "\n")

	type block struct {
		name      string
		startLine int // 1-indexed line of the ### heading
		bodyLines []string
		bodyAbs   []int // absolute line numbers (1-indexed) for each bodyLine
	}
	var blocks []block
	var cur *block

	for i, raw := range lines {
		if m := entityHeadingRE.FindStringSubmatch(raw); m != nil {
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			cur = &block{name: stripParenSuffix(m[1]), startLine: i + 1}
			continue
		}
		if cur == nil {
			continue
		}
		cur.bodyLines = append(cur.bodyLines, raw)
		cur.bodyAbs = append(cur.bodyAbs, i+1)
	}
	if cur != nil {
		blocks = append(blocks, *cur)
	}

	for _, b := range blocks {
		// Count "meaningful" lines: heading + non-blank, non-comment body lines.
		count := 1
		hasDetail := false
		hasStatus := false
		hasLast := false
		var lastVal string
		var statusVal string
		for k, ln := range b.bodyLines {
			trim := strings.TrimSpace(ln)
			if trim == "" {
				continue
			}
			if strings.HasPrefix(trim, "<!--") {
				continue
			}
			count++
			if entityDetailLinkRE.MatchString(ln) {
				hasDetail = true
			}
			if s, ok := extractInlineField(ln, "status"); ok {
				hasStatus = true
				statusVal = s
			}
			if v, ok := extractInlineField(ln, "last"); ok {
				hasLast = true
				lastVal = v
			}
			// Temporal markers anywhere in the body.
			for _, mm := range entityUntilRE.FindAllStringSubmatchIndex(ln, -1) {
				dateStr := ln[mm[2]:mm[3]]
				if isPastYYYYMM(dateStr, now) && !isStruckThrough(ln, mm[0], mm[1]) {
					res.TemporalViolations = append(res.TemporalViolations, EntityTemporalViolation{
						Path:   path,
						Domain: domainID,
						Name:   b.name,
						Line:   b.bodyAbs[k],
						Text:   strings.TrimSpace(ln),
						Needs:  "strikethrough",
					})
				}
			}
		}

		if count > 3 {
			res.FormatViolations = append(res.FormatViolations, EntityFormatViolation{
				Path:          path,
				Domain:        domainID,
				Name:          b.name,
				Lines:         count,
				Issue:         "exceeds_3_line_compact",
				HasDetailFile: hasDetail,
			})
		}

		var missing []string
		if !hasStatus {
			missing = append(missing, "status")
		}
		if !hasLast {
			missing = append(missing, "last")
		}
		if len(missing) > 0 {
			res.MissingMetadata = append(res.MissingMetadata, EntityMissingMetadata{
				Path:    path,
				Domain:  domainID,
				Name:    b.name,
				Missing: missing,
			})
		}

		// Glacier: status inactive, or last >180d old.
		isInactive := strings.EqualFold(statusVal, "inactive")
		ageDays := -1
		if hasLast {
			if d, err := time.Parse("2006-01-02", strings.TrimSpace(lastVal)); err == nil {
				diff := now.Sub(d)
				if diff > 0 {
					ageDays = int(diff / (24 * time.Hour))
				} else {
					ageDays = 0
				}
			}
		}
		if isInactive || (ageDays > 180) {
			res.GlacierCandidates = append(res.GlacierCandidates, EntityGlacierCandidate{
				Path:    path,
				Domain:  domainID,
				Name:    b.name,
				Status:  statusVal,
				Last:    lastVal,
				AgeDays: ageDays,
			})
		}
	}
}

// stripParenSuffix removes a trailing parenthetical from an entity name:
// "Microsoft (employer)" → "Microsoft". Leaves embedded parens alone.
func stripParenSuffix(name string) string {
	name = strings.TrimSpace(name)
	if !strings.HasSuffix(name, ")") {
		return name
	}
	idx := strings.LastIndex(name, "(")
	if idx <= 0 {
		return name
	}
	return strings.TrimSpace(name[:idx])
}

// extractInlineField scans a metadata-style body line for `field: value`
// where pipe-separated fields are allowed. Returns the trimmed value.
// e.g. extractInlineField("status: active | last: 2026-05-27", "last") →
// "2026-05-27", true.
func extractInlineField(line, field string) (string, bool) {
	parts := strings.Split(line, "|")
	prefix := strings.ToLower(field) + ":"
	for _, p := range parts {
		trim := strings.TrimSpace(p)
		lower := strings.ToLower(trim)
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		val := strings.TrimSpace(trim[len(prefix):])
		// Cut at the first " | " that may have leaked or a trailing arrow link.
		if cut := strings.Index(val, " ("); cut > 0 {
			val = strings.TrimSpace(val[:cut])
		}
		return val, true
	}
	return "", false
}

// isPastYYYYMM returns true when YYYY-MM (interpreted as the first of that
// month) is strictly before the first of `now`'s month. So a marker `(until
// 2026-05)` is "past" only on or after 2026-06-01.
func isPastYYYYMM(s string, now time.Time) bool {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return false
	}
	nowMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return t.Before(nowMonth)
}

// isStruckThrough reports whether the substring at line[start:end] is wrapped
// by `~~ ... ~~` markdown strikethrough.
func isStruckThrough(line string, start, end int) bool {
	if start >= 2 && line[start-2:start] == "~~" &&
		end+2 <= len(line) && line[end:end+2] == "~~" {
		return true
	}
	return false
}

// Git runs a git operation in the memory root directory.
// op must be one of: "status", "diff", "log", "commit".
// commit requires a non-empty message; it auto-stages with git add -A.
func (s *MemoryStore) Git(op, ref, message string, paths []string, limit int) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("store: git not available")
	}
	switch op {
	case "status":
		return s.runGit("status", "--short")
	case "diff":
		args := []string{"diff"}
		if ref != "" {
			args = append(args, ref)
		}
		if len(paths) > 0 {
			args = append(args, append([]string{"--"}, paths...)...)
		}
		return s.runGit(args...)
	case "log":
		n := limit
		if n <= 0 {
			n = 20
		}
		args := []string{"log", "--oneline", fmt.Sprintf("-n%d", n)}
		if ref != "" {
			args = append(args, ref)
		}
		return s.runGit(args...)
	case "commit":
		if message == "" {
			return "", fmt.Errorf("store: git commit requires message")
		}
		if _, err := s.runGit("add", "-A"); err != nil {
			return "", err
		}
		return s.runGit("commit", "-m", message)
	default:
		return "", fmt.Errorf("store: git: unknown op %q", op)
	}
}

// runGit executes a git command in the store root and returns trimmed stdout+stderr.
func (s *MemoryStore) runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = s.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("store: git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// ScenarioEntry describes one active scenario file in cog-meta/scenarios/.
// Status is one of "active", "due_now", "overdue". DaysUntilCheck is negative
// for overdue scenarios, zero for due_now (today == check-by), positive for
// active scenarios still ahead of their check date.
type ScenarioEntry struct {
	Path           string `json:"path"`
	CheckBy        string `json:"check_by"`
	Status         string `json:"status"`
	DaysUntilCheck int    `json:"days_until_check"`
}

// scenarioFrontmatter mirrors the fields scenario_check cares about.
// All other frontmatter fields are ignored.
type scenarioFrontmatter struct {
	Status  string `yaml:"status"`
	CheckBy string `yaml:"check-by"`
}

// ScenarioCheck walks cog-meta/scenarios/*.md and returns one entry per
// scenario file whose frontmatter declares status: active (or omits status —
// treated as active for backward compatibility). Files without a check-by
// date, with an unparseable check-by, or with a non-active status are
// skipped silently. Returns an empty slice (not nil) when the scenarios
// directory is absent.
//
// "today" is passed in so callers can pin time for tests; production callers
// pass time.Now().UTC().
func (s *MemoryStore) ScenarioCheck(today time.Time) ([]ScenarioEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := filepath.Join(s.root, "cog-meta", "scenarios")
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ScenarioEntry{}, nil
		}
		return nil, fmt.Errorf("store: scenario check: %w", err)
	}
	if !info.IsDir() {
		return []ScenarioEntry{}, nil
	}

	// Normalize today to midnight UTC so day-deltas are integer days.
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	entries := []ScenarioEntry{}
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		if strings.HasSuffix(path, ".tmp") {
			return nil
		}
		relPath, _ := filepath.Rel(s.root, path)
		relPath = filepath.ToSlash(relPath)

		f, openErr := os.Open(path)
		if openErr != nil {
			return nil
		}
		_ = lockShared(f)
		data, readErr := io.ReadAll(f)
		unlock(f)
		f.Close()
		if readErr != nil {
			return nil
		}

		fm, ok := scenarioExtractFrontmatter(data)
		if !ok {
			return nil
		}
		var parsed scenarioFrontmatter
		if err := yaml.Unmarshal(fm, &parsed); err != nil {
			return nil
		}
		// Only active scenarios are scheduled. Empty status is treated
		// as active so older files without explicit status still surface.
		status := strings.TrimSpace(parsed.Status)
		if status != "" && status != "active" {
			return nil
		}
		checkBy := strings.TrimSpace(parsed.CheckBy)
		if checkBy == "" {
			return nil
		}
		check, err := time.Parse("2006-01-02", checkBy)
		if err != nil {
			return nil
		}
		check = time.Date(check.Year(), check.Month(), check.Day(), 0, 0, 0, 0, time.UTC)

		days := int(check.Sub(today).Hours() / 24)
		var entryStatus string
		switch {
		case days < 0:
			entryStatus = "overdue"
		case days == 0:
			entryStatus = "due_now"
		default:
			entryStatus = "active"
		}
		entries = append(entries, ScenarioEntry{
			Path:           relPath,
			CheckBy:        checkBy,
			Status:         entryStatus,
			DaysUntilCheck: days,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("store: scenario check: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

// scenarioExtractFrontmatter pulls the YAML block delimited by leading "---"
// / "---" from a markdown file body. Returns (nil, false) if no frontmatter
// is present. A single leading HTML comment line (e.g. an L0 hint) is
// skipped so files that prepend one still parse. Local to scenario_check to
// avoid coupling with other in-flight RPC branches that introduce a shared
// helper; consolidate once those land.
func scenarioExtractFrontmatter(data []byte) ([]byte, bool) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i < len(lines) {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "<!--") && strings.HasSuffix(t, "-->") {
			i++
		}
	}
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return nil, false
	}
	start := i + 1
	for j := start; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) == "---" {
			return []byte(strings.Join(lines[start:j], "\n")), true
		}
	}
	return nil, false
}
