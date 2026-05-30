// Package domain provides the canonical domain registry for cogmemory.
//
// A "domain" is a logical grouping of memory files (e.g. "personal",
// "work/microsoft", "cog-meta") declared in domains.yml at the memory root.
// The controller is the single authoritative source the daemon consults when
// it needs to answer cross-domain questions ("which files belong to which
// domain?", "where do action-items.md live?", "is this write under a
// well-formed path for its domain?").
//
// RBAC remains in its own package — domain controller is schema, not policy.
package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Domain describes one logical domain declared in domains.yml.
type Domain struct {
	ID         string   `json:"id"          yaml:"id"`
	Path       string   `json:"path"        yaml:"path"`
	Label      string   `json:"label"       yaml:"label,omitempty"`
	Type       string   `json:"type,omitempty" yaml:"type,omitempty"`
	Triggers   []string `json:"triggers,omitempty" yaml:"triggers,omitempty"`
	Files      []string `json:"files,omitempty"    yaml:"files,omitempty"`
	Subdomains []Domain `json:"subdomains,omitempty" yaml:"subdomains,omitempty"`
}

// ActionTarget is a resolved action-items file: the relative file path the
// store should read, plus the canonical domain id to attribute its items to.
type ActionTarget struct {
	Domain string
	Path   string // POSIX-style relative path, e.g. "work/microsoft/action-items.md"
}

// manifest is the raw yaml shape: top-level `domains: [...]`.
type manifest struct {
	Version int      `yaml:"version,omitempty"`
	Domains []Domain `yaml:"domains"`
}

// Controller loads, validates, and serves the domain registry. It re-reads
// the manifest from disk when its mtime changes (check-on-call), so external
// edits to domains.yml take effect without a daemon restart.
type Controller struct {
	root         string // memory root
	manifestPath string // <root>/domains.yml

	mu      sync.RWMutex
	domains []Domain
	flat    map[string]*Domain // id -> domain pointer (includes subdomains)
	modTime time.Time
	lastErr error
}

// New constructs a Controller rooted at the given absolute memory root. It
// performs an initial load; a missing domains.yml is treated as "no domains"
// (empty registry) — only malformed yaml or schema errors return an error.
func New(memoryRoot string) (*Controller, error) {
	if !filepath.IsAbs(memoryRoot) {
		return nil, fmt.Errorf("domain: memoryRoot must be absolute, got %q", memoryRoot)
	}
	c := &Controller{
		root:         memoryRoot,
		manifestPath: filepath.Join(memoryRoot, "domains.yml"),
	}
	if err := c.reload(); err != nil {
		return nil, err
	}
	return c, nil
}

// reload reads + validates the manifest. Holds the write lock.
func (c *Controller) reload() error {
	data, err := os.ReadFile(c.manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.domains = nil
			c.flat = map[string]*Domain{}
			c.modTime = time.Time{}
			c.lastErr = nil
			return nil
		}
		return fmt.Errorf("domain: read %q: %w", c.manifestPath, err)
	}

	var m manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("domain: parse %q: %w", c.manifestPath, err)
	}
	if err := validate(m.Domains, ""); err != nil {
		return fmt.Errorf("domain: validate %q: %w", c.manifestPath, err)
	}

	info, statErr := os.Stat(c.manifestPath)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.domains = m.Domains
	c.flat = flatten(m.Domains)
	if statErr == nil {
		c.modTime = info.ModTime()
	}
	c.lastErr = nil
	return nil
}

// maybeReload checks the manifest mtime and re-reads if it changed. Errors
// during reload are cached on c.lastErr but do not invalidate the previously
// loaded registry — the daemon keeps serving the last-good snapshot.
func (c *Controller) maybeReload() {
	info, err := os.Stat(c.manifestPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return
		}
		c.mu.RLock()
		emptyAlready := len(c.domains) == 0 && c.modTime.IsZero()
		c.mu.RUnlock()
		if emptyAlready {
			return
		}
	}
	c.mu.RLock()
	cur := c.modTime
	c.mu.RUnlock()
	if err == nil && info.ModTime().Equal(cur) {
		return
	}
	if reloadErr := c.reload(); reloadErr != nil {
		c.mu.Lock()
		c.lastErr = reloadErr
		c.mu.Unlock()
	}
}

// List returns a copy of the top-level domains (subdomains nested as
// declared). Caller may mutate without affecting controller state.
func (c *Controller) List() []Domain {
	c.maybeReload()
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Domain, len(c.domains))
	copy(out, c.domains)
	return out
}

// Get returns the domain with the given id (matches top-level and
// subdomains). Returns an error if not found.
func (c *Controller) Get(id string) (Domain, error) {
	c.maybeReload()
	c.mu.RLock()
	defer c.mu.RUnlock()
	if d, ok := c.flat[id]; ok {
		return *d, nil
	}
	return Domain{}, fmt.Errorf("domain: unknown id %q", id)
}

// LastError returns the last error encountered by hot-reload (e.g. invalid
// yaml staged on disk while the controller continues serving the prior
// snapshot). nil when the live manifest validates clean.
func (c *Controller) LastError() error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastErr
}

// ActionItems returns one ActionTarget per domain (or subdomain) that declares
// "action-items" in its files list. The resolved path is POSIX-joined; the
// store decides whether the file actually exists on disk.
func (c *Controller) ActionItems() []ActionTarget {
	c.maybeReload()
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []ActionTarget
	walk(c.domains, func(d Domain) {
		if !declaresFile(d, "action-items") {
			return
		}
		out = append(out, ActionTarget{
			Domain: d.ID,
			Path:   joinPosix(d.Path, "action-items.md"),
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// ResolveFile returns the relative path on disk for a given domain id +
// declared file base name (e.g. "action-items" → "work/microsoft/action-items.md").
// Returns an error if the domain is unknown or doesn't declare the file.
func (c *Controller) ResolveFile(id, file string) (string, error) {
	d, err := c.Get(id)
	if err != nil {
		return "", err
	}
	if !declaresFile(d, file) {
		return "", fmt.Errorf("domain %q does not declare file %q", id, file)
	}
	return joinPosix(d.Path, file+".md"), nil
}

// DomainForPath returns the (id, file-basename, ok) for a write path. It
// matches the deepest declared domain that owns a prefix of the path. The
// file-basename is the declared name (without ".md"). If no domain owns the
// path, ok is false.
//
// Used by the write-validation hook to decide whether a write is "well-formed
// for its domain". Returning ok=false on a clean root-level write (e.g.
// "hot-memory.md" with no matching root-scoped domain) is intentional — the
// hook treats unknown paths as out-of-scope rather than malformed.
func (c *Controller) DomainForPath(relPath string) (string, string, bool) {
	c.maybeReload()
	c.mu.RLock()
	defer c.mu.RUnlock()

	relPath = filepath.ToSlash(filepath.Clean(relPath))
	var bestID, bestFile string
	bestPrefixLen := -1
	walk(c.domains, func(d Domain) {
		dpath := filepath.ToSlash(filepath.Clean(d.Path))
		if dpath == "." || dpath == "" {
			// root-anchored domain: any top-level file under root counts
			if !strings.Contains(relPath, "/") {
				if len(dpath) > bestPrefixLen {
					base := strings.TrimSuffix(relPath, ".md")
					bestID, bestFile, bestPrefixLen = d.ID, base, len(dpath)
				}
			}
			return
		}
		if relPath == dpath+"/"+filepath.Base(relPath) ||
			strings.HasPrefix(relPath, dpath+"/") {
			rest := strings.TrimPrefix(relPath, dpath+"/")
			// Only direct children count as "domain files"; deeper paths are
			// out-of-scope for the hygiene check.
			if strings.Contains(rest, "/") {
				return
			}
			if len(dpath) > bestPrefixLen {
				bestID = d.ID
				bestFile = strings.TrimSuffix(rest, ".md")
				bestPrefixLen = len(dpath)
			}
		}
	})
	if bestPrefixLen < 0 {
		return "", "", false
	}
	return bestID, bestFile, true
}

// ValidateWrite returns nil when the write is well-formed for its declaring
// domain, or a descriptive error when the file basename isn't in the
// domain's declared files list. Writes that don't fall under any declared
// domain return nil (out-of-scope, not malformed).
func (c *Controller) ValidateWrite(relPath string) error {
	id, file, ok := c.DomainForPath(relPath)
	if !ok {
		return nil
	}
	d, err := c.Get(id)
	if err != nil {
		return nil
	}
	if declaresFile(d, file) {
		return nil
	}
	return fmt.Errorf("write to %q is under domain %q but %q is not in its declared files %v",
		relPath, id, file, d.Files)
}

// --- helpers ---

func declaresFile(d Domain, file string) bool {
	for _, f := range d.Files {
		if f == file {
			return true
		}
	}
	return false
}

func joinPosix(parts ...string) string {
	var nonempty []string
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		nonempty = append(nonempty, strings.Trim(filepath.ToSlash(p), "/"))
	}
	return strings.Join(nonempty, "/")
}

func walk(ds []Domain, fn func(Domain)) {
	for _, d := range ds {
		fn(d)
		if len(d.Subdomains) > 0 {
			walk(d.Subdomains, fn)
		}
	}
}

func flatten(ds []Domain) map[string]*Domain {
	out := map[string]*Domain{}
	var rec func([]Domain)
	rec = func(in []Domain) {
		for i := range in {
			out[in[i].ID] = &in[i]
			if len(in[i].Subdomains) > 0 {
				rec(in[i].Subdomains)
			}
		}
	}
	rec(ds)
	return out
}

// validate enforces schema invariants:
//   - non-empty id and path
//   - ids globally unique (including subdomains)
//   - paths must not start with "/" or contain ".."
//   - file basenames must not contain "/" or end with ".md" (declared bare)
func validate(ds []Domain, parentPath string) error {
	seen := map[string]bool{}
	var rec func([]Domain) error
	rec = func(in []Domain) error {
		for _, d := range in {
			if d.ID == "" {
				return fmt.Errorf("domain has empty id")
			}
			if seen[d.ID] {
				return fmt.Errorf("duplicate domain id %q", d.ID)
			}
			seen[d.ID] = true
			if d.Path == "" {
				return fmt.Errorf("domain %q: empty path", d.ID)
			}
			if strings.HasPrefix(d.Path, "/") {
				return fmt.Errorf("domain %q: path must be relative, got %q", d.ID, d.Path)
			}
			if strings.Contains(d.Path, "..") {
				return fmt.Errorf("domain %q: path may not contain '..'", d.ID)
			}
			for _, f := range d.Files {
				if f == "" || strings.ContainsAny(f, "/\\") {
					return fmt.Errorf("domain %q: invalid file basename %q", d.ID, f)
				}
				if strings.HasSuffix(f, ".md") {
					return fmt.Errorf("domain %q: file %q should be declared without .md suffix", d.ID, f)
				}
			}
			if len(d.Subdomains) > 0 {
				if err := rec(d.Subdomains); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return rec(ds)
}
