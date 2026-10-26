package agentctx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	skill := fstest.MapFS{
		"s/SKILL.md":     {Data: []byte("skill")},
		"s/reference.md": {Data: []byte("ref")},
	}
	// The user's own notes in AGENTS.md and CLAUDE.md are kept.
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My notes\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("mine\n"), 0o644)
	for range 2 { // writing twice must not duplicate the block
		if err := Write(dir, skill, "s"); err != nil {
			t.Fatal(err)
		}
	}
	agents, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.HasPrefix(string(agents), "# My notes\n") || strings.Count(string(agents), beginMarker) != 1 ||
		!strings.Contains(string(agents), ".tcad/preview.json") {
		t.Fatalf("AGENTS.md:\n%s", agents)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "CLAUDE.md")); string(b) != "mine\n" {
		t.Fatalf("CLAUDE.md overwritten: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "GEMINI.md")); string(b) != "@AGENTS.md\n" {
		t.Fatalf("GEMINI.md: %q", b)
	}
	for _, root := range []string{".agents", ".claude"} {
		if b, _ := os.ReadFile(filepath.Join(dir, root, "skills", "tcad", "reference.md")); string(b) != "ref" {
			t.Fatalf("skill not copied to %s: %q", root, b)
		}
	}
}
