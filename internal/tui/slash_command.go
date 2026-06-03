package tui

// SlashCommand is a command invoked by starting a message with a leading slash.
//
// Port of codex-rs/tui/src/slash_command.rs SlashCommand. The enum order is the
// presentation order in the popup, so more frequently used commands come first.
// DO NOT alpha-sort.
type SlashCommand int

const (
	SlashModel SlashCommand = iota
	SlashIde
	SlashPermissions
	SlashKeymap
	SlashVim
	SlashElevateSandbox
	SlashSandboxReadRoot
	SlashExperimental
	SlashAutoReview
	SlashMemories
	SlashSkills
	SlashHooks
	SlashReview
	SlashRename
	SlashNew
	SlashArchive
	SlashResume
	SlashFork
	SlashInit
	SlashCompact
	SlashPlan
	SlashGoal
	SlashAgent
	SlashSide
	SlashBtw
	SlashCopy
	SlashRaw
	SlashDiff
	SlashMention
	SlashStatus
	SlashDebugConfig
	SlashTitle
	SlashStatusline
	SlashTheme
	SlashPets
	SlashMcp
	SlashApps
	SlashPlugins
	SlashLogout
	SlashQuit
	SlashExit
	SlashFeedback
	SlashRollout
	SlashPs
	SlashStop
	SlashClear
	SlashPersonality
	SlashRealtime
	SlashSettings
	SlashTestApproval
	SlashMultiAgents
	SlashMemoryDrop
	SlashMemoryUpdate
)

// slashCommandOrder is the canonical presentation order of all commands.
var slashCommandOrder = []SlashCommand{
	SlashModel, SlashIde, SlashPermissions, SlashKeymap, SlashVim,
	SlashElevateSandbox, SlashSandboxReadRoot, SlashExperimental, SlashAutoReview,
	SlashMemories, SlashSkills, SlashHooks, SlashReview, SlashRename, SlashNew,
	SlashArchive, SlashResume, SlashFork, SlashInit, SlashCompact, SlashPlan,
	SlashGoal, SlashAgent, SlashSide, SlashBtw, SlashCopy, SlashRaw, SlashDiff,
	SlashMention, SlashStatus, SlashDebugConfig, SlashTitle, SlashStatusline,
	SlashTheme, SlashPets, SlashMcp, SlashApps, SlashPlugins, SlashLogout,
	SlashQuit, SlashExit, SlashFeedback, SlashRollout, SlashPs, SlashStop,
	SlashClear, SlashPersonality, SlashRealtime, SlashSettings, SlashTestApproval,
	SlashMultiAgents, SlashMemoryDrop, SlashMemoryUpdate,
}

// slashCommandName maps a command to its canonical command string (no leading
// '/'). Aliases are handled separately in ParseSlashCommandName.
var slashCommandName = map[SlashCommand]string{
	SlashModel: "model", SlashIde: "ide", SlashPermissions: "permissions",
	SlashKeymap: "keymap", SlashVim: "vim", SlashElevateSandbox: "setup-default-sandbox",
	SlashSandboxReadRoot: "sandbox-add-read-dir", SlashExperimental: "experimental",
	SlashAutoReview: "approve", SlashMemories: "memories", SlashSkills: "skills",
	SlashHooks: "hooks", SlashReview: "review", SlashRename: "rename", SlashNew: "new",
	SlashArchive: "archive", SlashResume: "resume", SlashFork: "fork", SlashInit: "init",
	SlashCompact: "compact", SlashPlan: "plan", SlashGoal: "goal", SlashAgent: "agent",
	SlashSide: "side", SlashBtw: "btw", SlashCopy: "copy", SlashRaw: "raw",
	SlashDiff: "diff", SlashMention: "mention", SlashStatus: "status",
	SlashDebugConfig: "debug-config", SlashTitle: "title", SlashStatusline: "statusline",
	SlashTheme: "theme", SlashPets: "pets", SlashMcp: "mcp", SlashApps: "apps",
	SlashPlugins: "plugins", SlashLogout: "logout", SlashQuit: "quit", SlashExit: "exit",
	SlashFeedback: "feedback", SlashRollout: "rollout", SlashPs: "ps", SlashStop: "stop",
	SlashClear: "clear", SlashPersonality: "personality", SlashRealtime: "realtime",
	SlashSettings: "settings", SlashTestApproval: "test-approval",
	SlashMultiAgents: "subagents", SlashMemoryDrop: "debug-m-drop",
	SlashMemoryUpdate: "debug-m-update",
}

// slashCommandAliases maps additional accepted spellings to the canonical
// command. Port of the #[strum(serialize = ...)] entries.
var slashCommandAliases = map[string]SlashCommand{
	"clean": SlashStop,
	"pet":   SlashPets,
}

// slashCommandDesc holds the user-visible descriptions.
var slashCommandDesc = map[SlashCommand]string{
	SlashFeedback:        "send logs to maintainers",
	SlashNew:             "start a new chat during a conversation",
	SlashInit:            "create an AGENTS.md file with instructions for Codex",
	SlashCompact:         "summarize conversation to prevent hitting the context limit",
	SlashReview:          "review my current changes and find issues",
	SlashRename:          "rename the current thread",
	SlashResume:          "resume a saved chat",
	SlashArchive:         "archive this session and exit",
	SlashClear:           "clear the terminal and start a new chat",
	SlashFork:            "fork the current chat",
	SlashQuit:            "exit Codex",
	SlashExit:            "exit Codex",
	SlashCopy:            "copy last response as markdown",
	SlashRaw:             "toggle raw scrollback mode for copy-friendly terminal selection",
	SlashDiff:            "show git diff (including untracked files)",
	SlashMention:         "mention a file",
	SlashSkills:          "use skills to improve how Codex performs specific tasks",
	SlashHooks:           "view and manage lifecycle hooks",
	SlashStatus:          "show current session configuration and token usage",
	SlashDebugConfig:     "show config layers and requirement sources for debugging",
	SlashTitle:           "configure which items appear in the terminal title",
	SlashStatusline:      "configure which items appear in the status line",
	SlashTheme:           "choose a syntax highlighting theme",
	SlashPets:            "choose or hide the terminal pet",
	SlashPs:              "list background terminals",
	SlashStop:            "stop all background terminals",
	SlashMemoryDrop:      "DO NOT USE",
	SlashMemoryUpdate:    "DO NOT USE",
	SlashModel:           "choose what model and reasoning effort to use",
	SlashIde:             "include current selection, open files, and other context from your IDE",
	SlashPersonality:     "choose a communication style for Codex",
	SlashRealtime:        "toggle realtime voice mode (experimental)",
	SlashSettings:        "configure realtime microphone/speaker",
	SlashPlan:            "switch to Plan mode",
	SlashGoal:            "set or view the goal for a long-running task",
	SlashAgent:           "switch the active agent thread",
	SlashMultiAgents:     "switch the active agent thread",
	SlashSide:            "start a side conversation in an ephemeral fork",
	SlashBtw:             "start a side conversation in an ephemeral fork",
	SlashPermissions:     "choose what Codex is allowed to do",
	SlashKeymap:          "remap TUI shortcuts",
	SlashVim:             "toggle Vim mode for the composer",
	SlashElevateSandbox:  "set up elevated agent sandbox",
	SlashSandboxReadRoot: "let sandbox read a directory: /sandbox-add-read-dir <absolute_path>",
	SlashExperimental:    "toggle experimental features",
	SlashAutoReview:      "approve one retry of a recent auto-review denial",
	SlashMemories:        "configure memory use and generation",
	SlashMcp:             "list configured MCP tools; use /mcp verbose for details",
	SlashApps:            "manage apps",
	SlashPlugins:         "browse plugins",
	SlashLogout:          "log out of Codex",
	SlashRollout:         "print the rollout file path",
	SlashTestApproval:    "test approval request",
}

// Command returns the command string without the leading '/'.
func (c SlashCommand) Command() string { return slashCommandName[c] }

// Description returns the user-visible description shown in the popup.
func (c SlashCommand) Description() string { return slashCommandDesc[c] }

// SupportsInlineArgs reports whether the command accepts inline arguments.
//
// Port of SlashCommand::supports_inline_args.
func (c SlashCommand) SupportsInlineArgs() bool {
	switch c {
	case SlashReview, SlashRename, SlashPlan, SlashGoal, SlashIde, SlashKeymap,
		SlashMcp, SlashRaw, SlashPets, SlashSide, SlashBtw, SlashResume, SlashSandboxReadRoot:
		return true
	default:
		return false
	}
}

// AvailableInSideConversation reports whether the command stays usable inside an
// active side conversation.
//
// Port of SlashCommand::available_in_side_conversation.
func (c SlashCommand) AvailableInSideConversation() bool {
	switch c {
	case SlashCopy, SlashRaw, SlashDiff, SlashMention, SlashStatus, SlashIde:
		return true
	default:
		return false
	}
}

// AvailableDuringTask reports whether the command can be run while a task is in
// progress.
//
// Port of SlashCommand::available_during_task.
func (c SlashCommand) AvailableDuringTask() bool {
	switch c {
	case SlashNew, SlashArchive, SlashResume, SlashFork, SlashInit, SlashCompact,
		SlashModel, SlashPersonality, SlashPermissions, SlashKeymap, SlashVim,
		SlashElevateSandbox, SlashSandboxReadRoot, SlashExperimental, SlashMemories,
		SlashReview, SlashPlan, SlashClear, SlashLogout, SlashMemoryDrop,
		SlashMemoryUpdate, SlashTheme, SlashPets:
		return false
	default:
		return true
	}
}

// BuiltinCommandFlags gate which built-in commands are visible/usable for the
// current session state.
//
// Port of slash_commands.rs BuiltinCommandFlags.
type BuiltinCommandFlags struct {
	CollaborationModesEnabled   bool
	ConnectorsEnabled           bool
	PluginsCommandEnabled       bool
	ServiceTierCommandsEnabled  bool
	GoalCommandEnabled          bool
	PersonalityCommandEnabled   bool
	RealtimeConversationEnabled bool
	AudioDeviceSelectionEnabled bool
	AllowElevateSandbox         bool
	SideConversationActive      bool
}

// BuiltinsForInput returns the built-ins visible/usable for the current input.
//
// Port of slash_commands.rs builtins_for_input.
func BuiltinsForInput(flags BuiltinCommandFlags) []SlashCommand {
	out := make([]SlashCommand, 0, len(slashCommandOrder))
	for _, cmd := range slashCommandOrder {
		if !commandVisible(cmd) {
			continue
		}
		if !flags.AllowElevateSandbox && cmd == SlashElevateSandbox {
			continue
		}
		if !flags.CollaborationModesEnabled && cmd == SlashPlan {
			continue
		}
		if !flags.ConnectorsEnabled && cmd == SlashApps {
			continue
		}
		if !flags.PluginsCommandEnabled && cmd == SlashPlugins {
			continue
		}
		if !flags.GoalCommandEnabled && cmd == SlashGoal {
			continue
		}
		if !flags.PersonalityCommandEnabled && cmd == SlashPersonality {
			continue
		}
		if !flags.RealtimeConversationEnabled && cmd == SlashRealtime {
			continue
		}
		if !flags.AudioDeviceSelectionEnabled && cmd == SlashSettings {
			continue
		}
		if flags.SideConversationActive && !cmd.AvailableInSideConversation() {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// FindBuiltinCommand resolves a single built-in command by exact name, after
// feature gating (ignoring side-conversation gating for dispatch).
//
// Port of slash_commands.rs find_builtin_command.
func FindBuiltinCommand(name string, flags BuiltinCommandFlags) (SlashCommand, bool) {
	cmd, ok := ParseSlashCommandName(name)
	if !ok {
		return 0, false
	}
	gating := flags
	gating.SideConversationActive = false
	for _, visible := range BuiltinsForInput(gating) {
		if visible == cmd {
			return cmd, true
		}
	}
	return 0, false
}

// ParseSlashCommandName parses a command string (no leading '/') into a
// SlashCommand, resolving aliases. It mirrors strum's FromStr.
func ParseSlashCommandName(name string) (SlashCommand, bool) {
	for cmd, str := range slashCommandName {
		if str == name {
			return cmd, true
		}
	}
	if cmd, ok := slashCommandAliases[name]; ok {
		return cmd, true
	}
	return 0, false
}

// commandVisible mirrors SlashCommand::is_visible for the non-Windows,
// non-Android, release-build target. SandboxReadRoot is Windows-only and the
// debug-only commands (rollout/test-approval) are hidden.
func commandVisible(c SlashCommand) bool {
	switch c {
	case SlashSandboxReadRoot:
		return false // windows-only
	case SlashRollout, SlashTestApproval:
		return false // debug-build only
	default:
		return true
	}
}
