package store

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// HousekeepingCaps controls all numeric thresholds the scanner enforces.
// Defaults (DefaultHousekeepingCaps) match cog-prime's published housekeeping
// rules; callers can override per-deployment without recompiling the daemon.
type HousekeepingCaps struct {
	ObservationsEntries         int
	CompletedActions            int
	ImprovementsDone            int
	HotMemoryLines              int
	PatternsLines               int
	PatternsBytes               int64
	DormantDomainDays           int
	StaleActionItemDays         int
	ChangedRecentlyFallbackDays int
}

// DefaultHousekeepingCaps returns the canonical cog-prime thresholds.
func DefaultHousekeepingCaps() HousekeepingCaps {
	return HousekeepingCaps{
		ObservationsEntries:         50,
		CompletedActions:            10,
		ImprovementsDone:            10,
		HotMemoryLines:              50,
		PatternsLines:               70,
		PatternsBytes:               5500,
		DormantDomainDays:           28,
		StaleActionItemDays:         14,
		ChangedRecentlyFallbackDays: 7,
	}
}

// HousekeepingTarget is a per-domain set of candidate files the scanner
// should inspect. Empty paths are skipped. Built by the RPC layer from
// the domain controller after RBAC filtering.
type HousekeepingTarget struct {
	DomainID         string
	ObservationsPath string
	ActionItemsPath  string
	HotMemoryPath    string
}

// HousekeepingInput is the full set of targets + extra root-level files
// (improvements.md, patterns.md, root hot-memory.md) plus the marker
// file path used to compute "since".
type HousekeepingInput struct {
	Targets          []HousekeepingTarget
	RootHotMemory    string
	PatternsPath     string
	ImprovementsPath string
	MarkerPath       string
	Caps             HousekeepingCaps
	// Now is injectable for deterministic tests. Zero value -> time.Now().UTC().
	Now time.Time
}

// HousekeepingResult is the wire shape returned by the housekeeping_scan RPC.
// Field names mirror docs/RPC-CONSOLIDATION.md §2.
type HousekeepingResult struct {
	Since            string                 `json:"since"`
	ChangedRecently  []string               `json:"changed_recently"`
	Thresholds       HousekeepingThresholds `json:"thresholds"`
	DormantDomains   []DormantDomain        `json:"dormant_domains"`
	StaleActionItems []StaleActionItem      `json:"stale_action_items"`
}

// HousekeepingThresholds groups the per-cap-violation arrays. Every array
// is non-nil so JSON consumers can iterate without nil-checking.
type HousekeepingThresholds struct {
	ObservationsOverCap            []ObservationsOverCap            `json:"observations_over_cap"`
	CompletedActionsOverCap        []CompletedActionsOverCap        `json:"completed_actions_over_cap"`
	ImprovementsImplementedOverCap []ImprovementsImplementedOverCap `json:"improvements_implemented_over_cap"`
	HotMemoryOverCap               []HotMemoryOverCap               `json:"hot_memory_over_cap"`
	PatternsOverCap                []PatternsOverCap                `json:"patterns_over_cap"`
}

// ObservationsOverCap reports an observations.md exceeding the entry cap,
// pre-aggregated by primary tag so the agent doesn't re-parse line-by-line
// to plan tag-grouped archival.
type ObservationsOverCap struct {
	Path         string         `json:"path"`
	Entries      int            `json:"entries"`
	Cap          int            `json:"cap"`
	ByPrimaryTag map[string]int `json:"by_primary_tag"`
}

type CompletedActionsOverCap struct {
	Path      string `json:"path"`
	Completed int    `json:"completed"`
	Cap       int    `json:"cap"`
}

type ImprovementsImplementedOverCap struct {
	Path        string `json:"path"`
	Implemented int    `json:"implemented"`
	Cap         int    `json:"cap"`
}

type HotMemoryOverCap struct {
	Path  string `json:"path"`
	Lines int    `json:"lines"`
	Cap   int    `json:"cap"`
}

// PatternsOverCap is dual-axis because cog-prime caps patterns.md on both
// lines (70) and size (5.5KB). One or both may exceed.
type PatternsOverCap struct {
	Path     string `json:"path"`
	Lines    int    `json:"lines"`
	Size     int64  `json:"size"`
	LinesCap int    `json:"lines_cap"`
	SizeCap  int64  `json:"size_cap"`
}

// DormantDomain reports a domain whose observations.md has no entry within
// the dormancy window. LastObservation is "" when the file exists but has
// no parseable dated entry.
type DormantDomain struct {
	ID              string `json:"id"`
	LastObservation string `json:"last_observation"`
}

// StaleActionItem reports a single open `- [ ]` item whose `added:` date
// is older than the stale window. Items without an `added:` marker are
// skipped (no inferable age).
type StaleActionItem struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Text    string `json:"text"`
	Added   string `json:"added"`
	AgeDays int    `json:"age_days"`
}

// HousekeepingScan walks the supplied targets and root files, applies the
// configured caps, and returns one envelope. Missing files are silently
// skipped (consistent with OpenActions). The caller (rpc layer) is
// responsible for RBAC filtering before passing targets in — the store
// layer has no role concept.
func (s *MemoryStore) HousekeepingScan(in HousekeepingInput) (HousekeepingResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if in.Caps == (HousekeepingCaps{}) {
		in.Caps = DefaultHousekeepingCaps()
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	res := HousekeepingResult{
		ChangedRecently:  []string{},
		DormantDomains:   []DormantDomain{},
		StaleActionItems: []StaleActionItem{},
		Thresholds: HousekeepingThresholds{
			ObservationsOverCap:            []ObservationsOverCap{},
			CompletedActionsOverCap:        []CompletedActionsOverCap{},
			ImprovementsImplementedOverCap: []ImprovementsImplementedOverCap{},
			HotMemoryOverCap:               []HotMemoryOverCap{},
			PatternsOverCap:                []PatternsOverCap{},
		},
	}

	cutoff, sinceStr, err := s.resolveSinceCutoff(in.MarkerPath, in.Caps.ChangedRecentlyFallbackDays, now)
	if err != nil {
		return res, fmt.Errorf("store: housekeeping scan: %w", err)
	}
	res.Since = sinceStr

	files, err := s.scanFiles()
	if err != nil {
		return res, fmt.Errorf("store: housekeeping scan: %w", err)
	}
	for _, sf := range files {
		if sf.relPath == in.MarkerPath {
			continue
		}
		if sf.modTime.After(cutoff) {
			res.ChangedRecently = append(res.ChangedRecently, sf.relPath)
		}
	}
	sort.Strings(res.ChangedRecently)

	for _, t := range in.Targets {
		if t.ObservationsPath != "" {
			if v, ok := s.scanObservations(t.ObservationsPath, in.Caps.ObservationsEntries); ok {
				res.Thresholds.ObservationsOverCap = append(res.Thresholds.ObservationsOverCap, v)
			}
			if d, ok := s.scanDormancy(t.DomainID, t.ObservationsPath, in.Caps.DormantDomainDays, now); ok {
				res.DormantDomains = append(res.DormantDomains, d)
			}
		}
		if t.ActionItemsPath != "" {
			completed, stale := s.scanActionItems(t.ActionItemsPath, in.Caps.StaleActionItemDays, now)
			if completed > in.Caps.CompletedActions {
				res.Thresholds.CompletedActionsOverCap = append(res.Thresholds.CompletedActionsOverCap, CompletedActionsOverCap{
					Path: t.ActionItemsPath, Completed: completed, Cap: in.Caps.CompletedActions,
				})
			}
			res.StaleActionItems = append(res.StaleActionItems, stale...)
		}
		if t.HotMemoryPath != "" {
			if v, ok := s.scanHotMemory(t.HotMemoryPath, in.Caps.HotMemoryLines); ok {
				res.Thresholds.HotMemoryOverCap = append(res.Thresholds.HotMemoryOverCap, v)
			}
		}
	}

	if in.RootHotMemory != "" {
		if v, ok := s.scanHotMemory(in.RootHotMemory, in.Caps.HotMemoryLines); ok {
			res.Thresholds.HotMemoryOverCap = append(res.Thresholds.HotMemoryOverCap, v)
		}
	}
	if in.ImprovementsPath != "" {
		if v, ok := s.scanImprovements(in.ImprovementsPath, in.Caps.ImprovementsDone); ok {
			res.Thresholds.ImprovementsImplementedOverCap = append(res.Thresholds.ImprovementsImplementedOverCap, v)
		}
	}
	if in.PatternsPath != "" {
		if v, ok := s.scanPatterns(in.PatternsPath, in.Caps.PatternsLines, in.Caps.PatternsBytes); ok {
			res.Thresholds.PatternsOverCap = append(res.Thresholds.PatternsOverCap, v)
		}
	}

	sort.Slice(res.Thresholds.ObservationsOverCap, func(i, j int) bool {
		return res.Thresholds.ObservationsOverCap[i].Path < res.Thresholds.ObservationsOverCap[j].Path
	})
	sort.Slice(res.Thresholds.CompletedActionsOverCap, func(i, j int) bool {
		return res.Thresholds.CompletedActionsOverCap[i].Path < res.Thresholds.CompletedActionsOverCap[j].Path
	})
	sort.Slice(res.Thresholds.HotMemoryOverCap, func(i, j int) bool {
		return res.Thresholds.HotMemoryOverCap[i].Path < res.Thresholds.HotMemoryOverCap[j].Path
	})
	sort.Slice(res.DormantDomains, func(i, j int) bool { return res.DormantDomains[i].ID < res.DormantDomains[j].ID })
	sort.Slice(res.StaleActionItems, func(i, j int) bool {
		if res.StaleActionItems[i].Path != res.StaleActionItems[j].Path {
			return res.StaleActionItems[i].Path < res.StaleActionItems[j].Path
		}
		return res.StaleActionItems[i].Line < res.StaleActionItems[j].Line
	})
	return res, nil
}

func (s *MemoryStore) resolveSinceCutoff(markerPath string, fallbackDays int, now time.Time) (time.Time, string, error) {
	if markerPath != "" {
		abs, err := s.absPath(markerPath)
		if err != nil {
			return time.Time{}, "", err
		}
		info, err := os.Stat(abs)
		if err == nil {
			t := info.ModTime().UTC()
			return t, t.Format(time.RFC3339), nil
		}
		if !os.IsNotExist(err) {
			return time.Time{}, "", fmt.Errorf("stat marker %q: %w", markerPath, err)
		}
	}
	if fallbackDays <= 0 {
		fallbackDays = 7
	}
	cutoff := now.Add(-time.Duration(fallbackDays) * 24 * time.Hour).UTC()
	return cutoff, "", nil
}

// obsTagsRE captures the bracketed tag list from "- YYYY-MM-DD [tags]: text".
var obsTagsRE = regexp.MustCompile(`^-\s+(\d{4}-\d{2}-\d{2})\s+\[([^\]]+)\]:`)

func (s *MemoryStore) scanObservations(relPath string, cap int) (ObservationsOverCap, bool) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return ObservationsOverCap{}, false
	}
	f, err := os.Open(abs)
	if err != nil {
		return ObservationsOverCap{}, false
	}
	defer f.Close()
	_ = lockShared(f)
	defer unlock(f)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	entries := 0
	byTag := map[string]int{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		m := obsTagsRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		entries++
		primary := strings.TrimSpace(strings.SplitN(m[2], ",", 2)[0])
		if primary == "" {
			primary = "(untagged)"
		}
		byTag[primary]++
	}
	if entries <= cap {
		return ObservationsOverCap{}, false
	}
	return ObservationsOverCap{Path: relPath, Entries: entries, Cap: cap, ByPrimaryTag: byTag}, true
}

func (s *MemoryStore) scanDormancy(domainID, relPath string, windowDays int, now time.Time) (DormantDomain, bool) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return DormantDomain{}, false
	}
	f, err := os.Open(abs)
	if err != nil {
		return DormantDomain{}, false
	}
	defer f.Close()
	_ = lockShared(f)
	defer unlock(f)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var latest string
	var latestT time.Time
	for scanner.Scan() {
		m := obsTagsRE.FindStringSubmatch(strings.TrimSpace(scanner.Text()))
		if m == nil {
			continue
		}
		t, err := time.Parse("2006-01-02", m[1])
		if err != nil {
			continue
		}
		if t.After(latestT) {
			latestT = t
			latest = m[1]
		}
	}
	cutoff := now.Add(-time.Duration(windowDays) * 24 * time.Hour)
	if latest != "" && !latestT.Before(cutoff) {
		return DormantDomain{}, false
	}
	return DormantDomain{ID: domainID, LastObservation: latest}, true
}

var addedDateRE = regexp.MustCompile(`(?i)\badded:\s*(\d{4}-\d{2}-\d{2})`)

// scanActionItems uses the same comment/fence skipping as OpenActions so it
// can't double-count example lines inside <!-- ... --> or ``` fences.
func (s *MemoryStore) scanActionItems(relPath string, staleDays int, now time.Time) (int, []StaleActionItem) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return 0, nil
	}
	f, err := os.Open(abs)
	if err != nil {
		return 0, nil
	}
	defer f.Close()
	_ = lockShared(f)
	defer unlock(f)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var stale []StaleActionItem
	completed := 0
	inComment := false
	inFence := false
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		trimmed := strings.TrimSpace(scanner.Text())
		if skipActionLine(trimmed, &inComment, &inFence) {
			continue
		}
		if strings.HasPrefix(trimmed, "- [x] ") || strings.HasPrefix(trimmed, "- [X] ") {
			completed++
			continue
		}
		if !strings.HasPrefix(trimmed, "- [ ] ") {
			continue
		}
		item, ok := parseOpenActionItem("", relPath, lineNo, trimmed)
		if !ok {
			continue
		}
		added := item.Added
		if added == "" {
			if m := addedDateRE.FindStringSubmatch(trimmed); m != nil {
				added = m[1]
			}
		}
		if added == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", added)
		if err != nil {
			continue
		}
		age := int(now.Sub(t) / (24 * time.Hour))
		if age < staleDays {
			continue
		}
		stale = append(stale, StaleActionItem{
			Path: relPath, Line: lineNo, Text: item.Text, Added: added, AgeDays: age,
		})
	}
	return completed, stale
}

func (s *MemoryStore) scanHotMemory(relPath string, cap int) (HotMemoryOverCap, bool) {
	lines, _, ok := countFile(s, relPath)
	if !ok || lines <= cap {
		return HotMemoryOverCap{}, false
	}
	return HotMemoryOverCap{Path: relPath, Lines: lines, Cap: cap}, true
}

func (s *MemoryStore) scanPatterns(relPath string, lineCap int, byteCap int64) (PatternsOverCap, bool) {
	lines, size, ok := countFile(s, relPath)
	if !ok {
		return PatternsOverCap{}, false
	}
	if lines <= lineCap && size <= byteCap {
		return PatternsOverCap{}, false
	}
	return PatternsOverCap{
		Path: relPath, Lines: lines, Size: size,
		LinesCap: lineCap, SizeCap: byteCap,
	}, true
}

func (s *MemoryStore) scanImprovements(relPath string, cap int) (ImprovementsImplementedOverCap, bool) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return ImprovementsImplementedOverCap{}, false
	}
	f, err := os.Open(abs)
	if err != nil {
		return ImprovementsImplementedOverCap{}, false
	}
	defer f.Close()
	_ = lockShared(f)
	defer unlock(f)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	implemented := 0
	inComment := false
	inFence := false
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if skipActionLine(trimmed, &inComment, &inFence) {
			continue
		}
		if strings.HasPrefix(trimmed, "- [x] ") || strings.HasPrefix(trimmed, "- [X] ") {
			implemented++
		}
	}
	if implemented <= cap {
		return ImprovementsImplementedOverCap{}, false
	}
	return ImprovementsImplementedOverCap{Path: relPath, Implemented: implemented, Cap: cap}, true
}

func countFile(s *MemoryStore, relPath string) (lines int, size int64, ok bool) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return 0, 0, false
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return 0, 0, false
	}
	n := strings.Count(string(data), "\n")
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n, int64(len(data)), true
}
