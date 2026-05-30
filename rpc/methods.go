package rpc

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/bketelsen/cogmemory/domain"
	"github.com/bketelsen/cogmemory/store"
)

// baseParams contains the common role field present in all requests.
type baseParams struct {
	Role string `json:"role"`
}

// --- read ---

type readParams struct {
	baseParams
	Path    string `json:"path"`
	Section string `json:"section,omitempty"`
	Start   int    `json:"start,omitempty"`
	End     int    `json:"end,omitempty"`
}

func (srv *Server) handleRead(req Request) Response {
	var p readParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "read: invalid params: "+err.Error())
	}
	if p.Path == "" {
		return errorResponse(req.ID, CodeInvalidParams, "read: path is required")
	}
	// Special paths bypass RBAC (metadata, not file content)
	if p.Path != "L0_INDEX" && p.Path != "LIST" {
		if !srv.rbac.Check(p.Role, p.Path, "read") {
			return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("read denied for role %q on %q", p.Role, p.Path))
		}
	}
	content, err := srv.store.Read(p.Path, p.Section, p.Start, p.End)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "read: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"content": content,
		"found":   content != "",
	})
}

// --- write ---

type writeParams struct {
	baseParams
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (srv *Server) handleWrite(req Request) Response {
	var p writeParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "write: invalid params: "+err.Error())
	}
	if p.Path == "" {
		return errorResponse(req.ID, CodeInvalidParams, "write: path is required")
	}
	if !srv.rbac.Check(p.Role, p.Path, "write") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("write denied for role %q on %q", p.Role, p.Path))
	}
	srv.warnIfMalformed("write", p.Path)
	if err := srv.store.Write(p.Path, p.Content); err != nil {
		return errorResponse(req.ID, CodeStoreError, "write: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"bytes": len(p.Content),
	})
}

// --- append ---

type appendParams struct {
	baseParams
	Path    string `json:"path"`
	Text    string `json:"text"`
	Section string `json:"section,omitempty"`
}

func (srv *Server) handleAppend(req Request) Response {
	var p appendParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "append: invalid params: "+err.Error())
	}
	if p.Path == "" {
		return errorResponse(req.ID, CodeInvalidParams, "append: path is required")
	}
	if !srv.rbac.Check(p.Role, p.Path, "write") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("append denied for role %q on %q", p.Role, p.Path))
	}
	srv.warnIfMalformed("append", p.Path)
	if err := srv.store.AppendSection(p.Path, p.Section, p.Text); err != nil {
		return errorResponse(req.ID, CodeStoreError, "append: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"ok": true,
	})
}

// --- patch ---

type patchParams struct {
	baseParams
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

func (srv *Server) handlePatch(req Request) Response {
	var p patchParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "patch: invalid params: "+err.Error())
	}
	if p.Path == "" {
		return errorResponse(req.ID, CodeInvalidParams, "patch: path is required")
	}
	if !srv.rbac.Check(p.Role, p.Path, "write") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("patch denied for role %q on %q", p.Role, p.Path))
	}
	if err := srv.store.Patch(p.Path, p.OldText, p.NewText); err != nil {
		return errorResponse(req.ID, CodeStoreError, "patch: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"ok": true,
	})
}

// --- outline ---

type outlineParams struct {
	baseParams
	Path string `json:"path"`
}

func (srv *Server) handleOutline(req Request) Response {
	var p outlineParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "outline: invalid params: "+err.Error())
	}
	if p.Path == "" {
		return errorResponse(req.ID, CodeInvalidParams, "outline: path is required")
	}
	if !srv.rbac.Check(p.Role, p.Path, "read") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("outline denied for role %q on %q", p.Role, p.Path))
	}
	entries, err := srv.store.Outline(p.Path)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "outline: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"entries": entries,
	})
}

// --- move ---

type moveParams struct {
	baseParams
	From string `json:"from"`
	To   string `json:"to"`
}

func (srv *Server) handleMove(req Request) Response {
	var p moveParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "move: invalid params: "+err.Error())
	}
	if p.From == "" {
		return errorResponse(req.ID, CodeInvalidParams, "move: from is required")
	}
	if p.To == "" {
		return errorResponse(req.ID, CodeInvalidParams, "move: to is required")
	}
	if !srv.rbac.Check(p.Role, p.To, "write") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("move denied for role %q on %q", p.Role, p.To))
	}
	if err := srv.store.Move(p.From, p.To); err != nil {
		return errorResponse(req.ID, CodeStoreError, "move: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"ok": true,
	})
}

// --- search ---

type searchParams struct {
	baseParams
	Query string `json:"query"`
}

func (srv *Server) handleSearch(req Request) Response {
	var p searchParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "search: invalid params: "+err.Error())
	}
	if p.Query == "" {
		return errorResponse(req.ID, CodeInvalidParams, "search: query is required")
	}
	// Search requires read access on ** — use a wildcard check
	if !srv.rbac.Check(p.Role, "**", "read") && !srv.rbac.Check(p.Role, "hot-memory.md", "read") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("search denied for role %q", p.Role))
	}
	results, err := srv.store.Search(p.Query)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "search: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"results": results,
		"count":   len(results),
	})
}

// --- stats ---

type statsParams struct {
	baseParams
	Prefix string `json:"prefix"`
}

func (srv *Server) handleStats(req Request) Response {
	var p statsParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &p) //nolint:errcheck
	}
	// Stats is available to any authenticated role
	stats, err := srv.store.Stats(p.Prefix)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "stats: "+err.Error())
	}
	return okResponse(req.ID, stats)
}

// --- l0index ---

type l0indexParams struct {
	baseParams
	Domain string `json:"domain"`
}

func (srv *Server) handleL0Index(req Request) Response {
	var p l0indexParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &p) //nolint:errcheck
	}
	index, err := srv.store.L0Index(p.Domain)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "l0index: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"index": index,
	})
}

// --- list ---

type listParams struct {
	baseParams
}

func (srv *Server) handleList(req Request) Response {
	var p listParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &p) //nolint:errcheck
	}
	paths, err := srv.store.List()
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "list: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"paths": paths,
	})
}

// --- open_actions ---

type openActionsParams struct {
	baseParams
	// Domain, when set, restricts the scan to that single domain id.
	// Reduces wire chatter on busy boards.
	Domain string `json:"domain,omitempty"`
}

func (srv *Server) handleOpenActions(req Request) Response {
	var p openActionsParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "open_actions: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "open_actions: role required")
	}

	var targets []store.ActionTarget
	if srv.controller != nil {
		var ctrlTargets []domain.ActionTarget
		if p.Domain != "" {
			d, err := srv.controller.Get(p.Domain)
			if err != nil {
				return errorResponse(req.ID, CodeInvalidParams, "open_actions: "+err.Error())
			}
			// Only include action-items if the domain declares it; missing on
			// disk is fine (the store skips), but undeclared is a caller error.
			path, err := srv.controller.ResolveFile(d.ID, "action-items")
			if err != nil {
				return errorResponse(req.ID, CodeInvalidParams, "open_actions: "+err.Error())
			}
			ctrlTargets = []domain.ActionTarget{{Domain: d.ID, Path: path}}
		} else {
			ctrlTargets = srv.controller.ActionItems()
		}
		targets = make([]store.ActionTarget, 0, len(ctrlTargets))
		for _, t := range ctrlTargets {
			targets = append(targets, store.ActionTarget{Domain: t.Domain, Path: t.Path})
		}
	}

	items, err := srv.store.OpenActions(targets)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "open_actions: "+err.Error())
	}
	filtered := make([]store.OpenActionItem, 0, len(items))
	for _, item := range items {
		if srv.rbac.Check(p.Role, item.Path, "read") {
			filtered = append(filtered, item)
		}
	}
	return okResponse(req.ID, map[string]interface{}{
		"items": filtered,
	})
}

// --- recent_observations ---

type recentObservationsParams struct {
	baseParams
	// Since is an inclusive YYYY-MM-DD lower bound. Empty defaults to
	// "today minus 7 days" (reflect + foresight's standard window).
	Since string `json:"since,omitempty"`
	// ByTag, when set, restricts entries to those whose tag list contains
	// the given tag (case-sensitive). Aggregates reflect the filtered set.
	ByTag string `json:"by_tag,omitempty"`
	// ByDomain, when set, restricts the scan to a single canonical domain id.
	ByDomain string `json:"by_domain,omitempty"`
}

var dateOnlyRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (srv *Server) handleRecentObservations(req Request) Response {
	var p recentObservationsParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "recent_observations: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "recent_observations: role required")
	}
	if p.Since != "" && !dateOnlyRE.MatchString(p.Since) {
		return errorResponse(req.ID, CodeInvalidParams,
			fmt.Sprintf("recent_observations: since %q must be YYYY-MM-DD", p.Since))
	}
	since := p.Since
	if since == "" {
		since = time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02")
	}

	var targets []store.ObsTarget
	if srv.controller != nil {
		if p.ByDomain != "" {
			d, err := srv.controller.Get(p.ByDomain)
			if err != nil {
				return errorResponse(req.ID, CodeInvalidParams, "recent_observations: "+err.Error())
			}
			path, err := srv.controller.ResolveFile(d.ID, "observations")
			if err != nil {
				return errorResponse(req.ID, CodeInvalidParams, "recent_observations: "+err.Error())
			}
			targets = []store.ObsTarget{{Domain: d.ID, Path: path}}
		} else {
			ctrlTargets := srv.controller.Observations()
			targets = make([]store.ObsTarget, 0, len(ctrlTargets))
			for _, t := range ctrlTargets {
				targets = append(targets, store.ObsTarget{Domain: t.Domain, Path: t.Path})
			}
		}
	}

	// RBAC pre-filter on the target list — never read a file the role can't.
	allowed := make([]store.ObsTarget, 0, len(targets))
	for _, t := range targets {
		if srv.rbac.Check(p.Role, t.Path, "read") {
			allowed = append(allowed, t)
		}
	}

	result, err := srv.store.RecentObservations(allowed, since, p.ByTag, "")
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "recent_observations: "+err.Error())
	}
	return okResponse(req.ID, result)
}

// --- cluster_check ---

type clusterCheckParams struct {
	baseParams
	Domain         string `json:"domain,omitempty"`
	MinClusterSize int    `json:"min_cluster_size,omitempty"`
	Since          string `json:"since,omitempty"` // RFC3339 date, Go duration, or "Nd"
	SpanDays       int    `json:"span_days,omitempty"`
	SampleLimit    int    `json:"sample_limit,omitempty"`
}

func (srv *Server) handleClusterCheck(req Request) Response {
	var p clusterCheckParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "cluster_check: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "cluster_check: role required")
	}

	var targets []store.ClusterObsTarget
	if srv.controller != nil {
		var ctrlTargets []domain.ActionTarget
		if p.Domain != "" {
			d, err := srv.controller.Get(p.Domain)
			if err != nil {
				return errorResponse(req.ID, CodeInvalidParams, "cluster_check: "+err.Error())
			}
			path, err := srv.controller.ResolveFile(d.ID, "observations")
			if err != nil {
				return errorResponse(req.ID, CodeInvalidParams, "cluster_check: "+err.Error())
			}
			ctrlTargets = []domain.ActionTarget{{Domain: d.ID, Path: path}}
		} else {
			ctrlTargets = srv.controller.Observations()
		}
		targets = make([]store.ClusterObsTarget, 0, len(ctrlTargets))
		for _, t := range ctrlTargets {
			// RBAC per-path on the source observations file.
			if !srv.rbac.Check(p.Role, t.Path, "read") {
				continue
			}
			targets = append(targets, store.ClusterObsTarget{Domain: t.Domain, Path: t.Path})
		}
	}

	since, err := parseSince(p.Since, time.Now().UTC())
	if err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "cluster_check: "+err.Error())
	}

	result, err := srv.store.Cluster(targets, store.ClusterParams{
		MinClusterSize: p.MinClusterSize,
		Since:          since,
		SpanDays:       p.SpanDays,
		SampleLimit:    p.SampleLimit,
	})
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "cluster_check: "+err.Error())
	}
	return okResponse(req.ID, result)
}

// parseSince accepts an RFC3339 date/datetime, a Go duration string (e.g.
// "168h"), or a shorthand like "7d" / "30d". Empty string means "use store
// default" (zero time triggers the store's 7d default).
func parseSince(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	if strings.HasSuffix(raw, "d") {
		var n int
		if _, err := fmt.Sscanf(raw, "%dd", &n); err == nil && n > 0 {
			return now.AddDate(0, 0, -n), nil
		}
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return now.Add(-d), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid since %q (want RFC3339 date, duration, or Nd)", raw)
}

// --- scenario_check ---

type scenarioCheckParams struct {
	baseParams
}

// handleScenarioCheck returns the schedule of active scenario files in
// cog-meta/scenarios/. Assumption-verification stays with the LLM — this
// just answers "which scenarios are due, overdue, or still active, and by
// how many days?" RBAC is enforced per-file on read against cog-meta/scenarios/.
func (srv *Server) handleScenarioCheck(req Request) Response {
	var p scenarioCheckParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "scenario_check: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "scenario_check: role required")
	}

	entries, err := srv.store.ScenarioCheck(time.Now().UTC())
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "scenario_check: "+err.Error())
	}
	filtered := make([]store.ScenarioEntry, 0, len(entries))
	for _, e := range entries {
		if srv.rbac.Check(p.Role, e.Path, "read") {
			filtered = append(filtered, e)
		}
	}
	return okResponse(req.ID, map[string]interface{}{
		"scenarios": filtered,
	})
}

// --- domains.list / domains.get ---

type domainsListParams struct {
	baseParams
}

func (srv *Server) handleDomainsList(req Request) Response {
	var p domainsListParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "domains.list: invalid params: "+err.Error())
		}
	}
	if srv.controller == nil {
		return errorResponse(req.ID, CodeStoreError, "domains.list: controller unavailable")
	}
	all := srv.controller.List()
	// Filter by RBAC: a role only sees domains whose path it can read.
	visible := make([]domain.Domain, 0, len(all))
	for _, d := range all {
		if srv.rbac.Check(p.Role, d.Path, "read") {
			visible = append(visible, d)
		}
	}
	return okResponse(req.ID, map[string]interface{}{
		"domains": visible,
	})
}

type domainsGetParams struct {
	baseParams
	ID string `json:"id"`
}

func (srv *Server) handleDomainsGet(req Request) Response {
	var p domainsGetParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "domains.get: invalid params: "+err.Error())
		}
	}
	if p.ID == "" {
		return errorResponse(req.ID, CodeInvalidParams, "domains.get: id is required")
	}
	if srv.controller == nil {
		return errorResponse(req.ID, CodeStoreError, "domains.get: controller unavailable")
	}
	d, err := srv.controller.Get(p.ID)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "domains.get: "+err.Error())
	}
	if !srv.rbac.Check(p.Role, d.Path, "read") {
		return errorResponse(req.ID, CodeRBACDenied,
			fmt.Sprintf("domains.get denied for role %q on %q", p.Role, d.Path))
	}
	return okResponse(req.ID, map[string]interface{}{
		"domain": d,
	})
}

// --- glacier_index_compute ---

type glacierIndexParams struct {
	baseParams
}

func (srv *Server) handleGlacierIndexCompute(req Request) Response {
	var p glacierIndexParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "glacier_index_compute: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "glacier_index_compute: role required")
	}
	all, err := srv.store.GlacierIndex()
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "glacier_index_compute: "+err.Error())
	}
	filtered := make([]store.GlacierEntry, 0, len(all))
	for _, e := range all {
		if srv.rbac.Check(p.Role, e.Path, "read") {
			filtered = append(filtered, e)
		}
	}
	return okResponse(req.ID, map[string]interface{}{
		"entries": filtered,
		"count":   len(filtered),
	})
}

// --- domain_summary ---

type domainSummaryParams struct {
	baseParams
	Domain string `json:"domain"`
	Since  string `json:"since,omitempty"`
}

// DomainSummaryResult is the typed envelope returned by domain_summary.
// Field shape mirrors RPC-CONSOLIDATION.md §5.
type DomainSummaryResult struct {
	Domain                    string                   `json:"domain"`
	Label                     string                   `json:"label"`
	HotMemory                 string                   `json:"hot_memory"`
	OpenActionCount           int                      `json:"open_action_count"`
	CompletedActionCountSince int                      `json:"completed_action_count_since"`
	RecentObservations        []store.ObservationEntry `json:"recent_observations"`
	FilesPresent              []string                 `json:"files_present"`
	LastActivity              string                   `json:"last_activity"`
	Since                     string                   `json:"since"`
}

func (srv *Server) handleDomainSummary(req Request) Response {
	var p domainSummaryParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "domain_summary: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "domain_summary: role required")
	}
	if p.Domain == "" {
		return errorResponse(req.ID, CodeInvalidParams, "domain_summary: domain required")
	}
	if srv.controller == nil {
		return errorResponse(req.ID, CodeStoreError, "domain_summary: controller unavailable")
	}
	d, err := srv.controller.Get(p.Domain)
	if err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "domain_summary: "+err.Error())
	}
	// Per-domain RBAC gate: the domain's declared path. A role without
	// read access here gets CodeRBACDenied for the whole call.
	if !srv.rbac.Check(p.Role, d.Path, "read") {
		return errorResponse(req.ID, CodeRBACDenied,
			fmt.Sprintf("domain_summary denied for role %q on %q", p.Role, d.Path))
	}

	_, sinceDate, err := resolveSince(p.Since)
	if err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "domain_summary: "+err.Error())
	}

	result := DomainSummaryResult{
		Domain:             d.ID,
		Label:              d.Label,
		RecentObservations: []store.ObservationEntry{},
		FilesPresent:       []string{},
		Since:              sinceDate,
	}

	var lastActivity time.Time

	for _, file := range d.Files {
		rel, rerr := srv.controller.ResolveFile(d.ID, file)
		if rerr != nil {
			continue
		}
		// Per-file RBAC: a role allowed at the domain root can still be
		// denied on a specific file (e.g. cog-meta/self-observations.md).
		// Skip silently — caller already got the domain-level allow.
		if !srv.rbac.Check(p.Role, rel, "read") {
			continue
		}
		exists, _ := srv.store.FileExists(rel)
		if !exists {
			continue
		}
		result.FilesPresent = append(result.FilesPresent, file)

		if mt, _ := srv.store.FileModTime(rel); mt.After(lastActivity) {
			lastActivity = mt
		}

		switch file {
		case "hot-memory":
			if content, rerr := srv.store.Read(rel, "", 0, 0); rerr == nil {
				result.HotMemory = content
			}
		case "action-items":
			if open, completed, cerr := srv.store.CountActions(rel, sinceDate); cerr == nil {
				result.OpenActionCount = open
				result.CompletedActionCountSince = completed
			}
		case "observations":
			if obs, oerr := srv.store.RecentObservationsForFile(rel, sinceDate); oerr == nil {
				result.RecentObservations = obs
				// Bias last_activity off the newest observation date too —
				// mtime can lag if a file was touched without content edits.
				for _, o := range obs {
					if t, terr := time.Parse("2006-01-02", o.Date); terr == nil && t.After(lastActivity) {
						lastActivity = t
					}
				}
			}
		}
	}
	if !lastActivity.IsZero() {
		result.LastActivity = lastActivity.UTC().Format("2006-01-02")
	}
	return okResponse(req.ID, result)
}

// resolveSince parses the `since` param into a cutoff time + YYYY-MM-DD
// floor. Accepted forms: "" (→ 7d ago), YYYY-MM-DD, RFC3339, Go duration
// (with "Nd" rewritten to N*24h).
func resolveSince(s string) (time.Time, string, error) {
	now := time.Now().UTC()
	if s == "" {
		t := now.Add(-7 * 24 * time.Hour)
		return t, t.Format("2006-01-02"), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, t.Format("2006-01-02"), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, t.UTC().Format("2006-01-02"), nil
	}
	parseSpec := s
	if strings.HasSuffix(s, "d") {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil && days > 0 {
			parseSpec = fmt.Sprintf("%dh", days*24)
		}
	}
	if d, err := time.ParseDuration(parseSpec); err == nil {
		t := now.Add(-d)
		return t, t.Format("2006-01-02"), nil
	}
	return time.Time{}, "", fmt.Errorf("unrecognized `since` value %q (want YYYY-MM-DD, RFC3339, or duration like \"7d\"/\"168h\")", s)
}

// --- entity_audit ---

type entityAuditParams struct {
	baseParams
	// Domain, when set, restricts the audit to that single domain id.
	Domain string `json:"domain,omitempty"`
}

func (srv *Server) handleEntityAudit(req Request) Response {
	var p entityAuditParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "entity_audit: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "entity_audit: role required")
	}
	if srv.controller == nil {
		return errorResponse(req.ID, CodeStoreError, "entity_audit: controller unavailable")
	}

	var ctrlTargets []domain.ActionTarget
	if p.Domain != "" {
		d, err := srv.controller.Get(p.Domain)
		if err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "entity_audit: "+err.Error())
		}
		path, err := srv.controller.ResolveFile(d.ID, "entities")
		if err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "entity_audit: "+err.Error())
		}
		ctrlTargets = []domain.ActionTarget{{Domain: d.ID, Path: path}}
	} else {
		ctrlTargets = srv.controller.Entities()
	}

	// RBAC filter targets up front: a role only audits files it can read.
	targets := make([]store.ActionTarget, 0, len(ctrlTargets))
	for _, t := range ctrlTargets {
		if !srv.rbac.Check(p.Role, t.Path, "read") {
			continue
		}
		targets = append(targets, store.ActionTarget{Domain: t.Domain, Path: t.Path})
	}

	res, err := srv.store.EntityAudit(targets, time.Now().UTC())
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "entity_audit: "+err.Error())
	}
	return okResponse(req.ID, res)
}

// --- link_index_compute ---

type linkIndexComputeParams struct {
	baseParams
}

func (srv *Server) handleLinkIndexCompute(req Request) Response {
	var p linkIndexComputeParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "link_index_compute: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "link_index_compute: role required")
	}
	canRead := func(path string) bool { return srv.rbac.Check(p.Role, path, "read") }
	links, err := srv.store.LinkIndexFiltered(canRead)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "link_index_compute: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"links": links,
	})
}

// --- link_audit ---

type linkAuditParams struct {
	baseParams
}

func (srv *Server) handleLinkAudit(req Request) Response {
	var p linkAuditParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "link_audit: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "link_audit: role required")
	}
	canRead := func(path string) bool { return srv.rbac.Check(p.Role, path, "read") }
	candidates, err := srv.store.LinkAudit(canRead)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "link_audit: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"candidates": candidates,
	})
}

// --- health ---

func (srv *Server) handleHealth(req Request) Response {
	return okResponse(req.ID, map[string]interface{}{
		"ok": true,
	})
}

// --- git ---

type gitParams struct {
	baseParams
	Op      string   `json:"op"`
	Ref     string   `json:"ref,omitempty"`
	Message string   `json:"message,omitempty"`
	Paths   []string `json:"paths,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

func (srv *Server) handleGit(req Request) Response {
	var p gitParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "git: invalid params: "+err.Error())
	}
	if p.Op == "" {
		return errorResponse(req.ID, CodeInvalidParams, "git: op is required")
	}
	if p.Op == "commit" && !srv.rbac.Check(p.Role, "**", "write") {
		return errorResponse(req.ID, CodeRBACDenied, fmt.Sprintf("git commit denied for role %q", p.Role))
	}
	output, err := srv.store.Git(p.Op, p.Ref, p.Message, p.Paths, p.Limit)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "git: "+err.Error())
	}
	return okResponse(req.ID, map[string]interface{}{
		"output": output,
	})
}

// warnIfMalformed emits a log warning when a write/append targets a path that
// lives under a declared domain but isn't in that domain's `files` list.
// Pure hygiene signal — never blocks the operation.
func (srv *Server) warnIfMalformed(op, path string) {
	if srv.controller == nil {
		return
	}
	if err := srv.controller.ValidateWrite(path); err != nil {
		log.Printf("cogmemory: %s warning: %v", op, err)
	}
}
