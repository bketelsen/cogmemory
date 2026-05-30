package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeEntities(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEntityAuditEmpty(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.EntityAudit(nil, time.Now())
	if err != nil {
		t.Fatalf("EntityAudit: %v", err)
	}
	if res.FormatViolations == nil || res.GlacierCandidates == nil ||
		res.MissingMetadata == nil || res.TemporalViolations == nil {
		t.Fatal("all slices must be non-nil")
	}
	if len(res.FormatViolations)+len(res.GlacierCandidates)+
		len(res.MissingMetadata)+len(res.TemporalViolations) != 0 {
		t.Fatalf("expected all-empty, got %+v", res)
	}
}

func TestEntityAuditMissingFileSkipped(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	targets := []ActionTarget{{Domain: "work", Path: "work/entities.md"}}
	res, err := s.EntityAudit(targets, time.Now())
	if err != nil {
		t.Fatalf("EntityAudit: %v", err)
	}
	if len(res.FormatViolations) != 0 || len(res.MissingMetadata) != 0 {
		t.Fatalf("missing file should be silent; got %+v", res)
	}
}

func TestEntityAuditCompactBlockClean(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	body := `# Work — Entities

### Microsoft (employer)
Role: Principal Engineering Manager
status: active | last: 2026-05-27
`
	writeEntities(t, root, "work/entities.md", body)
	res, err := s.EntityAudit(
		[]ActionTarget{{Domain: "work", Path: "work/entities.md"}},
		time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FormatViolations) != 0 {
		t.Fatalf("clean 3-line block should not violate format: %+v", res.FormatViolations)
	}
	if len(res.MissingMetadata) != 0 {
		t.Fatalf("status+last present, should not flag: %+v", res.MissingMetadata)
	}
	if len(res.GlacierCandidates) != 0 {
		t.Fatalf("recent active should not be glacier: %+v", res.GlacierCandidates)
	}
}

func TestEntityAuditFormatViolation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	body := `### Kyle Gospodnetich (direct report)
Software Engineer II, Microsoft | GitHub: KyleGospo
Founded Bazzite
Founding member OGC
SCaLE speaker
status: active | last: 2026-05-27 | → [[wiki:pages/people/kyle]]
`
	writeEntities(t, root, "work/entities.md", body)
	res, err := s.EntityAudit(
		[]ActionTarget{{Domain: "work", Path: "work/entities.md"}},
		time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.FormatViolations) != 1 {
		t.Fatalf("want 1 format violation, got %d (%+v)", len(res.FormatViolations), res.FormatViolations)
	}
	v := res.FormatViolations[0]
	if v.Name != "Kyle Gospodnetich" {
		t.Errorf("name: want 'Kyle Gospodnetich', got %q", v.Name)
	}
	if !v.HasDetailFile {
		t.Errorf("wiki link should set has_detail_file")
	}
	if v.Lines <= 3 {
		t.Errorf("lines should be >3, got %d", v.Lines)
	}
	if v.Issue != "exceeds_3_line_compact" {
		t.Errorf("issue: %q", v.Issue)
	}
}

func TestEntityAuditMissingMetadata(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, _ := New(root)
	body := `### Acme Corp
Some prose without status or last marker.
`
	writeEntities(t, root, "work/entities.md", body)
	res, err := s.EntityAudit(
		[]ActionTarget{{Domain: "work", Path: "work/entities.md"}},
		time.Now(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.MissingMetadata) != 1 {
		t.Fatalf("want 1 missing-metadata, got %+v", res.MissingMetadata)
	}
	m := res.MissingMetadata[0]
	if len(m.Missing) != 2 {
		t.Errorf("want both status+last missing, got %v", m.Missing)
	}
}

func TestEntityAuditGlacierByInactive(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, _ := New(root)
	body := `### Old Project
status: inactive | last: 2026-05-01
`
	writeEntities(t, root, "work/entities.md", body)
	res, err := s.EntityAudit(
		[]ActionTarget{{Domain: "work", Path: "work/entities.md"}},
		time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.GlacierCandidates) != 1 {
		t.Fatalf("inactive should flag, got %+v", res.GlacierCandidates)
	}
	g := res.GlacierCandidates[0]
	if g.Status != "inactive" || g.Name != "Old Project" {
		t.Errorf("wrong candidate: %+v", g)
	}
}

func TestEntityAuditGlacierByAge(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, _ := New(root)
	body := `### Forgotten Colleague
Used to work on the data team.
status: active | last: 2025-10-01
`
	writeEntities(t, root, "work/entities.md", body)
	res, err := s.EntityAudit(
		[]ActionTarget{{Domain: "work", Path: "work/entities.md"}},
		time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.GlacierCandidates) != 1 {
		t.Fatalf("aged entry should flag, got %+v", res.GlacierCandidates)
	}
	g := res.GlacierCandidates[0]
	if g.AgeDays <= 180 {
		t.Errorf("age should be >180, got %d", g.AgeDays)
	}
}

func TestEntityAuditTemporalViolation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, _ := New(root)
	body := `### Dana Lead
Role: (until 2026-04) VP of platform — now at competitor
status: active | last: 2026-05-01

### Eli Recent
Role: (until 2026-06) interim PM
status: active | last: 2026-05-15

### Frank Struck
Role: ~~(until 2026-04)~~ retired
status: active | last: 2026-05-20
`
	writeEntities(t, root, "work/entities.md", body)
	res, err := s.EntityAudit(
		[]ActionTarget{{Domain: "work", Path: "work/entities.md"}},
		time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TemporalViolations) != 1 {
		t.Fatalf("want 1 temporal violation (Dana only), got %+v", res.TemporalViolations)
	}
	v := res.TemporalViolations[0]
	if v.Name != "Dana Lead" {
		t.Errorf("wrong entity flagged: %q", v.Name)
	}
	if v.Needs != "strikethrough" {
		t.Errorf("needs: %q", v.Needs)
	}
}

func TestEntityAuditMultipleFilesSorted(t *testing.T) {
	root := filepath.Join(t.TempDir(), "mem")
	s, _ := New(root)
	writeEntities(t, root, "personal/entities.md", "### A\nstatus: active | last: 2026-05-01\n")
	writeEntities(t, root, "work/entities.md", "### B\nstatus: inactive | last: 2026-05-01\n")
	targets := []ActionTarget{
		{Domain: "work", Path: "work/entities.md"},
		{Domain: "personal", Path: "personal/entities.md"},
	}
	res, err := s.EntityAudit(targets, time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.GlacierCandidates) != 1 || res.GlacierCandidates[0].Name != "B" {
		t.Fatalf("wrong glacier set: %+v", res.GlacierCandidates)
	}
}
