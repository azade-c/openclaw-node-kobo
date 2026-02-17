package power

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var ErrSuspendInProgress = errors.New("power: suspend already in progress")
var ErrSuspendBlocked = errors.New("power: suspend blocked")

type timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

type clock interface {
	Now() time.Time
	NewTimer(d time.Duration) timer
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func (systemClock) NewTimer(d time.Duration) timer {
	return &systemTimer{timer: time.NewTimer(d)}
}

type systemTimer struct {
	timer *time.Timer
}

func (t *systemTimer) C() <-chan time.Time {
	return t.timer.C
}

func (t *systemTimer) Stop() bool {
	return t.timer.Stop()
}

func (t *systemTimer) Reset(d time.Duration) bool {
	return t.timer.Reset(d)
}

type Manager struct {
	IdleTimeout    time.Duration
	SuspendEnabled bool
	OnSuspend      func()
	OnResume       func()

	clock        clock
	suspendFunc  func() error
	debounce     time.Duration
	initOnce     sync.Once
	idleMu       sync.Mutex
	idleTimer    timer
	suspending   atomic.Bool
	wifiBusy     atomic.Bool
	commandBusy  atomic.Bool
	lastWakeNano atomic.Int64
}

func (m *Manager) ResetIdle() {
	m.init()
	if !m.SuspendEnabled || m.IdleTimeout <= 0 {
		return
	}
	m.idleMu.Lock()
	defer m.idleMu.Unlock()
	if m.idleTimer == nil {
		m.idleTimer = m.clock.NewTimer(m.IdleTimeout)
		return
	}
	if !m.idleTimer.Stop() {
		drainTimer(m.idleTimer)
	}
	m.idleTimer.Reset(m.IdleTimeout)
}

func (m *Manager) Suspend() error {
	m.init()
	if !m.SuspendEnabled {
		return nil
	}
	if !m.suspending.CompareAndSwap(false, true) {
		return ErrSuspendInProgress
	}
	defer m.suspending.Store(false)
	if !m.canSuspend() {
		return ErrSuspendBlocked
	}
	if m.OnSuspend != nil {
		m.OnSuspend()
	}
	if err := m.suspendFunc(); err != nil {
		return err
	}
	m.lastWakeNano.Store(m.clock.Now().UnixNano())
	if m.OnResume != nil {
		m.OnResume()
	}
	m.ResetIdle()
	return nil
}

func (m *Manager) Run(ctx context.Context) error {
	m.init()
	if !m.SuspendEnabled || m.IdleTimeout <= 0 {
		<-ctx.Done()
		return ctx.Err()
	}
	m.idleMu.Lock()
	if m.idleTimer == nil {
		m.idleTimer = m.clock.NewTimer(m.IdleTimeout)
	}
	timer := m.idleTimer
	m.idleMu.Unlock()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C():
			_ = m.Suspend()
			m.ResetIdle()
		}
	}
}

func (m *Manager) SetWiFiConnecting(busy bool) {
	m.wifiBusy.Store(busy)
}

func (m *Manager) SetCommandProcessing(busy bool) {
	m.commandBusy.Store(busy)
}

func (m *Manager) canSuspend() bool {
	if m.wifiBusy.Load() || m.commandBusy.Load() {
		return false
	}
	lastWakeNano := m.lastWakeNano.Load()
	if lastWakeNano != 0 {
		lastWake := time.Unix(0, lastWakeNano)
		if m.clock.Now().Sub(lastWake) < m.debounce {
			return false
		}
	}
	return true
}

func (m *Manager) init() {
	m.initOnce.Do(func() {
		if m.clock == nil {
			m.clock = systemClock{}
		}
		if m.suspendFunc == nil {
			m.suspendFunc = suspendToRAM
		}
		if m.debounce == 0 {
			m.debounce = 30 * time.Second
		}
	})
}

func drainTimer(t timer) {
	for {
		select {
		case <-t.C():
			continue
		default:
			return
		}
	}
}

func suspendToRAM() error {
	return os.WriteFile("/sys/power/state", []byte("mem"), 0)
}
