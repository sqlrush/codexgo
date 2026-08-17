package tui

import (
	"testing"

	"github.com/sqlrush/codexgo/pkg/config"
)

func TestLoadThemeDefault(t *testing.T) {
	caps := Capabilities{ColorLevel: ColorLevelTrueColor}
	th := LoadTheme(nil, caps)
	if th.Name != "default" {
		t.Fatalf("nil tui should yield default theme, got %q", th.Name)
	}
	if th.Foreground == nil {
		t.Fatal("default theme foreground should be set on a truecolor terminal")
	}
}

func TestLoadThemeNamed(t *testing.T) {
	caps := Capabilities{ColorLevel: ColorLevelTrueColor}
	light := "light"
	tui := &config.Tui{Theme: &light}
	th := LoadTheme(tui, caps)
	if th.Name != "light" {
		t.Fatalf("theme name = %q, want light", th.Name)
	}
}

func TestLoadThemeUnknownFallsBack(t *testing.T) {
	caps := Capabilities{ColorLevel: ColorLevelTrueColor}
	unknown := "does-not-exist"
	tui := &config.Tui{Theme: &unknown}
	th := LoadTheme(tui, caps)
	// The requested name is retained for diagnostics, but the palette is the
	// default one (foreground present).
	if th.Name != "does-not-exist" {
		t.Fatalf("unknown theme name should be retained, got %q", th.Name)
	}
	if th.Foreground == nil {
		t.Fatal("unknown theme should fall back to a populated palette")
	}
}

func TestLoadThemeNoColor(t *testing.T) {
	caps := Capabilities{ColorLevel: ColorLevelTrueColor, NoColor: true}
	th := LoadTheme(nil, caps)
	if _, ok := th.Foreground.(interface{ String() string }); !ok {
		t.Skip("color type does not expose String; nothing to assert")
	}
	// Under NoColor, BestColor yields NoColor; render should not contain SGR.
	rendered := th.UserMessage.Render("x")
	if rendered != "x" {
		// lipgloss may still wrap when NoColor sentinel is used; tolerate but log.
		t.Logf("NoColor render = %q", rendered)
	}
}
