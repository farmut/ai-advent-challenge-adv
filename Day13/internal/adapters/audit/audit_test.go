package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"agile-consultant/internal/ports"
)

func TestJSONLPermsAndLines(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "audit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "a.jsonl")
	j, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := j.Write(context.Background(), ports.AuditRecord{V: 1, RequestID: "r", HTTPStatus: i}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if !j.Ready() {
		t.Fatal("must be ready")
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal("Close must be idempotent")
	}
	if j.Ready() || j.Write(context.Background(), ports.AuditRecord{}) != ErrClosed {
		t.Fatal("closed log must reject writes")
	}
	tests := []struct {
		path string
		mode os.FileMode
	}{{dir, 0o700}, {path, 0o600}}
	for _, tt := range tests {
		st, err := os.Stat(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != tt.mode {
			t.Fatalf("%s mode = %v, want %v", tt.path, st.Mode().Perm(), tt.mode)
		}
	}
	f, _ := os.Open(path)
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r ports.AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("line %d: %v", n, err)
		}
		n++
	}
	if n != 50 {
		t.Fatalf("lines = %d", n)
	}
}

func TestHMAC(t *testing.T) {
	tests := []struct {
		name    string
		pepper  string
		wantErr bool
	}{{"set", "0123456789abcdef", false}, {"random", "", false}, {"short", "short", true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := NewHMAC(tt.pepper, nil)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v", err)
			}
			if err != nil {
				return
			}
			a := h.Fingerprint("email", "a@b.io")
			if len(a) != 12 || a != h.Fingerprint("email", "a@b.io") || a == h.Fingerprint("phone", "a@b.io") {
				t.Fatalf("fingerprint %q", a)
			}
		})
	}
	h1, _ := NewHMAC("0123456789abcdef", nil)
	h2, _ := NewHMAC("fedcba9876543210", nil)
	if h1.Fingerprint("x", "v") == h2.Fingerprint("x", "v") {
		t.Fatal("pepper must change fingerprints")
	}
}
