package domain_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bketelsen/cogmemory/domain"
)

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "domains.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodManifest = `version: 1
domains:
  - id: personal
    path: personal
    label: Personal
    triggers: [personal, home]
    files: [hot-memory, action-items, observations, entities]
  - id: work
    path: work/microsoft
    label: MSFT
    files: [hot-memory, action-items]
    subdomains:
      - id: work-sub
        path: work/microsoft/team
        files: [observations]
  - id: cog-meta
    path: cog-meta
    files: [patterns, improvements]
`

func TestControllerLoadAndList(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, goodManifest)
	c, err := domain.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ds := c.List()
	if len(ds) != 3 {
		t.Fatalf("List len = %d, want 3", len(ds))
	}
	if ds[1].ID != "work" || len(ds[1].Subdomains) != 1 {
		t.Fatalf("subdomain missing: %+v", ds[1])
	}
}

func TestControllerGetIncludesSubdomains(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, goodManifest)
	c, _ := domain.New(dir)
	d, err := c.Get("work-sub")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if d.Path != "work/microsoft/team" {
		t.Fatalf("got %+v", d)
	}
	if _, err := c.Get("nope"); err == nil {
		t.Fatal("Get(unknown) returned nil error")
	}
}

func TestControllerActionItemsResolves(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, goodManifest)
	c, _ := domain.New(dir)
	targets := c.ActionItems()
	// personal + work declare action-items. work-sub and cog-meta don't.
	var got []string
	for _, t := range targets {
		got = append(got, t.Domain+":"+t.Path)
	}
	want := []string{
		"personal:personal/action-items.md",
		"work:work/microsoft/action-items.md",
	}
	if len(got) != len(want) {
		t.Fatalf("ActionItems = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ActionItems = %v, want %v", got, want)
		}
	}
}

func TestControllerResolveFile(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, goodManifest)
	c, _ := domain.New(dir)
	p, err := c.ResolveFile("personal", "action-items")
	if err != nil || p != "personal/action-items.md" {
		t.Fatalf("ResolveFile = %q, %v", p, err)
	}
	if _, err := c.ResolveFile("personal", "nope"); err == nil {
		t.Fatal("ResolveFile undeclared file: want error")
	}
}

func TestControllerValidateWriteWarnsUnknown(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, goodManifest)
	c, _ := domain.New(dir)
	// Declared file under declared domain → no error.
	if err := c.ValidateWrite("personal/hot-memory.md"); err != nil {
		t.Fatalf("ValidateWrite declared: %v", err)
	}
	// Undeclared file under declared domain → error (hygiene signal).
	err := c.ValidateWrite("personal/random.md")
	if err == nil {
		t.Fatal("ValidateWrite undeclared: want error")
	}
	if !strings.Contains(err.Error(), `domain "personal"`) {
		t.Fatalf("error doesn't name domain: %v", err)
	}
	// Path not under any domain → silently ok (out of scope).
	if err := c.ValidateWrite("scratch/foo.md"); err != nil {
		t.Fatalf("ValidateWrite out-of-scope: %v", err)
	}
}

func TestControllerHotReloadOnMtimeChange(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, goodManifest)
	c, _ := domain.New(dir)
	if len(c.List()) != 3 {
		t.Fatal("initial load wrong")
	}
	// Rewrite manifest with a different set of domains.
	time.Sleep(15 * time.Millisecond)
	newBody := `version: 1
domains:
  - id: solo
    path: solo
    files: [hot-memory]
`
	writeManifest(t, dir, newBody)
	// Bump mtime explicitly to defeat low-resolution filesystems.
	now := time.Now()
	_ = os.Chtimes(filepath.Join(dir, "domains.yml"), now, now)
	got := c.List()
	if len(got) != 1 || got[0].ID != "solo" {
		t.Fatalf("hot reload did not pick up new manifest: %+v", got)
	}
}

func TestControllerMalformedYAMLRejected(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "domains: [: not yaml\n")
	if _, err := domain.New(dir); err == nil {
		t.Fatal("New(malformed): want error")
	}
}

func TestControllerInvalidSchemaRejected(t *testing.T) {
	cases := map[string]string{
		"duplicate-id": "domains:\n  - {id: a, path: a}\n  - {id: a, path: b}\n",
		"empty-id":     "domains:\n  - {id: '', path: a}\n",
		"empty-path":   "domains:\n  - {id: a, path: ''}\n",
		"absolute":     "domains:\n  - {id: a, path: /etc}\n",
		"dotdot":       "domains:\n  - {id: a, path: ../escape}\n",
		"bad-file":     "domains:\n  - {id: a, path: a, files: ['hot-memory.md']}\n",
		"slash-file":   "domains:\n  - {id: a, path: a, files: ['sub/file']}\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, body)
			if _, err := domain.New(dir); err == nil {
				t.Fatalf("%s: expected error", name)
			}
		})
	}
}

func TestControllerMissingManifestEmpty(t *testing.T) {
	dir := t.TempDir()
	c, err := domain.New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(c.List()) != 0 {
		t.Fatalf("expected empty registry, got %v", c.List())
	}
	if c.LastError() != nil {
		t.Fatalf("LastError = %v, want nil", c.LastError())
	}
}
