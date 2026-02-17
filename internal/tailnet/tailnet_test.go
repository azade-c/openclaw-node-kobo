package tailnet

import (
	"context"
	"testing"
	"time"
)

func TestNewServer(t *testing.T) {
	s := New(Config{Hostname: "kobo", StateDir: "/tmp"})
	if s == nil {
		t.Fatalf("expected server")
	}
}

func TestServerUpCancelable(t *testing.T) {
	s := New(Config{Hostname: "kobo", StateDir: "/tmp"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- s.Up(ctx)
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("expected Up to return an error for canceled context")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("Up did not return after context timeout")
	}
}
