// Package tcad holds repository files that the programs embed: the agent
// skill that TCAD Studio copies into every folder it opens, and the studio's
// UI.
package tcad

import (
	"embed"
	"io/fs"
)

// Skill is .agents/skills/tcad: SKILL.md, reference.md and patterns.md, in the
// Agent Skills format that Codex, Gemini CLI, Antigravity and Claude Code
// read (.claude/skills/tcad links to it for Claude Code).
//
//go:embed .agents/skills/tcad/*.md
var Skill embed.FS

// SkillDir is the folder of the skill inside Skill.
const SkillDir = ".agents/skills/tcad"

// frontend is TCAD Studio's UI, built from frontend/ (make frontend). The
// build is committed, so the programs build without Node.
//
//go:embed all:frontend/dist
var frontend embed.FS

// UI returns the built UI (index.html at its root).
func UI() fs.FS {
	ui, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		panic(err)
	}
	return ui
}
