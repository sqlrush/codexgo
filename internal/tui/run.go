package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sqlrush/codexgo/internal/config"
	"github.com/sqlrush/codexgo/internal/filesearch"
)

// RunConfig parameterizes [Run], the reusable launcher that wires an in-process
// engine to the bubbletea program. Callers build the in-process client (the
// engine seam the [Engine] drives) and supply it here; Run owns the remaining
// terminal-UI assembly so each entry point (the codex default path, codex-tui,
// resume/fork) stays a thin shell.
type RunConfig struct {
	// Client drives the engine; required. It is the in-process app-server client
	// (the type [NewEngine] accepts via [EngineConfig.Client]).
	Client engineClient
	// Workdir is the working directory used to seed @mention file search when no
	// explicit FileSearch is supplied. Defaults to "." when empty.
	Workdir string
	// Tui is the resolved [tui] config block used to load the theme. May be nil
	// for defaults.
	Tui *config.Tui
	// FileSearch resolves @mention completions. When nil, one is built over
	// Workdir via [filesearch.Run].
	FileSearch FileSearchFunc
	// ResumeThreadID resumes an existing thread when non-empty; otherwise a fresh
	// thread is started.
	ResumeThreadID string
	// Model is the configured default model slug shown in the session-header
	// card. When empty the card omits the model value.
	Model string
	// Version is the CLI version string shown in the session-header card's title
	// (rendered as "(v<Version>)"). When empty it defaults to [CodexVersion], the
	// upstream codex version codexgo impersonates as a drop-in, so the first frame
	// matches real codex (which renders its own CARGO_PKG_VERSION).
	Version string
}

// Run launches the interactive TUI against the engine reachable through
// cfg.Client. It performs the full wiring: capability detection, the app event
// sender, engine start (or resume), theme load, model construction with the chat
// transcript and bottom pane, and the bubbletea program (inline scrollback mode,
// ctx scoped). The engine event pump runs on a background goroutine for the
// lifetime of the program. Run blocks until the program exits or ctx is
// cancelled.
func Run(ctx context.Context, cfg RunConfig) error {
	if cfg.Client == nil {
		return fmt.Errorf("tui: run: client is required")
	}

	caps := DetectCapabilities()
	sender := NewAppEventSender()
	engine := NewEngine(EngineConfig{Client: cfg.Client, Sender: sender})

	if _, err := engine.Start(ctx, cfg.ResumeThreadID); err != nil {
		return fmt.Errorf("tui: run: start thread: %w", err)
	}

	theme := LoadTheme(cfg.Tui, caps)
	transcript := NewChatTranscript(theme).WithSessionHeader(
		cfg.version(),
		cfg.Model,
		FormatDirectoryDisplay(cfg.workdir()),
	)
	model := NewModel(ModelConfig{
		Caps:       caps,
		Tui:        cfg.Tui,
		Sender:     sender,
		Engine:     engine,
		Transcript: transcript,
		Inline:     true,
		Bottom: NewChatBottomPane(ChatBottomPaneConfig{
			Theme:      theme,
			FileSearch: cfg.fileSearch(),
			ColorLevel: caps.ColorLevel,
			Footer: ComposerFooter{
				Model:     cfg.Model,
				Directory: FormatDirectoryDisplay(cfg.workdir()),
			},
		}),
	})

	// Inline-scrollback mode (no alternate screen): finalized history cells are
	// printed into the terminal's native scrollback (tea.Println) while the live
	// viewport renders only the composer/status region. This mirrors codex's
	// inline ratatui viewport (codex-rs/tui/src/tui.rs init + insert_history_lines)
	// rather than holding the whole transcript in an alt-screen buffer.
	program := tea.NewProgram(model, tea.WithContext(ctx))
	sender.Attach(program)

	// Pump engine events into the program on a background goroutine.
	go engine.Pump(ctx)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("tui: run: run program: %w", err)
	}
	return nil
}

// fileSearch returns the configured FileSearch, or a default built over the
// working directory when none was supplied.
func (cfg RunConfig) fileSearch() FileSearchFunc {
	if cfg.FileSearch != nil {
		return cfg.FileSearch
	}
	return newWorkdirFileSearch(cfg.workdir())
}

// workdir returns the configured working directory, defaulting to ".".
func (cfg RunConfig) workdir() string {
	if cfg.Workdir == "" {
		return "."
	}
	return cfg.Workdir
}

// version returns the configured version, defaulting to the impersonated
// upstream codex version ([CodexVersion]) when empty so the session-header card
// matches real codex.
func (cfg RunConfig) version() string {
	if cfg.Version == "" {
		return CodexVersion
	}
	return cfg.Version
}

// newWorkdirFileSearch builds an @mention completion function over root.
func newWorkdirFileSearch(root string) FileSearchFunc {
	return func(query string) []string {
		if query == "" {
			return nil
		}
		res, err := filesearch.Run(query, []string{root}, filesearch.DefaultOptions(), nil)
		if err != nil {
			return nil
		}
		paths := make([]string, 0, len(res.Matches))
		for _, m := range res.Matches {
			paths = append(paths, m.Path)
		}
		return paths
	}
}
