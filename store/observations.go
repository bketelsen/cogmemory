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

// ObsTarget is one resolved observations file the store should scan. The
// Domain field is the canonical domain id supplied by the caller (typically
// the domain controller); the store does not infer it from the path.
type ObsTarget struct {
	Domain string
	Path   string // POSIX-style relative path, e.g. "personal/observations.md"
}

// ObservationEntry holds one parsed observation line.
type ObservationEntry struct {
	Domain string   `json:"domain"`
	Path   string   `json:"path"`
	Line   int      `json:"line"`
	Date   string   `json:"date"` // YYYY-MM-DD
	Tags   []string `json:"tags"`
	Text   string   `json:"text"`
}

// RecentObservationsResult is the aggregate envelope returned to RPC callers.
//
// All collections are non-nil even when empty so the JSON shape stays stable
// (`[]` / `{}` rather than `null`). Entries are sorted newest-first by date,
// then by (path, line) for determinism within a date. ByDomain and ByTag are
// computed from the same filtered/RBAC-checked entries that appear in
// Entries — consumers never need to re-aggregate client-side.
type RecentObservationsResult struct {
	Since    string             `json:"since"` // YYYY-MM-DD inclusive
	Entries  []ObservationEntry `json:"entries"`
	ByDomain map[string]int     `json:"by_domain"`
	ByTag    map[string]int     `json:"by_tag"`
}

// obsLineParseRE captures the date, the bracketed tag list, and the trailing
// text from observation lines that already passed obsLineRE. Kept distinct
// from obsLineRE (which is the strict validator) so a future tweak to the
// parser doesn't accidentally loosen the writer's invariant.
var obsLineParseRE = regexp.MustCompile(`^-\s+(\d{4}-\d{2}-\d{2})\s+\[([^\]]+)\]:\s*(.+)$`)

// RecentObservations scans the supplied observations targets and returns
// parsed entries with date >= since, plus pre-computed by-domain and by-tag
// aggregate counts. Missing files are skipped silently (a domain that
// declares observations but hasn't been written to yet is fine); read or
// scan errors surface as errors so the caller can distinguish "no data" from
// "couldn't read".
//
// since must be a YYYY-MM-DD string; the comparison is inclusive
// (date >= since). Empty since returns all entries (no time filter).
//
// byTag, when non-empty, restricts entries to those whose tag list contains
// the value (case-sensitive). byDomain, when non-empty, restricts to that
// canonical domain id. Both apply before aggregation, so ByTag / ByDomain
// counts reflect what the caller actually sees in Entries.
func (s *MemoryStore) RecentObservations(targets []ObsTarget, since, byTag, byDomain string) (RecentObservationsResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := RecentObservationsResult{
		Since:    since,
		Entries:  []ObservationEntry{},
		ByDomain: map[string]int{},
		ByTag:    map[string]int{},
	}

	// Deterministic file order regardless of caller-supplied order.
	sorted := make([]ObsTarget, len(targets))
	copy(sorted, targets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	for _, t := range sorted {
		if byDomain != "" && t.Domain != byDomain {
			continue
		}
		abs, err := s.absPath(t.Path)
		if err != nil {
			return RecentObservationsResult{}, fmt.Errorf("store: recent observations: %w", err)
		}
		f, err := os.Open(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return RecentObservationsResult{}, fmt.Errorf("store: recent observations: open %q: %w", t.Path, err)
		}
		_ = lockShared(f)
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		lineNo := 0
		inComment := false
		inFence := false
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			// Reuse the action-items skipper for the markdown noise filter
			// (HTML comments + fenced code blocks). Observation files don't
			// contain checkbox items, so the "- [ ]" check downstream is
			// irrelevant; only the comment/fence side effects matter.
			if skipActionLine(trimmed, &inComment, &inFence) {
				continue
			}
			entry, ok := parseObservationLine(t.Domain, t.Path, lineNo, trimmed)
			if !ok {
				continue
			}
			if since != "" && entry.Date < since {
				continue
			}
			if byTag != "" && !containsTag(entry.Tags, byTag) {
				continue
			}
			result.Entries = append(result.Entries, entry)
		}
		scanErr := scanner.Err()
		unlock(f)
		f.Close()
		if scanErr != nil {
			return RecentObservationsResult{}, fmt.Errorf("store: recent observations: scan %q: %w", t.Path, scanErr)
		}
	}

	// Aggregate. Sort newest-first; ties broken by (path, line) for
	// determinism so the same store state always yields the same output.
	sort.SliceStable(result.Entries, func(i, j int) bool {
		a, b := result.Entries[i], result.Entries[j]
		if a.Date != b.Date {
			return a.Date > b.Date
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Line < b.Line
	})
	for _, e := range result.Entries {
		result.ByDomain[e.Domain]++
		for _, tag := range e.Tags {
			result.ByTag[tag]++
		}
	}
	return result, nil
}

// parseObservationLine extracts (date, tags, text) from a validated
// observation line. Returns ok=false when the line does not match the
// observation grammar (e.g. heading, blank, free prose under a section).
func parseObservationLine(domain, path string, line int, raw string) (ObservationEntry, bool) {
	m := obsLineParseRE.FindStringSubmatch(raw)
	if m == nil {
		return ObservationEntry{}, false
	}
	// Sanity: confirm against the strict validator so parser drift can't
	// admit a line the writer wouldn't accept.
	if !obsLineRE.MatchString(raw) {
		return ObservationEntry{}, false
	}
	date := m[1]
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return ObservationEntry{}, false
	}
	rawTags := strings.Split(m[2], ",")
	tags := make([]string, 0, len(rawTags))
	for _, t := range rawTags {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return ObservationEntry{
		Domain: domain,
		Path:   path,
		Line:   line,
		Date:   date,
		Tags:   tags,
		Text:   strings.TrimSpace(m[3]),
	}, true
}

func containsTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// RecentObservationsForFile reads a single observations.md file at relPath and
// returns parsed entries whose date is >= sinceDate (YYYY-MM-DD lexical compare).
// sinceDate "" disables the filter. Missing file → (nil, nil). Single-file
// helper kept for domain_summary; the multi-file aggregator is RecentObservations.
func (s *MemoryStore) RecentObservationsForFile(relPath, sinceDate string) ([]ObservationEntry, error) {
	abs, err := s.absPath(relPath)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("store: recent observations: %w", err)
	}
	defer f.Close()
	if err := lockShared(f); err != nil {
		return nil, fmt.Errorf("store: lock %q: %w", relPath, err)
	}
	defer unlock(f)

	out := []ObservationEntry{}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		trimmed := strings.TrimSpace(scanner.Text())
		entry, ok := parseObservationLine("", relPath, lineNo, trimmed)
		if !ok {
			continue
		}
		if sinceDate != "" && entry.Date < sinceDate {
			continue
		}
		out = append(out, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("store: recent observations scan %q: %w", relPath, err)
	}
	return out, nil
}
