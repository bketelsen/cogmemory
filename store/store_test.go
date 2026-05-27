package store_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

func newGitStore(t *testing.T) *store.MemoryStore {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, strings.TrimSpace(string(out)))
	}
	s, err := store.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// --- Read ---

func TestRead(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "hello world\n")

	content, err := s.Read("hot-memory.md", "", 0, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if content != "hello world\n" {
		t.Errorf("Read = %q, want %q", content, "hello world\n")
	}
}

func TestReadMissing(t *testing.T) {
	s := newStore(t)
	content, err := s.Read("nonexistent.md", "", 0, 0)
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

	result, err := s.Read("L0_INDEX", "", 0, 0)
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

	result, err := s.Read("LIST", "", 0, 0)
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

func TestReadFullContentWithNoExtractionParams(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	const want = "# Project\n\n## Active Projects\nalpha\n\n## Done\nomega\n"
	writeFile(t, dir, "projects.md", want)

	got, err := s.Read("projects.md", "", 0, 0)
	if err != nil {
		t.Fatalf("Read full content: %v", err)
	}
	if got != want {
		t.Errorf("Read full content = %q, want %q", got, want)
	}
}

func TestReadSectionByHeading(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "projects.md", "# Projects\n\n## Backlog\nold\n\n## Active Projects\nalpha\nbeta\n\n## Done\nomega\n")

	got, err := s.Read("projects.md", "## Active Projects", 0, 0)
	if err != nil {
		t.Fatalf("Read section: %v", err)
	}
	const want = "## Active Projects\nalpha\nbeta\n"
	if got != want {
		t.Errorf("Read section = %q, want %q", got, want)
	}
}

func TestReadSectionNotFoundReturnsError(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "projects.md", "# Projects\n\n## Active Projects\nalpha\n")

	_, err := s.Read("projects.md", "## Missing", 0, 0)
	if err == nil {
		t.Fatal("expected section not found error")
	}
	if !strings.Contains(err.Error(), "section not found") {
		t.Errorf("error should mention section not found, got: %v", err)
	}
}

func TestReadLineRangeStartAndEnd(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "lines.md", "line1\nline2\nline3\nline4\nline5\n")

	got, err := s.Read("lines.md", "", 2, 4)
	if err != nil {
		t.Fatalf("Read line range: %v", err)
	}
	const want = "line2\nline3\nline4"
	if got != want {
		t.Errorf("Read line range = %q, want %q", got, want)
	}
}

func TestReadLineRangeDefaultStart(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "lines.md", "line1\nline2\nline3\nline4\nline5\n")

	got, err := s.Read("lines.md", "", 0, 3)
	if err != nil {
		t.Fatalf("Read line range default start: %v", err)
	}
	const want = "line1\nline2\nline3"
	if got != want {
		t.Errorf("Read line range default start = %q, want %q", got, want)
	}
}

func TestReadLineRangeDefaultEnd(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "lines.md", "line1\nline2\nline3\nline4\nline5\n")

	got, err := s.Read("lines.md", "", 5, 0)
	if err != nil {
		t.Fatalf("Read line range default end: %v", err)
	}
	const want = "line5"
	if got != want {
		t.Errorf("Read line range default end = %q, want %q", got, want)
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
	content, _ := s.Read("new.md", "", 0, 0)
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
	content, _ := s.Read("noeol.md", "", 0, 0)
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
	content, _ := s.Read("already.md", "", 0, 0)
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
	content, _ := s.Read("trail.md", "", 0, 0)
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

// --- Outline ---

func TestOutlineReturnsMarkdownHeadingsInOrder(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "notes.md", strings.Join([]string{
		"intro",
		"# ignored h1",
		"## Section One",
		"body",
		"### Detail",
		"#### ignored h4",
		"## Section Two",
		"### Final Detail",
	}, "\n"))

	got, err := s.Outline("notes.md")
	if err != nil {
		t.Fatalf("Outline: %v", err)
	}
	want := []store.OutlineEntry{
		{Line: 3, Text: "Section One", Level: 2},
		{Line: 5, Text: "Detail", Level: 3},
		{Line: 7, Text: "Section Two", Level: 2},
		{Line: 8, Text: "Final Detail", Level: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Outline() = %#v, want %#v", got, want)
	}
}

func TestOutlineMissingFileReturnsError(t *testing.T) {
	s := newStore(t)

	_, err := s.Outline("missing.md")
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

// --- Move ---

func TestMoveFile(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "old/path.md", "content\n")

	if err := s.Move("old/path.md", "new/path.md"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old/path.md")); !os.IsNotExist(err) {
		t.Fatalf("old path still exists or stat failed unexpectedly: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "new/path.md"))
	if err != nil {
		t.Fatalf("new path missing: %v", err)
	}
	if string(got) != "content\n" {
		t.Fatalf("new path content = %q, want %q", got, "content\n")
	}
}

func TestMoveExistingDestinationReturnsError(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "source.md", "source\n")
	writeFile(t, dir, "dest.md", "dest\n")

	err := s.Move("source.md", "dest.md")
	if err == nil {
		t.Fatal("expected existing destination error")
	}
}

func TestMovePathTraversalRejected(t *testing.T) {
	s := newStore(t)

	cases := []struct {
		name string
		from string
		to   string
	}{
		{name: "from", from: "../source.md", to: "dest.md"},
		{name: "to", from: "source.md", to: "../dest.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Move(tc.from, tc.to)
			if err == nil {
				t.Fatalf("expected traversal error for from=%q to=%q", tc.from, tc.to)
			}
		})
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
		_, err := s.Read(c, "", 0, 0)
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

	stats, err := s.Stats("")
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
	if len(stats.PerFile) != 2 {
		t.Fatalf("PerFile = %d, want 2", len(stats.PerFile))
	}
	// Sorted by path: a.md, b.md
	if stats.PerFile[0].Path != "a.md" {
		t.Errorf("PerFile[0].Path = %q, want a.md", stats.PerFile[0].Path)
	}
	if stats.PerFile[0].Lines != 2 {
		t.Errorf("PerFile[0].Lines = %d, want 2", stats.PerFile[0].Lines)
	}
	if stats.PerFile[1].Path != "b.md" {
		t.Errorf("PerFile[1].Path = %q, want b.md", stats.PerFile[1].Path)
	}
	if stats.PerFile[1].Lines != 1 {
		t.Errorf("PerFile[1].Lines = %d, want 1", stats.PerFile[1].Lines)
	}
	if stats.PerFile[0].Modified == "" {
		t.Error("PerFile[0].Modified should not be empty")
	}
	// Per-file totals should sum to overall totals.
	var sumLines, sumSize int64
	for _, f := range stats.PerFile {
		sumLines += f.Lines
		sumSize += f.Size
	}
	if sumLines != stats.Lines {
		t.Errorf("sum of PerFile.Lines = %d, want %d", sumLines, stats.Lines)
	}
	if sumSize != stats.Size {
		t.Errorf("sum of PerFile.Size = %d, want %d", sumSize, stats.Size)
	}
}

func TestStatsPrefix(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "projects/a.md", "line1\nline2\n")
	writeFile(t, dir, "projects/sub/b.md", "alpha\nbeta\ngamma\n")
	writeFile(t, dir, "personal/c.md", "ignored\n")
	writeFile(t, dir, "root.md", "ignored too\n")

	stats, err := s.Stats("projects")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Files != 2 {
		t.Errorf("Files = %d, want 2 (projects/* only)", stats.Files)
	}
	if len(stats.PerFile) != 2 {
		t.Fatalf("PerFile = %d, want 2", len(stats.PerFile))
	}
	for _, f := range stats.PerFile {
		if !strings.HasPrefix(f.Path, "projects/") {
			t.Errorf("PerFile.Path = %q, expected projects/ prefix", f.Path)
		}
	}

	// Trailing slash should be tolerated.
	statsSlash, err := s.Stats("projects/")
	if err != nil {
		t.Fatalf("Stats(projects/): %v", err)
	}
	if statsSlash.Files != stats.Files {
		t.Errorf("trailing slash should match: Files = %d, want %d", statsSlash.Files, stats.Files)
	}

	// Non-matching prefix returns zero, with non-nil empty PerFile slice.
	empty, err := s.Stats("nonexistent")
	if err != nil {
		t.Fatalf("Stats(nonexistent): %v", err)
	}
	if empty.Files != 0 || empty.Lines != 0 || empty.Size != 0 {
		t.Errorf("nonexistent prefix should produce zero totals, got %+v", empty)
	}
	if empty.PerFile == nil {
		t.Error("PerFile should be non-nil empty slice for JSON consistency")
	}

	// Exact-file prefix matches just that file.
	one, err := s.Stats("projects/a.md")
	if err != nil {
		t.Fatalf("Stats(projects/a.md): %v", err)
	}
	if one.Files != 1 {
		t.Errorf("exact file prefix: Files = %d, want 1", one.Files)
	}

	// Prefix must not match by partial directory name (projects vs project).
	none, err := s.Stats("project")
	if err != nil {
		t.Fatalf("Stats(project): %v", err)
	}
	if none.Files != 0 {
		t.Errorf("partial-name prefix should not match: Files = %d, want 0", none.Files)
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

func TestFileScansSkipGitDirectory(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, ".git/HEAD", "<!-- L0: Git head -->\nref: refs/heads/main\n")
	writeFile(t, dir, "hot-memory.md", "<!-- L0: Current overview -->\n# Hot Memory\n")

	paths, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, path := range paths {
		if path == filepath.Join(".git", "HEAD") {
			t.Fatalf("List returned git-internal file %q", path)
		}
	}

	index, err := s.L0Index("")
	if err != nil {
		t.Fatalf("L0Index: %v", err)
	}
	if strings.Contains(index, ".git"+string(os.PathSeparator)) || strings.Contains(index, ".git/") {
		t.Fatalf("L0Index returned git-internal path: %q", index)
	}
}

// --- L0Index ---

func TestL0Index(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "<!-- L0: Current overview -->\n# Hot Memory\n")
	writeFile(t, dir, "projects/hot-memory.md", "<!-- L0: Projects overview -->\n# Projects\n")
	writeFile(t, dir, "work/hot-memory.md", "<!-- L0: Work overview -->\n# Work\n")
	writeFile(t, dir, "no-l0.md", "# No L0 header\n")

	result, err := s.L0Index("")
	if err != nil {
		t.Fatalf("L0Index: %v", err)
	}
	if !strings.Contains(result, "Current overview") {
		t.Errorf("L0Index should contain summary, got: %q", result)
	}
	if !strings.Contains(result, "Projects overview") {
		t.Errorf("L0Index should contain projects summary, got: %q", result)
	}
	if !strings.Contains(result, "Work overview") {
		t.Errorf("L0Index should contain work summary, got: %q", result)
	}
	if strings.Contains(result, "no-l0.md") {
		t.Errorf("L0Index should not list files without L0 header")
	}
}

func TestL0IndexFiltersByDomain(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "hot-memory.md", "<!-- L0: Root overview -->\n# Hot Memory\n")
	writeFile(t, dir, "projects/hot-memory.md", "<!-- L0: Projects overview -->\n# Projects\n")
	writeFile(t, dir, "projects/cogmemory/hot-memory.md", "<!-- L0: Cogmemory overview -->\n# Cogmemory\n")
	writeFile(t, dir, "work/hot-memory.md", "<!-- L0: Work overview -->\n# Work\n")

	result, err := s.L0Index("projects")
	if err != nil {
		t.Fatalf("L0Index filtered by projects: %v", err)
	}
	for _, line := range strings.Split(result, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "projects/") {
			t.Fatalf("filtered L0Index returned non-projects line %q in:\n%s", line, result)
		}
	}
	if !strings.Contains(result, "projects/hot-memory.md: Projects overview") {
		t.Errorf("L0Index should contain projects summary, got: %q", result)
	}
	if !strings.Contains(result, "projects/cogmemory/hot-memory.md: Cogmemory overview") {
		t.Errorf("L0Index should contain nested projects summary, got: %q", result)
	}
	if strings.Contains(result, "work/hot-memory.md") {
		t.Errorf("L0Index should not contain work entries, got: %q", result)
	}
}

func TestL0IndexMissingDomainReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, _ := store.New(dir)
	writeFile(t, dir, "projects/hot-memory.md", "<!-- L0: Projects overview -->\n# Projects\n")

	result, err := s.L0Index("nonexistent")
	if err != nil {
		t.Fatalf("L0Index filtered by nonexistent domain: %v", err)
	}
	if result != "" {
		t.Errorf("L0Index missing domain = %q, want empty string", result)
	}
}

// --- Constructor ---

func TestNewRelativePathRejected(t *testing.T) {
	_, err := store.New("relative/path")
	if err == nil {
		t.Fatal("expected error for relative root")
	}
}

// --- Git ---

func TestGitStatusCleanRepo(t *testing.T) {
	s := newGitStore(t)

	output, err := s.Git("status", "", "", nil, 0)
	if err != nil {
		t.Fatalf("Git status: %v", err)
	}
	if output != "" {
		t.Errorf("Git status output = %q, want empty string", output)
	}
}

func TestGitCommitRequiresMessage(t *testing.T) {
	s := newGitStore(t)

	_, err := s.Git("commit", "", "", nil, 0)
	if err == nil {
		t.Fatal("expected error for commit without message")
	}
	if !strings.Contains(err.Error(), "requires message") {
		t.Errorf("error should mention message, got: %v", err)
	}
}

func TestGitUnknownOp(t *testing.T) {
	s := newGitStore(t)

	_, err := s.Git("nope", "", "", nil, 0)
	if err == nil {
		t.Fatal("expected error for unknown git op")
	}
	if !strings.Contains(err.Error(), "unknown op") {
		t.Errorf("error should mention unknown op, got: %v", err)
	}
}
