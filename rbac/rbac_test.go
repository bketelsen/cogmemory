package rbac_test

import (
	"testing"

	"github.com/bketelsen/cogmemory/config"
	"github.com/bketelsen/cogmemory/rbac"
)

func buildEngine() *rbac.Engine {
	cfg := config.RBACConfig{
		Roles: map[string][]config.Rule{
			"siona": {
				{Pattern: "**", Read: true, Write: true},
			},
			"researcher": {
				{Pattern: "cog-meta/**", Read: true, Write: false},
				{Pattern: "glacier/**", Read: true, Write: false},
				{Pattern: "**", Read: true, Write: false},
			},
			"architect": {
				{Pattern: "projects/**", Read: true, Write: true},
				{Pattern: "cog-meta/**", Read: true, Write: false},
				{Pattern: "**", Read: true, Write: false},
			},
			"coder": {
				{Pattern: "projects/**", Read: true, Write: true},
				{Pattern: "cog-meta/self-observations.md", Read: false, Write: false},
				{Pattern: "**", Read: true, Write: false},
			},
		},
	}
	return rbac.New(cfg)
}

func TestCheckSionaAllAccess(t *testing.T) {
	e := buildEngine()
	paths := []string{
		"hot-memory.md",
		"projects/siona/dev-log.md",
		"cog-meta/patterns.md",
		"glacier/personal/observations-insight.md",
	}
	for _, p := range paths {
		if !e.Check("siona", p, "read") {
			t.Errorf("siona should have read on %q", p)
		}
		if !e.Check("siona", p, "write") {
			t.Errorf("siona should have write on %q", p)
		}
	}
}

func TestCheckResearcherReadOnly(t *testing.T) {
	e := buildEngine()

	// researcher can read cog-meta
	if !e.Check("researcher", "cog-meta/patterns.md", "read") {
		t.Error("researcher should be able to read cog-meta/patterns.md")
	}
	// researcher cannot write anything
	if e.Check("researcher", "cog-meta/patterns.md", "write") {
		t.Error("researcher should not be able to write cog-meta/patterns.md")
	}
	if e.Check("researcher", "hot-memory.md", "write") {
		t.Error("researcher should not be able to write hot-memory.md")
	}
}

func TestCheckCoderProjectsWrite(t *testing.T) {
	e := buildEngine()

	if !e.Check("coder", "projects/siona/dev-log.md", "write") {
		t.Error("coder should be able to write projects/**")
	}
	if !e.Check("coder", "projects/siona/dev-log.md", "read") {
		t.Error("coder should be able to read projects/**")
	}
}

func TestCheckCoderCogMetaDenied(t *testing.T) {
	e := buildEngine()

	// cog-meta/self-observations.md is explicitly blocked for coder
	if e.Check("coder", "cog-meta/self-observations.md", "read") {
		t.Error("coder should not be able to read cog-meta/self-observations.md")
	}
	if e.Check("coder", "cog-meta/self-observations.md", "write") {
		t.Error("coder should not be able to write cog-meta/self-observations.md")
	}
}

func TestCheckFirstMatchWins(t *testing.T) {
	e := buildEngine()

	// For architect: projects/** (write: true) should win over ** (write: false)
	if !e.Check("architect", "projects/siona/hot-memory.md", "write") {
		t.Error("architect specific projects/** rule should win over ** catch-all")
	}
	// cog-meta/** (write: false) should win over ** (write: false) — same result but
	// ensures ordering is correct
	if e.Check("architect", "cog-meta/patterns.md", "write") {
		t.Error("architect should not be able to write cog-meta/**")
	}
}

func TestCheckUnknownRole(t *testing.T) {
	e := buildEngine()

	if e.Check("unknown-role", "anything.md", "read") {
		t.Error("unknown role should be denied read")
	}
	if e.Check("unknown-role", "anything.md", "write") {
		t.Error("unknown role should be denied write")
	}
}

func TestCheckEmptyRoles(t *testing.T) {
	e := rbac.New(config.RBACConfig{Roles: map[string][]config.Rule{}})
	if e.Check("siona", "anything.md", "read") {
		t.Error("empty RBAC config should deny all")
	}
}

func TestCheckDefaultDeny(t *testing.T) {
	// A role with rules that don't match the given path
	cfg := config.RBACConfig{
		Roles: map[string][]config.Rule{
			"limited": {
				{Pattern: "specific/file.md", Read: true, Write: true},
			},
		},
	}
	e := rbac.New(cfg)
	if e.Check("limited", "other/file.md", "read") {
		t.Error("no matching rule should default to deny")
	}
}

func TestCheckDoublestarGlob(t *testing.T) {
	cfg := config.RBACConfig{
		Roles: map[string][]config.Rule{
			"test": {
				{Pattern: "projects/**", Read: true, Write: true},
			},
		},
	}
	e := rbac.New(cfg)

	paths := []string{
		"projects/siona/dev-log.md",
		"projects/siona/sub/deep/file.md",
		"projects/other/notes.md",
	}
	for _, p := range paths {
		if !e.Check("test", p, "read") {
			t.Errorf("projects/** should match %q", p)
		}
	}
	if e.Check("test", "other/file.md", "read") {
		t.Error("projects/** should not match other/file.md")
	}
}
