// Package tcad holds repository files that the programs embed: the Claude
// skill that TCAD Studio copies into every folder it opens.
package tcad

import "embed"

// Skill is .claude/skills/tcad: SKILL.md, reference.md and patterns.md.
//
//go:embed .claude/skills/tcad/*.md
var Skill embed.FS

// SkillDir is the folder of the skill inside Skill.
const SkillDir = ".claude/skills/tcad"
