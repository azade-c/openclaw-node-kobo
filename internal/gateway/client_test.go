package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
)

type fakeConn struct {
	writing atomic.Bool
}

func (f *fakeConn) WriteMessage(messageType int, data []byte) error {
	if !f.writing.CompareAndSwap(false, true) {
		panic("concurrent write")
	}
	time.Sleep(1 * time.Millisecond)
	f.writing.Store(false)
	return nil
}

func (f *fakeConn) ReadMessage() (int, []byte, error) {
	return 0, nil, errors.New("not implemented")
}

func (f *fakeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (f *fakeConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (f *fakeConn) SetReadLimit(limit int64) {}

func (f *fakeConn) SetPongHandler(h func(appData string) error) {}

func (f *fakeConn) Close() error {
	return nil
}

type mockConn struct {
	readCh  chan []byte
	writeCh chan writeRecord
	pingCh  chan struct{}
}

type writeRecord struct {
	messageType int
	data        []byte
}

func newMockConn() *mockConn {
	return &mockConn{
		readCh:  make(chan []byte, 10),
		writeCh: make(chan writeRecord, 10),
		pingCh:  make(chan struct{}, 10),
	}
}

func (m *mockConn) WriteMessage(messageType int, data []byte) error {
	m.writeCh <- writeRecord{messageType: messageType, data: data}
	if messageType == websocket.PingMessage {
		m.pingCh <- struct{}{}
	}
	return nil
}

func (m *mockConn) ReadMessage() (int, []byte, error) {
	data, ok := <-m.readCh
	if !ok {
		return 0, nil, errors.New("connection closed")
	}
	return websocket.TextMessage, data, nil
}

func (m *mockConn) SetWriteDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (m *mockConn) SetReadLimit(limit int64) {}

func (m *mockConn) SetPongHandler(h func(appData string) error) {}

func (m *mockConn) Close() error {
	close(m.readCh)
	return nil
}

func TestClientWriteMutex(t *testing.T) {
	client := New(Config{})
	client.setConn(&fakeConn{})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := client.SendEvent(context.Background(), "node.event", NodeEventParams{Event: "test"}); err != nil {
				t.Errorf("send event: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestClientConnectHandshake(t *testing.T) {
	mock := newMockConn()
	registeredCh := make(chan struct{})
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "device.json")
	deviceTokenPath := filepath.Join(dir, "device-token.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}

	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
		Identity:        identity,
		DeviceTokenPath: deviceTokenPath,
		OnRegistered: func(ctx context.Context) error {
			close(registeredCh)
			return nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := client.registerNode(ctx)
		if err == nil && client.onRegistered != nil {
			_ = client.onRegistered(ctx)
		}
		done <- err
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal connect params: %v", err)
	}
	if params.MinProtocol != ProtocolVersion || params.MaxProtocol != ProtocolVersion {
		t.Fatalf("unexpected protocol range: %d-%d", params.MinProtocol, params.MaxProtocol)
	}
	if params.Device == nil || params.Device.ID == "" || params.Device.PublicKey == "" || params.Device.Signature == "" {
		t.Fatalf("expected device info in connect params")
	}

	select {
	case <-registeredCh:
		t.Fatalf("registered before hello-ok")
	default:
	}

	res := ResponseFrame{
		Type: "res",
		ID:   req.ID,
		OK:   true,
		Payload: json.RawMessage(`{
			"type":"hello-ok",
			"auth":{"deviceToken":"device-token-value"}
		}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}

	select {
	case <-registeredCh:
	case <-ctx.Done():
		t.Fatalf("registered callback not called")
	}

	if client.deviceToken != "device-token-value" {
		t.Fatalf("device token not stored")
	}
	storedToken, err := LoadDeviceToken(deviceTokenPath)
	if err != nil {
		t.Fatalf("load device token: %v", err)
	}
	if storedToken != "device-token-value" {
		t.Fatalf("device token not persisted")
	}
}

func TestClientConnectAuth(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:       zerolog.Nop(),
		Register:     DefaultRegistration(),
		AuthToken:    "token-value",
		AuthPassword: "password-value",
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal connect params: %v", err)
	}
	if params.Auth == nil {
		t.Fatalf("expected auth fields")
	}
	if params.Auth.Token != "token-value" || params.Auth.Password != "password-value" {
		t.Fatalf("unexpected auth values: token=%q password=%q", params.Auth.Token, params.Auth.Password)
	}

	res := ResponseFrame{
		Type:    "res",
		ID:      req.ID,
		OK:      true,
		Payload: json.RawMessage(`{"type":"hello-ok"}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClientConnectChallenge(t *testing.T) {
	mock := newMockConn()
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}

	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
		Identity: identity,
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	secondReq := waitForConnectRequest(t, ctx, mock)
	var secondParams ConnectParams
	if err := json.Unmarshal(secondReq.Params, &secondParams); err != nil {
		t.Fatalf("unmarshal second connect params: %v", err)
	}
	if secondParams.Device == nil || secondParams.Device.Nonce != "nonce-123" {
		t.Fatalf("expected nonce in second connect")
	}

	res := ResponseFrame{
		Type:    "res",
		ID:      secondReq.ID,
		OK:      true,
		Payload: json.RawMessage(`{"type":"hello-ok"}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClientPingTicker(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: 10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go client.pingLoop(ctx, mock, done)

	received := 0
	timeout := time.After(50 * time.Millisecond)
	for received < 2 {
		select {
		case <-mock.pingCh:
			received++
		case <-timeout:
			close(done)
			t.Fatalf("expected ping frames, got %d", received)
		}
	}
	close(done)
}

func TestClientDeviceTokenMismatchClearsToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token-value"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	client.deviceToken = "token-value"
	_ = client.handleCloseError(&websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "device token mismatch"})
	if client.deviceToken != "" {
		t.Fatalf("expected token cleared")
	}
	if _, err := os.Stat(tokenPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected token file removed")
	}
}

func TestClient_New_DefaultPingInterval(t *testing.T) {
	client := New(Config{})
	if client.pingInterval != 30*time.Second {
		t.Fatalf("expected default ping interval 30s, got %v", client.pingInterval)
	}
}

func TestClient_New_CustomPingInterval(t *testing.T) {
	client := New(Config{PingInterval: 5 * time.Second})
	if client.pingInterval != 5*time.Second {
		t.Fatalf("expected custom ping interval, got %v", client.pingInterval)
	}
}

func TestClient_New_LoadsDeviceToken(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token-value"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	if client.deviceToken != "token-value" {
		t.Fatalf("expected loaded token")
	}
}

func TestClient_New_NoIdentity(t *testing.T) {
	client := New(Config{
		Register: DefaultRegistration(),
	})
	req, err := client.buildConnectRequest("")
	if err != nil {
		t.Fatalf("build connect request: %v", err)
	}
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Device != nil {
		t.Fatalf("expected no device info without identity")
	}
}

func TestClient_ConnectHandshake_RejectsNonHelloOk(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	res := ResponseFrame{
		Type:    "res",
		ID:      req.ID,
		OK:      true,
		Payload: json.RawMessage(`{"type":"not-hello"}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err == nil || err.Error() != "gateway: unexpected handshake payload" {
			t.Fatalf("expected unexpected handshake payload error, got %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClient_ConnectHandshake_ServerRejects(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	res := ResponseFrame{
		Type: "res",
		ID:   req.ID,
		OK:   false,
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err == nil || err.Error() != "gateway: connect rejected" {
			t.Fatalf("expected connect rejected error, got %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClient_ConnectHandshake_ServerRejectsWithError(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	res := ResponseFrame{
		Type: "res",
		ID:   req.ID,
		OK:   false,
		Error: &GatewayError{
			Message: "nope",
		},
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err == nil || err.Error() != "nope" {
			t.Fatalf("expected nope error, got %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClient_ConnectHandshake_DeviceTokenStored(t *testing.T) {
	mock := newMockConn()
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	client := New(Config{
		Logger:          zerolog.Nop(),
		Register:        DefaultRegistration(),
		OnInvoke:        func(ctx context.Context, req InvokeRequestParams) (interface{}, error) { return nil, nil },
		DeviceTokenPath: tokenPath,
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	res := ResponseFrame{
		Type: "res",
		ID:   req.ID,
		OK:   true,
		Payload: json.RawMessage(`{
			"type":"hello-ok",
			"auth":{"deviceToken":"device-token-value"}
		}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}

	if client.deviceToken != "device-token-value" {
		t.Fatalf("device token not stored")
	}
	if token, err := LoadDeviceToken(tokenPath); err != nil || token != "device-token-value" {
		t.Fatalf("device token not persisted")
	}
}

func TestClient_ConnectHandshake_ExplicitTokenPreferred(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:       zerolog.Nop(),
		Register:     DefaultRegistration(),
		AuthToken:    "shared-token",
		AuthPassword: "password",
		OnInvoke:     func(ctx context.Context, req InvokeRequestParams) (interface{}, error) { return nil, nil },
	})
	client.deviceToken = "device-token"
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	req := waitForConnectRequest(t, ctx, mock)
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Auth == nil || params.Auth.Token != "shared-token" {
		t.Fatalf("expected explicit token auth")
	}
	res := ResponseFrame{
		Type:    "res",
		ID:      req.ID,
		OK:      true,
		Payload: json.RawMessage(`{"type":"hello-ok"}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClient_ConnectChallenge_IgnoresSameNonce(t *testing.T) {
	mock := newMockConn()
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) { return nil, nil },
		Identity: identity,
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "nonce-123")
	secondReq := waitForConnectRequest(t, ctx, mock)

	sendConnectChallenge(t, mock, "nonce-123")

	select {
	case <-mock.writeCh:
		t.Fatalf("unexpected reconnect for same nonce")
	case <-time.After(20 * time.Millisecond):
	}

	res := ResponseFrame{
		Type:    "res",
		ID:      secondReq.ID,
		OK:      true,
		Payload: json.RawMessage(`{"type":"hello-ok"}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClient_ConnectChallenge_IgnoresEmptyNonce(t *testing.T) {
	mock := newMockConn()
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	client := New(Config{
		Logger:   zerolog.Nop(),
		Register: DefaultRegistration(),
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) { return nil, nil },
		Identity: identity,
	})
	client.setConn(mock)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.registerNode(ctx)
	}()

	sendConnectChallenge(t, mock, "")

	select {
	case <-mock.writeCh:
		t.Fatalf("unexpected reconnect for empty nonce")
	case <-time.After(20 * time.Millisecond):
	}

	sendConnectChallenge(t, mock, "nonce-456")
	req := waitForConnectRequest(t, ctx, mock)
	res := ResponseFrame{
		Type:    "res",
		ID:      req.ID,
		OK:      true,
		Payload: json.RawMessage(`{"type":"hello-ok"}`),
	}
	resData, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal res: %v", err)
	}
	mock.readCh <- resData

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("register failed: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("register did not finish")
	}
}

func TestClient_ReadLoop_InvokeEvent(t *testing.T) {
	mock := newMockConn()
	invoked := make(chan InvokeRequestParams, 1)
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: time.Hour,
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			invoked <- req
			return map[string]bool{"ok": true}, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.readLoop(ctx)
	}()

	event := EventFrame{
		Type:  "event",
		Event: "node.invoke.request",
		Payload: json.RawMessage(`{
			"id":"req-1",
			"nodeId":"node-1",
			"command":"canvas.present",
			"params":{"value":true}
		}`),
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	mock.readCh <- data

	select {
	case req := <-invoked:
		if req.RequestID != "req-1" || req.Command != "canvas.present" {
			t.Fatalf("unexpected invoke request")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("invoke handler not called")
	}

	cancel()
	mock.Close()
	<-done
}

func TestClient_ReadLoop_InvokeRequest(t *testing.T) {
	mock := newMockConn()
	invoked := make(chan InvokeRequestParams, 1)
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: time.Hour,
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			invoked <- req
			return "ok", nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.readLoop(ctx)
	}()

	req := RequestFrame{
		Type:   "req",
		ID:     "frame-1",
		Method: "node.invoke.request",
		Params: json.RawMessage(`{"id":"req-2","nodeId":"node-2","command":"canvas.hide"}`),
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal req: %v", err)
	}
	mock.readCh <- data

	select {
	case got := <-invoked:
		if got.RequestID != "req-2" || got.Command != "canvas.hide" {
			t.Fatalf("unexpected invoke request")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("invoke handler not called")
	}

	cancel()
	mock.Close()
	<-done
}

func TestClient_ReadLoop_UnknownEventIgnored(t *testing.T) {
	mock := newMockConn()
	invoked := make(chan struct{}, 1)
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: time.Hour,
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			invoked <- struct{}{}
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.readLoop(ctx)
	}()

	event := EventFrame{
		Type:    "event",
		Event:   "unknown.event",
		Payload: json.RawMessage(`{"id":"req-1","nodeId":"node-1","command":"noop"}`),
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	mock.readCh <- data

	select {
	case <-invoked:
		t.Fatalf("unexpected invoke handler call")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	mock.Close()
	<-done
}

func TestClient_ReadLoop_VoicewakeIgnored(t *testing.T) {
	mock := newMockConn()
	invoked := make(chan struct{}, 1)
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: time.Hour,
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			invoked <- struct{}{}
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.readLoop(ctx)
	}()

	event := EventFrame{
		Type:    "event",
		Event:   "voicewake.changed",
		Payload: json.RawMessage(`{"state":"on"}`),
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	mock.readCh <- data

	select {
	case <-invoked:
		t.Fatalf("unexpected invoke handler call")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	mock.Close()
	<-done
}

func TestClient_ReadLoop_TickEvent(t *testing.T) {
	mock := newMockConn()
	invoked := make(chan struct{}, 1)
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: time.Hour,
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			invoked <- struct{}{}
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- client.readLoop(ctx)
	}()

	event := EventFrame{
		Type:    "event",
		Event:   "tick",
		Payload: json.RawMessage(`{}`),
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	mock.readCh <- data

	select {
	case <-invoked:
		t.Fatalf("unexpected invoke handler call")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	mock.Close()
	<-done
}

func TestClient_ReadLoop_ShutdownEvent(t *testing.T) {
	mock := newMockConn()
	client := New(Config{
		Logger:       zerolog.Nop(),
		PingInterval: time.Hour,
		OnInvoke: func(ctx context.Context, req InvokeRequestParams) (interface{}, error) {
			return nil, nil
		},
	})
	client.setConn(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- client.readLoop(ctx)
	}()

	event := EventFrame{
		Type:  "event",
		Event: "shutdown",
		Payload: json.RawMessage(`{
			"reason":"maintenance",
			"restartExpectedMs":5000
		}`),
	}
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	mock.readCh <- data

	select {
	case err := <-done:
		if err == nil || !errors.Is(err, errGatewayShutdown) {
			t.Fatalf("expected shutdown error, got %v", err)
		}
		backoff := backoffFromErr(t, err)
		if backoff != 5*time.Second {
			t.Fatalf("expected shutdown backoff 5s, got %v", backoff)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("shutdown did not end read loop")
	}
}

func TestClient_SendEvent_NoConnection(t *testing.T) {
	client := New(Config{})
	if err := client.SendEvent(context.Background(), "node.event", NodeEventParams{Event: "test"}); err == nil {
		t.Fatalf("expected error without connection")
	}
}

func TestClient_SendEvent_MarshalsCorrectly(t *testing.T) {
	mock := newMockConn()
	client := New(Config{})
	client.setConn(mock)

	if err := client.SendEvent(context.Background(), "node.event", NodeEventParams{Event: "hello", Payload: map[string]bool{"ok": true}}); err != nil {
		t.Fatalf("send event: %v", err)
	}
	record := <-mock.writeCh
	var frame RequestFrame
	if err := json.Unmarshal(record.data, &frame); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if frame.Type != "req" || frame.Method != "node.event" {
		t.Fatalf("unexpected frame type/method")
	}
	var params NodeEventParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Event != "hello" {
		t.Fatalf("unexpected event params")
	}
}

func TestClient_SelectConnectAuth_ExplicitTokenOverDeviceToken(t *testing.T) {
	client := New(Config{
		AuthToken: "shared-token",
	})
	client.deviceToken = "device-token"
	auth, token := client.selectConnectAuth()
	if auth == nil || auth.Token != "shared-token" || token != "shared-token" {
		t.Fatalf("expected explicit token auth")
	}
}

func TestClient_SelectConnectAuth_FallbackToSharedSecret(t *testing.T) {
	client := New(Config{
		AuthToken: "shared-token",
	})
	auth, token := client.selectConnectAuth()
	if auth == nil || auth.Token != "shared-token" || token != "shared-token" {
		t.Fatalf("expected shared secret auth")
	}
}

func TestClient_SelectConnectAuth_PasswordPreserved(t *testing.T) {
	client := New(Config{
		AuthPassword: "password",
	})
	client.deviceToken = "device-token"
	auth, _ := client.selectConnectAuth()
	if auth == nil || auth.Token != "device-token" || auth.Password != "password" {
		t.Fatalf("expected device token with password preserved")
	}
}

func TestClient_SelectConnectAuth_NoAuth(t *testing.T) {
	client := New(Config{})
	auth, token := client.selectConnectAuth()
	if auth != nil || token != "" {
		t.Fatalf("expected no auth")
	}
}

func TestClient_BuildConnectRequest_WithNonce(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	client := New(Config{
		Register: DefaultRegistration(),
		Identity: identity,
	})
	req, err := client.buildConnectRequest("nonce-1")
	if err != nil {
		t.Fatalf("build connect request: %v", err)
	}
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Device == nil || params.Device.Nonce != "nonce-1" {
		t.Fatalf("expected nonce in device info")
	}
}

func TestClient_BuildConnectRequest_WithoutNonce(t *testing.T) {
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "device.json")
	identity, err := LoadOrCreateIdentity(identityPath)
	if err != nil {
		t.Fatalf("create identity: %v", err)
	}
	client := New(Config{
		Register: DefaultRegistration(),
		Identity: identity,
	})
	req, err := client.buildConnectRequest("")
	if err != nil {
		t.Fatalf("build connect request: %v", err)
	}
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Device == nil || params.Device.Nonce != "" {
		t.Fatalf("expected empty nonce in device info")
	}
}

func TestClient_BuildConnectRequest_NoIdentity(t *testing.T) {
	client := New(Config{
		Register: DefaultRegistration(),
	})
	req, err := client.buildConnectRequest("")
	if err != nil {
		t.Fatalf("build connect request: %v", err)
	}
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.Device != nil {
		t.Fatalf("expected no device info")
	}
}

func TestClient_BuildConnectRequest_AllFields(t *testing.T) {
	reg := NodeRegistration{
		Client: ClientInfo{
			ID:              "client-id",
			DisplayName:     "Display",
			Version:         "1.0",
			Platform:        "linux",
			DeviceFamily:    "kobo",
			ModelIdentifier: "model",
			Mode:            "node",
			InstanceID:      "instance-1",
		},
		Role:     "node",
		Caps:     []string{"canvas"},
		Commands: []string{"canvas.present"},
		Permissions: map[string]bool{
			"nodes.register": true,
		},
		PathEnv:   "/usr/bin",
		Scopes:    []string{"scope-a"},
		Locale:    "en-US",
		UserAgent: "ua",
	}
	client := New(Config{
		Register: reg,
	})
	req, err := client.buildConnectRequest("")
	if err != nil {
		t.Fatalf("build connect request: %v", err)
	}
	var params ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if !reflect.DeepEqual(params.Client, reg.Client) {
		t.Fatalf("client info mismatch")
	}
	if params.Role != reg.Role || params.PathEnv != reg.PathEnv || params.Locale != reg.Locale || params.UserAgent != reg.UserAgent {
		t.Fatalf("registration fields mismatch")
	}
	if !reflect.DeepEqual(params.Permissions, reg.Permissions) || !reflect.DeepEqual(params.Scopes, reg.Scopes) {
		t.Fatalf("permissions/scopes mismatch")
	}
}

func TestClient_HandleCloseError_NonWebsocket(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	client.deviceToken = "token"
	_ = client.handleCloseError(errors.New("nope"))
	if client.deviceToken == "" {
		t.Fatalf("expected token to remain")
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("expected token file to remain")
	}
}

func TestClient_HandleCloseError_WrongCode(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	client.deviceToken = "token"
	_ = client.handleCloseError(&websocket.CloseError{Code: websocket.CloseNormalClosure, Text: "device token mismatch"})
	if client.deviceToken == "" {
		t.Fatalf("expected token to remain")
	}
}

func TestClient_HandleCloseError_WrongReason(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	client.deviceToken = "token"
	_ = client.handleCloseError(&websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "other reason"})
	if client.deviceToken == "" {
		t.Fatalf("expected token to remain")
	}
}

func TestClient_HandleCloseError_PairingRequired(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	client.deviceToken = "token"
	err := client.handleCloseError(&websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "pairing required"})
	backoff := backoffFromErr(t, err)
	if backoff != 10*time.Second {
		t.Fatalf("expected pairing backoff 10s, got %v", backoff)
	}
	if client.deviceToken == "" {
		t.Fatalf("expected token to remain")
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("expected token file to remain")
	}
}

func TestClient_HandleCloseError_DeviceIdentityRequired(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "device-token.json")
	if err := SaveDeviceToken(tokenPath, "token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	client := New(Config{
		Logger:          zerolog.Nop(),
		DeviceTokenPath: tokenPath,
	})
	client.deviceToken = "token"
	err := client.handleCloseError(&websocket.CloseError{Code: websocket.ClosePolicyViolation, Text: "device identity required"})
	backoff := backoffFromErr(t, err)
	if backoff != 10*time.Second {
		t.Fatalf("expected device identity backoff 10s, got %v", backoff)
	}
	if client.deviceToken == "" {
		t.Fatalf("expected token to remain")
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatalf("expected token file to remain")
	}
}

func TestClient_BackoffGrowth(t *testing.T) {
	backoff := time.Millisecond
	client := New(Config{})
	if err := client.waitBackoff(context.Background(), &backoff); err != nil {
		t.Fatalf("wait backoff: %v", err)
	}
	if backoff != 2*time.Millisecond {
		t.Fatalf("expected backoff to double, got %v", backoff)
	}

	backoff = 30 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = client.waitBackoff(ctx, &backoff)
	if backoff != 30*time.Second {
		t.Fatalf("expected backoff to remain capped, got %v", backoff)
	}
}

func TestClient_InvokeResult_Success(t *testing.T) {
	mock := newMockConn()
	client := New(Config{})
	client.setConn(mock)

	req := InvokeRequestParams{RequestID: "req-1", NodeID: "node-1", Command: "cmd"}
	if err := client.sendInvokeResult(context.Background(), req, map[string]string{"status": "ok"}, nil); err != nil {
		t.Fatalf("send invoke result: %v", err)
	}
	record := <-mock.writeCh
	var frame RequestFrame
	if err := json.Unmarshal(record.data, &frame); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if frame.Method != "node.invoke.result" {
		t.Fatalf("unexpected method: %s", frame.Method)
	}
	var params InvokeResultParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if !params.OK {
		t.Fatalf("expected ok result")
	}
}

func TestClient_InvokeResult_Error(t *testing.T) {
	mock := newMockConn()
	client := New(Config{})
	client.setConn(mock)

	req := InvokeRequestParams{RequestID: "req-1", NodeID: "node-1", Command: "cmd"}
	if err := client.sendInvokeResult(context.Background(), req, nil, errors.New("bad")); err != nil {
		t.Fatalf("send invoke result: %v", err)
	}
	record := <-mock.writeCh
	var frame RequestFrame
	if err := json.Unmarshal(record.data, &frame); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	var params InvokeResultParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if params.OK || params.Error == nil || params.Error.Message != "bad" {
		t.Fatalf("expected error result")
	}
}

func TestParseInvokePayload_ParamsJSON(t *testing.T) {
	raw := json.RawMessage(`{"id":"req","nodeId":"node","command":"cmd","paramsJSON":"{\"value\":1}"}`)
	params, err := parseInvokePayload(raw)
	if err != nil {
		t.Fatalf("parse invoke payload: %v", err)
	}
	if string(params.Args) != `{"value":1}` {
		t.Fatalf("unexpected args: %s", string(params.Args))
	}
}

func TestParseInvokePayload_Params(t *testing.T) {
	raw := json.RawMessage(`{"id":"req","nodeId":"node","command":"cmd","params":{"value":2}}`)
	params, err := parseInvokePayload(raw)
	if err != nil {
		t.Fatalf("parse invoke payload: %v", err)
	}
	if string(params.Args) != `{"value":2}` {
		t.Fatalf("unexpected args: %s", string(params.Args))
	}
}

func TestParseInvokePayload_BothFields(t *testing.T) {
	raw := json.RawMessage(`{"id":"req","nodeId":"node","command":"cmd","paramsJSON":"{\"value\":3}","params":{"value":2}}`)
	params, err := parseInvokePayload(raw)
	if err != nil {
		t.Fatalf("parse invoke payload: %v", err)
	}
	if string(params.Args) != `{"value":3}` {
		t.Fatalf("expected paramsJSON to win, got %s", string(params.Args))
	}
}

func TestParseInvokePayload_MissingRequestID(t *testing.T) {
	raw := json.RawMessage(`{"nodeId":"node","command":"cmd","params":{"value":2}}`)
	if _, err := parseInvokePayload(raw); err == nil {
		t.Fatalf("expected error for missing request id")
	}
}

func backoffFromErr(t *testing.T, err error) time.Duration {
	t.Helper()
	var provider interface {
		Backoff() time.Duration
	}
	if !errors.As(err, &provider) {
		t.Fatalf("expected backoff provider")
	}
	return provider.Backoff()
}

func sendConnectChallenge(t *testing.T, mock *mockConn, nonce string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"nonce": nonce})
	if err != nil {
		t.Fatalf("marshal challenge payload: %v", err)
	}
	challenge := EventFrame{
		Type:  "event",
		Event: "connect.challenge",
		Payload: payload,
	}
	data, err := json.Marshal(challenge)
	if err != nil {
		t.Fatalf("marshal challenge: %v", err)
	}
	mock.readCh <- data
}

func waitForConnectRequest(t *testing.T, ctx context.Context, mock *mockConn) RequestFrame {
	t.Helper()
	var req RequestFrame
	select {
	case record := <-mock.writeCh:
		if record.messageType != websocket.TextMessage {
			t.Fatalf("unexpected message type: %d", record.messageType)
		}
		if err := json.Unmarshal(record.data, &req); err != nil {
			t.Fatalf("unmarshal req: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("connect request not sent")
	}
	if req.Type != "req" || req.Method != "connect" {
		t.Fatalf("unexpected request: type=%s method=%s", req.Type, req.Method)
	}
	return req
}
