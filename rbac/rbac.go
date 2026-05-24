// Package rbac implements role-based access control for memory file operations.
package rbac

import (
	"github.com/bketelsen/cogmemory/config"
	"github.com/bmatcuk/doublestar/v4"
)

// Engine evaluates RBAC rules for memory file access.
type Engine struct {
	cfg config.RBACConfig
}

// New creates an RBAC engine from the provided configuration.
func New(cfg config.RBACConfig) *Engine {
	return &Engine{cfg: cfg}
}

// Check returns true if the given role is allowed to perform op ("read" or "write")
// on relPath. Rules are evaluated in order; the first matching rule wins.
// Unknown roles and paths with no matching rule default to deny.
func (e *Engine) Check(role, relPath, op string) bool {
	rules, ok := e.cfg.Roles[role]
	if !ok {
		return false
	}
	for _, rule := range rules {
		matched, err := doublestar.Match(rule.Pattern, relPath)
		if err != nil || !matched {
			continue
		}
		// First match wins
		switch op {
		case "read":
			return rule.Read
		case "write":
			return rule.Write
		}
		return false
	}
	// No rule matched — deny by default
	return false
}
