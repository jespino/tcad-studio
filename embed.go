// Package tcad holds repository files that the programs embed: the agent
// skill that TCAD Studio copies into every folder it opens.
package tcad

import "embed"

// Skill is .agents/skills/tcad: SKILL.md, reference.md and patterns.md, in the
// Agent Skills format that Codex, Gemini CLI, Antigravity and Claude Code
// read (.claude/skills/tcad links to it for Claude Code).
//
//go:embed .agents/skills/tcad/*.md
var Skill embed.FS

// SkillDir is the folder of the skill inside Skill.
const SkillDir = ".agents/skills/tcad"
