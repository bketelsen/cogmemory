package store

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LinkEntry is one row of the reverse wiki-link index.
type LinkEntry struct {
	Target  string   `json:"target"`
	Sources []string `json:"sources"`
}

// LinkAuditCandidate is one suspected missing-link occurrence: an unlinked
// mention of an entity name that has a canonical home elsewhere in memory.
type LinkAuditCandidate struct {
	SourcePath string `json:"source_path"`
	Line       int    `json:"line"`
	EntityName string `json:"entity_name"`
	TargetLink string `json:"target_link"`
	Context    string `json:"context"`
}

// wikiLinkRE matches [[anything-not-containing-double-brackets]]. The captured
// group is the raw inner text; downstream code strips `#section` suffixes and
// drops any trailing `.md`.
var wikiLinkRE = regexp.MustCompile(`\[\[([^\[\]]+?)\]\]`)

// entityHeaderRE matches "### Name" entity headers — the canonical home for an
// entity in a `*/entities.md` file. Parenthetical role suffixes like
// "### Jane Smith (CTO)" are stripped by the caller.
var entityHeaderRE = regexp.MustCompile(`^###\s+(.+?)\s*$`)

// normalizeLinkTarget canonicalises a wiki-link target the way the existing
// housekeeping skill does:
//   - strip a trailing `#section`
//   - drop a trailing `.md` (path-relative-without-extension is the convention)
//   - keep `wiki:` prefixed external links verbatim (minus the `#section`).
func normalizeLinkTarget(raw string) string {
	target := strings.TrimSpace(raw)
	if i := strings.Index(target, "#"); i >= 0 {
		target = target[:i]
	}
	target = strings.TrimSuffix(target, ".md")
	return target
}

// relatedFrontmatter extracts the `related:` array from a markdown file's YAML
// frontmatter, if present. These are first-class curated cross-references (the
// canonical wiki link mechanism) and are indexed as links in addition to body
// `[[wiki-link]]` references. A missing frontmatter block, missing `related`
// key, or malformed YAML all yield nil (fail-open).
func relatedFrontmatter(data string) []string {
	fm, ok := extractFrontmatter([]byte(data))
	if !ok {
		return nil
	}
	var parsed struct {
		Related []string `yaml:"related"`
	}
	if yaml.Unmarshal(fm, &parsed) != nil {
		return nil
	}
	return parsed.Related
}

// readFileShared reads a file under shared lock and returns its contents.
func (s *MemoryStore) readFileShared(abs string) (string, error) {
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_ = lockShared(f)
	defer unlock(f)
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// LinkIndex walks every memory file and builds a reverse `[[wiki-link]]`
// index: target -> sorted list of source files. Source paths use the same
// extensionless convention as link-index.md (e.g. "personal/observations").
// Targets are returned as-encoded in the source (after #section trim + .md
// trim). Results are sorted by target then by source for deterministic output.
func (s *MemoryStore) LinkIndex() ([]LinkEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return nil, fmt.Errorf("store: link index: %w", err)
	}

	// target -> set of sources (sets prevent multi-mention dupes per file).
	idx := map[string]map[string]struct{}{}
	for _, sf := range files {
		if !strings.HasSuffix(sf.relPath, ".md") {
			continue
		}
		data, err := s.readFileShared(sf.absPath)
		if err != nil {
			continue
		}
		source := strings.TrimSuffix(sf.relPath, ".md")
		matches := wikiLinkRE.FindAllStringSubmatch(data, -1)
		for _, m := range matches {
			target := normalizeLinkTarget(m[1])
			if target == "" || target == source {
				continue
			}
			set, ok := idx[target]
			if !ok {
				set = map[string]struct{}{}
				idx[target] = set
			}
			set[source] = struct{}{}
		}
		// Also index `related:` frontmatter entries as first-class links —
		// the canonical curated cross-reference mechanism for the wiki tier.
		for _, raw := range relatedFrontmatter(data) {
			target := normalizeLinkTarget(raw)
			if target == "" || target == source {
				continue
			}
			set, ok := idx[target]
			if !ok {
				set = map[string]struct{}{}
				idx[target] = set
			}
			set[source] = struct{}{}
		}
	}

	out := make([]LinkEntry, 0, len(idx))
	for target, set := range idx {
		sources := make([]string, 0, len(set))
		for src := range set {
			sources = append(sources, src)
		}
		sort.Strings(sources)
		out = append(out, LinkEntry{Target: target, Sources: sources})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

// LinkIndexFiltered is LinkIndex with a per-source RBAC predicate applied.
// A source the role can't read is removed; targets that lose all sources are
// dropped from the result.
func (s *MemoryStore) LinkIndexFiltered(canRead func(relPath string) bool) ([]LinkEntry, error) {
	all, err := s.LinkIndex()
	if err != nil {
		return nil, err
	}
	out := make([]LinkEntry, 0, len(all))
	for _, e := range all {
		kept := make([]string, 0, len(e.Sources))
		for _, src := range e.Sources {
			if canRead(src + ".md") {
				kept = append(kept, src)
			}
		}
		if len(kept) > 0 {
			out = append(out, LinkEntry{Target: e.Target, Sources: kept})
		}
	}
	return out, nil
}

// LinkAudit walks `*/entities.md` files to build an entity registry, then
// scans every other markdown file for unlinked mentions of those entities.
// A mention counts as unlinked when the entity name appears as a whole-word
// substring on a line that does NOT already contain `[[Name` (the prefix of
// any wiki-link to that entity). Mentions in an entity's own home file are
// skipped (the ### header IS the canonical mention).
//
// canRead is consulted twice: once to filter out entity homes the role can't
// see (so we don't surface candidates pointing at hidden targets), and once
// per source path to filter out hits from files the role can't read. Pass a
// no-op predicate (always true) to bypass RBAC.
func (s *MemoryStore) LinkAudit(canRead func(relPath string) bool) ([]LinkAuditCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	files, err := s.scanFiles()
	if err != nil {
		return nil, fmt.Errorf("store: link audit: %w", err)
	}

	type entity struct {
		name   string
		target string // e.g. "personal/entities#Jane"
		source string // entities.md relative path (extensionless)
	}
	var entities []entity
	seen := map[string]struct{}{}

	for _, sf := range files {
		base := sf.relPath
		if !(strings.HasSuffix(base, "/entities.md") || base == "entities.md") {
			continue
		}
		if !canRead(base) {
			continue
		}
		data, err := s.readFileShared(sf.absPath)
		if err != nil {
			continue
		}
		homeSource := strings.TrimSuffix(base, ".md")
		for _, line := range strings.Split(data, "\n") {
			m := entityHeaderRE.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := strings.TrimSpace(m[1])
			if i := strings.Index(name, " ("); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			if len(name) < 2 {
				continue
			}
			key := homeSource + "#" + name
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			entities = append(entities, entity{
				name:   name,
				target: homeSource + "#" + name,
				source: homeSource,
			})
		}
	}

	if len(entities) == 0 {
		return []LinkAuditCandidate{}, nil
	}

	candidates := []LinkAuditCandidate{}
	for _, sf := range files {
		if !strings.HasSuffix(sf.relPath, ".md") {
			continue
		}
		if !canRead(sf.relPath) {
			continue
		}
		source := strings.TrimSuffix(sf.relPath, ".md")
		data, err := s.readFileShared(sf.absPath)
		if err != nil {
			continue
		}
		lines := strings.Split(data, "\n")
		for i, line := range lines {
			linkSpans := wikiLinkRE.FindAllStringIndex(line, -1)
			for _, e := range entities {
				if source == e.source {
					continue
				}
				pos := wholeWordIndex(line, e.name)
				if pos < 0 {
					continue
				}
				// If the match sits inside an existing [[...]] span, treat it as
				// already linked — the agent chose to reference it via wiki-link
				// (possibly with a path prefix like [[personal/entities#Bob]]).
				if insideAnySpan(linkSpans, pos, pos+len(e.name)) {
					continue
				}
				candidates = append(candidates, LinkAuditCandidate{
					SourcePath: sf.relPath,
					Line:       i + 1,
					EntityName: e.name,
					TargetLink: e.target,
					Context:    strings.TrimSpace(line),
				})
			}
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].SourcePath != candidates[j].SourcePath {
			return candidates[i].SourcePath < candidates[j].SourcePath
		}
		if candidates[i].Line != candidates[j].Line {
			return candidates[i].Line < candidates[j].Line
		}
		return candidates[i].EntityName < candidates[j].EntityName
	})
	return candidates, nil
}

// containsWholeWord reports whether name appears in line on a word boundary.
func containsWholeWord(line, name string) bool {
	return wholeWordIndex(line, name) >= 0
}

// wholeWordIndex returns the byte offset of the first whole-word occurrence of
// name in line, or -1 if absent. Letters, digits, and underscores are word
// characters; everything else (and line edges) is a boundary. Case-sensitive
// — entity names are proper nouns in this codebase.
func wholeWordIndex(line, name string) int {
	if name == "" {
		return -1
	}
	idx := 0
	for {
		j := strings.Index(line[idx:], name)
		if j < 0 {
			return -1
		}
		start := idx + j
		end := start + len(name)
		if isWordBoundary(line, start-1) && isWordBoundary(line, end) {
			return start
		}
		idx = start + 1
	}
}

// insideAnySpan reports whether [start,end) intersects any of the supplied
// [a,b) byte spans.
func insideAnySpan(spans [][]int, start, end int) bool {
	for _, sp := range spans {
		if start < sp[1] && end > sp[0] {
			return true
		}
	}
	return false
}

func isWordBoundary(line string, i int) bool {
	if i < 0 || i >= len(line) {
		return true
	}
	c := line[i]
	switch {
	case c >= 'a' && c <= 'z':
		return false
	case c >= 'A' && c <= 'Z':
		return false
	case c >= '0' && c <= '9':
		return false
	case c == '_':
		return false
	}
	return true
}
