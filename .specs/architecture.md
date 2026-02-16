# openclaw-node-kobo — Architecture Spec

A lightweight Go binary that runs on Kobo e-readers as an OpenClaw **node**, providing **canvas-only** capability with direct framebuffer rendering.

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
│   ├── gateway/
│   │   ├── client.go          # WebSocket client, reconnect, auth
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

## E-ink Considerations

- **Refresh rate**: e-ink is slow; canvas updates should debounce/batch. A2UI push model works well since the gateway pushes updates and the node renders at its own pace.
- **Grayscale**: canvas rendering should be monochrome/grayscale. The `?platform=kobo` query param on the A2UI host URL can signal this to the gateway.
- **Snapshot format**: PNG preferred over JPEG (sharp text, no compression artifacts on grayscale).
- **Partial refresh**: use partial e-ink refresh for incremental A2UI updates; full refresh periodically to clear ghosting.

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
