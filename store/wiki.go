package store

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// WikiEntry describes one wiki page with its YAML frontmatter metadata. Tags is
// always a non-nil slice in JSON output, mirroring GlacierEntry. Category is the
// page's entity_type frontmatter field (the validated taxonomy category).
type WikiEntry struct {
	Path     string   `json:"path"`
	Category string   `json:"category,omitempty"`
	Title    string   `json:"title,omitempty"`
	Status   string   `json:"status,omitempty"`
	Tags     []string `json:"tags"`
	Summary  string   `json:"summary,omitempty"`
	Updated  string   `json:"updated,omitempty"`
	Related  []string `json:"related,omitempty"`
}

// WikiIndex walks wiki/**/*.md, parses YAML frontmatter, and returns one entry
// per content page sorted by path. Files without parseable frontmatter still
// appear in the index (with only Path set) so the caller can spot orphans. The
// generated catalog (wiki/index.md) and the registry (wiki/_meta/*) are
// excluded — they are derived, not content. Returns an empty slice (not nil)
// when the wiki directory is absent or is not a directory.
func (s *MemoryStore) WikiIndex() ([]WikiEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	wikiRoot := filepath.Join(s.root, "wiki")
	info, err := os.Stat(wikiRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return []WikiEntry{}, nil
		}
		return nil, fmt.Errorf("store: wiki index: %w", err)
	}
	if !info.IsDir() {
		return []WikiEntry{}, nil
	}

	entries := []WikiEntry{}
	walkErr := filepath.WalkDir(wikiRoot, func(path string, d fs.DirEntry, err error) error {
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

		// Exclude the generated catalog and the registry/meta files: these are
		// derived, not content pages.
		if relPath == "wiki/index.md" {
			return nil
		}
		if strings.HasPrefix(relPath, "wiki/_meta/") {
			return nil
		}

		entry := WikiEntry{Path: relPath, Tags: []string{}}

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
				Title      string   `yaml:"title"`
				Summary    string   `yaml:"summary"`
				Updated    string   `yaml:"updated"`
				EntityType string   `yaml:"entity_type"`
				Status     string   `yaml:"status"`
				Tags       []string `yaml:"tags"`
				Related    []string `yaml:"related"`
			}
			if yaml.Unmarshal(fm, &parsed) == nil {
				entry.Title = parsed.Title
				entry.Summary = parsed.Summary
				entry.Updated = parsed.Updated
				entry.Category = parsed.EntityType
				entry.Status = parsed.Status
				if parsed.Tags != nil {
					entry.Tags = parsed.Tags
				}
				entry.Related = parsed.Related
			}
		}
		entries = append(entries, entry)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("store: wiki index: %w", walkErr)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}
