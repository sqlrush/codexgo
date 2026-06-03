package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// This file ports the first-run onboarding flow from
// codex-rs/tui/src/onboarding/: a small state machine over visible steps
// (welcome -> auth login -> trust directory). It is a faithful behavioral port:
// the same step ordering, the same fixed keybindings (keys.rs), the same
// text-entry quit guard while typing an API key, and the same trust-directory
// prompt and selection model.
//
// Cosmetic deviations from the Rust original:
//   - The ASCII welcome animation is reduced to a static banner; the area's
//     concern here is the login + trust flow, not the animation frames.
//   - Rendering uses lipgloss styles via the foundation Theme rather than
//     ratatui buffers; the dynamic per-step height measurement from
//     onboarding_screen.rs is unnecessary because bubbletea owns the full frame.
//
// The live OAuth/device-code transport (internal/login) is driven by the host
// program; this widget models the visible state and emits AppEvents the host
// translates into login calls. Async login results are fed back via
// [SignInStateMsg].

// --- fixed onboarding keys (port of onboarding/keys.rs) ----------------------

// onboardingKeyKind classifies a key for the fixed onboarding bindings.
type onboardingKeyKind int

const (
	onbKeyNone onboardingKeyKind = iota
	onbKeyMoveUp
	onbKeyMoveDown
	onbKeySelectFirst
	onbKeySelectSecond
	onbKeySelectThird
	onbKeyConfirm
	onbKeyCancel
	onbKeyQuit
	onbKeyToggleAnimation
)

// classifyOnboardingKey maps a tea.KeyMsg to one of the fixed onboarding key
// kinds. Bindings mirror onboarding/keys.rs. Selection digits/letters and quit
// keys are reported alongside whether the key is a bare printable character
// (used by the API-key text-entry quit guard).
//
// The returned printable rune is 0 when the key is not a bare character.
func classifyOnboardingKey(msg tea.KeyMsg) (kind onboardingKeyKind, printable rune) {
	s := msg.String()
	switch s {
	case "up", "k":
		return onbKeyMoveUp, runeOf(s)
	case "down", "j":
		return onbKeyMoveDown, runeOf(s)
	case "1", "y":
		return onbKeySelectFirst, runeOf(s)
	case "2", "n":
		return onbKeySelectSecond, runeOf(s)
	case "3":
		return onbKeySelectThird, '3'
	case "enter":
		return onbKeyConfirm, 0
	case "esc":
		return onbKeyCancel, 0
	case "q":
		return onbKeyQuit, 'q'
	case "ctrl+c", "ctrl+d":
		return onbKeyQuit, 0
	case "ctrl+.", "ctrl+shift+.":
		return onbKeyToggleAnimation, 0
	default:
		return onbKeyNone, runeOf(s)
	}
}

// runeOf returns the single rune of a one-character key string, or 0.
func runeOf(s string) rune {
	r := []rune(s)
	if len(r) == 1 {
		return r[0]
	}
	return 0
}

// isBarePrintable reports whether a key string is a single printable character
// with no control/alt modifier (used by the API-key quit guard).
func isBarePrintable(msg tea.KeyMsg) bool {
	s := msg.String()
	if strings.HasPrefix(s, "ctrl+") || strings.HasPrefix(s, "alt+") {
		return false
	}
	return len([]rune(s)) == 1
}

// --- step state (port of onboarding_screen.rs StepState) ---------------------

// StepState is the lifecycle of one onboarding step.
type StepState int

const (
	// StepHidden means the step does not apply (e.g. welcome when already logged in).
	StepHidden StepState = iota
	// StepInProgress means the step is awaiting user input.
	StepInProgress
	// StepComplete means the step has been satisfied.
	StepComplete
)

// --- sign-in model (port of onboarding/auth.rs SignInState/SignInOption) -----

// SignInOption is a login method the user can choose.
type SignInOption int

const (
	// SignInChatGPT signs in with ChatGPT via browser OAuth.
	SignInChatGPT SignInOption = iota
	// SignInDeviceCode signs in with a device code.
	SignInDeviceCode
	// SignInAPIKey connects an API key for usage-based billing.
	SignInAPIKey
)

// signInOptionDescription returns the one-line description shown under each
// highlighted option, mirroring auth.rs render_pick_mode.
func signInOptionDescription(opt SignInOption) string {
	switch opt {
	case SignInChatGPT:
		return "Sign in with ChatGPT"
	case SignInDeviceCode:
		return "Sign in with a device code"
	case SignInAPIKey:
		return "Provide your own API key"
	default:
		return ""
	}
}

// signInOptionLabel returns the menu label for an option.
func signInOptionLabel(opt SignInOption) string {
	switch opt {
	case SignInChatGPT:
		return "Sign in with ChatGPT"
	case SignInDeviceCode:
		return "Sign in with device code"
	case SignInAPIKey:
		return "Provide your own API key"
	default:
		return ""
	}
}

// SignInPhase is the high-level phase of the auth step (port of SignInState).
type SignInPhase int

const (
	// PhasePickMode shows the login method menu.
	PhasePickMode SignInPhase = iota
	// PhaseChatGPTContinueInBrowser is awaiting browser OAuth completion.
	PhaseChatGPTContinueInBrowser
	// PhaseChatGPTSuccessMessage shows the "logged in" confirmation.
	PhaseChatGPTSuccessMessage
	// PhaseChatGPTSuccess is the terminal success state (advances the flow).
	PhaseChatGPTSuccess
	// PhaseAPIKeyEntry is the API-key text-input state.
	PhaseAPIKeyEntry
	// PhaseAPIKeyConfigured is the terminal state after a key is accepted.
	PhaseAPIKeyConfigured
)

// ForcedLoginMethod restricts which login methods are offered.
type ForcedLoginMethod int

const (
	// ForcedLoginNone offers all login methods.
	ForcedLoginNone ForcedLoginMethod = iota
	// ForcedLoginChatGPT restricts login to ChatGPT.
	ForcedLoginChatGPT
	// ForcedLoginAPI restricts login to API key.
	ForcedLoginAPI
)

// apiKeyDisabledMessage mirrors auth.rs API_KEY_DISABLED_MESSAGE.
const apiKeyDisabledMessage = "API key login is disabled."

// AuthStep models the onboarding login step (port of AuthModeWidget).
//
// It is a value type; mutating helpers return a new copy (immutability).
type AuthStep struct {
	Highlighted SignInOption
	Phase       SignInPhase
	Forced      ForcedLoginMethod
	APIKey      string
	// APIKeyFromEnv reports whether the API-key field was prepopulated from the
	// environment (affects the entry hint).
	APIKeyFromEnv bool
	// Error is the currently displayed inline error, if any.
	Error string
}

// NewAuthStep builds an auth step highlighting the method appropriate for the
// forced login policy, mirroring OnboardingScreen::new.
func NewAuthStep(forced ForcedLoginMethod) AuthStep {
	highlighted := SignInChatGPT
	if forced == ForcedLoginAPI {
		highlighted = SignInAPIKey
	}
	return AuthStep{Highlighted: highlighted, Phase: PhasePickMode, Forced: forced}
}

// State returns the step state, mirroring AuthModeWidget::get_step_state: the
// step stays in progress until a terminal success state is reached.
func (a AuthStep) State() StepState {
	switch a.Phase {
	case PhaseChatGPTSuccess, PhaseAPIKeyConfigured:
		return StepComplete
	default:
		return StepInProgress
	}
}

func (a AuthStep) chatgptAllowed() bool { return a.Forced != ForcedLoginAPI }
func (a AuthStep) apiAllowed() bool     { return a.Forced != ForcedLoginChatGPT }

// displayedOptions returns the options shown in the menu (port of
// displayed_sign_in_options): ChatGPT is always displayed.
func (a AuthStep) displayedOptions() []SignInOption {
	opts := []SignInOption{SignInChatGPT}
	if a.chatgptAllowed() {
		opts = append(opts, SignInDeviceCode)
	}
	if a.apiAllowed() {
		opts = append(opts, SignInAPIKey)
	}
	return opts
}

// selectableOptions returns the options the highlight may move between (port of
// selectable_sign_in_options).
func (a AuthStep) selectableOptions() []SignInOption {
	var opts []SignInOption
	if a.chatgptAllowed() {
		opts = append(opts, SignInChatGPT, SignInDeviceCode)
	}
	if a.apiAllowed() {
		opts = append(opts, SignInAPIKey)
	}
	return opts
}

// moveHighlight returns a copy with the highlight moved by delta over the
// selectable options, wrapping (port of move_highlight).
func (a AuthStep) moveHighlight(delta int) AuthStep {
	opts := a.selectableOptions()
	if len(opts) == 0 {
		return a
	}
	cur := 0
	for i, o := range opts {
		if o == a.Highlighted {
			cur = i
			break
		}
	}
	n := len(opts)
	next := ((cur+delta)%n + n) % n
	a.Highlighted = opts[next]
	return a
}

// isAPIKeyEntryActive reports whether the step is rendering API-key entry.
func (a AuthStep) isAPIKeyEntryActive() bool { return a.Phase == PhaseAPIKeyEntry }

// apiKeyEntryHasText reports whether the API-key field has user text.
func (a AuthStep) apiKeyEntryHasText() bool {
	return a.Phase == PhaseAPIKeyEntry && a.APIKey != ""
}

// --- trust directory step (port of onboarding/trust_directory.rs) ------------

// TrustSelection is the user's trust-prompt choice.
type TrustSelection int

const (
	// TrustNone means no selection yet.
	TrustNone TrustSelection = iota
	// TrustYes trusts the directory.
	TrustYes
	// TrustQuit declines and quits.
	TrustQuit
)

// TrustStep models the trust-directory prompt (port of TrustDirectoryWidget).
//
// It is a value type; mutating helpers return a new copy (immutability).
type TrustStep struct {
	// Cwd is the current working directory.
	Cwd string
	// TrustTarget is the directory trust will be applied to (the git project
	// root when Cwd is a subdirectory).
	TrustTarget string
	// ShowSandboxHint requests the "create a sandbox" continuation hint.
	ShowSandboxHint bool
	// Highlighted is the highlighted option (default Trust).
	Highlighted TrustSelection
	// Selection is the confirmed choice, or TrustNone.
	Selection TrustSelection
	// ShouldQuit is set when the user chose to quit.
	ShouldQuit bool
	// Error is an inline error (e.g. trust persistence failed).
	Error string
}

// NewTrustStep builds a trust step with Trust highlighted by default.
func NewTrustStep(cwd, trustTarget string, showSandboxHint bool) TrustStep {
	return TrustStep{
		Cwd:             cwd,
		TrustTarget:     trustTarget,
		ShowSandboxHint: showSandboxHint,
		Highlighted:     TrustYes,
	}
}

// State returns the step state (port of TrustDirectoryWidget::get_step_state).
func (t TrustStep) State() StepState {
	if t.Selection != TrustNone || t.ShouldQuit {
		return StepComplete
	}
	return StepInProgress
}

// HandleKey applies a key to the trust step, returning the updated step (port of
// TrustDirectoryWidget::handle_key_event).
func (t TrustStep) HandleKey(msg tea.KeyMsg) TrustStep {
	kind, _ := classifyOnboardingKey(msg)
	switch kind {
	case onbKeyMoveUp:
		t.Highlighted = TrustYes
	case onbKeyMoveDown:
		t.Highlighted = TrustQuit
	case onbKeySelectFirst:
		return t.trust()
	case onbKeySelectSecond, onbKeyQuit, onbKeyCancel:
		return t.quit()
	case onbKeyConfirm:
		if t.Highlighted == TrustYes {
			return t.trust()
		}
		return t.quit()
	}
	return t
}

func (t TrustStep) trust() TrustStep {
	t.Highlighted = TrustYes
	t.Error = ""
	t.Selection = TrustYes
	return t
}

func (t TrustStep) quit() TrustStep {
	t.Highlighted = TrustQuit
	t.ShouldQuit = true
	return t
}

// --- onboarding screen (port of onboarding_screen.rs OnboardingScreen) --------

// OnboardingScreen orchestrates the visible onboarding steps and routes keys,
// mirroring OnboardingScreen. It is a value type; Update returns a new copy.
type OnboardingScreen struct {
	// ShowWelcome controls whether the welcome banner is rendered (false when
	// already logged in).
	ShowWelcome bool
	// Auth is the login step, present when ShowLogin is true.
	Auth *AuthStep
	// Trust is the trust step, present when ShowTrust is true.
	Trust *TrustStep
	// done is set when the flow has finished.
	done bool
	// shouldExit is set when onboarding ended by user-requested exit.
	shouldExit bool
}

// OnboardingArgs parameterizes a new onboarding screen.
type OnboardingArgs struct {
	// ShowLoginScreen requests the auth login step.
	ShowLoginScreen bool
	// ShowTrustScreen requests the trust-directory step.
	ShowTrustScreen bool
	// IsLoggedIn reports whether the user is already authenticated (hides the
	// welcome banner per WelcomeWidget::get_step_state).
	IsLoggedIn bool
	// Forced restricts the offered login methods.
	Forced ForcedLoginMethod
	// Cwd is the working directory for the trust prompt.
	Cwd string
	// TrustTarget is the directory trust applies to (defaults to Cwd).
	TrustTarget string
	// ShowSandboxHint requests the sandbox continuation hint.
	ShowSandboxHint bool
}

// NewOnboardingScreen builds the screen with the requested steps, mirroring
// OnboardingScreen::new step ordering (welcome, login, trust).
func NewOnboardingScreen(args OnboardingArgs) OnboardingScreen {
	s := OnboardingScreen{ShowWelcome: !args.IsLoggedIn}
	if args.ShowLoginScreen {
		auth := NewAuthStep(args.Forced)
		s.Auth = &auth
	}
	if args.ShowTrustScreen {
		target := args.TrustTarget
		if target == "" {
			target = args.Cwd
		}
		trust := NewTrustStep(args.Cwd, target, args.ShowSandboxHint)
		s.Trust = &trust
	}
	return s
}

// IsDone reports whether onboarding has finished, either explicitly or because
// no step remains in progress (port of OnboardingScreen::is_done).
func (s OnboardingScreen) IsDone() bool {
	if s.done {
		return true
	}
	if s.Auth != nil && s.Auth.State() == StepInProgress {
		return false
	}
	if s.Trust != nil && s.Trust.State() == StepInProgress {
		return false
	}
	return true
}

// ShouldExit reports whether the flow ended by user-requested exit.
func (s OnboardingScreen) ShouldExit() bool { return s.shouldExit }

// isAuthInProgress reports whether the auth step is awaiting input.
func (s OnboardingScreen) isAuthInProgress() bool {
	return s.Auth != nil && s.Auth.State() == StepInProgress
}

// suppressQuitWhileTypingAPIKey returns true when a quit key should be treated
// as text input instead, mirroring suppress_quit_while_typing_api_key: only when
// API-key entry is active, the field has text, and the key is a bare printable.
func (s OnboardingScreen) suppressQuitWhileTypingAPIKey(msg tea.KeyMsg) bool {
	if s.Auth == nil {
		return false
	}
	return s.Auth.isAPIKeyEntryActive() &&
		s.Auth.apiKeyEntryHasText() &&
		isBarePrintable(msg)
}

// HandleKey routes a key event through onboarding, returning the updated screen.
// It mirrors OnboardingScreen::handle_key_event including the quit guard and the
// trust-quit exit propagation.
func (s OnboardingScreen) HandleKey(msg tea.KeyMsg) OnboardingScreen {
	kind, _ := classifyOnboardingKey(msg)
	quit := kind == onbKeyQuit && !s.suppressQuitWhileTypingAPIKey(msg)
	if quit {
		if s.isAuthInProgress() {
			// Cancelling the auth menu exits rather than leaving the user unauthed.
			s.shouldExit = true
		}
		s.done = true
		return s
	}

	// The active step receives the key. Trust takes priority once login is
	// complete, matching the current_steps ordering (welcome+complete steps,
	// then the single in-progress step).
	if s.Auth != nil && s.Auth.State() == StepInProgress {
		next := handleAuthKey(*s.Auth, msg)
		s.Auth = &next
	} else if s.Trust != nil && s.Trust.State() == StepInProgress {
		next := s.Trust.HandleKey(msg)
		s.Trust = &next
		if s.Trust.ShouldQuit {
			s.shouldExit = true
			s.done = true
		}
	}
	return s
}

// handleAuthKey applies a key to the auth step, returning the updated step. It
// mirrors AuthModeWidget::handle_key_event's pick-mode and API-key-entry paths.
// The actual login transport is performed by the host on the emitted intent;
// here we move highlights, enter/leave API-key entry, and edit the field.
func handleAuthKey(a AuthStep, msg tea.KeyMsg) AuthStep {
	// API-key entry consumes text editing first.
	if a.Phase == PhaseAPIKeyEntry {
		switch msg.String() {
		case "esc":
			a.Phase = PhasePickMode
			a.APIKey = ""
			return a
		case "backspace":
			r := []rune(a.APIKey)
			if len(r) > 0 {
				a.APIKey = string(r[:len(r)-1])
			}
			return a
		case "enter":
			// Submitting is handled by the host (it calls login). The widget
			// keeps the entered value; a successful login transitions the phase
			// via SignInStateMsg.
			return a
		}
		if isBarePrintable(msg) {
			a.APIKey += msg.String()
		}
		return a
	}

	kind, _ := classifyOnboardingKey(msg)
	switch kind {
	case onbKeyMoveUp:
		return a.moveHighlight(-1)
	case onbKeyMoveDown:
		return a.moveHighlight(1)
	case onbKeySelectFirst:
		return a.selectByIndex(0)
	case onbKeySelectSecond:
		return a.selectByIndex(1)
	case onbKeySelectThird:
		return a.selectByIndex(2)
	case onbKeyConfirm:
		switch a.Phase {
		case PhasePickMode:
			return a.activate(a.Highlighted)
		case PhaseChatGPTSuccessMessage:
			a.Phase = PhaseChatGPTSuccess
			return a
		}
	}
	return a
}

// selectByIndex activates the displayed option at index (port of
// select_option_by_index).
func (a AuthStep) selectByIndex(index int) AuthStep {
	opts := a.displayedOptions()
	if index < 0 || index >= len(opts) {
		return a
	}
	return a.activate(opts[index])
}

// activate begins the chosen login method (port of handle_sign_in_option). For
// ChatGPT/device-code the host performs the OAuth round trip; here we move into
// the continue-in-browser phase. For API key we open text entry.
func (a AuthStep) activate(opt SignInOption) AuthStep {
	switch opt {
	case SignInChatGPT:
		if a.chatgptAllowed() {
			a.Highlighted = SignInChatGPT
			a.Phase = PhaseChatGPTContinueInBrowser
			a.Error = ""
		}
	case SignInDeviceCode:
		if a.chatgptAllowed() {
			a.Highlighted = SignInDeviceCode
			a.Phase = PhaseChatGPTContinueInBrowser
			a.Error = ""
		}
	case SignInAPIKey:
		if a.apiAllowed() {
			a.Highlighted = SignInAPIKey
			a.Phase = PhaseAPIKeyEntry
			a.Error = ""
		} else {
			a.Highlighted = SignInChatGPT
			a.Error = apiKeyDisabledMessage
			a.Phase = PhasePickMode
		}
	}
	return a
}

// --- async login feedback ----------------------------------------------------

// SignInStateMsg is delivered by the host to update the auth phase after an
// async login round trip (port of the AccountLoginCompleted / AccountUpdated
// notification handlers). It is a tea.Msg.
type SignInStateMsg struct {
	// Phase is the new sign-in phase.
	Phase SignInPhase
	// Error is an inline error to display, if any.
	Error string
}

// ApplySignInState returns a copy of the screen with the auth phase updated from
// an async login result, mirroring on_account_login_completed.
func (s OnboardingScreen) ApplySignInState(m SignInStateMsg) OnboardingScreen {
	if s.Auth == nil {
		return s
	}
	next := *s.Auth
	next.Phase = m.Phase
	next.Error = m.Error
	s.Auth = &next
	return s
}

// PersistedTrustTarget returns the trust target to persist when the user chose
// to trust, or "" otherwise (port of persist_selected_trust's selection check).
func (s OnboardingScreen) PersistedTrustTarget() string {
	if s.Trust != nil && s.Trust.Selection == TrustYes {
		return s.Trust.TrustTarget
	}
	return ""
}

// MarkTrustPersistFailed returns a copy clearing the trust selection and setting
// an error, mirroring persist_selected_trust's failure path so the step returns
// to in-progress.
func (s OnboardingScreen) MarkTrustPersistFailed(errMsg string) OnboardingScreen {
	if s.Trust == nil {
		return s
	}
	next := *s.Trust
	next.Selection = TrustNone
	next.Error = errMsg
	s.Trust = &next
	return s
}
