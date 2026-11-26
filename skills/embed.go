package skillassets

import "embed"

const SkillName = "govmc"
const SkillPath = SkillName

// Skill embeds the entire maintained skill, including nested reference files.
//
//go:embed govmc
var Skill embed.FS
