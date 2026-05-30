package store

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ClusterObsTarget mirrors domain.ClusterObsTarget but lives in the store
// package so the store does not import domain. The RPC layer translates.
type ClusterObsTarget struct {
	Domain string
	Path   string
}

// Observation is one parsed entry from an observations.md file.
type Observation struct {
	Domain string    `json:"domain"`
	Path   string    `json:"path"`
	Line   int       `json:"line"`
	Date   time.Time `json:"-"`
	DateS  string    `json:"date"`
	Tags   []string  `json:"tags"`
	Text   string    `json:"text"`
}

// TagCluster groups observations sharing a tag.
type TagCluster struct {
	Tag       string      `json:"tag"`
	Count     int         `json:"count"`
	SpansDays int         `json:"spans_days"`
	Domains   []string    `json:"domains"`
	Samples   []SampleObs `json:"samples"`
}

// KeywordCluster groups observations whose Text contains a recurring keyword
// (naive substring frequency — not semantic).
type KeywordCluster struct {
	Keyword   string      `json:"keyword"`
	Count     int         `json:"count"`
	SpansDays int         `json:"spans_days"`
	Domains   []string    `json:"domains"`
	Samples   []SampleObs `json:"samples"`
}

// SampleObs is a trimmed observation used for cluster previews.
type SampleObs struct {
	Date   string `json:"date"`
	Domain string `json:"domain"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Text   string `json:"text"`
}

// ThreadCandidate is a topic surfacing across span_days+ days with
// min_cluster_size+ fragments.
type ThreadCandidate struct {
	Topic         string `json:"topic"`
	FragmentCount int    `json:"fragment_count"`
	DateRange     string `json:"date_range"`
}

// ClusterResult is the cluster_check envelope.
type ClusterResult struct {
	ByTag            []TagCluster      `json:"by_tag"`
	ByKeyword        []KeywordCluster  `json:"by_keyword"`
	ThreadCandidates []ThreadCandidate `json:"thread_candidates"`
}

// ClusterParams controls Cluster behavior. Defaults applied when fields are
// zero: MinClusterSize=3, SpanDays=14, SampleLimit=3, Since=now-7d.
type ClusterParams struct {
	MinClusterSize int
	Since          time.Time
	SpanDays       int
	SampleLimit    int
	Now            time.Time // injected for tests
}

// Cluster scans the supplied observation targets within [since, +inf) and
// groups by tag and recurring keyword. Thread candidates surface topics that
// span at least span_days. Missing files are skipped silently; format errors
// surface as errors.
func (s *MemoryStore) Cluster(targets []ClusterObsTarget, p ClusterParams) (ClusterResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p.MinClusterSize <= 0 {
		p.MinClusterSize = 3
	}
	if p.SpanDays <= 0 {
		p.SpanDays = 14
	}
	if p.SampleLimit <= 0 {
		p.SampleLimit = 3
	}
	if p.Now.IsZero() {
		p.Now = time.Now().UTC()
	}
	if p.Since.IsZero() {
		p.Since = p.Now.AddDate(0, 0, -7)
	}
	since := p.Since.Truncate(24 * time.Hour)

	sorted := make([]ClusterObsTarget, len(targets))
	copy(sorted, targets)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	var all []Observation
	for _, t := range sorted {
		abs, err := s.absPath(t.Path)
		if err != nil {
			return ClusterResult{}, fmt.Errorf("store: cluster: %w", err)
		}
		f, err := os.Open(abs)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return ClusterResult{}, fmt.Errorf("store: cluster: open %q: %w", t.Path, err)
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
			if skipActionLine(trimmed, &inComment, &inFence) {
				continue
			}
			obs, ok := parseClusterObs(t.Domain, t.Path, lineNo, trimmed)
			if !ok {
				continue
			}
			if obs.Date.Before(since) {
				continue
			}
			all = append(all, obs)
		}
		scanErr := scanner.Err()
		unlock(f)
		f.Close()
		if scanErr != nil {
			return ClusterResult{}, fmt.Errorf("store: cluster: scan %q: %w", t.Path, scanErr)
		}
	}

	return ClusterResult{
		ByTag:            buildTagClusters(all, p),
		ByKeyword:        buildKeywordClusters(all, p),
		ThreadCandidates: buildThreadCandidates(all, p),
	}, nil
}

// clusterObsRE captures date, tags, text from "- YYYY-MM-DD [tags]: text".
var clusterObsRE = regexp.MustCompile(`^-\s+(\d{4}-\d{2}-\d{2})\s+\[([^\]]+)\]:\s*(.+)$`)

func parseClusterObs(domain, path string, line int, trimmed string) (Observation, bool) {
	m := clusterObsRE.FindStringSubmatch(trimmed)
	if m == nil {
		return Observation{}, false
	}
	date, err := time.Parse("2006-01-02", m[1])
	if err != nil {
		return Observation{}, false
	}
	tagFields := strings.FieldsFunc(m[2], func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	tags := make([]string, 0, len(tagFields))
	seen := map[string]bool{}
	for _, tag := range tagFields {
		t := strings.ToLower(strings.TrimSpace(tag))
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		tags = append(tags, t)
	}
	return Observation{
		Domain: domain,
		Path:   path,
		Line:   line,
		Date:   date,
		DateS:  m[1],
		Tags:   tags,
		Text:   strings.TrimSpace(m[3]),
	}, true
}

func buildTagClusters(all []Observation, p ClusterParams) []TagCluster {
	byTag := map[string][]Observation{}
	for _, o := range all {
		for _, t := range o.Tags {
			byTag[t] = append(byTag[t], o)
		}
	}
	out := []TagCluster{}
	for tag, obs := range byTag {
		if len(obs) < p.MinClusterSize {
			continue
		}
		out = append(out, TagCluster{
			Tag:       tag,
			Count:     len(obs),
			SpansDays: spanDays(obs),
			Domains:   uniqueDomains(obs),
			Samples:   makeSamples(obs, p.SampleLimit),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Tag < out[j].Tag
	})
	return out
}

// keywordRE extracts word-ish tokens (letters/digits, len>=4).
var keywordRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_-]{3,}`)

var stopwords = map[string]bool{
	"this": true, "that": true, "with": true, "from": true, "have": true,
	"will": true, "into": true, "been": true, "were": true, "their": true,
	"they": true, "what": true, "when": true, "then": true, "than": true,
	"there": true, "about": true, "which": true, "would": true, "could": true,
	"should": true, "after": true, "before": true, "still": true, "also": true,
	"just": true, "like": true, "some": true, "more": true, "most": true,
	"only": true, "your": true, "yours": true, "ours": true, "them": true,
	"http": true, "https": true,
}

func buildKeywordClusters(all []Observation, p ClusterParams) []KeywordCluster {
	byTerm := map[string][]Observation{}
	for _, o := range all {
		seen := map[string]bool{}
		for _, m := range keywordRE.FindAllString(o.Text, -1) {
			term := strings.ToLower(m)
			if stopwords[term] || seen[term] {
				continue
			}
			seen[term] = true
			byTerm[term] = append(byTerm[term], o)
		}
	}
	out := []KeywordCluster{}
	for term, obs := range byTerm {
		if len(obs) < p.MinClusterSize {
			continue
		}
		out = append(out, KeywordCluster{
			Keyword:   term,
			Count:     len(obs),
			SpansDays: spanDays(obs),
			Domains:   uniqueDomains(obs),
			Samples:   makeSamples(obs, p.SampleLimit),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Keyword < out[j].Keyword
	})
	return out
}

func buildThreadCandidates(all []Observation, p ClusterParams) []ThreadCandidate {
	type bucket struct{ obs []Observation }
	buckets := map[string]*bucket{}

	add := func(key string, o Observation) {
		b, ok := buckets[key]
		if !ok {
			b = &bucket{}
			buckets[key] = b
		}
		b.obs = append(b.obs, o)
	}

	for _, o := range all {
		for _, t := range o.Tags {
			add("tag:"+t, o)
		}
		seen := map[string]bool{}
		for _, m := range keywordRE.FindAllString(o.Text, -1) {
			term := strings.ToLower(m)
			if stopwords[term] || seen[term] {
				continue
			}
			seen[term] = true
			add("keyword:"+term, o)
		}
	}

	out := []ThreadCandidate{}
	for key, b := range buckets {
		if len(b.obs) < p.MinClusterSize {
			continue
		}
		span := spanDays(b.obs)
		if span < p.SpanDays {
			continue
		}
		minD, maxD := dateRange(b.obs)
		out = append(out, ThreadCandidate{
			Topic:         key,
			FragmentCount: len(b.obs),
			DateRange:     minD + ".." + maxD,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FragmentCount != out[j].FragmentCount {
			return out[i].FragmentCount > out[j].FragmentCount
		}
		return out[i].Topic < out[j].Topic
	})
	return out
}

func spanDays(obs []Observation) int {
	if len(obs) == 0 {
		return 0
	}
	minT, maxT := obs[0].Date, obs[0].Date
	for _, o := range obs[1:] {
		if o.Date.Before(minT) {
			minT = o.Date
		}
		if o.Date.After(maxT) {
			maxT = o.Date
		}
	}
	return int(maxT.Sub(minT).Hours()/24) + 1
}

func dateRange(obs []Observation) (string, string) {
	if len(obs) == 0 {
		return "", ""
	}
	minT, maxT := obs[0].Date, obs[0].Date
	for _, o := range obs[1:] {
		if o.Date.Before(minT) {
			minT = o.Date
		}
		if o.Date.After(maxT) {
			maxT = o.Date
		}
	}
	return minT.Format("2006-01-02"), maxT.Format("2006-01-02")
}

func uniqueDomains(obs []Observation) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, o := range obs {
		if seen[o.Domain] || o.Domain == "" {
			continue
		}
		seen[o.Domain] = true
		out = append(out, o.Domain)
	}
	sort.Strings(out)
	return out
}

func makeSamples(obs []Observation, limit int) []SampleObs {
	sorted := make([]Observation, len(obs))
	copy(sorted, obs)
	sort.Slice(sorted, func(i, j int) bool {
		if !sorted[i].Date.Equal(sorted[j].Date) {
			return sorted[i].Date.After(sorted[j].Date)
		}
		if sorted[i].Path != sorted[j].Path {
			return sorted[i].Path < sorted[j].Path
		}
		return sorted[i].Line < sorted[j].Line
	})
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	out := make([]SampleObs, 0, len(sorted))
	for _, o := range sorted {
		out = append(out, SampleObs{
			Date:   o.DateS,
			Domain: o.Domain,
			Path:   o.Path,
			Line:   o.Line,
			Text:   o.Text,
		})
	}
	return out
}
