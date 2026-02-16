# AGENTS.md

## Project overview

- Go module: `github.com/openclaw/openclaw-node-kobo`
- Binary: `openclaw-node-kobo`
- Target: Kobo Glo HD (armv7) with `/dev/fb0` and Linux evdev input
- Networking: embedded Tailscale via `tsnet` (no external `tailscaled`)
- Gateway: WebSocket JSON-RPC style (node registration + invoke requests)
- Rendering: A2UI JSON to grayscale framebuffer

## Key paths

- `cmd/openclaw-node-kobo/main.go` entrypoint
- `internal/tailnet/` tsnet wrapper
- `internal/gateway/` WebSocket JSON-RPC protocol and client
- `internal/canvas/` A2UI state, renderer, snapshot, command handler
- `internal/eink/` framebuffer, refresh ioctl, input events
- `start.sh`, `enable-wifi.sh`, `disable-wifi.sh` Kobo launcher and WiFi scripts

## Local dev

- Build: `make build`
- Cross-compile: `make build-arm`
- Test: `make test`

## Notes for changes

- Keep rendering grayscale and avoid heavy dependencies.
- Keep framebuffer writes and refreshes fast; prefer partial refresh for A2UI pushes.
- Touch input is raw evdev; Kobo uses 32-bit `timeval` structs.
- Follow the gateway JSON-RPC envelope in `internal/gateway/protocol.go`.
- Prefer ASCII-only edits unless file already uses Unicode.

## Hardware-specific behavior

- Power button long-press exits; short-press suspends via `/sys/power/state`.
- WiFi scripts are adapted from KOReader patterns and should stay shell-only.
