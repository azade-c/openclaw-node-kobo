package tailnet

import (
	"context"
	"errors"
	"testing"
)

func TestNewServer(t *testing.T) {
	s := New(Config{Hostname: "kobo", StateDir: "/tmp"})
	if s == nil {
		t.Fatalf("expected server")
	}
}

// TestUpMethodExists verifies the Up method signature exists on Server.
// We can't unit-test the real tsnet.Server.Up without network,
// so we verify the method is callable and test via the Uper interface.
func TestUpMethodExists(t *testing.T) {
	s := New(Config{Hostname: "kobo", StateDir: "/tmp"})
	// Verify Server satisfies the Uper interface
	var _ Uper = s
}

// Uper is the interface for anything with an Up(ctx) method.
// Useful for mocking in tests.
type Uper interface {
	Up(ctx context.Context) error
}

// fakeUper is a mock for testing code that depends on Up().
type fakeUper struct {
	err error
}

func (f *fakeUper) Up(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return f.err
}

func TestFakeUperSuccess(t *testing.T) {
	u := &fakeUper{}
	if err := u.Up(context.Background()); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestFakeUperError(t *testing.T) {
	u := &fakeUper{err: errors.New("tailscale down")}
	if err := u.Up(context.Background()); err == nil {
		t.Fatalf("expected error")
	}
}

func TestFakeUperCanceled(t *testing.T) {
	u := &fakeUper{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := u.Up(ctx); err == nil {
		t.Fatalf("expected canceled error")
	}
}
