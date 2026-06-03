package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSuppressQuitWhileTypingAPIKey(t *testing.T) {
	tests := []struct {
		name   string
		auth   *AuthStep
		key    tea.KeyMsg
		expect bool
	}{
		{
			name:   "printable q suppressed when typing api key with text",
			auth:   &AuthStep{Phase: PhaseAPIKeyEntry, APIKey: "sk-abc"},
			key:    keyMsg("q"),
			expect: true,
		},
		{
			name:   "printable q not suppressed when api key empty",
			auth:   &AuthStep{Phase: PhaseAPIKeyEntry, APIKey: ""},
			key:    keyMsg("q"),
			expect: false,
		},
		{
			name:   "ctrl chord never suppressed",
			auth:   &AuthStep{Phase: PhaseAPIKeyEntry, APIKey: "sk-abc"},
			key:    tea.KeyMsg{Type: tea.KeyCtrlC},
			expect: false,
		},
		{
			name:   "not in api key entry not suppressed",
			auth:   &AuthStep{Phase: PhasePickMode, APIKey: "sk-abc"},
			key:    keyMsg("q"),
			expect: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := OnboardingScreen{Auth: tc.auth}
			if got := s.suppressQuitWhileTypingAPIKey(tc.key); got != tc.expect {
				t.Fatalf("suppressQuitWhileTypingAPIKey = %v, want %v", got, tc.expect)
			}
		})
	}
}

func TestOnboardingQuitDuringAuthExits(t *testing.T) {
	s := NewOnboardingScreen(OnboardingArgs{ShowLoginScreen: true})
	s = s.HandleKey(keyMsg("q"))
	if !s.IsDone() {
		t.Fatalf("expected done after quit")
	}
	if !s.ShouldExit() {
		t.Fatalf("expected exit after quitting an in-progress auth step")
	}
}

func TestOnboardingQuitWhileTypingAPIKeyAddsText(t *testing.T) {
	s := NewOnboardingScreen(OnboardingArgs{ShowLoginScreen: true})
	// Enter API-key mode (3rd displayed option) and type a char first.
	s.Auth.Phase = PhaseAPIKeyEntry
	s.Auth.APIKey = "k"
	s = s.HandleKey(keyMsg("q"))
	if s.IsDone() {
		t.Fatalf("q while typing api key should not quit")
	}
	if s.Auth.APIKey != "kq" {
		t.Fatalf("expected api key 'kq', got %q", s.Auth.APIKey)
	}
}

func TestTrustStepSelectionAndState(t *testing.T) {
	step := NewTrustStep("/work/project", "/work/project", false)
	if step.State() != StepInProgress {
		t.Fatalf("new trust step should be in progress")
	}
	if step.Highlighted != TrustYes {
		t.Fatalf("trust should default to Yes highlight")
	}

	// Enter confirms the highlighted Trust option.
	step = step.HandleKey(keyMsg("enter"))
	if step.Selection != TrustYes {
		t.Fatalf("enter on Yes should select Trust")
	}
	if step.State() != StepComplete {
		t.Fatalf("trust step should be complete after selection")
	}
}

func TestTrustStepQuit(t *testing.T) {
	step := NewTrustStep(".", ".", false)
	step = step.HandleKey(keyMsg("down")) // highlight quit
	step = step.HandleKey(keyMsg("enter"))
	if !step.ShouldQuit {
		t.Fatalf("enter on No should quit")
	}
	if step.State() != StepComplete {
		t.Fatalf("quit should complete the step")
	}
}

func TestTrustStepSubdirWarningTarget(t *testing.T) {
	step := NewTrustStep("/work/project/sub", "/work/project", false)
	if step.PersistTargetForTest() != "/work/project" {
		t.Fatalf("trust target should be repo root")
	}
}

// PersistTargetForTest exposes TrustTarget for tests.
func (t TrustStep) PersistTargetForTest() string { return t.TrustTarget }

func TestOnboardingTrustQuitPropagatesExit(t *testing.T) {
	s := NewOnboardingScreen(OnboardingArgs{
		ShowTrustScreen: true,
		IsLoggedIn:      true,
		Cwd:             "/work",
		TrustTarget:     "/work",
	})
	s = s.HandleKey(keyMsg("down"))  // highlight quit
	s = s.HandleKey(keyMsg("enter")) // confirm quit
	if !s.ShouldExit() {
		t.Fatalf("trust quit should propagate exit")
	}
	if !s.IsDone() {
		t.Fatalf("trust quit should finish the flow")
	}
}

func TestOnboardingPersistedTrustTarget(t *testing.T) {
	s := NewOnboardingScreen(OnboardingArgs{
		ShowTrustScreen: true,
		IsLoggedIn:      true,
		Cwd:             "/work",
		TrustTarget:     "/work",
	})
	if s.PersistedTrustTarget() != "" {
		t.Fatalf("no trust target before selection")
	}
	s = s.HandleKey(keyMsg("enter")) // trust (Yes highlighted by default)
	if s.PersistedTrustTarget() != "/work" {
		t.Fatalf("expected /work as persisted target, got %q", s.PersistedTrustTarget())
	}
}

func TestMarkTrustPersistFailedReturnsInProgress(t *testing.T) {
	s := NewOnboardingScreen(OnboardingArgs{
		ShowTrustScreen: true,
		IsLoggedIn:      true,
		Cwd:             "/work",
		TrustTarget:     "/work",
	})
	s = s.HandleKey(keyMsg("enter")) // trust
	s = s.MarkTrustPersistFailed("app server unavailable")
	if s.Trust.Selection != TrustNone {
		t.Fatalf("failed persist should clear selection")
	}
	if s.Trust.State() != StepInProgress {
		t.Fatalf("failed persist should return trust step to in progress")
	}
	if s.Trust.Error == "" {
		t.Fatalf("failed persist should set an error")
	}
}

func TestAuthMoveHighlightWraps(t *testing.T) {
	a := NewAuthStep(ForcedLoginNone)
	if a.Highlighted != SignInChatGPT {
		t.Fatalf("default highlight should be ChatGPT")
	}
	a = a.moveHighlight(-1)
	// Wrapping up from the first option lands on the last selectable (ApiKey).
	if a.Highlighted != SignInAPIKey {
		t.Fatalf("move up from first should wrap to ApiKey, got %v", a.Highlighted)
	}
}

func TestAuthForcedAPIHighlightsAPIKey(t *testing.T) {
	a := NewAuthStep(ForcedLoginAPI)
	if a.Highlighted != SignInAPIKey {
		t.Fatalf("forced API should highlight ApiKey")
	}
	if a.chatgptAllowed() {
		t.Fatalf("forced API should disallow chatgpt")
	}
}

func TestAuthAPIKeyEntryEditing(t *testing.T) {
	a := AuthStep{Phase: PhaseAPIKeyEntry}
	a = handleAuthKey(a, keyMsg("s"))
	a = handleAuthKey(a, keyMsg("k"))
	if a.APIKey != "sk" {
		t.Fatalf("expected 'sk', got %q", a.APIKey)
	}
	a = handleAuthKey(a, keyMsg("backspace"))
	if a.APIKey != "s" {
		t.Fatalf("expected 's' after backspace, got %q", a.APIKey)
	}
	a = handleAuthKey(a, keyMsg("esc"))
	if a.Phase != PhasePickMode || a.APIKey != "" {
		t.Fatalf("esc should reset to pick mode and clear key")
	}
}

func TestApplySignInState(t *testing.T) {
	s := NewOnboardingScreen(OnboardingArgs{ShowLoginScreen: true})
	s = s.ApplySignInState(SignInStateMsg{Phase: PhaseChatGPTSuccess})
	if s.Auth.Phase != PhaseChatGPTSuccess {
		t.Fatalf("sign-in state should be applied")
	}
	if s.Auth.State() != StepComplete {
		t.Fatalf("success phase should complete the auth step")
	}
	if !s.IsDone() {
		t.Fatalf("flow should be done once auth completes and no trust step")
	}
}
