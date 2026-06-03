package skills

import (
	"strings"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
)

// BuildSkillNameCounts counts how often each skill name appears, both exactly
// and case-insensitively (ASCII-lowercased), excluding skills whose SKILL.md
// path is in disabledPaths. It mirrors the Rust `build_skill_name_counts` and is
// used to disambiguate explicit `$SkillName` mentions.
//
// The first returned map is keyed by exact name; the second by ASCII-lowercased
// name.
func BuildSkillNameCounts(skills []SkillMetadata, disabledPaths map[abspath.AbsolutePathBuf]struct{}) (map[string]int, map[string]int) {
	exact := make(map[string]int)
	lower := make(map[string]int)
	for _, skill := range skills {
		if disabledPaths != nil {
			if _, ok := disabledPaths[skill.PathToSkillsMd]; ok {
				continue
			}
		}
		exact[skill.Name]++
		lower[asciiLower(skill.Name)]++
	}
	return exact, lower
}

// asciiLower lowercases only ASCII letters, mirroring Rust's
// `str::to_ascii_lowercase`.
func asciiLower(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, s)
}
