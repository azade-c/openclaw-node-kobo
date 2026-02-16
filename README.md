# openclaw-node-kobo

Go-based OpenClaw node for Kobo e-readers. It connects to the OpenClaw gateway over Tailscale (tsnet), renders A2UI components directly to the e-ink framebuffer, and exposes canvas-only commands.

## Features

- Tailscale embedded networking via `tsnet`
- WebSocket JSON-RPC gateway client (node registration + invoke handling)
- A2UI renderer to grayscale framebuffer
- E-ink refresh via mxcfb ioctl (partial and fast update)
- Touchscreen input via Linux evdev
- Kobo-ready launcher and WiFi scripts (KOReader style)

## Build

```sh
make build
```

### Cross-compile for Kobo (armv7)

```sh
make build-arm
```

## Config

Create `/mnt/onboard/.adds/openclaw/config.json`:

```json
{
  "gateway": "azade.airplane-catfish.ts.net",
  "name": "kobo-glohd",
  "touchDevice": "/dev/input/event1"
}
```

Optional fields:

- `gatewayPort` (default 80 or 443 if `gatewayTLS` is true)
- `gatewayTLS` (default false)
- `gatewayPath` (default `/ws`)
- `stateDir` (default `./tsnet-state`)
- `framebuffer` (default `/dev/fb0`)

## Install (Kobo)

Copy files to `/mnt/onboard/.adds/openclaw/`:

```
openclaw-node-kobo
start.sh
enable-wifi.sh
disable-wifi.sh
config.json
```

Add a NickelMenu entry at `/mnt/onboard/.adds/nm/openclaw`:

```
menu_item :main :OpenClaw :cmd_spawn :quiet :/mnt/onboard/.adds/openclaw/start.sh
```

## Commands

This node registers canvas-only commands:

- `canvas.present`
- `canvas.hide`
- `canvas.navigate` (returns error)
- `canvas.eval` (returns error)
- `canvas.snapshot`
- `canvas.a2ui.push`
- `canvas.a2ui.pushJSONL`
- `canvas.a2ui.reset`

## A2UI Rendering

A2UI components are rendered into an 8bpp grayscale `image.Gray` and copied to `/dev/fb0`. Supported components:

- `text`
- `box`
- `card`
- `button`
- `list` (simple vertical stacking)

Interactive components can include an `action` payload. Touch events hit-test against rendered components and send `canvas.a2ui.action` events to the gateway.

## Tests

```sh
make test
```

## Notes

- The Kobo kernel is 32-bit; input event parsing uses 32-bit `timeval` sizes.
- `tsnet` stores state in `tsnet-state/` to avoid repeated auth.
- E-ink refresh uses mxcfb ioctl values derived from KOReader references.
