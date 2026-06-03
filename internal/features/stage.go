// Package features is a faithful Go port of the codex `codex-features` crate.
//
// It defines the centralized feature-flag registry plus the logic used to
// resolve an effective feature set from config-like inputs. The on-disk
// `[features]` TOML format, the legacy aliases, and the under-development
// warning event are all replicated to match codex byte-for-byte (after
// key-order canonicalization).
package features

// StageKind identifies the lifecycle stage variant of a feature. It mirrors the
// Rust `Stage` enum discriminant.
type StageKind int

const (
	// StageUnderDevelopment marks features that are still under development and
	// not ready for external use.
	StageUnderDevelopment StageKind = iota
	// StageExperimental marks experimental features surfaced through the
	// `/experimental` menu. The associated Name/MenuDescription/Announcement
	// carry the menu metadata.
	StageExperimental
	// StageStable marks stable features. The flag is retained for ad-hoc
	// enabling/disabling.
	StageStable
	// StageDeprecated marks a feature that should no longer be used.
	StageDeprecated
	// StageRemoved marks a flag that is now a no-op but kept for backward
	// compatibility so old configs still parse.
	StageRemoved
)

// Stage is the high-level lifecycle stage for a feature. It mirrors the Rust
// `Stage` enum: the experimental variant carries menu metadata, all other
// variants leave those fields empty.
type Stage struct {
	Kind StageKind
	// Name is the experimental menu title (StageExperimental only).
	Name string
	// MenuDescription is the experimental menu description (StageExperimental
	// only).
	MenuDescription string
	// Announcement is the experimental announcement banner (StageExperimental
	// only); an empty string means "no announcement".
	Announcement string
}

// underDevelopment returns the StageUnderDevelopment stage.
func underDevelopment() Stage { return Stage{Kind: StageUnderDevelopment} }

// stable returns the StageStable stage.
func stable() Stage { return Stage{Kind: StageStable} }

// deprecated returns the StageDeprecated stage.
func deprecated() Stage { return Stage{Kind: StageDeprecated} }

// removed returns the StageRemoved stage.
func removed() Stage { return Stage{Kind: StageRemoved} }

// experimental returns a StageExperimental stage with the given menu metadata.
func experimental(name, menuDescription, announcement string) Stage {
	return Stage{
		Kind:            StageExperimental,
		Name:            name,
		MenuDescription: menuDescription,
		Announcement:    announcement,
	}
}

// ExperimentalMenuName returns the experimental menu title, or "" with ok=false
// when the stage is not experimental. Mirrors `Stage::experimental_menu_name`.
func (s Stage) ExperimentalMenuName() (string, bool) {
	if s.Kind == StageExperimental {
		return s.Name, true
	}
	return "", false
}

// ExperimentalMenuDescription returns the experimental menu description, or ""
// with ok=false when the stage is not experimental. Mirrors
// `Stage::experimental_menu_description`.
func (s Stage) ExperimentalMenuDescription() (string, bool) {
	if s.Kind == StageExperimental {
		return s.MenuDescription, true
	}
	return "", false
}

// ExperimentalAnnouncement returns the experimental announcement, or "" with
// ok=false when the stage is not experimental or the announcement is empty.
// Mirrors `Stage::experimental_announcement`.
func (s Stage) ExperimentalAnnouncement() (string, bool) {
	if s.Kind == StageExperimental && s.Announcement != "" {
		return s.Announcement, true
	}
	return "", false
}
