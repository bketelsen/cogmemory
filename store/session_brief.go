package store

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

// SessionBriefResult is the store-side payload backing the session_brief RPC.
// RBAC filtering of DomainActionCounts happens in the RPC layer — the store
// reports raw counts for every target it was asked about. HotMemory and
// Patterns are owner-canonical files; the store reads them unconditionally
// (empty string when absent) and the RPC layer always returns them.
type SessionBriefResult struct {
	HotMemory          string                  `json:"hot_memory"`
	Patterns           string                  `json:"patterns"`
	DomainActionCounts []DomainActionCountItem `json:"domain_action_counts"`
	PriHighAnywhere    bool                    `json:"pri_high_anywhere"`
}

// DomainActionCountItem is the per-domain open-action count plus the path
// the store scanned. Path is the resolved relative path (POSIX) so the RPC
// layer can apply per-path RBAC filtering.
type DomainActionCountItem struct {
	Domain       string `json:"domain"`
	Path         string `json:"path"`
	OpenCount    int    `json:"open_count"`
	PriHighCount int    `json:"pri_high_count"`
}

// SessionBrief reads memory/hot-memory.md and memory/cog-meta/patterns.md
// from disk and counts open action items in each supplied target. Missing
// files are treated as empty / zero. Returns deterministic ordering.
//
// Path conventions are baked in (hot-memory.md at root, cog-meta/patterns.md)
// because they're owner-canonical contracts; the RPC consolidation doc §1
// names them explicitly.
func (s *MemoryStore) SessionBrief(targets []ActionTarget) (SessionBriefResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out SessionBriefResult

	hot, err := s.readOptional("hot-memory.md")
	if err != nil {
		return SessionBriefResult{}, fmt.Errorf("store: session_brief: hot-memory: %w", err)
	}
	out.HotMemory = hot

	patterns, err := s.readOptional("cog-meta/patterns.md")
	if err != nil {
		return SessionBriefResult{}, fmt.Errorf("store: session_brief: patterns: %w", err)
	}
	out.Patterns = patterns

	sorted := make([]ActionTarget, len(targets))
	copy(sorted, targets)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Domain == sorted[j].Domain {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].Domain < sorted[j].Domain
	})

	out.DomainActionCounts = make([]DomainActionCountItem, 0, len(sorted))
	for _, t := range sorted {
		open, pri, err := s.countOpenActions(t.Path)
		if err != nil {
			return SessionBriefResult{}, fmt.Errorf("store: session_brief: count %q: %w", t.Path, err)
		}
		out.DomainActionCounts = append(out.DomainActionCounts, DomainActionCountItem{
			Domain:       t.Domain,
			Path:         t.Path,
			OpenCount:    open,
			PriHighCount: pri,
		})
		if pri > 0 {
			out.PriHighAnywhere = true
		}
	}
	return out, nil
}

// readOptional returns "" if the file does not exist; other errors propagate.
func (s *MemoryStore) readOptional(relPath string) (string, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// countOpenActions returns (open, pri_high) counts for an action-items file.
// Missing files report (0, 0). Uses the same skip rules as OpenActions
// (HTML comments, fenced code blocks).
func (s *MemoryStore) countOpenActions(relPath string) (int, int, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return 0, 0, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	defer f.Close()
	_ = lockShared(f)
	defer unlock(f)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var open, pri int
	inComment := false
	inFence := false
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if skipActionLine(trimmed, &inComment, &inFence) {
			continue
		}
		if !strings.HasPrefix(trimmed, "- [ ] ") {
			continue
		}
		open++
		if item, ok := parseOpenActionItem("", relPath, 0, trimmed); ok {
			if strings.EqualFold(item.Priority, "high") {
				pri++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}
	return open, pri, nil
}
