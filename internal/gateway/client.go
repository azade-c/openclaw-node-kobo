package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

type DialContextFunc func(ctx context.Context, network, addr string) (net.Conn, error)

type InvokeHandler func(ctx context.Context, req InvokeRequestParams) (interface{}, error)

type wsConn interface {
	WriteMessage(messageType int, data []byte) error
	ReadMessage() (messageType int, p []byte, err error)
	SetWriteDeadline(t time.Time) error
	SetReadDeadline(t time.Time) error
	SetReadLimit(limit int64)
	SetPongHandler(h func(appData string) error)
	Close() error
}

var errGatewayShutdown = errors.New("gateway: shutdown")

type Client struct {
	url             string
	header          http.Header
	dialer          DialContextFunc
	logger          zerolog.Logger
	register        NodeRegistration
	onInvoke        InvokeHandler
	onRegistered    func(context.Context) error
	connectAuth     *ConnectAuth
	identity        *DeviceIdentity
	deviceToken     string
	deviceTokenPath string
	connMu          sync.Mutex
	conn            wsConn
	writeMu         sync.Mutex
	requestSeq      atomic.Uint64
	pingInterval    time.Duration
}

type backoffProvider interface {
	Backoff() time.Duration
}

type backoffError struct {
	err     error
	backoff time.Duration
}

func (e backoffError) Error() string {
	return e.err.Error()
}

func (e backoffError) Unwrap() error {
	return e.err
}

func (e backoffError) Backoff() time.Duration {
	return e.backoff
}

type Config struct {
	URL             string
	Header          http.Header
	Dialer          DialContextFunc
	Logger          zerolog.Logger
	Register        NodeRegistration
	OnInvoke        InvokeHandler
	OnRegistered    func(context.Context) error
	PingInterval    time.Duration
	AuthToken       string
	AuthPassword    string
	Identity        *DeviceIdentity
	DeviceTokenPath string
}

func New(cfg Config) *Client {
	pingInterval := cfg.PingInterval
	if pingInterval == 0 {
		pingInterval = 30 * time.Second
	}
	var connectAuth *ConnectAuth
	if cfg.AuthToken != "" || cfg.AuthPassword != "" {
		connectAuth = &ConnectAuth{
			Token:    cfg.AuthToken,
			Password: cfg.AuthPassword,
		}
	}
	deviceToken := ""
	if cfg.DeviceTokenPath != "" {
		token, err := LoadDeviceToken(cfg.DeviceTokenPath)
		if err != nil {
			cfg.Logger.Warn().Err(err).Msg("gateway: failed to load device token")
		} else {
			deviceToken = token
		}
	}
	return &Client{
		url:             cfg.URL,
		header:          cfg.Header,
		dialer:          cfg.Dialer,
		logger:          cfg.Logger,
		register:        cfg.Register,
		onInvoke:        cfg.OnInvoke,
		onRegistered:    cfg.OnRegistered,
		connectAuth:     connectAuth,
		identity:        cfg.Identity,
		deviceToken:     deviceToken,
		deviceTokenPath: cfg.DeviceTokenPath,
		pingInterval:    pingInterval,
	}
}

func (c *Client) Run(ctx context.Context) error {
	if c.dialer == nil {
		return errors.New("gateway: dialer required")
	}
	if c.onInvoke == nil {
		return errors.New("gateway: invoke handler required")
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		conn, err := c.connect(ctx)
		if err != nil {
			c.logger.Warn().Err(err).Msg("gateway connect failed")
			if err := c.waitBackoff(ctx, &backoff); err != nil {
				return err
			}
			continue
		}
		c.setConn(conn)
		if err := c.registerNode(ctx); err != nil {
			c.logger.Error().Err(err).Msg("gateway registration failed")
			c.closeConn()
			c.applyBackoffOverride(err, &backoff)
			if err := c.waitBackoff(ctx, &backoff); err != nil {
				return err
			}
			continue
		}
		backoff = time.Second
		if c.onRegistered != nil {
			if err := c.onRegistered(ctx); err != nil {
				c.logger.Warn().Err(err).Msg("gateway registered callback failed")
			}
		}
		if err := c.readLoop(ctx); err != nil {
			c.logger.Warn().Err(err).Msg("gateway read loop ended")
			c.closeConn()
			c.applyBackoffOverride(err, &backoff)
			if err := c.waitBackoff(ctx, &backoff); err != nil {
				return err
			}
			continue
		}
	}
}

func (c *Client) SendEvent(ctx context.Context, method string, params interface{}) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	req := RequestFrame{
		Type:   "req",
		ID:     c.nextID(),
		Method: method,
		Params: payload,
	}
	return c.sendFrame(ctx, req)
}

func (c *Client) sendFrame(ctx context.Context, frame interface{}) error {
	conn := c.getConn()
	if conn == nil {
		return errors.New("gateway: no connection")
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return c.writeMessage(conn, websocket.TextMessage, data)
}

func (c *Client) writeMessage(conn wsConn, messageType int, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return conn.WriteMessage(messageType, data)
}

func (c *Client) connect(ctx context.Context) (wsConn, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		HandshakeTimeout: 10 * time.Second,
		NetDialContext:   c.dialer,
	}
	conn, _, err := dialer.DialContext(ctx, c.url, c.header)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return conn, nil
}

func (c *Client) registerNode(ctx context.Context) error {
	conn := c.getConn()
	if conn == nil {
		return errors.New("gateway: no connection")
	}
	nonce := ""
	connectSent := false
	connectID := ""
	sendConnect := func(nonce string) error {
		req, err := c.buildConnectRequest(nonce)
		if err != nil {
			return err
		}
		if err := c.sendFrame(ctx, req); err != nil {
			return err
		}
		connectSent = true
		connectID = req.ID
		return nil
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return c.handleCloseError(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			c.logger.Warn().Err(err).Msg("gateway: invalid handshake message")
			continue
		}
		if base.Type == "event" {
			var evt EventFrame
			if err := json.Unmarshal(data, &evt); err != nil {
				c.logger.Warn().Err(err).Msg("gateway: invalid handshake event")
				continue
			}
			switch evt.Event {
			case "connect.challenge":
				var payload struct {
					Nonce string `json:"nonce"`
				}
				if err := json.Unmarshal(evt.Payload, &payload); err != nil {
					c.logger.Warn().Err(err).Msg("gateway: invalid connect challenge")
					continue
				}
				if payload.Nonce == "" || payload.Nonce == nonce {
					continue
				}
				nonce = payload.Nonce
				if !connectSent {
					if err := sendConnect(nonce); err != nil {
						return err
					}
				}
			case "tick":
				c.logger.Debug().Msg("gateway: tick")
			default:
			}
			continue
		}
		if base.Type != "res" {
			continue
		}
		var res ResponseFrame
		if err := json.Unmarshal(data, &res); err != nil {
			c.logger.Warn().Err(err).Msg("gateway: invalid handshake response")
			continue
		}
		if !connectSent || res.ID != connectID {
			continue
		}
		if !res.OK {
			if res.Error != nil && res.Error.Message != "" {
				return errors.New(res.Error.Message)
			}
			return errors.New("gateway: connect rejected")
		}
		var hello HelloOkPayload
		if err := json.Unmarshal(res.Payload, &hello); err != nil {
			return err
		}
		if hello.Type != "hello-ok" {
			return errors.New("gateway: unexpected handshake payload")
		}
		if hello.Auth != nil && hello.Auth.DeviceToken != "" {
			c.deviceToken = hello.Auth.DeviceToken
			if c.deviceTokenPath != "" {
				if err := SaveDeviceToken(c.deviceTokenPath, c.deviceToken); err != nil {
					c.logger.Warn().Err(err).Msg("gateway: failed to save device token")
				}
			}
		}
		return nil
	}
}

func (c *Client) readLoop(ctx context.Context) error {
	conn := c.getConn()
	if conn == nil {
		return errors.New("gateway: no connection")
	}
	done := make(chan struct{})
	go c.pingLoop(ctx, conn, done)
	defer close(done)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, data, err := conn.ReadMessage()
		if err != nil {
			return c.handleCloseError(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		var base struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &base); err != nil {
			c.logger.Warn().Err(err).Msg("gateway: invalid frame")
			continue
		}
		switch base.Type {
		case "event":
			var evt EventFrame
			if err := json.Unmarshal(data, &evt); err != nil {
				c.logger.Warn().Err(err).Msg("gateway: invalid event frame")
				continue
			}
			switch evt.Event {
			case "node.invoke.request":
				if err := c.handleInvokeEvent(ctx, evt); err != nil {
					c.logger.Warn().Err(err).Msg("gateway: invoke handler error")
				}
			case "shutdown":
				var payload ShutdownPayload
				if err := json.Unmarshal(evt.Payload, &payload); err != nil {
					c.logger.Warn().Err(err).Msg("gateway: invalid shutdown payload")
					return err
				}
				restartMs := payload.RestartExpectedMs
				if restartMs <= 0 {
					restartMs = int(time.Second / time.Millisecond)
				}
				c.logger.Info().Str("reason", payload.Reason).Msg(fmt.Sprintf("gateway shutting down, reconnect in %dms", restartMs))
				return backoffError{err: errGatewayShutdown, backoff: time.Duration(restartMs) * time.Millisecond}
			case "tick":
				c.logger.Debug().Msg("gateway: tick")
				continue
			case "connect.challenge", "voicewake.changed":
				continue
			}
		case "req":
			var req RequestFrame
			if err := json.Unmarshal(data, &req); err != nil {
				c.logger.Warn().Err(err).Msg("gateway: invalid request frame")
				continue
			}
			if req.Method == "node.invoke.request" {
				if err := c.handleInvokeRequest(ctx, req); err != nil {
					c.logger.Warn().Err(err).Msg("gateway: invoke handler error")
				}
			}
		case "res":
			continue
		}
	}
}

func (c *Client) handleInvokeEvent(ctx context.Context, evt EventFrame) error {
	params, err := parseInvokePayload(evt.Payload)
	if err != nil {
		return err
	}
	return c.handleInvoke(ctx, params)
}

func (c *Client) handleInvokeRequest(ctx context.Context, req RequestFrame) error {
	params, err := parseInvokePayload(req.Params)
	if err != nil {
		return err
	}
	return c.handleInvoke(ctx, params)
}

func (c *Client) handleInvoke(ctx context.Context, params InvokeRequestParams) error {
	result, err := c.onInvoke(ctx, params)
	return c.sendInvokeResult(ctx, params, result, err)
}

func (c *Client) sendInvokeResult(ctx context.Context, req InvokeRequestParams, result interface{}, err error) error {
	params := InvokeResultParams{
		RequestID: req.RequestID,
		NodeID:    req.NodeID,
		OK:        err == nil,
		Result:    result,
	}
	if err != nil {
		params.Error = &NodeInvokeError{Message: err.Error()}
	}
	payload, marshalErr := json.Marshal(params)
	if marshalErr != nil {
		return marshalErr
	}
	frame := RequestFrame{
		Type:   "req",
		ID:     c.nextID(),
		Method: "node.invoke.result",
		Params: payload,
	}
	return c.sendFrame(ctx, frame)
}

func parseInvokePayload(raw json.RawMessage) (InvokeRequestParams, error) {
	var payload struct {
		ID             string          `json:"id"`
		NodeID         string          `json:"nodeId"`
		Command        string          `json:"command"`
		ParamsJSON     *string         `json:"paramsJSON,omitempty"`
		Params         json.RawMessage `json:"params,omitempty"`
		IdempotencyKey string          `json:"idempotencyKey,omitempty"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return InvokeRequestParams{}, err
	}
	if payload.ID == "" || payload.NodeID == "" || payload.Command == "" {
		return InvokeRequestParams{}, errors.New("gateway: invalid invoke payload")
	}
	var args json.RawMessage
	if payload.ParamsJSON != nil && *payload.ParamsJSON != "" {
		args = json.RawMessage([]byte(*payload.ParamsJSON))
	} else if len(payload.Params) > 0 {
		args = payload.Params
	}
	return InvokeRequestParams{
		RequestID: payload.ID,
		NodeID:    payload.NodeID,
		Command:   payload.Command,
		Args:      args,
	}, nil
}

func (c *Client) pingLoop(ctx context.Context, conn wsConn, done <-chan struct{}) {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if err := c.writeMessage(conn, websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) nextID() string {
	val := c.requestSeq.Add(1)
	seed := rand.Int63n(9999)
	return fmt.Sprintf("%d-%d", val, seed)
}

func (c *Client) getConn() wsConn {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.conn
}

func (c *Client) setConn(conn wsConn) {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	c.conn = conn
}

func (c *Client) closeConn() {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) waitBackoff(ctx context.Context, backoff *time.Duration) error {
	timer := time.NewTimer(*backoff)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
	}
	if *backoff < 30*time.Second {
		*backoff *= 2
	}
	return nil
}

func (c *Client) applyBackoffOverride(err error, backoff *time.Duration) {
	var override backoffProvider
	if !errors.As(err, &override) {
		return
	}
	target := override.Backoff()
	if target <= 0 {
		return
	}
	if errors.Is(err, errGatewayShutdown) {
		*backoff = target
		return
	}
	if *backoff < target {
		*backoff = target
	}
}

func (c *Client) selectConnectAuth() (*ConnectAuth, string) {
	if c.connectAuth != nil {
		auth := *c.connectAuth
		if auth.Token != "" {
			return &auth, auth.Token
		}
		if auth.Password != "" {
			if c.deviceToken == "" {
				return &auth, ""
			}
		}
	}
	if c.deviceToken != "" {
		password := ""
		if c.connectAuth != nil {
			password = c.connectAuth.Password
		}
		if password != "" {
			return &ConnectAuth{Token: c.deviceToken, Password: password}, c.deviceToken
		}
		return &ConnectAuth{Token: c.deviceToken}, c.deviceToken
	}
	if c.connectAuth != nil && c.connectAuth.Password != "" {
		auth := *c.connectAuth
		return &auth, ""
	}
	return nil, ""
}

func (c *Client) buildConnectRequest(nonce string) (RequestFrame, error) {
	id := c.nextID()
	auth, tokenForPayload := c.selectConnectAuth()
	var deviceInfo *DeviceInfo
	if c.identity != nil {
		signedAtMs := time.Now().UnixMilli()
		payload := BuildDeviceAuthPayload(
			c.identity.DeviceID,
			c.register.Client.ID,
			c.register.Client.Mode,
			c.register.Role,
			c.register.Scopes,
			signedAtMs,
			tokenForPayload,
			nonce,
		)
		deviceInfo = &DeviceInfo{
			ID:        c.identity.DeviceID,
			PublicKey: c.identity.PublicKeyRawBase64Url(),
			Signature: c.identity.Sign(payload),
			SignedAt:  signedAtMs,
			Nonce:     nonce,
		}
	}
	params, err := json.Marshal(ConnectParams{
		MinProtocol: ProtocolVersion,
		MaxProtocol: ProtocolVersion,
		Client:      c.register.Client,
		Role:        c.register.Role,
		Caps:        c.register.Caps,
		Commands:    c.register.Commands,
		Permissions: c.register.Permissions,
		PathEnv:     c.register.PathEnv,
		Scopes:      c.register.Scopes,
		Auth:        auth,
		Device:      deviceInfo,
		Locale:      c.register.Locale,
		UserAgent:   c.register.UserAgent,
	})
	if err != nil {
		return RequestFrame{}, err
	}
	return RequestFrame{
		Type:   "req",
		ID:     id,
		Method: "connect",
		Params: params,
	}, nil
}

func (c *Client) handleCloseError(err error) error {
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		return err
	}
	if closeErr.Code != websocket.ClosePolicyViolation {
		return err
	}
	reason := strings.ToLower(closeErr.Text)
	if strings.Contains(reason, "pairing required") {
		c.logger.Warn().Msg("pairing required — waiting for approval")
		return backoffError{err: err, backoff: 10 * time.Second}
	}
	if strings.Contains(reason, "device identity required") {
		c.logger.Warn().Msg("device identity required — waiting for approval")
		return backoffError{err: err, backoff: 10 * time.Second}
	}
	if strings.Contains(reason, "device token mismatch") {
		c.clearDeviceToken()
	}
	return err
}

func (c *Client) clearDeviceToken() {
	if c.deviceToken == "" && c.deviceTokenPath == "" {
		return
	}
	c.deviceToken = ""
	if c.deviceTokenPath == "" {
		return
	}
	if err := ClearDeviceToken(c.deviceTokenPath); err != nil {
		c.logger.Warn().Err(err).Msg("gateway: failed to clear device token")
	} else {
		c.logger.Info().Msg("gateway: cleared stale device token")
	}
}
