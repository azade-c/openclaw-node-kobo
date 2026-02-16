package tailnet

import "testing"

func TestNewServer(t *testing.T) {
	s := New(Config{Hostname: "kobo", StateDir: "/tmp"})
	if s == nil {
		t.Fatalf("expected server")
	}
}
