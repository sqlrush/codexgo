package cliutil

import (
	"errors"
	"reflect"
	"testing"
)

func strptr(s string) *string { return &s }

// --- approval / sandbox mode CLI args -------------------------------------

func TestApprovalModeToAskForApproval(t *testing.T) {
	tests := []struct {
		name string
		arg  ApprovalModeCliArg
		want AskForApproval
	}{
		{"untrusted", ApprovalModeUntrusted, AskForApprovalUnlessTrusted},
		{"on-failure", ApprovalModeOnFailure, AskForApprovalOnFailure},
		{"on-request", ApprovalModeOnRequest, AskForApprovalOnRequest},
		{"never", ApprovalModeNever, AskForApprovalNever},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.arg.ToAskForApproval(); got != tt.want {
				t.Fatalf("ToAskForApproval() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseApprovalModeCliArg(t *testing.T) {
	tests := []struct {
		in      string
		want    ApprovalModeCliArg
		wantErr bool
	}{
		{"untrusted", ApprovalModeUntrusted, false},
		{"on-failure", ApprovalModeOnFailure, false},
		{"on-request", ApprovalModeOnRequest, false},
		{"never", ApprovalModeNever, false},
		{"Untrusted", "", true},
		{"bogus", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseApprovalModeCliArg(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSandboxModeToSandboxMode(t *testing.T) {
	tests := []struct {
		name string
		arg  SandboxModeCliArg
		want SandboxMode
	}{
		{"read-only", SandboxArgReadOnly, SandboxModeReadOnly},
		{"workspace-write", SandboxArgWorkspaceWrite, SandboxModeWorkspaceWrite},
		{"danger-full-access", SandboxArgDangerFullAccess, SandboxModeDangerFullAccess},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.arg.ToSandboxMode(); got != tt.want {
				t.Fatalf("ToSandboxMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSandboxModeCliArg(t *testing.T) {
	if _, err := ParseSandboxModeCliArg("read-only"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := ParseSandboxModeCliArg("readonly"); err == nil {
		t.Fatalf("expected error for invalid sandbox mode")
	}
}

// --- profile name ---------------------------------------------------------

func TestParseProfileV2Name(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
	}{
		{"work", false},
		{"work-2", false},
		{"work_2", false},
		{"WORK", false},
		{"", true},
		{"has space", true},
		{"has/slash", true},
		{"dot.name", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseProfileV2Name(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.in || got.AsStr() != tt.in {
				t.Fatalf("round-trip mismatch: got %q", got.String())
			}
		})
	}
}

func TestParseProfileV2NameErrorMessage(t *testing.T) {
	_, err := ParseProfileV2Name("bad name")
	if err == nil {
		t.Fatal("expected error")
	}
	want := "invalid --profile value `bad name`; pass a plain name such as `work`"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// --- thread id ------------------------------------------------------------

func TestParseThreadID(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"123e4567-e89b-12d3-a456-426614174000", "123e4567-e89b-12d3-a456-426614174000", false},
		{"123E4567-E89B-12D3-A456-426614174000", "123e4567-e89b-12d3-a456-426614174000", false},
		{"not-a-uuid", "", true},
		{"123e4567e89b12d3a456426614174000", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseThreadID(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %q, want %q", got.String(), tt.want)
			}
		})
	}
}

// --- resume command / hint ------------------------------------------------

func mustThreadID(t *testing.T, s string) ThreadID {
	t.Helper()
	id, err := ParseThreadID(s)
	if err != nil {
		t.Fatalf("ParseThreadID(%q): %v", s, err)
	}
	return id
}

func TestResumeCommand(t *testing.T) {
	id := mustThreadID(t, "123e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name       string
		threadName *string
		threadID   *ThreadID
		want       *string
	}{
		{"prefers name over id", strptr("my-thread"), &id, strptr("codex resume my-thread")},
		{"id when name missing", nil, &id, strptr("codex resume 123e4567-e89b-12d3-a456-426614174000")},
		{"empty name falls back to id", strptr(""), &id, strptr("codex resume 123e4567-e89b-12d3-a456-426614174000")},
		{"none without target", nil, nil, nil},
		{"leading dash needs double dash", strptr("-starts-with-dash"), nil, strptr("codex resume -- -starts-with-dash")},
		{"two words single quoted", strptr("two words"), nil, strptr("codex resume 'two words'")},
		{"single quote uses double quotes", strptr("quote'case"), nil, strptr(`codex resume "quote'case"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResumeCommand(tt.threadName, tt.threadID)
			assertOptStringEqual(t, got, tt.want)
		})
	}
}

func TestResumeHint(t *testing.T) {
	id := mustThreadID(t, "123e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name       string
		threadName *string
		threadID   *ThreadID
		want       *string
	}{
		{
			"names picker item with id",
			strptr("my-thread"), &id,
			strptr("codex resume, then select my-thread (123e4567-e89b-12d3-a456-426614174000)"),
		},
		{
			"direct id command without name",
			nil, &id,
			strptr("codex resume 123e4567-e89b-12d3-a456-426614174000"),
		},
		{"requires thread id", strptr("my-thread"), nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResumeHint(tt.threadName, tt.threadID)
			assertOptStringEqual(t, got, tt.want)
		})
	}
}

func assertOptStringEqual(t *testing.T, got, want *string) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil || want == nil:
		t.Fatalf("got %v, want %v", derefOrNil(got), derefOrNil(want))
	case *got != *want:
		t.Fatalf("got %q, want %q", *got, *want)
	}
}

func derefOrNil(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// --- format env display ---------------------------------------------------

func TestFormatEnvDisplay(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		envVars []string
		want    string
	}{
		{"both nil", nil, nil, "-"},
		{"empty map empty vars", map[string]string{}, []string{}, "-"},
		{
			"sorted env pairs",
			map[string]string{"B": "two", "A": "one"}, nil,
			"A=*****, B=*****",
		},
		{
			"env vars in order",
			nil, []string{"TOKEN", "PATH"},
			"TOKEN=*****, PATH=*****",
		},
		{
			"map then vars",
			map[string]string{"HOME": "/tmp"}, []string{"TOKEN"},
			"HOME=*****, TOKEN=*****",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatEnvDisplay(tt.env, tt.envVars); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatEnvDisplayDoesNotMutateInputs(t *testing.T) {
	env := map[string]string{"B": "two", "A": "one"}
	vars := []string{"Z", "Y"}
	_ = FormatEnvDisplay(env, vars)
	if len(env) != 2 || env["A"] != "one" || env["B"] != "two" {
		t.Fatalf("env map was mutated: %v", env)
	}
	if !reflect.DeepEqual(vars, []string{"Z", "Y"}) {
		t.Fatalf("vars slice was mutated: %v", vars)
	}
}

// --- TOML value parsing ---------------------------------------------------

func TestParseTOMLValueScalars(t *testing.T) {
	t.Run("integer", func(t *testing.T) {
		v, err := parseTOMLValue("42")
		if err != nil {
			t.Fatal(err)
		}
		if i, ok := v.AsInteger(); !ok || i != 42 {
			t.Fatalf("got %v ok=%v, want 42", i, ok)
		}
	})
	t.Run("bool true", func(t *testing.T) {
		v, err := parseTOMLValue("true")
		if err != nil {
			t.Fatal(err)
		}
		if b, ok := v.AsBool(); !ok || !b {
			t.Fatalf("got %v ok=%v, want true", b, ok)
		}
	})
	t.Run("bool false", func(t *testing.T) {
		v, err := parseTOMLValue("false")
		if err != nil {
			t.Fatal(err)
		}
		if b, ok := v.AsBool(); !ok || b {
			t.Fatalf("got %v ok=%v, want false", b, ok)
		}
	})
	t.Run("unquoted string fails", func(t *testing.T) {
		if _, err := parseTOMLValue("hello"); err == nil {
			t.Fatal("expected error for bare word")
		}
	})
	t.Run("array", func(t *testing.T) {
		v, err := parseTOMLValue("[1, 2, 3]")
		if err != nil {
			t.Fatal(err)
		}
		arr, ok := v.AsArray()
		if !ok || len(arr) != 3 {
			t.Fatalf("got len=%d ok=%v, want 3", len(arr), ok)
		}
	})
	t.Run("inline table", func(t *testing.T) {
		v, err := parseTOMLValue("{a = 1, b = 2}")
		if err != nil {
			t.Fatal(err)
		}
		a, ok := v.Get("a")
		if !ok {
			t.Fatal("missing key a")
		}
		if i, ok := a.AsInteger(); !ok || i != 1 {
			t.Fatalf("a = %v, want 1", i)
		}
		b, ok := v.Get("b")
		if !ok {
			t.Fatal("missing key b")
		}
		if i, ok := b.AsInteger(); !ok || i != 2 {
			t.Fatalf("b = %v, want 2", i)
		}
	})
	t.Run("quoted string", func(t *testing.T) {
		v, err := parseTOMLValue(`"o3"`)
		if err != nil {
			t.Fatal(err)
		}
		if s, ok := v.AsString(); !ok || s != "o3" {
			t.Fatalf("got %q ok=%v, want o3", s, ok)
		}
	})
	t.Run("float", func(t *testing.T) {
		v, err := parseTOMLValue("3.5")
		if err != nil {
			t.Fatal(err)
		}
		if f, ok := v.AsFloat(); !ok || f != 3.5 {
			t.Fatalf("got %v ok=%v, want 3.5", f, ok)
		}
	})
	t.Run("error is parse class", func(t *testing.T) {
		_, err := parseTOMLValue("hello")
		if !errors.Is(err, errTOMLParse) {
			t.Fatalf("error %v is not errTOMLParse", err)
		}
	})
}

// --- config overrides -----------------------------------------------------

func TestParseOverridesBasic(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{`model="o3"`, "count=42", "flag=true"})
	parsed, err := overrides.ParseOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed) != 3 {
		t.Fatalf("got %d overrides, want 3", len(parsed))
	}
	if parsed[0].Path != "model" {
		t.Fatalf("path = %q, want model", parsed[0].Path)
	}
	if s, ok := parsed[0].Value.AsString(); !ok || s != "o3" {
		t.Fatalf("model = %q, want o3", s)
	}
	if i, ok := parsed[1].Value.AsInteger(); !ok || i != 42 {
		t.Fatalf("count = %v, want 42", i)
	}
	if b, ok := parsed[2].Value.AsBool(); !ok || !b {
		t.Fatalf("flag = %v, want true", b)
	}
}

func TestParseOverridesUnquotedFallsBackToString(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{"model=o3"})
	parsed, err := overrides.ParseOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := parsed[0].Value.AsString(); !ok || s != "o3" {
		t.Fatalf("model = %q ok=%v, want o3 string", s, ok)
	}
}

func TestParseOverridesStripsQuotesOnFallback(t *testing.T) {
	// A value that is not valid TOML but is wrapped in quotes should have one
	// layer of quotes stripped when used as a literal string.
	overrides := NewCliConfigOverrides([]string{`weird='a b'c`})
	parsed, err := overrides.ParseOverrides()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parsed[0].Value.AsString()
	if !ok {
		t.Fatalf("expected string value, got kind %v", parsed[0].Value.Kind())
	}
	if got != "a b'c" {
		t.Fatalf("got %q, want %q", got, "a b'c")
	}
}

func TestParseOverridesCanonicalizesLegacyLandlock(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{"use_legacy_landlock=true"})
	parsed, err := overrides.ParseOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if parsed[0].Path != "features.use_legacy_landlock" {
		t.Fatalf("path = %q, want features.use_legacy_landlock", parsed[0].Path)
	}
	if b, ok := parsed[0].Value.AsBool(); !ok || !b {
		t.Fatalf("value = %v, want true", b)
	}
}

func TestParseOverridesErrors(t *testing.T) {
	t.Run("missing equals", func(t *testing.T) {
		_, err := NewCliConfigOverrides([]string{"noequals"}).ParseOverrides()
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("empty key", func(t *testing.T) {
		_, err := NewCliConfigOverrides([]string{"=value"}).ParseOverrides()
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestParseOverridesSplitsOnFirstEqualsOnly(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{`note="a=b=c"`})
	parsed, err := overrides.ParseOverrides()
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := parsed[0].Value.AsString(); !ok || s != "a=b=c" {
		t.Fatalf("got %q, want a=b=c", s)
	}
}

func TestPrependRootOverrides(t *testing.T) {
	sub := NewCliConfigOverrides([]string{`model="gpt-5.2"`})
	root := NewCliConfigOverrides([]string{`model="gpt-5.1"`})
	merged := sub.PrependRootOverrides(root)

	want := []string{`model="gpt-5.1"`, `model="gpt-5.2"`}
	if !reflect.DeepEqual(merged.RawOverrides(), want) {
		t.Fatalf("got %v, want %v", merged.RawOverrides(), want)
	}
	// Originals must be untouched.
	if !reflect.DeepEqual(sub.RawOverrides(), []string{`model="gpt-5.2"`}) {
		t.Fatalf("sub mutated: %v", sub.RawOverrides())
	}
	if !reflect.DeepEqual(root.RawOverrides(), []string{`model="gpt-5.1"`}) {
		t.Fatalf("root mutated: %v", root.RawOverrides())
	}
}

func TestApplyOnValueCreatesNestedTables(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{"shell_environment_policy.inherit=true"})
	result, err := overrides.ApplyOnValue(EmptyTOMLTable())
	if err != nil {
		t.Fatal(err)
	}
	policy, ok := result.Get("shell_environment_policy")
	if !ok {
		t.Fatal("missing shell_environment_policy table")
	}
	inherit, ok := policy.Get("inherit")
	if !ok {
		t.Fatal("missing inherit key")
	}
	if b, ok := inherit.AsBool(); !ok || !b {
		t.Fatalf("inherit = %v, want true", b)
	}
}

func TestApplyOnValueReplacesNonTable(t *testing.T) {
	// Start with a scalar at "a"; overriding "a.b" must replace it with a table.
	base := EmptyTOMLTable().withTableEntry("a", TOMLIntValue(5))
	overrides := NewCliConfigOverrides([]string{"a.b=1"})
	result, err := overrides.ApplyOnValue(base)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := result.Get("a")
	if !ok || a.Kind() != TOMLTable {
		t.Fatalf("a kind = %v, want table", a.Kind())
	}
	b, ok := a.Get("b")
	if !ok {
		t.Fatal("missing b")
	}
	if i, ok := b.AsInteger(); !ok || i != 1 {
		t.Fatalf("b = %v, want 1", i)
	}
	// The original base must not have been mutated.
	origA, _ := base.Get("a")
	if origA.Kind() != TOMLInteger {
		t.Fatalf("base mutated: a kind = %v", origA.Kind())
	}
}

func TestApplyOnValueLaterOverrideWins(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{`model="a"`, `model="b"`})
	result, err := overrides.ApplyOnValue(EmptyTOMLTable())
	if err != nil {
		t.Fatal(err)
	}
	m, _ := result.Get("model")
	if s, ok := m.AsString(); !ok || s != "b" {
		t.Fatalf("model = %q, want b", s)
	}
}

// --- shared cli options ---------------------------------------------------

func TestInheritExecRootOptions(t *testing.T) {
	root := SharedCliOptions{
		Model:       strptr("root-model"),
		OSS:         true,
		Cwd:         strptr("/root"),
		Images:      []string{"r1.png"},
		AddDir:      []string{"/rdir"},
		SandboxMode: sandboxPtr(SandboxArgReadOnly),
	}
	child := SharedCliOptions{
		Images: []string{"c1.png"},
		AddDir: []string{"/cdir"},
	}

	got := child.InheritExecRootOptions(root)

	if got.Model == nil || *got.Model != "root-model" {
		t.Fatalf("model = %v, want root-model", got.Model)
	}
	if !got.OSS {
		t.Fatal("oss should be inherited true")
	}
	if got.Cwd == nil || *got.Cwd != "/root" {
		t.Fatalf("cwd = %v, want /root", got.Cwd)
	}
	if got.SandboxMode == nil || *got.SandboxMode != SandboxArgReadOnly {
		t.Fatalf("sandbox = %v, want read-only", got.SandboxMode)
	}
	if !reflect.DeepEqual(got.Images, []string{"r1.png", "c1.png"}) {
		t.Fatalf("images = %v, want [r1 c1]", got.Images)
	}
	if !reflect.DeepEqual(got.AddDir, []string{"/rdir", "/cdir"}) {
		t.Fatalf("add_dir = %v, want [/rdir /cdir]", got.AddDir)
	}
	// child must not be mutated.
	if child.Model != nil {
		t.Fatalf("child mutated: model = %v", child.Model)
	}
	if !reflect.DeepEqual(child.Images, []string{"c1.png"}) {
		t.Fatalf("child images mutated: %v", child.Images)
	}
}

func TestInheritExecRootOptionsKeepsSelfSelectedSandbox(t *testing.T) {
	root := SharedCliOptions{
		DangerouslyBypassApprovalsAndSandbox: true,
	}
	child := SharedCliOptions{
		SandboxMode: sandboxPtr(SandboxArgWorkspaceWrite),
	}
	got := child.InheritExecRootOptions(root)
	// Because the child selected a sandbox mode, the dangerous bypass flag must
	// NOT be inherited from root.
	if got.DangerouslyBypassApprovalsAndSandbox {
		t.Fatal("dangerous bypass should not be inherited when child selected sandbox")
	}
}

func TestApplySubcommandOverrides(t *testing.T) {
	base := SharedCliOptions{
		Model:  strptr("base-model"),
		AddDir: []string{"/base"},
		Images: []string{"base.png"},
	}
	sub := SharedCliOptions{
		Model:           strptr("sub-model"),
		OSS:             true,
		BypassHookTrust: true,
		AddDir:          []string{"/sub"},
		Images:          []string{"sub.png"},
		SandboxMode:     sandboxPtr(SandboxArgDangerFullAccess),
	}
	got := base.ApplySubcommandOverrides(sub)

	if got.Model == nil || *got.Model != "sub-model" {
		t.Fatalf("model = %v, want sub-model", got.Model)
	}
	if !got.OSS {
		t.Fatal("oss should be true")
	}
	if !got.BypassHookTrust {
		t.Fatal("bypass hook trust should be true")
	}
	if got.SandboxMode == nil || *got.SandboxMode != SandboxArgDangerFullAccess {
		t.Fatalf("sandbox = %v, want danger-full-access", got.SandboxMode)
	}
	// Images replaced (not merged) when subcommand provides them.
	if !reflect.DeepEqual(got.Images, []string{"sub.png"}) {
		t.Fatalf("images = %v, want [sub.png]", got.Images)
	}
	// AddDir appended.
	if !reflect.DeepEqual(got.AddDir, []string{"/base", "/sub"}) {
		t.Fatalf("add_dir = %v, want [/base /sub]", got.AddDir)
	}
	// base unchanged.
	if base.Model == nil || *base.Model != "base-model" {
		t.Fatalf("base model mutated: %v", base.Model)
	}
	if !reflect.DeepEqual(base.AddDir, []string{"/base"}) {
		t.Fatalf("base add_dir mutated: %v", base.AddDir)
	}
}

func TestApplySubcommandOverridesNoSandboxSelectionKeepsBase(t *testing.T) {
	base := SharedCliOptions{SandboxMode: sandboxPtr(SandboxArgReadOnly)}
	sub := SharedCliOptions{Model: strptr("x")}
	got := base.ApplySubcommandOverrides(sub)
	if got.SandboxMode == nil || *got.SandboxMode != SandboxArgReadOnly {
		t.Fatalf("sandbox = %v, want read-only (kept from base)", got.SandboxMode)
	}
}

func sandboxPtr(s SandboxModeCliArg) *SandboxModeCliArg { return &s }

// --- additional coverage --------------------------------------------------

func TestAsCliArg(t *testing.T) {
	if got := ApprovalModeUntrusted.AsCliArg(); got != "untrusted" {
		t.Fatalf("approval AsCliArg = %q", got)
	}
	if got := SandboxArgWorkspaceWrite.AsCliArg(); got != "workspace-write" {
		t.Fatalf("sandbox AsCliArg = %q", got)
	}
}

func TestVariantsAreFreshCopies(t *testing.T) {
	a1 := ApprovalModeCliArgVariants()
	a1[0] = "mutated"
	if ApprovalModeCliArgVariants()[0] != ApprovalModeUntrusted {
		t.Fatal("ApprovalModeCliArgVariants returned shared storage")
	}
	s1 := SandboxModeCliArgVariants()
	s1[0] = "mutated"
	if SandboxModeCliArgVariants()[0] != SandboxArgReadOnly {
		t.Fatal("SandboxModeCliArgVariants returned shared storage")
	}
}

func TestProfileV2NameIsZero(t *testing.T) {
	var zero ProfileV2Name
	if !zero.IsZero() {
		t.Fatal("zero ProfileV2Name should report IsZero")
	}
	name, err := ParseProfileV2Name("work")
	if err != nil {
		t.Fatal(err)
	}
	if name.IsZero() {
		t.Fatal("non-empty ProfileV2Name should not report IsZero")
	}
}

func TestNewThreadIDAndIsZero(t *testing.T) {
	id := NewThreadID("123e4567-e89b-12d3-a456-426614174000")
	if id.String() != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("got %q", id.String())
	}
	if id.IsZero() {
		t.Fatal("non-empty ThreadID should not be zero")
	}
	var zero ThreadID
	if !zero.IsZero() {
		t.Fatal("zero ThreadID should report IsZero")
	}
}

func TestParseThreadIDRejectsBadHyphen(t *testing.T) {
	// Right length but a hyphen in the wrong place.
	if _, err := ParseThreadID("123e4567Xe89b-12d3-a456-426614174000"); err == nil {
		t.Fatal("expected error for misplaced separator")
	}
	// Right length and hyphens but a non-hex digit.
	if _, err := ParseThreadID("123e4567-e89b-12d3-a456-42661417400g"); err == nil {
		t.Fatal("expected error for non-hex digit")
	}
}

func TestShlexQuoteEmptyAndNul(t *testing.T) {
	q, err := shlexQuote("")
	if err != nil || q != "''" {
		t.Fatalf("empty quote = %q err=%v, want ''", q, err)
	}
	if _, err := shlexQuote("a\x00b"); err == nil {
		t.Fatal("expected error for NUL byte")
	}
}

func TestShlexJoinNulFallback(t *testing.T) {
	got := shlexJoin([]string{"ok", "bad\x00"})
	if got != "<command included NUL byte>" {
		t.Fatalf("got %q, want NUL fallback", got)
	}
}

func TestShlexJoinMultiple(t *testing.T) {
	got := shlexJoin([]string{"a", "two words", "c"})
	if got != "a 'two words' c" {
		t.Fatalf("got %q", got)
	}
}

func TestShlexQuoteEscapesInsideDoubleQuotes(t *testing.T) {
	// Contains a single quote (forcing double quotes) plus characters that must
	// be backslash-escaped inside double quotes.
	got, err := shlexQuote(`a'b$c` + "`d" + `"e\f!g`)
	if err != nil {
		t.Fatal(err)
	}
	want := `"a'b\$c\` + "`d" + `\"e\\f\!g"`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTOMLValueAccessorsTypeMismatch(t *testing.T) {
	s := TOMLStringValue("x")
	if _, ok := s.AsInteger(); ok {
		t.Fatal("string should not be integer")
	}
	if _, ok := s.AsBool(); ok {
		t.Fatal("string should not be bool")
	}
	if _, ok := s.AsFloat(); ok {
		t.Fatal("string should not be float")
	}
	if _, ok := s.AsArray(); ok {
		t.Fatal("string should not be array")
	}
	if _, ok := s.AsTable(); ok {
		t.Fatal("string should not be table")
	}
	if _, ok := s.Get("k"); ok {
		t.Fatal("Get on non-table should be false")
	}
}

func TestTOMLValueConstructorsCopyInputs(t *testing.T) {
	items := []TOMLValue{TOMLIntValue(1)}
	arr := TOMLArrayValue(items)
	items[0] = TOMLIntValue(99)
	got, _ := arr.AsArray()
	if v, _ := got[0].AsInteger(); v != 1 {
		t.Fatalf("array aliased caller storage: %d", v)
	}

	entries := []TOMLEntry{{Key: "a", Value: TOMLIntValue(1)}}
	tbl := TOMLTableValue(entries)
	entries[0] = TOMLEntry{Key: "a", Value: TOMLIntValue(99)}
	a, _ := tbl.Get("a")
	if v, _ := a.AsInteger(); v != 1 {
		t.Fatalf("table aliased caller storage: %d", v)
	}
}

func TestTOMLValueEqual(t *testing.T) {
	a := TOMLTableValue([]TOMLEntry{
		{Key: "k", Value: TOMLArrayValue([]TOMLValue{TOMLIntValue(1), TOMLStringValue("x")})},
	})
	b := TOMLTableValue([]TOMLEntry{
		{Key: "k", Value: TOMLArrayValue([]TOMLValue{TOMLIntValue(1), TOMLStringValue("x")})},
	})
	if !a.Equal(b) {
		t.Fatal("structurally equal values should be Equal")
	}
	c := TOMLTableValue([]TOMLEntry{
		{Key: "k", Value: TOMLArrayValue([]TOMLValue{TOMLIntValue(2), TOMLStringValue("x")})},
	})
	if a.Equal(c) {
		t.Fatal("values with differing elements should not be Equal")
	}
	if TOMLIntValue(1).Equal(TOMLStringValue("1")) {
		t.Fatal("differing kinds should not be Equal")
	}
	if TOMLBoolValue(true).Equal(TOMLBoolValue(false)) {
		t.Fatal("differing bools should not be Equal")
	}
	if TOMLFloatValue(1.5).Equal(TOMLFloatValue(2.5)) {
		t.Fatal("differing floats should not be Equal")
	}
}

func TestParseTOMLValueBasicStringEscapes(t *testing.T) {
	v, err := parseTOMLValue(`"a\tb\n\"c\\dA"`)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.AsString()
	if !ok {
		t.Fatalf("expected string, got kind %v", v.Kind())
	}
	want := "a\tb\n\"c\\dA"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseTOMLValueLiteralString(t *testing.T) {
	v, err := parseTOMLValue(`'no \n escapes'`)
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := v.AsString(); s != `no \n escapes` {
		t.Fatalf("got %q", s)
	}
}

func TestParseTOMLValueUnderscoreInteger(t *testing.T) {
	v, err := parseTOMLValue("1_000")
	if err != nil {
		t.Fatal(err)
	}
	if i, ok := v.AsInteger(); !ok || i != 1000 {
		t.Fatalf("got %v, want 1000", i)
	}
}

func TestParseTOMLValueNestedInlineTableAndArray(t *testing.T) {
	v, err := parseTOMLValue(`{ a = [1, 2], b = { c = "d" } }`)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := v.Get("a")
	if !ok {
		t.Fatal("missing a")
	}
	arr, ok := a.AsArray()
	if !ok || len(arr) != 2 {
		t.Fatalf("a is not 2-element array: %v", a.Kind())
	}
	b, ok := v.Get("b")
	if !ok {
		t.Fatal("missing b")
	}
	c, ok := b.Get("c")
	if !ok {
		t.Fatal("missing b.c")
	}
	if s, _ := c.AsString(); s != "d" {
		t.Fatalf("b.c = %q, want d", s)
	}
}

func TestParseTOMLValueEmptyContainers(t *testing.T) {
	arr, err := parseTOMLValue("[]")
	if err != nil {
		t.Fatal(err)
	}
	if items, ok := arr.AsArray(); !ok || len(items) != 0 {
		t.Fatalf("empty array mismatch: ok=%v len=%d", ok, len(items))
	}
	tbl, err := parseTOMLValue("{}")
	if err != nil {
		t.Fatal(err)
	}
	if entries, ok := tbl.AsTable(); !ok || len(entries) != 0 {
		t.Fatalf("empty table mismatch: ok=%v len=%d", ok, len(entries))
	}
}

func TestParseTOMLValueQuotedInlineKey(t *testing.T) {
	v, err := parseTOMLValue(`{ "weird key" = 1 }`)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.Get("weird key")
	if !ok {
		t.Fatal("missing quoted key")
	}
	if i, _ := got.AsInteger(); i != 1 {
		t.Fatalf("got %v, want 1", i)
	}
}

func TestParseTOMLValueErrors(t *testing.T) {
	bad := []string{
		"",
		"[1, 2",  // unterminated array
		"{a = 1", // unterminated inline table
		`"unterminated`,
		`'unterminated`,
		"[1 2]",    // missing comma
		"{a 1}",    // missing '='
		`"\x41"`,   // invalid escape
		`"\uZZZZ"`, // invalid unicode escape
	}
	for _, s := range bad {
		if _, err := parseTOMLValue(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestApplyOnValuePropagatesParseError(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{"noequals"})
	if _, err := overrides.ApplyOnValue(EmptyTOMLTable()); err == nil {
		t.Fatal("expected error from ApplyOnValue")
	}
}

func TestApplyOnValueDeepNesting(t *testing.T) {
	overrides := NewCliConfigOverrides([]string{"a.b.c.d=1"})
	result, err := overrides.ApplyOnValue(EmptyTOMLTable())
	if err != nil {
		t.Fatal(err)
	}
	cur := result
	for _, k := range []string{"a", "b", "c"} {
		next, ok := cur.Get(k)
		if !ok || next.Kind() != TOMLTable {
			t.Fatalf("missing intermediate table %q", k)
		}
		cur = next
	}
	d, ok := cur.Get("d")
	if !ok {
		t.Fatal("missing leaf d")
	}
	if i, _ := d.AsInteger(); i != 1 {
		t.Fatalf("d = %v, want 1", i)
	}
}

func TestApplySubcommandOverridesEmptySubKeepsBase(t *testing.T) {
	base := SharedCliOptions{
		Model:  strptr("base"),
		Images: []string{"i.png"},
		AddDir: []string{"/d"},
	}
	got := base.ApplySubcommandOverrides(SharedCliOptions{})
	if got.Model == nil || *got.Model != "base" {
		t.Fatalf("model = %v, want base", got.Model)
	}
	if !reflect.DeepEqual(got.Images, []string{"i.png"}) {
		t.Fatalf("images = %v", got.Images)
	}
	if !reflect.DeepEqual(got.AddDir, []string{"/d"}) {
		t.Fatalf("add_dir = %v", got.AddDir)
	}
}

func TestInheritExecRootOptionsProfileAndProvider(t *testing.T) {
	prof, err := ParseProfileV2Name("work")
	if err != nil {
		t.Fatal(err)
	}
	root := SharedCliOptions{
		OSSProvider:     strptr("ollama"),
		ConfigProfileV2: &prof,
		BypassHookTrust: true,
	}
	child := SharedCliOptions{}
	got := child.InheritExecRootOptions(root)
	if got.OSSProvider == nil || *got.OSSProvider != "ollama" {
		t.Fatalf("oss provider = %v", got.OSSProvider)
	}
	if got.ConfigProfileV2 == nil || got.ConfigProfileV2.AsStr() != "work" {
		t.Fatalf("profile = %v", got.ConfigProfileV2)
	}
	if !got.BypassHookTrust {
		t.Fatal("bypass hook trust should be inherited")
	}
	// Inherited profile pointer must be a distinct copy.
	if got.ConfigProfileV2 == root.ConfigProfileV2 {
		t.Fatal("profile pointer should be a copy, not the same pointer")
	}
}
