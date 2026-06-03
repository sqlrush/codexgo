package shellcmd

import "testing"

func TestDetectShellType(t *testing.T) {
	tests := []struct {
		path     string
		want     ShellType
		wantOK   bool
		wantName string
	}{
		{"zsh", ShellTypeZsh, true, "zsh"},
		{"bash", ShellTypeBash, true, "bash"},
		{"sh", ShellTypeSh, true, "sh"},
		{"cmd", ShellTypeCmd, true, "cmd"},
		{"pwsh", ShellTypePowerShell, true, "powershell"},
		{"powershell", ShellTypePowerShell, true, "powershell"},
		{"/bin/bash", ShellTypeBash, true, "bash"},
		{"/usr/bin/zsh", ShellTypeZsh, true, "zsh"},
		{"/bin/sh", ShellTypeSh, true, "sh"},
		{"powershell.exe", ShellTypePowerShell, true, "powershell"},
		{"/usr/local/bin/powershell.exe", ShellTypePowerShell, true, "powershell"},
		{"python", ShellTypeUnknown, false, "unknown"},
		{"/usr/bin/python3", ShellTypeUnknown, false, "unknown"},
		{"", ShellTypeUnknown, false, "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got, ok := DetectShellType(tc.path)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("DetectShellType(%q) = (%v, %v), want (%v, %v)",
					tc.path, got, ok, tc.want, tc.wantOK)
			}
			if got.String() != tc.wantName {
				t.Fatalf("String() = %q, want %q", got.String(), tc.wantName)
			}
		})
	}
}
