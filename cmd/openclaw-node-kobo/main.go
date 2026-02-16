package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/openclaw/openclaw-node-kobo/internal/canvas"
	"github.com/openclaw/openclaw-node-kobo/internal/eink"
	"github.com/openclaw/openclaw-node-kobo/internal/gateway"
	"github.com/openclaw/openclaw-node-kobo/internal/tailnet"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type FileConfig struct {
	Gateway      string `json:"gateway"`
	GatewayPort  int    `json:"gatewayPort,omitempty"`
	GatewayTLS   bool   `json:"gatewayTLS,omitempty"`
	GatewayPath  string `json:"gatewayPath,omitempty"`
	Name         string `json:"name"`
	StateDir     string `json:"stateDir,omitempty"`
	TouchDevice  string `json:"touchDevice,omitempty"`
	Framebuffer  string `json:"framebuffer,omitempty"`
	LogLevel     string `json:"logLevel,omitempty"`
	HTTPUserAgent string `json:"httpUserAgent,omitempty"`
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
	client := gateway.New(gateway.Config{
		URL:    wsURL,
		Header: http.Header{"User-Agent": {userAgent(cfg)}},
		Dialer: tail.DialContext,
		Logger: log.Logger,
		Register: gateway.DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req gateway.InvokeRequestParams) (interface{}, error) {
			if handler == nil {
				return nil, errors.New("handler not ready")
			}
			return handler.HandleInvokeRequest(ctx, canvas.InvokeRequest{Command: req.Command, Args: req.Args})
		},
	})
	handler = canvas.NewHandler(fb, renderer, client, log.Logger)

	if cfg.TouchDevice != "" {
		go startTouchLoop(ctx, cfg.TouchDevice, handler, log.Logger, cancel)
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

func startTouchLoop(ctx context.Context, device string, handler *canvas.Handler, logger zerolog.Logger, cancel context.CancelFunc) {
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
			if touch.Down {
				handler.HandleTouch(ctx, touch.X, touch.Y)
			}
		case power, ok := <-powerCh:
			if !ok {
				return
			}
			if power.Pressed {
				powerDownAt = power.At
			} else if !powerDownAt.IsZero() {
				duration := power.At.Sub(powerDownAt)
				powerDownAt = time.Time{}
				if duration >= 3*time.Second {
					logger.Info().Msg("power long press: exiting")
					cancel()
				} else {
					if err := suspend(); err != nil {
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

func suspend() error {
	return os.WriteFile("/sys/power/state", []byte("mem"), 0)
}
