// Package agentctx writes the context files that coding agents (Claude Code,
// Codex, Gemini CLI...) read when they start in a folder, so an agent
// launched from TCAD Studio knows the TCAD language, how the studio shows its
// work and what the user is looking at. The writing is agentstudio's
// agentctx.
package agentctx

import (
	_ "embed"
	"io/fs"
	"path"

	shared "github.com/jespino/agentstudio/agentctx"
)

//go:embed templates/AGENTS.md
var agentsTemplate []byte

// Context is what Write writes, with the skill read from skillDir in skill.
func Context(skill fs.FS, skillDir string) (shared.Context, error) {
	files, err := shared.SkillFS(skill, path.Join(skillDir, "*.md"))
	if err != nil {
		return shared.Context{}, err
	}
	return shared.Context{ID: "tcad", Product: "TCAD Studio", Agents: agentsTemplate, SkillName: "tcad", Skill: files}, nil
}

// Write creates or refreshes the context files in dir:
//
//	AGENTS.md                managed block (whatever the user writes outside it is kept)
//	CLAUDE.md, GEMINI.md     "@AGENTS.md", only if missing
//	.agents/skills/tcad/     the skill, copied from skill (a folder of .md files),
//	                         where Codex, Gemini CLI and Antigravity look for it
//	.claude/skills/tcad/     the same skill again, for Claude Code
func Write(dir string, skill fs.FS, skillDir string) error {
	c, err := Context(skill, skillDir)
	if err != nil {
		return err
	}
	return shared.Write(dir, c)
}
