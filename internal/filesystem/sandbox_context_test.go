package filesystem

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sqlrush/codexgo/internal/protocol"
)

func TestFromPermissionProfileDefaults(t *testing.T) {
	profile := protocol.DefaultPermissionProfile()
	ctx := FromPermissionProfile(profile)
	if ctx.Cwd != nil {
		t.Fatal("expected nil cwd")
	}
	if ctx.WindowsSandboxLevel != protocol.WindowsSandboxLevelDisabled {
		t.Fatalf("WindowsSandboxLevel = %q", ctx.WindowsSandboxLevel)
	}
	if ctx.WindowsSandboxPrivateDesktop || ctx.UseLegacyLandlock {
		t.Fatal("expected windows/landlock flags false")
	}
}

func TestFromPermissionProfileWithCwdDoesNotMutate(t *testing.T) {
	profile := protocol.DefaultPermissionProfile()
	cwd := protocol.AbsolutePath("/work")
	ctx := FromPermissionProfileWithCwd(profile, cwd)
	if ctx.Cwd == nil || *ctx.Cwd != cwd {
		t.Fatalf("cwd = %v", ctx.Cwd)
	}
	// Mutating the returned pointer must not affect the original argument.
	*ctx.Cwd = "/elsewhere"
	if cwd != "/work" {
		t.Fatal("original cwd argument was mutated")
	}
}

func TestSandboxContextJSONRoundTrip(t *testing.T) {
	cwd := protocol.AbsolutePath("/work")
	original := FileSystemSandboxContext{
		Permissions:                  protocol.DefaultPermissionProfile(),
		Cwd:                          &cwd,
		WindowsSandboxLevel:          protocol.WindowsSandboxLevelDisabled,
		WindowsSandboxPrivateDesktop: true,
		UseLegacyLandlock:            true,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// camelCase keys are required for wire compatibility.
	for _, key := range []string{
		`"permissions"`,
		`"cwd"`,
		`"windowsSandboxLevel"`,
		`"windowsSandboxPrivateDesktop"`,
		`"useLegacyLandlock"`,
	} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("missing key %s in %s", key, data)
		}
	}

	var decoded FileSystemSandboxContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Cwd == nil || *decoded.Cwd != cwd {
		t.Fatalf("decoded cwd = %v", decoded.Cwd)
	}
	if !decoded.WindowsSandboxPrivateDesktop || !decoded.UseLegacyLandlock {
		t.Fatal("decoded flags lost")
	}
}

func TestSandboxContextJSONOmitsNilCwd(t *testing.T) {
	ctx := FromPermissionProfile(protocol.DefaultPermissionProfile())
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"cwd"`) {
		t.Fatalf("nil cwd should be omitted: %s", data)
	}
}

func TestSandboxContextJSONDefaultsBooleans(t *testing.T) {
	// A payload missing the optional booleans decodes them as false.
	payload := `{"permissions":{"type":"disabled"},"windowsSandboxLevel":"disabled"}`
	var ctx FileSystemSandboxContext
	if err := json.Unmarshal([]byte(payload), &ctx); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if ctx.WindowsSandboxPrivateDesktop || ctx.UseLegacyLandlock {
		t.Fatal("missing booleans should default to false")
	}
	if ctx.Permissions.Kind != protocol.PermissionProfileDisabled {
		t.Fatalf("permissions kind = %q", ctx.Permissions.Kind)
	}
}
