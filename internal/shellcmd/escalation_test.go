package shellcmd

import (
	"reflect"
	"testing"
)

func TestDetectEscalation(t *testing.T) {
	tests := []struct {
		name        string
		command     []string
		wantKind    Escalator
		wantWrapped []string
		wantOK      bool
	}{
		{"sudo", []string{"sudo", "rm", "-rf", "/"}, EscalatorSudo, []string{"rm", "-rf", "/"}, true},
		{"su", []string{"su", "-", "root"}, EscalatorSu, []string{"-", "root"}, true},
		{"doas", []string{"doas", "pkg", "install"}, EscalatorDoas, []string{"pkg", "install"}, true},
		{"abs_sudo", []string{"/usr/bin/sudo", "ls"}, EscalatorSudo, []string{"ls"}, true},
		{"not_escalation", []string{"ls", "-la"}, EscalatorNone, nil, false},
		{"empty", nil, EscalatorNone, nil, false},
		{"bare_sudo", []string{"sudo"}, EscalatorSudo, []string{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, wrapped, ok := DetectEscalation(tc.command)
			if kind != tc.wantKind || ok != tc.wantOK {
				t.Fatalf("got (kind=%v, ok=%v), want (kind=%v, ok=%v)",
					kind, ok, tc.wantKind, tc.wantOK)
			}
			if ok && !reflect.DeepEqual(wrapped, tc.wantWrapped) {
				t.Fatalf("wrapped = %v, want %v", wrapped, tc.wantWrapped)
			}
		})
	}
}

func TestEscalatorString(t *testing.T) {
	cases := map[Escalator]string{
		EscalatorNone: "",
		EscalatorSudo: "sudo",
		EscalatorSu:   "su",
		EscalatorDoas: "doas",
	}
	for e, want := range cases {
		if got := e.String(); got != want {
			t.Errorf("Escalator(%d).String() = %q, want %q", e, got, want)
		}
	}
}

func TestCommandMightBeDangerous(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		want    bool
	}{
		{"rm_rf", []string{"rm", "-rf", "/"}, true},
		{"rm_f", []string{"rm", "-f", "x"}, true},
		{"rm_plain", []string{"rm", "x"}, false},
		{"sudo_rm_rf", []string{"sudo", "rm", "-rf", "/"}, true},
		{"sudo_ls", []string{"sudo", "ls"}, false},
		{"plain_ls", []string{"ls"}, false},
		{"empty", nil, false},
		{"bash_lc_rm_rf", []string{"bash", "-lc", "rm -rf /"}, true},
		{"bash_lc_pipeline_with_rm", []string{"bash", "-lc", "ls && rm -f foo"}, true},
		{"bash_lc_safe", []string{"bash", "-lc", "ls && pwd"}, false},
		{"bash_lc_sudo_rm", []string{"bash", "-lc", "sudo rm -rf /"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CommandMightBeDangerous(tc.command); got != tc.want {
				t.Errorf("CommandMightBeDangerous(%v) = %v, want %v", tc.command, got, tc.want)
			}
		})
	}
}
