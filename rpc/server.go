// Package rpc implements a JSON-RPC 2.0 server over Unix Domain Sockets.
package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/bketelsen/cogmemory/domain"
	"github.com/bketelsen/cogmemory/rbac"
	"github.com/bketelsen/cogmemory/store"
)

// JSON-RPC 2.0 error codes
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeRBACDenied     = -32000
	CodeStoreError     = -32001
)

// Request is a JSON-RPC 2.0 request object.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response is a JSON-RPC 2.0 response object.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

// Server is a JSON-RPC 2.0 server backed by a MemoryStore, RBAC engine,
// and the domain Controller (canonical registry of memory domains).
type Server struct {
	store      *store.MemoryStore
	rbac       *rbac.Engine
	controller *domain.Controller
	wg         sync.WaitGroup
}

// New creates a new RPC server. controller may be nil — in that case the
// domain.* methods return errors and open_actions runs with an empty target
// list (i.e. produces no items). Production callers should always pass one.
func New(s *store.MemoryStore, r *rbac.Engine, c *domain.Controller) *Server {
	return &Server{store: s, rbac: r, controller: c}
}

// Listen creates a Unix Domain Socket listener, removing any stale socket file.
func Listen(socketPath string) (net.Listener, error) {
	// Remove stale socket file if it exists
	if _, err := os.Stat(socketPath); err == nil {
		if err := os.Remove(socketPath); err != nil {
			return nil, fmt.Errorf("rpc: remove stale socket: %w", err)
		}
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("rpc: listen on %q: %w", socketPath, err)
	}
	return ln, nil
}

// Serve accepts connections from the listener and handles each in a goroutine.
// Returns when the listener is closed.
func (srv *Server) Serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener was closed — normal shutdown
			return
		}
		srv.wg.Add(1)
		go func(c net.Conn) {
			defer srv.wg.Done()
			srv.handleConn(c)
		}(conn)
	}
}

// Wait blocks until all in-flight connections are done.
func (srv *Server) Wait() {
	srv.wg.Wait()
}

func (srv *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		line := scanner.Bytes()
		resp := srv.dispatch(line)
		out, _ := json.Marshal(resp)
		out = append(out, '\n')
		conn.Write(out) //nolint:errcheck
	}
}

func (srv *Server) dispatch(line []byte) Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return errorResponse(nil, CodeParseError, "parse error: "+err.Error())
	}
	if req.JSONRPC == "" {
		req.JSONRPC = "2.0"
	}

	switch req.Method {
	case "read":
		return srv.handleRead(req)
	case "write":
		return srv.handleWrite(req)
	case "append":
		return srv.handleAppend(req)
	case "patch":
		return srv.handlePatch(req)
	case "outline":
		return srv.handleOutline(req)
	case "move":
		return srv.handleMove(req)
	case "search":
		return srv.handleSearch(req)
	case "stats":
		return srv.handleStats(req)
	case "l0index":
		return srv.handleL0Index(req)
	case "list":
		return srv.handleList(req)
	case "open_actions":
		return srv.handleOpenActions(req)
	case "session_brief":
		return srv.handleSessionBrief(req)
	case "housekeeping_scan":
		return srv.handleHousekeepingScan(req)
	case "domains.list":
		return srv.handleDomainsList(req)
	case "domains.get":
		return srv.handleDomainsGet(req)
	case "glacier_index_compute":
		return srv.handleGlacierIndexCompute(req)
	case "health":
		return srv.handleHealth(req)
	case "git":
		return srv.handleGit(req)
	default:
		return errorResponse(req.ID, CodeMethodNotFound, fmt.Sprintf("method not found: %q", req.Method))
	}
}

func errorResponse(id interface{}, code int, msg string) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
}

func okResponse(id interface{}, result interface{}) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
}
