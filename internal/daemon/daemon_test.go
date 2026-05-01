package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPIDFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".yaad"), 0755)

	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	pid := ReadPID(dir)
	if pid != os.Getpid() {
		t.Fatalf("expected PID %d, got %d", os.Getpid(), pid)
	}

	if !IsRunning(dir) {
		t.Fatal("expected IsRunning to be true for own PID")
	}

	RemovePID(dir)
	if ReadPID(dir) != 0 {
		t.Fatal("expected PID 0 after removal")
	}
}

func TestHealthCheck(t *testing.T) {
	// Healthy server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	// Pass full URL so HealthCheck doesn't prepend localhost
	if err := HealthCheck(srv.URL); err != nil {
		t.Fatalf("expected healthy, got: %v", err)
	}

	// Unhealthy: nothing listening
	if err := HealthCheck(":19999"); err == nil {
		t.Fatal("expected error for non-existent server")
	}
}

func TestNormalizeAddr(t *testing.T) {
	tests := []struct{ in, want string }{
		{":3456", ":3456"},
		{"3456", ":3456"},
		{"localhost:3456", ":localhost:3456"},
	}
	for _, tt := range tests {
		got := normalizeAddr(tt.in)
		if got != tt.want {
			t.Errorf("normalizeAddr(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
