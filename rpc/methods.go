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
