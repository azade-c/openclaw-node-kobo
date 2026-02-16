# openclaw-node-kobo — Architecture Spec

A lightweight Go binary that runs on Kobo e-readers as an OpenClaw **node**, providing **canvas-only** capability with direct framebuffer rendering.

## Target Hardware

- **Device**: Kobo Glo HD (2015)
- **SoC**: Freescale i.MX6 SoloLite (ARM Cortex-A9, 1 GHz)
- **RAM**: 512 MB
- **Storage**: 4 GB internal (microSD, replaceable)
- **Display**: 6" e-ink 1448×1072 (300 ppi), 8bpp grayscale
- **Touch**: Capacitive touchscreen
- **WiFi**: Broadcom 43362
- **OS**: Linux (kernel 3.0.35)
- **Stock software**: Nickel (Qt-based reading app)

## Naming Convention

- **Repo**: `github.com/openclaw/openclaw-node-kobo`
- **Go module**: `github.com/openclaw/openclaw-node-kobo`
- **Binary**: `openclaw-node-kobo`
- **Pattern**: `openclaw-node-{device}` (scales to `openclaw-node-remarkable`, `openclaw-node-kindle`, etc.)

## What is a Node?

A **node** is a device that connects to the OpenClaw **gateway** via WebSocket and registers:

1. **Role**: `"node"`
2. **Capabilities** (`caps`): what the device can do
3. **Commands** (`commands`): specific invocable operations

The gateway dispatches `node.invoke.request` events to connected nodes, and the node responds with `node.invoke.result`.

## Kobo Node Registration

This node advertises canvas-only capability:

```json
{
  "role": "node",
  "caps": ["canvas"],
  "commands": [
    "canvas.present",
    "canvas.hide",
    "canvas.navigate",
    "canvas.eval",
    "canvas.snapshot",
    "canvas.a2ui.push",
    "canvas.a2ui.pushJSONL",
    "canvas.a2ui.reset"
  ]
}
```

## Project Structure

```
openclaw-node-kobo/
├── cmd/
│   └── openclaw-node-kobo/
│       └── main.go            # entry point, CLI flags (gateway host/port/tls)
├── internal/
│   ├── tailnet/
│   │   └── tailnet.go         # tsnet setup, auth, persistent state
│   ├── gateway/
│   │   ├── client.go          # WebSocket client over tailnet, reconnect, auth
│   │   ├── protocol.go        # JSON frame types (invoke request/result, events)
│   │   └── node.go            # node registration (role, caps, commands)
│   ├── canvas/
│   │   ├── handler.go         # dispatch canvas.* invoke commands
│   │   ├── renderer.go        # HTML → bitmap (headless rendering)
│   │   ├── snapshot.go        # framebuffer → PNG/JPEG base64
│   │   └── a2ui.go            # A2UI push/reset state management
│   └── eink/
│       ├── framebuffer.go     # /dev/fb0 mmap, write pixels
│       ├── refresh.go         # e-ink refresh ioctl (full/partial/fast)
│       └── input.go           # touchscreen input events (optional, for A2UI interaction)
├── go.mod
├── go.sum
├── Makefile                   # cross-compile for ARM (Kobo is armhf/armv7)
├── README.md
└── AGENTS.md
```

## Network: Tailscale via tsnet

The Kobo connects to the OpenClaw gateway **through Tailscale**, not over the public internet. The binary embeds [`tsnet`](https://pkg.go.dev/tailscale.com/tsnet) to join the tailnet directly:

- No port forwarding, no public gateway exposure
- The Kobo appears as a device on the tailnet (e.g. `kobo-glohd`)
- WebSocket connection to the gateway uses the Tailscale IP/hostname
- First-time auth: `tsnet` generates an auth URL; user approves in Tailscale admin
- Subsequent launches: auto-reconnects (state stored on device)

Single binary, ~15 Mo cross-compiled, includes the full Tailscale userspace networking stack.

## Gateway Protocol

Reimplement the thin WebSocket JSON-RPC that `GatewayClient` in the main openclaw TypeScript repo uses. Reference implementation: `src/gateway/client.ts` in `github.com/openclaw/openclaw`.

## Canvas Commands

| Command | Description |
|---------|-------------|
| `canvas.present` | Show the canvas on the e-ink display |
| `canvas.hide` | Clear/dismiss the canvas |
| `canvas.navigate` | Load a URL (limited use on e-ink; primary path is A2UI) |
| `canvas.eval` | Run JS in the rendering context (if browser-based) |
| `canvas.snapshot` | Capture current display as PNG base64 |
| `canvas.a2ui.push` | Push A2UI component update |
| `canvas.a2ui.pushJSONL` | Push batch A2UI updates (JSONL) |
| `canvas.a2ui.reset` | Reset A2UI state |

## HTML Rendering Strategy

Options (in order of preference for Kobo):

1. **Native A2UI renderer** — Since the primary use case is A2UI (structured UI components: text, lists, cards), render A2UI JSON directly to pixels using a Go 2D graphics library (`gg`, `freetype`). Skip the browser entirely. Fast, lightweight, e-ink optimized.

2. **Hybrid** — Native A2UI renderer for fast e-ink rendering, optional lightweight WebKit fallback for `canvas.navigate` (arbitrary URLs).

3. **go-rod / chromedp** — Headless Chrome. Too heavy for Kobo; avoid unless absolutely necessary.

## Framebuffer Rendering

Kobo e-readers expose `/dev/fb0` on an i.MX SoC. The rendering flow:

1. Render to an in-memory image (`image.Gray` for e-ink grayscale)
2. Write pixels to the mmap'd framebuffer
3. Trigger e-ink refresh via ioctl (`MXCFB_SEND_UPDATE`)

## Snapshot

Read back from the in-memory image (or framebuffer), encode to PNG, return base64.

## Touchscreen Input

The Kobo Glo HD has a capacitive touchscreen exposed via Linux input events (`/dev/input/eventX`). The node reads touch events and maps them to A2UI actions:

1. Read `EV_ABS` events (x, y coordinates) from the input device
2. Hit-test against the current A2UI component tree (buttons, links, interactive elements)
3. On match, send the action to the gateway via `openclawCanvasA2UIAction` (same action payload as iOS/Android nodes use via their WebView bridge)

This enables interactive A2UI interfaces: tap buttons, scroll lists, navigate.

**Reference**: iOS/Android nodes use `window.webkit.messageHandlers.openclawCanvasA2UIAction.postMessage(...)` in their WebView. The Kobo node produces the same action payloads, just from raw touch coordinates instead of DOM events.

## E-ink Considerations

- **Refresh rate**: e-ink is slow; canvas updates should debounce/batch. A2UI push model works well since the gateway pushes updates and the node renders at its own pace.
- **Grayscale**: canvas rendering should be monochrome/grayscale. The `?platform=kobo` query param on the A2UI host URL can signal this to the gateway.
- **Snapshot format**: PNG preferred over JPEG (sharp text, no compression artifacts on grayscale).
- **Partial refresh**: use partial e-ink refresh for incremental A2UI updates; full refresh periodically to clear ghosting.

## Deployment (NickelMenu)

Initial deployment uses **NickelMenu** — the standard way to launch custom apps on Kobo alongside the stock Nickel firmware.

### Installation

1. Install NickelMenu on the Kobo (one-time, via `.adds/nm/` on the SD card)
2. Place the binary and config in `/mnt/onboard/.adds/openclaw/`:
   ```
   /mnt/onboard/.adds/openclaw/
   ├── openclaw-node-kobo          # the Go binary
   ├── start.sh                    # launcher script (WiFi check, env setup)
   └── tsnet-state/                # Tailscale persistent state
   ```
3. Add a NickelMenu entry:
   ```
   menu_item :main :OpenClaw :cmd_spawn :quiet :/mnt/onboard/.adds/openclaw/start.sh
   ```
4. The user taps "OpenClaw" in the Kobo menu → the node starts, joins tailnet, connects to gateway

### Coexistence with Nickel

- The node runs as a background process alongside Nickel
- When actively displaying canvas content, it writes to the framebuffer (takes over the display)
- `canvas.hide` restores Nickel's display
- Future: auto-start at boot via init script (once stable)

## Cross-Compilation

Kobo devices are ARM (armhf/armv7):

```makefile
build:
	GOOS=linux GOARCH=arm GOARM=7 go build -o openclaw-node-kobo ./cmd/openclaw-node-kobo
```

## Future Expansion

If shared Go code is extracted for other e-ink devices:
- `openclaw-node-go` — shared Go library (gateway client, protocol, framebuffer abstractions)
- `openclaw-node-kobo` / `openclaw-node-remarkable` — thin wrappers importing the shared lib
