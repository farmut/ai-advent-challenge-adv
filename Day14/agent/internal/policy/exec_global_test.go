package policy

import "testing"

func TestCheckGlobalExec(t *testing.T) {
	tests := []struct {
		argv []string
		ok   bool
	}{
		{[]string{"go", "test", "./..."}, true},
		{[]string{"gofmt", "-l", "."}, true},
		{nil, false},
		{[]string{""}, false},
		{[]string{"/bin/go", "test"}, false},
		{[]string{"go", "run", "."}, false},
		{[]string{"go", "test", "-exec", "x"}, false},
		{[]string{"go", "vet", "-vettool=x"}, false},
		{[]string{"go", "build", "-o", "x"}, false},
		{[]string{"go", "env", "-w", "GOPROXY=x"}, false},
		{[]string{"gofmt", "-w", "."}, false},
		{[]string{"npm", "install"}, false},
		{[]string{"npx", "x"}, false},
		{[]string{"go", "test", "$(id)"}, false},
	}
	for _, tt := range tests {
		err := CheckGlobalExec(tt.argv)
		if (err == nil) != tt.ok {
			t.Errorf("%q: err = %v, want ok=%v", tt.argv, err, tt.ok)
		}
		if err != nil && CodeOf(err) == "" {
			t.Errorf("%q: not a policy error: %v", tt.argv, err)
		}
	}
	for _, b := range SandboxBinaries {
		if !IsSandboxBinary(b) {
			t.Errorf("IsSandboxBinary(%q) = false", b)
		}
	}
	if IsSandboxBinary("sh") || IsSandboxBinary("curl") {
		t.Error("shell/curl must not be sandbox binaries")
	}
}
