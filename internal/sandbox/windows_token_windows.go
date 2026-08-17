//go:build windows

package sandbox

import (
	"fmt"
	"unsafe"

	"github.com/sqlrush/codexgo/pkg/protocol"
	"golang.org/x/sys/windows"
)

// CreateRestrictedToken flags (winnt.h). Not wrapped by x/sys/windows, so they
// are reproduced here for the lazy-DLL call.
const (
	disableMaxPrivilege = 0x1
	luaToken            = 0x4
	writeRestricted     = 0x8
)

// lowIntegritySID is the well-known SID string for the low mandatory integrity
// level (S-1-16-4096). Setting it on the token confines the sandboxed process to
// low integrity so it cannot write to medium/high-integrity objects.
const lowIntegritySID = "S-1-16-4096"

// procCreateRestrictedToken binds advapi32!CreateRestrictedToken lazily so the
// package stays cgo-free while calling an API x/sys/windows does not wrap.
var (
	modAdvapi32             = windows.NewLazySystemDLL("advapi32.dll")
	procCreateRestrictedTok = modAdvapi32.NewProc("CreateRestrictedToken")
)

// buildSandboxToken constructs the primary token the sandboxed command runs
// under, according to the requested WindowsSandboxLevel:
//
//	disabled:         the current process token (no confinement).
//	restricted-token: a WRITE_RESTRICTED, LUA, max-privilege-disabled restricted
//	                  token at low integrity (the default enforcement tier).
//	elevated:         a low-integrity primary token derived from the current
//	                  token (privileges retained for the elevated workflow).
//
// The caller owns the returned token and must Close it. Mirrors the token
// construction in token.rs.
func buildSandboxToken(level protocol.WindowsSandboxLevel) (windows.Token, error) {
	base, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return 0, fmt.Errorf("sandbox: open current process token: %w", err)
	}
	defer base.Close()

	switch level {
	case protocol.WindowsSandboxLevelDisabled:
		return duplicatePrimaryToken(base)
	case protocol.WindowsSandboxLevelElevated:
		token, err := duplicatePrimaryToken(base)
		if err != nil {
			return 0, err
		}
		if err := setLowIntegrity(token); err != nil {
			token.Close()
			return 0, err
		}
		return token, nil
	default: // restricted-token
		token, err := createRestrictedToken(base)
		if err != nil {
			return 0, err
		}
		if err := setLowIntegrity(token); err != nil {
			token.Close()
			return 0, err
		}
		return token, nil
	}
}

// duplicatePrimaryToken duplicates base into a primary token usable with
// CreateProcessAsUser.
func duplicatePrimaryToken(base windows.Token) (windows.Token, error) {
	var dup windows.Token
	err := windows.DuplicateTokenEx(
		base,
		windows.TOKEN_ALL_ACCESS,
		nil,
		windows.SecurityImpersonation,
		windows.TokenPrimary,
		&dup,
	)
	if err != nil {
		return 0, fmt.Errorf("sandbox: duplicate primary token: %w", err)
	}
	return dup, nil
}

// createRestrictedToken builds a WRITE_RESTRICTED restricted token from base with
// max privileges disabled and the LUA flag set. Mirrors the CreateRestrictedToken
// call in token.rs (without the explicit capability/restricting SID list, which
// the simplified native port leaves to the default WRITE_RESTRICTED behavior).
func createRestrictedToken(base windows.Token) (windows.Token, error) {
	var restricted windows.Token
	flags := uintptr(disableMaxPrivilege | luaToken | writeRestricted)

	ret, _, callErr := procCreateRestrictedTok.Call(
		uintptr(base),
		flags,
		0, 0, // DisableSidCount, SidsToDisable
		0, 0, // DeletePrivilegeCount, PrivilegesToDelete
		0, 0, // RestrictedSidCount, SidsToRestrict
		uintptr(unsafe.Pointer(&restricted)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("sandbox: CreateRestrictedToken: %w", callErr)
	}
	return restricted, nil
}

// setLowIntegrity sets the token's mandatory integrity level to low so the
// sandboxed process cannot modify medium/high-integrity objects. Mirrors the
// integrity-level handling around SetTokenInformation in token.rs.
func setLowIntegrity(token windows.Token) error {
	sid, err := windows.StringToSid(lowIntegritySID)
	if err != nil {
		return fmt.Errorf("sandbox: parse low-integrity SID: %w", err)
	}

	label := windows.Tokenmandatorylabel{
		Label: windows.SIDAndAttributes{
			Sid:        sid,
			Attributes: windows.SE_GROUP_INTEGRITY,
		},
	}

	if err := windows.SetTokenInformation(
		token,
		windows.TokenIntegrityLevel,
		(*byte)(unsafe.Pointer(&label)),
		label.Size(),
	); err != nil {
		return fmt.Errorf("sandbox: set low integrity level: %w", err)
	}
	return nil
}

// sandboxPrincipalSID returns the SID the deny-read ACLs target: the user SID of
// the token the sandboxed process runs under. Denying read to this SID prevents
// the sandboxed process from reading the protected paths.
func sandboxPrincipalSID(token windows.Token) (*windows.SID, error) {
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("sandbox: query token user SID: %w", err)
	}
	return user.User.Sid, nil
}
