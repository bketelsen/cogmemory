package rpc

import (
	"encoding/json"
	"fmt"
)

// baseParams contains the common role field present in all requests.
type baseParams struct {
	Role string `json:"role"`
}

// --- read ---

type readParams struct {
	baseParams
	Path string `json:"path"`
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
	content, err := srv.store.Read(p.Path)
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
	Path string `json:"path"`
	Text string `json:"text"`
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
	if err := srv.store.Append(p.Path, p.Text); err != nil {
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
}

func (srv *Server) handleStats(req Request) Response {
	var p statsParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &p) //nolint:errcheck
	}
	// Stats is available to any authenticated role
	stats, err := srv.store.Stats()
	if err != nil {
		return errorResponse(req.ID, CodeStoreError, "stats: "+err.Error())
	}
	return okResponse(req.ID, stats)
}

// --- l0index ---

type l0indexParams struct {
	baseParams
}

func (srv *Server) handleL0Index(req Request) Response {
	var p l0indexParams
	if req.Params != nil {
		json.Unmarshal(req.Params, &p) //nolint:errcheck
	}
	index, err := srv.store.L0Index()
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

// --- health ---

func (srv *Server) handleHealth(req Request) Response {
	return okResponse(req.ID, map[string]interface{}{
		"ok": true,
	})
}
