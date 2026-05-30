package rpc

import (
	"encoding/json"
	"fmt"
	"log"

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
