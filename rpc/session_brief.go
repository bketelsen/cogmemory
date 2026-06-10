package rpc

import (
	"encoding/json"

	"github.com/bketelsen/cogmemory/domain"
	"github.com/bketelsen/cogmemory/store"
)

// session_brief consolidates the "read hot-memory.md + patterns.md +
// domains.yml + open-action counts" convention every consumer does at session
// start. See docs/RPC-CONSOLIDATION.md §1.
//
// Envelope:
//
//	{
//	  "hot_memory":  "<content of hot-memory.md>",
//	  "patterns":    "<content of cog-meta/patterns.md>",
//	  "domains":     [{id,path,label,triggers}, ...],     // RBAC-filtered
//	  "action_counts": {
//	    "<domain-id>":         <open-count>,              // RBAC-filtered
//	    "_pri_high_anywhere": <bool>
//	  },
//	  "controller_last_error": null | "<msg>"
//	}
//
// hot_memory and patterns are owner-canonical and always returned. domains
// and action_counts are filtered to the role's readable subset. Explicitly
// out of scope: per-domain hot-memory, recent observations, briefing-bridge
// body — those belong to reflect/foresight domain-summary RPCs.

type sessionBriefParams struct {
	baseParams
}

type sessionBriefDomain struct {
	ID       string   `json:"id"`
	Path     string   `json:"path"`
	Label    string   `json:"label,omitempty"`
	Triggers []string `json:"triggers,omitempty"`
}

func (srv *Server) handleSessionBrief(req Request) Response {
	var p sessionBriefParams
	if req.Params != nil {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return errorResponse(req.ID, CodeInvalidParams, "session_brief: invalid params: "+err.Error())
		}
	}
	if p.Role == "" {
		return errorResponse(req.ID, CodeInvalidParams, "session_brief: role required")
	}

	var (
		targets           []store.ActionTarget
		visibleDomains    = []sessionBriefDomain{}
		controllerLastErr interface{}
	)
	if srv.controller != nil {
		if e := srv.controller.LastError(); e != nil {
			controllerLastErr = e.Error()
		}
		for _, d := range srv.controller.List() {
			collectDomain(d, p.Role, srv, &visibleDomains)
		}
		ctrlTargets := srv.controller.ActionItems()
		targets = make([]store.ActionTarget, 0, len(ctrlTargets))
		for _, t := range ctrlTargets {
			targets = append(targets, store.ActionTarget{Domain: t.Domain, Path: t.Path})
		}
	}

	brief, err := srv.store.SessionBrief(targets)
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "session_brief: "+err.Error())
	}

	actionCounts := map[string]interface{}{}
	priHigh := false
	for _, c := range brief.DomainActionCounts {
		if !srv.rbac.Check(p.Role, c.Path, "read") {
			continue
		}
		actionCounts[c.Domain] = c.OpenCount
		if c.PriHighCount > 0 {
			priHigh = true
		}
	}
	actionCounts["_pri_high_anywhere"] = priHigh

	return okResponse(req.ID, map[string]interface{}{
		"hot_memory":            brief.HotMemory,
		"patterns":              brief.Patterns,
		"domains":               visibleDomains,
		"action_counts":         actionCounts,
		"controller_last_error": controllerLastErr,
	})
}

// collectDomain appends d (and its subdomains) to out when readable for role.
func collectDomain(d domain.Domain, role string, srv *Server, out *[]sessionBriefDomain) {
	if srv.rbac.Check(role, d.Path, "read") {
		*out = append(*out, sessionBriefDomain{
			ID: d.ID, Path: d.Path, Label: d.Label, Triggers: d.Triggers,
		})
	}
	for _, sd := range d.Subdomains {
		collectDomain(sd, role, srv, out)
	}
}
