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

## WiFi

The node does **not** manage WiFi credentials. It reuses the networks already configured by the user in Nickel's stock UI.

### How it works

Nickel stores WiFi config in `/etc/wpa_supplicant/wpa_supplicant.conf`. When our node launches and Nickel is frozen, it brings WiFi up itself (same approach as KOReader):

1. **Power on** the WiFi chip (`insmod sdio_wifi_pwr.ko` or ioctl, device-dependent)
2. **Load** the WiFi kernel module (Broadcom `dhd` on Glo HD)
3. **Bring up** the interface (`ifconfig eth0 up` — older Kobos use `eth0`, not `wlan0`)
4. **Start** `wpa_supplicant` with Nickel's existing config → auto-connects to known networks
5. **tsnet** uses this WiFi connection to join the tailnet

At exit, the launcher script tears down WiFi cleanly before resuming Nickel.

**Reference**: KOReader's [`platform/kobo/enable-wifi.sh`](https://github.com/koreader/koreader/blob/master/platform/kobo/enable-wifi.sh) handles all Kobo WiFi chip variants. We can reuse or adapt this script.

### tsnet and kernel tun/tap

`tsnet` normally uses a userspace networking stack (no kernel tun/tap device needed). This is important because the Glo HD's kernel 3.0.35 may not have `CONFIG_TUN` enabled. To verify: check `/dev/net/tun` on the device. If absent, tsnet's userspace mode is the fallback — it works, but routes traffic through SOCKS/HTTP proxy internally.

## Power Management

E-ink displays retain their image without power. This is a key advantage for our node.

### Sleep/Wake Cycle

1. **Active** — node is running, WiFi on, gateway connected, canvas updating
2. **Sleep** — triggered by power button short press or inactivity timeout:
   - Node renders a final frame (last canvas state, clock, or status summary)
   - WiFi is powered down (saves battery)
   - System suspends (`echo mem > /sys/power/state`)
   - Display retains the last rendered image — zero power consumption
3. **Wake** — triggered by power button press:
   - System resumes from suspend
   - Node re-enables WiFi (re-runs enable-wifi sequence)
   - tsnet reconnects to tailnet
   - Gateway WebSocket reconnects
   - Canvas refreshes with any pending updates

### Power button handling

The power button is a Linux input event (`/dev/input/eventX`, `EV_KEY` code `KEY_POWER`). The node listens for:
- **Short press** → sleep/wake toggle
- **Long press (3s)** → exit node, return to Nickel

This is the hardware escape — always works even if the UI is unresponsive.

### Battery life

With WiFi off during sleep and e-ink retaining the display, battery life should be similar to normal Kobo usage (weeks of standby). Active use with WiFi will drain faster, but the Glo HD's battery handles hours of connected use.

## Deployment (NickelMenu)

The node is launched via **NickelMenu** — the standard way to run custom apps on Kobo. It follows the same lifecycle pattern as KOReader: freeze Nickel, take over, resume Nickel on exit.

### Installation

1. Install NickelMenu on the Kobo (one-time, via `KoboRoot.tgz` in `.kobo/`)
2. Place files in `/mnt/onboard/.adds/openclaw/`:
   ```
   /mnt/onboard/.adds/openclaw/
   ├── openclaw-node-kobo          # the Go binary
   ├── start.sh                    # launcher script
   ├── enable-wifi.sh              # WiFi bringup (adapted from KOReader)
   ├── disable-wifi.sh             # WiFi teardown
   ├── config.json                 # user config (gateway hostname, device name)
   ├── tsnet-state/                # Tailscale persistent state (auto-generated)
   └── logs/                       # crash logs
   ```
3. Add a NickelMenu entry in `/mnt/onboard/.adds/nm/openclaw`:
   ```
   menu_item :main :OpenClaw :cmd_spawn :quiet :/mnt/onboard/.adds/openclaw/start.sh
   ```
4. The user taps "OpenClaw" in the Kobo menu → node starts

### config.json

Minimal config — no secrets needed thanks to Tailscale auth:

```json
{
  "gateway": "azade.airplane-catfish.ts.net",
  "name": "kobo-glohd"
}
```

The gateway has `allowTailscale: true`, so the node authenticates via Tailscale identity alone. No gateway token required.

### Launcher script (start.sh)

```bash
#!/bin/sh
cd /mnt/onboard/.adds/openclaw

# Freeze Nickel (don't kill — we want to resume it later)
killall -STOP nickel

# Save Nickel's framebuffer for restoration
dd if=/dev/fb0 of=.nickel_screen.raw 2>/dev/null

# Bring up WiFi with Nickel's saved networks
./enable-wifi.sh

# Run the node
./openclaw-node-kobo 2>> logs/crash.log

# Tear down WiFi
./disable-wifi.sh

# Restore Nickel's screen
cat .nickel_screen.raw > /dev/fb0 2>/dev/null
rm -f .nickel_screen.raw

# Resume Nickel
killall -CONT nickel
```

### Exit gestures

Since the canvas is fullscreen, the node provides two ways to exit:

- **Long press power button (3s)** — hardware escape, always works
- **Swipe from top edge** — shows an overlay menu (Quit / Sleep / Refresh screen)

### Coexistence with Nickel

- Nickel is **frozen** (`SIGSTOP`), not killed — it resumes exactly where it was
- On reboot (battery death, crash, firmware update), Nickel starts normally; the node is not auto-launched
- Future option: auto-start at boot via init script (opt-in, once stable)

### First-time Tailscale auth

On first launch, tsnet has no state yet. The node:
1. Displays a Tailscale auth URL on the e-ink screen (and/or a QR code)
2. User scans/visits the URL on their phone → approves in Tailscale admin
3. tsnet saves its state to `tsnet-state/` — subsequent launches auto-connect

## Cross-Compilation

Kobo devices are ARM (armhf/armv7):

```makefile
build:
	GOOS=linux GOARCH=arm GOARM=7 go build -o openclaw-node-kobo ./cmd/openclaw-node-kobo
```

## Why Go

The language choice is driven by **tsnet** — Tailscale's Go library that embeds the full userspace networking stack into a single binary. Without Go, we'd need a separate `tailscaled` daemon plus coordination scripts.

Trade-offs vs KOReader's Lua/LuaJIT stack:
- **Binary size**: ~15-20 MB (Go + tsnet) vs ~2 MB (LuaJIT). Acceptable on 4 GB storage.
- **RAM**: Go runtime + GC uses ~10-30 MB on 512 MB. Fine.
- **ioctl/framebuffer**: LuaJIT FFI is more elegant, but Go's `syscall.Syscall` works. KOReader's mxcfb constants and structures serve as reference.
- **Networking/concurrency**: Go excels (goroutines, mature WebSocket libs). Lua is weaker here.

Go for application logic + KOReader's shell scripts for system plumbing = best of both worlds.

## References & Prior Art

### KOReader (primary reference)

[github.com/koreader/koreader](https://github.com/koreader/koreader) — GPL-3.0

The most mature custom app running on Kobo devices. Written in **Lua/LuaJIT** with C libraries (MuPDF, djvulibre, CREngine) and shell scripts for system integration.

**What we reuse from KOReader:**

| Component | How | Files |
|-----------|-----|-------|
| WiFi bringup/teardown | Copy/adapt shell scripts | [`platform/kobo/enable-wifi.sh`](https://github.com/koreader/koreader/blob/master/platform/kobo/enable-wifi.sh), `disable-wifi.sh` |
| Launcher pattern | Same approach (freeze Nickel, run, restore) | [`platform/kobo/koreader.sh`](https://github.com/koreader/koreader/blob/master/platform/kobo/koreader.sh) |
| mxcfb ioctl constants | Reference for Go reimplementation | [`ffi/framebuffer_mxcfb.lua`](https://github.com/koreader/koreader-base/blob/master/ffi/framebuffer_mxcfb.lua) |
| Framebuffer setup | Reference for Go reimplementation | [`ffi/framebuffer_linux.lua`](https://github.com/koreader/koreader-base/blob/master/ffi/framebuffer_linux.lua) |
| Input device mapping | Which `/dev/input/eventX` for which Kobo model | [`frontend/device/kobo/device.lua`](https://github.com/koreader/koreader/blob/master/frontend/device/kobo/device.lua) |
| Power management | Sleep/wake patterns, power button events | [`frontend/device/kobo/powerd.lua`](https://github.com/koreader/koreader/blob/master/frontend/device/kobo/powerd.lua) |
| Kobo model detection | Hardware ID → device capabilities | [`frontend/device/kobo/device.lua`](https://github.com/koreader/koreader/blob/master/frontend/device/kobo/device.lua) |

**What we don't reuse:**
- Lua/LuaJIT code (we're in Go)
- Rendering engine (we render A2UI components, not PDF/EPUB)
- C libraries (MuPDF, djvulibre — not needed)
- UI framework (KOReader's widget system)

### NickelMenu

[github.com/pgaskin/NickelMenu](https://github.com/pgaskin/NickelMenu) — MIT

Injects custom menu entries into Kobo's stock Nickel UI. Our launcher entry point. No code reused directly — we just register a `menu_item` that calls our `start.sh`.

### Tailscale tsnet

[pkg.go.dev/tailscale.com/tsnet](https://pkg.go.dev/tailscale.com/tsnet) — BSD-3-Clause

Go library to embed a Tailscale node into any Go program. Provides userspace networking without requiring a kernel tun/tap device or a separate `tailscaled` daemon.

### OpenClaw (gateway protocol reference)

[github.com/openclaw/openclaw](https://github.com/openclaw/openclaw)

The node WebSocket protocol is defined by the gateway implementation:
- `src/gateway/client.ts` — client-side WebSocket JSON-RPC
- `src/gateway/server-node-events.ts` — server-side node event handling
- `src/canvas-host/a2ui.ts` — A2UI component format and action payloads

### kobli.me

[kobli.me](https://kobli.me) — Kobo Clara hack project. Confirms Go cross-compilation works on Kobo ARM devices.

## Future Expansion

If shared Go code is extracted for other e-ink devices:
- `openclaw-node-go` — shared Go library (gateway client, protocol, framebuffer abstractions)
- `openclaw-node-kobo` / `openclaw-node-remarkable` — thin wrappers importing the shared lib
