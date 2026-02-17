package power

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) NewTimer(d time.Duration) timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{
		clock:    c,
		c:        make(chan time.Time, 1),
		deadline: c.now.Add(d),
		active:   true,
	}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	timers := append([]*fakeTimer(nil), c.timers...)
	c.mu.Unlock()
	for _, t := range timers {
		t.fireIfDue(now)
	}
}

type fakeTimer struct {
	mu       sync.Mutex
	clock    *fakeClock
	c        chan time.Time
	deadline time.Time
	active   bool
}

func (t *fakeTimer) C() <-chan time.Time {
	return t.c
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	wasActive := t.active
	t.active = false
	return wasActive
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	wasActive := t.active
	t.active = true
	t.mu.Unlock()
	t.clock.mu.Lock()
	now := t.clock.now
	t.clock.mu.Unlock()
	t.mu.Lock()
	t.deadline = now.Add(d)
	t.mu.Unlock()
	drainChannel(t.c)
	return wasActive
}

func (t *fakeTimer) fireIfDue(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active || now.Before(t.deadline) {
		return
	}
	t.active = false
	select {
	case t.c <- now:
	default:
	}
}

func drainChannel(ch chan time.Time) {
	for {
		select {
		case <-ch:
			continue
		default:
			return
		}
	}
}

func TestManagerIdleTimer(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	suspendCh := make(chan struct{}, 1)
	m := &Manager{
		IdleTimeout:    5 * time.Second,
		SuspendEnabled: true,
		clock:          clock,
		suspendFunc: func() error {
			suspendCh <- struct{}{}
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- m.Run(ctx)
	}()

	m.ResetIdle()
	clock.Advance(4 * time.Second)
	select {
	case <-suspendCh:
		t.Fatalf("suspend fired early")
	default:
	}

	m.ResetIdle()
	clock.Advance(4 * time.Second)
	select {
	case <-suspendCh:
		t.Fatalf("suspend fired after idle reset")
	default:
	}

	clock.Advance(2 * time.Second)
	select {
	case <-suspendCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("suspend did not fire after idle timeout")
	}
	cancel()
	<-doneCh
}

func TestManagerSuspendGuard(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	m := &Manager{
		IdleTimeout:    time.Second,
		SuspendEnabled: true,
		clock:          clock,
		suspendFunc:    func() error { return nil },
	}
	m.SetWiFiConnecting(true)
	if err := m.Suspend(); !errors.Is(err, ErrSuspendBlocked) {
		t.Fatalf("expected suspend blocked, got %v", err)
	}
	m.SetWiFiConnecting(false)
	m.SetCommandProcessing(true)
	if err := m.Suspend(); !errors.Is(err, ErrSuspendBlocked) {
		t.Fatalf("expected suspend blocked for command, got %v", err)
	}
}

func TestManagerSuspendDebounce(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	m := &Manager{
		IdleTimeout:    time.Second,
		SuspendEnabled: true,
		clock:          clock,
		suspendFunc:    func() error { return nil },
	}
	m.lastWakeNano.Store(clock.Now().UnixNano())
	if err := m.Suspend(); !errors.Is(err, ErrSuspendBlocked) {
		t.Fatalf("expected debounce suspend blocked, got %v", err)
	}
}

func TestManagerSuspendCallbacks(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	var order []string
	m := &Manager{
		IdleTimeout:    time.Second,
		SuspendEnabled: true,
		clock:          clock,
		suspendFunc: func() error {
			order = append(order, "suspend")
			return nil
		},
	}
	m.OnSuspend = func() {
		order = append(order, "onSuspend")
	}
	m.OnResume = func() {
		order = append(order, "onResume")
	}
	if err := m.Suspend(); err != nil {
		t.Fatalf("expected suspend to succeed, got %v", err)
	}
	want := []string{"onSuspend", "suspend", "onResume"}
	if len(order) != len(want) {
		t.Fatalf("expected %v callbacks, got %v", want, order)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("expected callback order %v, got %v", want, order)
		}
	}
}

func TestManagerSuspendInProgress(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	blockCh := make(chan struct{})
	m := &Manager{
		IdleTimeout:    time.Second,
		SuspendEnabled: true,
		clock:          clock,
		suspendFunc: func() error {
			<-blockCh
			return nil
		},
	}
	go func() {
		_ = m.Suspend()
	}()
	if !waitForSuspendState(m, true, 500*time.Millisecond) {
		t.Fatalf("suspend did not start")
	}
	if err := m.Suspend(); !errors.Is(err, ErrSuspendInProgress) {
		t.Fatalf("expected suspend in progress, got %v", err)
	}
	close(blockCh)
}

func TestManagerResumeReconnectSequence(t *testing.T) {
	clock := newFakeClock(time.Unix(1, 0))
	var order []string
	m := &Manager{
		IdleTimeout:    time.Second,
		SuspendEnabled: true,
		clock:          clock,
		suspendFunc:    func() error { return nil },
	}
	m.OnResume = func() {
		// Gateway reconnect happens when the WS read loop exits and Run() retries.
		order = append(order, "wifi-enable", "tailscale-up", "gateway-reconnect")
	}
	if err := m.Suspend(); err != nil {
		t.Fatalf("expected suspend to succeed, got %v", err)
	}
	want := []string{"wifi-enable", "tailscale-up", "gateway-reconnect"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("expected resume sequence %v, got %v", want, order)
	}
}

func waitForSuspendState(m *Manager, want bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.suspending.Load() == want {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
