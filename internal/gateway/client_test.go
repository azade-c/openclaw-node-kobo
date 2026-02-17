package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
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
