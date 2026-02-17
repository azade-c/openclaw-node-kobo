# Sleep/Wake Lifecycle Spec

## Overview

The Kobo node must handle suspend-to-RAM (sleep) and resume (wake) gracefully, including WiFi recovery, gateway reconnection, and canvas refresh.

## Current State

- Power button short press → `echo mem > /sys/power/state` (blocking, returns on wake)
- No post-resume logic (WiFi stays dead, no screen refresh)
- No auto-sleep on inactivity
- Gateway WebSocket dies during sleep (60s read timeout server-side)
- Gateway does NOT queue or replay commands to offline nodes

## Requirements

### 1. Auto-sleep on inactivity

- After N minutes (configurable, default 5) without touch input, automatically suspend
- Reset the idle timer on any touch event
- Reset the idle timer on any canvas command received from gateway

### 2. Post-resume sequence

When `suspend()` returns (= device woke up):

1. **Re-enable WiFi** — run `enable-wifi.sh` (or equivalent Go calls)
2. **Wait for network** — poll for IP on `wlan0`, timeout 15s
3. **Wait for Tailscale** — call `tsnet.Server.Up(ctx)` which blocks until the Tailscale node is connected to the coordination server and has a working WireGuard tunnel. Timeout 30s. This avoids the gateway reconnect loop dialing into the void and accumulating backoff/error logs while Tailscale re-negotiates endpoints.
4. **Gateway reconnect** — now deterministic: tsnet is up, dial will succeed on first try
4. **Send `node.ready` event** — after successful re-registration, emit a `node.ready` event so the gateway/agent knows the node just woke up and can re-push canvas state
5. **Full screen refresh** — trigger a GC16 (full) e-ink refresh to clear ghosting artifacts from sleep

### 3. `node.ready` event

On every successful gateway registration (not just after sleep), the node should send:

```json
{
  "method": "node.event",
  "params": {
    "event": "node.ready",
    "data": {
      "reason": "wake" | "boot" | "reconnect",
      "timestamp": 1234567890
    }
  }
}
```

This lets the agent re-push the last canvas state. The gateway passes this through as a node event.

### 4. Suspend guard

Do NOT suspend if:
- WiFi is in the middle of connecting
- A canvas command is being processed
- The device just woke up less than 30s ago (debounce)

### 5. Power button behavior (unchanged)

- **Short press** → suspend (or wake if sleeping)
- **Long press (≥3s)** → clean exit (cancel context, restore Nickel)

## Implementation Plan

### New file: `internal/power/power.go`

```go
package power

type Manager struct {
    idleTimeout   time.Duration
    idleTimer     *time.Timer
    suspendGuard  atomic.Bool
    wifiScript    string  // path to enable-wifi.sh
    onSuspend     func()  // called before suspend
    onResume      func()  // called after resume
}

func (m *Manager) ResetIdle()          // call on touch or canvas command
func (m *Manager) Suspend() error      // manual suspend (power button)
func (m *Manager) Run(ctx context.Context) error  // auto-sleep loop
```

### Changes to `cmd/openclaw-node-kobo/main.go`

- Create `power.Manager` with callbacks
- `onSuspend`: disable WiFi (optional, saves power)
- `onResume`: enable WiFi, wait for IP, wait for Tailscale (`tsnet.Up()`), trigger screen refresh
- Wire idle reset into touch handler and canvas handler
- After gateway `registerNode`, send `node.ready` event with reason

### Changes to `internal/tailnet/tailnet.go`

- Add `Up(ctx context.Context) error` method wrapping `tsnet.Server.Up(ctx)`
- Blocks until Tailscale is connected (coordination server + WireGuard tunnel ready)
- Called in `OnResume` after `waitForIP()`, before letting the gateway reconnect proceed

### Changes to `internal/gateway/client.go`

- After successful `registerNode()`, call a `OnRegistered` callback (from Config)
- This callback sends the `node.ready` event

### Changes to `internal/canvas/handler.go`

- Add `ResetIdle()` call on every `HandleInvoke`
- Add `FullRefresh()` method that re-renders current state with GC16

### Config additions (`config.json`)

```json
{
  "idleTimeoutMin": 5,
  "suspendEnabled": true
}
```

## Testing

- Unit test `power.Manager` idle timer logic (mock clock)
- Unit test suspend guard prevents double-suspend
- Integration: manual test on Kobo hardware

## References

- KOReader suspend: `frontend/device/kobo/device.lua` → `Kobo:suspend()`
- Linux suspend: `Documentation/power/states.txt`
- WiFi recovery: `enable-wifi.sh` already in repo
