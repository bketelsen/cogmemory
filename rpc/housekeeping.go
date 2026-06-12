package rpc

import (
	"github.com/bketelsen/cogmemory/domain"
	"github.com/bketelsen/cogmemory/store"
)

// Conventional file locations the housekeeping_scan RPC consults. Hard-coded
// (not parameterized over the wire) because cog-prime's housekeeping skill
// hardcodes them — making the daemon configurable per call would only let
// callers drift from the documented contract.
const (
	housekeepingRootHotMemory = "hot-memory.md"
	housekeepingPatternsPath  = "cog-meta/patterns.md"
	housekeepingImprovements  = "cog-meta/improvements.md"
	housekeepingMarkerPath    = "cog-meta/.housekeeping-marker"
)

type housekeepingScanParams struct {
	baseParams
}

// handleHousekeepingScan collapses housekeeping's §0+§1+§2 orientation into
// one call. Builds the target list from the domain controller, RBAC-filters
// per path (a role only sees thresholds on files it can read), and returns
// the envelope documented in docs/RPC-CONSOLIDATION.md §2.
func (srv *Server) handleHousekeepingScan(req Request) Response {
	var p housekeepingScanParams
	if err := decodeParams(req.Params, &p); err != nil {
		return errorResponse(req.ID, CodeInvalidParams, "housekeeping_scan: invalid params: "+err.Error())
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "housekeeping_scan: role required")
	}

	in := store.HousekeepingInput{
		Caps:             store.DefaultHousekeepingCaps(),
		MarkerPath:       housekeepingMarkerPath,
		RootHotMemory:    housekeepingRootHotMemory,
		PatternsPath:     housekeepingPatternsPath,
		ImprovementsPath: housekeepingImprovements,
	}

	if srv.controller != nil {
		for _, d := range srv.controller.List() {
			collectHousekeepingTargets(srv.controller, d, &in.Targets)
		}
	}

	// RBAC: drop per-path fields the role can't read. Done pre-scan so we
	// don't waste a disk read either; root-level files are also gated.
	in.Targets = srv.filterHousekeepingTargets(p.Role, in.Targets)
	if !srv.rbac.Check(p.Role, in.RootHotMemory, "read") {
		in.RootHotMemory = ""
	}
	if !srv.rbac.Check(p.Role, in.PatternsPath, "read") {
		in.PatternsPath = ""
	}
	if !srv.rbac.Check(p.Role, in.ImprovementsPath, "read") {
		in.ImprovementsPath = ""
	}

	result, err := srv.store.HousekeepingScan(in)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "housekeeping_scan: "+err.Error())
	}

	// changed_recently is built from a full tree walk in the store layer.
	// Apply RBAC there too — leaking paths via this metadata channel would
	// defeat the per-path gate on the threshold arrays.
	result.ChangedRecently = filterReadablePaths(srv, p.Role, result.ChangedRecently)
	return okResponse(req.ID, result)
}

// collectHousekeepingTargets walks a domain (and its subdomains) and emits
// one target per domain that declares at least one of the canonical files.
func collectHousekeepingTargets(c *domain.Controller, d domain.Domain, out *[]store.HousekeepingTarget) {
	t := store.HousekeepingTarget{DomainID: d.ID}
	if path, err := c.ResolveFile(d.ID, "observations"); err == nil {
		t.ObservationsPath = path
	}
	if path, err := c.ResolveFile(d.ID, "action-items"); err == nil {
		t.ActionItemsPath = path
	}
	if path, err := c.ResolveFile(d.ID, "hot-memory"); err == nil {
		t.HotMemoryPath = path
	}
	if t.ObservationsPath != "" || t.ActionItemsPath != "" || t.HotMemoryPath != "" {
		*out = append(*out, t)
	}
	for _, sub := range d.Subdomains {
		collectHousekeepingTargets(c, sub, out)
	}
}

// filterHousekeepingTargets blanks paths the role cannot read. A target
// reduced to all-empty paths is dropped entirely.
func (srv *Server) filterHousekeepingTargets(role string, targets []store.HousekeepingTarget) []store.HousekeepingTarget {
	out := make([]store.HousekeepingTarget, 0, len(targets))
	for _, t := range targets {
		if t.ObservationsPath != "" && !srv.rbac.Check(role, t.ObservationsPath, "read") {
			t.ObservationsPath = ""
		}
		if t.ActionItemsPath != "" && !srv.rbac.Check(role, t.ActionItemsPath, "read") {
			t.ActionItemsPath = ""
		}
		if t.HotMemoryPath != "" && !srv.rbac.Check(role, t.HotMemoryPath, "read") {
			t.HotMemoryPath = ""
		}
		if t.ObservationsPath == "" && t.ActionItemsPath == "" && t.HotMemoryPath == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func filterReadablePaths(srv *Server, role string, paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if srv.rbac.Check(role, p, "read") {
			out = append(out, p)
		}
	}
	return out
}
