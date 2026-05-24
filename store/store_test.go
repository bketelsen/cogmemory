package store_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/bketelsen/cogmemory/store"
)

func newStore(t *testing.T) *store.MemoryStore {
	t.Helper()
	dir := t.TempDir()
	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	abs := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func storeDirOf(s *store.MemoryStore) string {
	// We need the root for helper funcs; expose it through a read+write cycle
	_ = s
	return ""
}

// --- Read ---

func TestRead(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "hello world\n")

	content, err := s.Read("hot-memory.md")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "hello world\n" {
		t.Errorf("Read = %q, want %q", content, "hello world\n")
	}
}

func TestReadMissing(t *testing.T) {
	s := newStore(t)
	content, err := s.Read("nonexistent.md")
	if err != nil {
		t.Fatalf("Read missing file should not error, got: %v", err)
	}
	if content != "" {
		t.Errorf("Read missing = %q, want empty string", content)
	}
}

func TestReadL0INDEX(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "<!-- L0: Current state overview -->\n# Hot Memory\n")
	writeFile(t, dir, "sub/other.md", "no l0 here\n")

	result, err := s.Read("L0_INDEX")
	if err != nil {
		t.Fatalf("Read L0_INDEX: %v", err)
	}
	if !strings.Contains(result, "Current state overview") {
		t.Errorf("L0_INDEX should contain L0 summary, got: %q", result)
	}
}

func TestReadLIST(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "a.md", "a")
	writeFile(t, dir, "sub/b.md", "b")

	result, err := s.Read("LIST")
	if err != nil {
		t.Fatalf("Read LIST: %v", err)
	}
	if !strings.Contains(result, "a.md") {
		t.Errorf("LIST should contain a.md, got: %q", result)
	}
	if !strings.Contains(result, "sub/b.md") {
		t.Errorf("LIST should contain sub/b.md, got: %q", result)
	}
}

// --- Write ---

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)

	if err := s.Write("notes.md", "new content\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.md"))
	if string(data) != "new content\n" {
		t.Errorf("got %q, want %q", data, "new content\n")
	}
}

func TestWriteOverwrite(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "notes.md", "old\n")

	if err := s.Write("notes.md", "new\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "notes.md"))
	if string(data) != "new\n" {
		t.Errorf("got %q, want %q", data, "new\n")
	}
}

func TestWriteCreatesSubdir(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)

	if err := s.Write("projects/siona/notes.md", "content\n"); err != nil {
		t.Fatalf("Write nested: %v", err)
	}
	abs := filepath.Join(dir, "projects", "siona", "notes.md")
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content\n" {
		t.Errorf("got %q", data)
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)

	const goroutines = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			content := fmt.Sprintf("writer %d\n", n)
			if err := s.Write("concurrent.md", content); err != nil {
				t.Errorf("Write goroutine %d: %v", n, err)
			}
		}(i)
	}
	wg.Wait()

	// File should contain exactly one writer's content (no corruption)
	data, err := os.ReadFile(filepath.Join(dir, "concurrent.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "writer ") {
		t.Errorf("file content looks corrupted: %q", data)
	}
}

// --- Append ---

func TestAppend(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "log.md", "line1\n")

	if err := s.Append("log.md", "line2\n"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "log.md"))
	if string(data) != "line1\nline2\n" {
		t.Errorf("got %q", data)
	}
}

func TestAppendCreatesFile(t *testing.T) {
	s := newStore(t)
	if err := s.Append("new.md", "hello\n"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, _ := s.Read("new.md")
	if content != "hello\n" {
		t.Errorf("got %q", content)
	}
}

func TestAppendAddsSeparatorWhenExistingLacksTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "noeol.md", "line1")

	if err := s.Append("noeol.md", "line2"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, _ := s.Read("noeol.md")
	if content != "line1\nline2\n" {
		t.Errorf("got %q", content)
	}
}

func TestAppendDoesNotDoubleInsertSeparatorWhenTextStartsWithNewline(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "already.md", "line1")

	if err := s.Append("already.md", "\nline2"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, _ := s.Read("already.md")
	if content != "line1\nline2\n" {
		t.Errorf("got %q", content)
	}
}

func TestAppendAddsTrailingNewlineWhenMissing(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "trail.md", "line1\n")

	if err := s.Append("trail.md", "line2"); err != nil {
		t.Fatalf("Append: %v", err)
	}
	content, _ := s.Read("trail.md")
	if content != "line1\nline2\n" {
		t.Errorf("got %q", content)
	}
}

func TestAppendObsEnforcement(t *testing.T) {
	s := newStore(t)

	valid := "- 2025-01-01 [insight]: valid observation\n"
	if err := s.Append("domain/observations.md", valid); err != nil {
		t.Fatalf("valid obs should not error: %v", err)
	}

	invalid := "this is not an observation\n"
	err := s.Append("domain/observations.md", invalid)
	if err == nil {
		t.Fatal("invalid obs should return error")
	}
	if !strings.Contains(err.Error(), "observation format") {
		t.Errorf("error should mention observation format, got: %v", err)
	}
}

func TestAppendObsEnforcementBarePathName(t *testing.T) {
	s := newStore(t)
	valid := "- 2025-06-15 [work]: bare path test\n"
	if err := s.Append("observations.md", valid); err != nil {
		t.Fatalf("valid obs at bare path: %v", err)
	}
}

func TestAppendNonObsPathNotValidated(t *testing.T) {
	s := newStore(t)
	if err := s.Append("notes.md", "anything goes here\n"); err != nil {
		t.Fatalf("non-obs path should not validate: %v", err)
	}
}

// --- Patch ---

func TestPatch(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "doc.md", "hello world\n")

	if err := s.Patch("doc.md", "hello", "goodbye"); err != nil {
		t.Fatalf("Patch: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "doc.md"))
	if string(data) != "goodbye world\n" {
		t.Errorf("got %q", data)
	}
}

func TestPatchNotFound(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "doc.md", "hello world\n")

	err := s.Patch("doc.md", "xyz", "abc")
	if err == nil {
		t.Fatal("expected error for not-found oldText")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say not found, got: %v", err)
	}
}

func TestPatchAmbiguous(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "doc.md", "hello hello\n")

	err := s.Patch("doc.md", "hello", "hi")
	if err == nil {
		t.Fatal("expected error for ambiguous match")
	}
	if !strings.Contains(err.Error(), "2 times") {
		t.Errorf("error should mention count, got: %v", err)
	}
}

// --- Search ---

func TestSearch(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "notes.md", "hello world\nfoo bar\n")
	writeFile(t, dir, "other.md", "no match here\nHELLO again\n")

	results, err := s.Search("hello")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("expected at least 2 matches, got %d", len(results))
	}
	for _, r := range results {
		if !strings.Contains(strings.ToLower(r.Text), "hello") {
			t.Errorf("result doesn't contain hello: %+v", r)
		}
	}
}

func TestSearchCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "f.md", "UPPER lower MiXeD\n")

	results, err := s.Search("upper")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("case-insensitive search should find UPPER when querying upper")
	}
}

// --- Path traversal ---

func TestPathTraversal(t *testing.T) {
	s := newStore(t)

	cases := []string{
		"../../../etc/passwd",
		"../secret",
		"sub/../../etc/passwd",
	}
	for _, c := range cases {
		_, err := s.Read(c)
		if err == nil {
			t.Errorf("expected traversal error for %q", c)
		}
	}
}

func TestPathTraversalWrite(t *testing.T) {
	s := newStore(t)
	err := s.Write("../evil.md", "bad")
	if err == nil {
		t.Error("expected traversal error on Write")
	}
}

// --- Stats ---

func TestStats(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "a.md", "line1\nline2\n")
	writeFile(t, dir, "b.md", "only one line")

	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Files != 2 {
		t.Errorf("Files = %d, want 2", stats.Files)
	}
	if stats.Lines < 3 {
		t.Errorf("Lines = %d, want at least 3", stats.Lines)
	}
	if stats.Size <= 0 {
		t.Error("Size should be > 0")
	}
}

// --- List ---

func TestList(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "a.md", "a")
	writeFile(t, dir, "sub/b.md", "b")

	paths, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	set := make(map[string]bool)
	for _, p := range paths {
		set[p] = true
	}
	if !set["a.md"] {
		t.Error("List should contain a.md")
	}
	if !set["sub/b.md"] {
		t.Error("List should contain sub/b.md")
	}
}

// --- L0Index ---

func TestL0Index(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "<!-- L0: Current overview -->\n# Hot Memory\n")
	writeFile(t, dir, "no-l0.md", "# No L0 header\n")

	result, err := s.L0Index()
	if err != nil {
		t.Fatalf("L0Index: %v", err)
	}
	if !strings.Contains(result, "Current overview") {
		t.Errorf("L0Index should contain summary, got: %q", result)
	}
	if strings.Contains(result, "no-l0.md") {
		t.Errorf("L0Index should not list files without L0 header")
	}
}

// --- Constructor ---

func TestNewRelativePathRejected(t *testing.T) {
	_, err := store.New("relative/path")
	if err == nil {
		t.Fatal("expected error for relative root")
	}
}
