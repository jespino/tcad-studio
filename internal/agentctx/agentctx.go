// Package agentctx writes the context files that coding-agent harnesses
// (Claude Code, Codex, Gemini CLI...) read when they start in a folder, so an
// agent launched from TCAD Studio knows the TCAD language, how the studio
// shows its work and what the user is looking at.
package agentctx

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

//go:embed templates/*
var templates embed.FS

const (
	beginMarker = "<!-- tcad:begin (lo gestiona TCAD Studio; lo que escribas dentro de este bloque se sobrescribe) -->"
	endMarker   = "<!-- tcad:end -->"
)

// Write creates or refreshes the context files in dir:
//
//	AGENTS.md                bloque gestionado (lo que el usuario escriba fuera se conserva)
//	CLAUDE.md, GEMINI.md     "@AGENTS.md", solo si no existen
//	.claude/skills/tcad/     the skill, copied from skill (a folder of .md files)
func Write(dir string, skill fs.FS, skillDir string) error {
	agents, err := templates.ReadFile("templates/AGENTS.md")
	if err != nil {
		return err
	}
	if err := writeManaged(filepath.Join(dir, "AGENTS.md"), agents); err != nil {
		return err
	}
	for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); os.IsNotExist(err) {
			if err := os.WriteFile(p, []byte("@AGENTS.md\n"), 0o644); err != nil {
				return err
			}
		}
	}
	files, err := fs.Glob(skill, path.Join(skillDir, "*.md"))
	if err != nil {
		return err
	}
	for _, f := range files {
		content, err := fs.ReadFile(skill, f)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, ".claude", "skills", "tcad", path.Base(f))
		if err := writeIfChanged(dst, content); err != nil {
			return err
		}
	}
	return nil
}

func writeIfChanged(p string, content []byte) error {
	if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, content, 0o644)
}

// writeManaged replaces the TCAD block of a file, keeping everything the
// user wrote outside it.
func writeManaged(p string, block []byte) error {
	managed := beginMarker + "\n" + strings.TrimRight(string(block), "\n") + "\n" + endMarker + "\n"
	old, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return os.WriteFile(p, []byte(managed), 0o644)
	}
	if err != nil {
		return err
	}
	s := string(old)
	i := strings.Index(s, beginMarker)
	j := strings.Index(s, endMarker)
	var out string
	if i >= 0 && j > i {
		out = s[:i] + managed + strings.TrimPrefix(s[j+len(endMarker):], "\n")
	} else {
		out = strings.TrimRight(s, "\n") + "\n\n" + managed
	}
	if out == s {
		return nil
	}
	return os.WriteFile(p, []byte(out), 0o644)
}
