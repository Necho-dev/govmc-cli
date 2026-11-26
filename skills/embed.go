package skillassets

import "embed"

const SkillName = "esxi-govc"
const SkillPath = SkillName

// Skill embeds the entire maintained skill, including nested reference files.
//
//go:embed esxi-govc
var Skill embed.FS
