package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/openclaw/openclaw-node-kobo/internal/canvas"
	"github.com/openclaw/openclaw-node-kobo/internal/eink"
	"github.com/openclaw/openclaw-node-kobo/internal/gateway"
	"github.com/openclaw/openclaw-node-kobo/internal/power"
	"github.com/openclaw/openclaw-node-kobo/internal/tailnet"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type FileConfig struct {
	Gateway        string `json:"gateway"`
	GatewayPort    int    `json:"gatewayPort,omitempty"`
	GatewayTLS     bool   `json:"gatewayTLS,omitempty"`
	GatewayPath    string `json:"gatewayPath,omitempty"`
	Name           string `json:"name"`
	StateDir       string `json:"stateDir,omitempty"`
	TouchDevice    string `json:"touchDevice,omitempty"`
	Framebuffer    string `json:"framebuffer,omitempty"`
	LogLevel       string `json:"logLevel,omitempty"`
	HTTPUserAgent  string `json:"httpUserAgent,omitempty"`
	IdleTimeoutMin *int   `json:"idleTimeoutMin,omitempty"`
	SuspendEnabled *bool  `json:"suspendEnabled,omitempty"`
}

func main() {
	cfgPath := flag.String("config", "config.json", "path to config file")
	gatewayHost := flag.String("gateway", "", "gateway hostname")
	gatewayPort := flag.Int("gateway-port", 0, "gateway port")
	gatewayTLS := flag.Bool("gateway-tls", false, "use TLS for gateway")
	gatewayPath := flag.String("gateway-path", "", "gateway websocket path")
	name := flag.String("name", "", "node name")
	stateDir := flag.String("state-dir", "", "tsnet state directory")
	touchDevice := flag.String("touch-device", "", "touch input device path")
	framebuffer := flag.String("framebuffer", "/dev/fb0", "framebuffer device path")
	logLevel := flag.String("log-level", "info", "log level")
	flag.Parse()

	cfg, err := loadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	applyOverrides(&cfg, *gatewayHost, *gatewayPort, *gatewayTLS, *gatewayPath, *name, *stateDir, *touchDevice, *framebuffer, *logLevel)
	setupLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if cfg.StateDir == "" {
		cfg.StateDir = filepath.Join(filepath.Dir(*cfgPath), "tsnet-state")
	}
	if cfg.GatewayPath == "" {
		cfg.GatewayPath = "/ws"
	}
	if cfg.GatewayPort == 0 {
		cfg.GatewayPort = 443
		if !cfg.GatewayTLS {
			cfg.GatewayPort = 80
		}
	}
	if cfg.Framebuffer == "" {
		cfg.Framebuffer = "/dev/fb0"
	}
	if cfg.Name == "" {
		fmt.Fprintln(os.Stderr, "config requires name")
		os.Exit(1)
	}
	if cfg.Gateway == "" {
		fmt.Fprintln(os.Stderr, "config requires gateway")
		os.Exit(1)
	}

	tail := tailnet.New(tailnet.Config{
		Hostname: cfg.Name,
		StateDir: cfg.StateDir,
		Logf:     log.Printf,
	})
	defer func() {
		_ = tail.Close()
	}()

	fb, err := eink.Open(cfg.Framebuffer)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to open framebuffer")
	}
	defer func() {
		_ = fb.Close()
	}()

	renderer := canvas.NewRenderer(fb.Width, fb.Height)

	wsURL := gatewayURL(cfg.GatewayTLS, cfg.Gateway, cfg.GatewayPort, cfg.GatewayPath)
	var handler *canvas.Handler
	readyState := newReadyState()
	powerManager := newPowerManager(cfg, *cfgPath, log.Logger)
	var client *gateway.Client
	client = gateway.New(gateway.Config{
		URL:      wsURL,
		Header:   http.Header{"User-Agent": {userAgent(cfg)}},
		Dialer:   tail.DialContext,
		Logger:   log.Logger,
		Register: gateway.DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req gateway.InvokeRequestParams) (interface{}, error) {
			if handler == nil {
				return nil, errors.New("handler not ready")
			}
			return handler.HandleInvokeRequest(ctx, canvas.InvokeRequest{Command: req.Command, Args: req.Args})
		},
		OnRegistered: func(ctx context.Context) error {
			return sendNodeReady(ctx, client, readyState.NextReason())
		},
	})
	handler = canvas.NewHandler(fb, renderer, client, log.Logger)
	handler.SetIdleResetter(powerManager.ResetIdle)
	handler.SetCommandProcessing(powerManager.SetCommandProcessing)

	powerManager.OnResume = func() {
		readyState.SetReason("wake")
		powerManager.SetWiFiConnecting(true)
		defer powerManager.SetWiFiConnecting(false)

		if err := runScript(context.Background(), filepath.Join(filepath.Dir(*cfgPath), "enable-wifi.sh")); err != nil {
			log.Warn().Err(err).Msg("failed to enable wifi")
		}
		waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := waitForIP(waitCtx, wifiInterface()); err != nil {
			log.Warn().Err(err).Msg("wifi did not acquire IP")
		}
		if err := handler.FullRefresh(); err != nil {
			log.Warn().Err(err).Msg("failed full refresh after wake")
		}
	}

	powerManager.OnSuspend = func() {
		if err := runScript(context.Background(), filepath.Join(filepath.Dir(*cfgPath), "disable-wifi.sh")); err != nil {
			log.Warn().Err(err).Msg("failed to disable wifi")
		}
	}

	if cfg.TouchDevice != "" {
		go startTouchLoop(ctx, cfg.TouchDevice, handler, powerManager, log.Logger, cancel)
	}
	if powerManager.SuspendEnabled && powerManager.IdleTimeout > 0 {
		go func() {
			if err := powerManager.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Warn().Err(err).Msg("power manager exited")
			}
		}()
	}

	if err := client.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal().Err(err).Msg("gateway client exited")
	}
}

func loadConfig(path string) (FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return FileConfig{}, nil
		}
		return FileConfig{}, err
	}
	var cfg FileConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return FileConfig{}, err
	}
	return cfg, nil
}

func applyOverrides(cfg *FileConfig, gatewayHost string, gatewayPort int, gatewayTLS bool, gatewayPath, name, stateDir, touchDevice, framebuffer, logLevel string) {
	if gatewayHost != "" {
		cfg.Gateway = gatewayHost
	}
	if gatewayPort != 0 {
		cfg.GatewayPort = gatewayPort
	}
	if gatewayPath != "" {
		cfg.GatewayPath = gatewayPath
	}
	if name != "" {
		cfg.Name = name
	}
	if stateDir != "" {
		cfg.StateDir = stateDir
	}
	if touchDevice != "" {
		cfg.TouchDevice = touchDevice
	}
	if framebuffer != "" {
		cfg.Framebuffer = framebuffer
	}
	if logLevel != "" {
		cfg.LogLevel = logLevel
	}
	cfg.GatewayTLS = gatewayTLS || cfg.GatewayTLS
}

func setupLogger(level string) {
	zerolog.TimeFieldFormat = time.RFC3339
	if parsed, err := zerolog.ParseLevel(level); err == nil {
		log.Logger = log.Level(parsed)
	}
}

func gatewayURL(tls bool, host string, port int, path string) string {
	scheme := "ws"
	if tls {
		scheme = "wss"
	}
	return fmt.Sprintf("%s://%s:%d%s", scheme, host, port, path)
}

func userAgent(cfg FileConfig) string {
	if cfg.HTTPUserAgent != "" {
		return cfg.HTTPUserAgent
	}
	return "openclaw-node-kobo/0.1"
}

func startTouchLoop(ctx context.Context, device string, handler *canvas.Handler, powerManager *power.Manager, logger zerolog.Logger, cancel context.CancelFunc) {
	input, err := eink.OpenInputDevice(device)
	if err != nil {
		logger.Warn().Err(err).Msg("failed to open touch device")
		return
	}
	defer func() {
		_ = input.Close()
	}()
	touchCh, powerCh, errCh := input.ReadEvents()

	var powerDownAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case touch, ok := <-touchCh:
			if !ok {
				return
			}
			if powerManager != nil {
				powerManager.ResetIdle()
			}
			if touch.Down {
				handler.HandleTouch(ctx, touch.X, touch.Y)
			}
		case powerEvent, ok := <-powerCh:
			if !ok {
				return
			}
			if powerEvent.Pressed {
				powerDownAt = powerEvent.At
			} else if !powerDownAt.IsZero() {
				duration := powerEvent.At.Sub(powerDownAt)
				powerDownAt = time.Time{}
				if duration >= 3*time.Second {
					logger.Info().Msg("power long press: exiting")
					cancel()
				} else {
					if powerManager == nil {
						continue
					}
					if err := powerManager.Suspend(); err != nil && !errors.Is(err, power.ErrSuspendBlocked) {
						logger.Warn().Err(err).Msg("failed to suspend")
					}
				}
			}
		case err, ok := <-errCh:
			if ok {
				logger.Warn().Err(err).Msg("input error")
			}
			return
		}
	}
}

func newPowerManager(cfg FileConfig, cfgPath string, logger zerolog.Logger) *power.Manager {
	idleTimeoutMin := 5
	if cfg.IdleTimeoutMin != nil {
		idleTimeoutMin = *cfg.IdleTimeoutMin
	}
	suspendEnabled := true
	if cfg.SuspendEnabled != nil {
		suspendEnabled = *cfg.SuspendEnabled
	}
	manager := &power.Manager{
		IdleTimeout:    time.Duration(idleTimeoutMin) * time.Minute,
		SuspendEnabled: suspendEnabled,
		WiFiScript:     filepath.Join(filepath.Dir(cfgPath), "enable-wifi.sh"),
	}
	if idleTimeoutMin <= 0 {
		manager.IdleTimeout = 0
	}
	if !suspendEnabled {
		logger.Info().Msg("suspend disabled by config")
	}
	return manager
}

func wifiInterface() string {
	if _, err := os.Stat("/sys/class/net/wlan0"); err == nil {
		return "wlan0"
	}
	return "eth0"
}

func waitForIP(ctx context.Context, ifaceName string) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if hasIP(ifaceName) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func hasIP(ifaceName string) bool {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return false
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		if ipNet.IP == nil {
			continue
		}
		return true
	}
	return false
}

func runScript(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type readyState struct {
	reason string
	mu     sync.Mutex
}

func newReadyState() *readyState {
	return &readyState{reason: "boot"}
}

func (r *readyState) SetReason(reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reason = reason
}

func (r *readyState) NextReason() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	reason := r.reason
	if reason != "reconnect" {
		r.reason = "reconnect"
	}
	return reason
}

func sendNodeReady(ctx context.Context, client *gateway.Client, reason string) error {
	if client == nil {
		return errors.New("gateway client not ready")
	}
	payload := gateway.EventParams{
		Event: "node.ready",
		Data: map[string]interface{}{
			"reason":    reason,
			"timestamp": time.Now().UnixMilli(),
		},
	}
	return client.SendEvent(ctx, "node.event", payload)
}
