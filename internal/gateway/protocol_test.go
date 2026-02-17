package gateway

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDefaultRegistration(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Role != "node" {
		t.Fatalf("expected role node")
	}
	if reg.Client.ID == "" {
		t.Fatalf("expected client id")
	}
	if reg.Client.Mode != "node" {
		t.Fatalf("expected client mode node")
	}
	if len(reg.Caps) == 0 || reg.Caps[0] != "canvas" {
		t.Fatalf("expected canvas cap")
	}
	if len(reg.Commands) == 0 {
		t.Fatalf("expected commands")
	}
}

func TestDeviceInfoJSON(t *testing.T) {
	params := ConnectParams{
		Device: &DeviceInfo{
			ID:        "device-id",
			PublicKey: "public-key",
			Signature: "signature",
			SignedAt:  123456789,
			Nonce:     "nonce-value",
		},
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var decoded ConnectParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if decoded.Device == nil || decoded.Device.ID != "device-id" {
		t.Fatalf("device info missing")
	}
	if decoded.Device.SignedAt != 123456789 {
		t.Fatalf("unexpected signedAt")
	}
}

func TestConnectParams_JSONRoundtrip(t *testing.T) {
	params := ConnectParams{
		MinProtocol: ProtocolVersion,
		MaxProtocol: ProtocolVersion,
		Client: ClientInfo{
			ID:              "client-id",
			DisplayName:     "Display",
			Version:         "1.2.3",
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
		Auth:      &ConnectAuth{Token: "token", Password: "password"},
		Device:    &DeviceInfo{ID: "device-id", PublicKey: "public", Signature: "sig", SignedAt: 123, Nonce: "nonce"},
		Locale:    "en-US",
		UserAgent: "agent",
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var decoded ConnectParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if !reflect.DeepEqual(params, decoded) {
		t.Fatalf("roundtrip mismatch")
	}
}

func TestConnectParams_OmitsEmptyFields(t *testing.T) {
	params := ConnectParams{
		MinProtocol: ProtocolVersion,
		MaxProtocol: ProtocolVersion,
		Client: ClientInfo{
			ID:       "client-id",
			Version:  "1.2.3",
			Platform: "linux",
			Mode:     "node",
		},
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	optionalKeys := []string{"caps", "commands", "permissions", "pathEnv", "scopes", "auth", "device", "locale", "userAgent", "role"}
	for _, key := range optionalKeys {
		if _, ok := raw[key]; ok {
			t.Fatalf("expected %s to be omitted", key)
		}
	}
	var client map[string]json.RawMessage
	if err := json.Unmarshal(raw["client"], &client); err != nil {
		t.Fatalf("unmarshal client: %v", err)
	}
	clientOptional := []string{"displayName", "deviceFamily", "modelIdentifier", "instanceId"}
	for _, key := range clientOptional {
		if _, ok := client[key]; ok {
			t.Fatalf("expected client %s to be omitted", key)
		}
	}
}

func TestHelloOkPayload_WithAuth(t *testing.T) {
	data := []byte(`{"type":"hello-ok","auth":{"deviceToken":"token","role":"node","scopes":["a"],"issuedAtMs":123}}`)
	var payload HelloOkPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Type != "hello-ok" {
		t.Fatalf("unexpected type: %s", payload.Type)
	}
	if payload.Auth == nil || payload.Auth.DeviceToken != "token" {
		t.Fatalf("expected auth data")
	}
}

func TestHelloOkPayload_WithoutAuth(t *testing.T) {
	data := []byte(`{"type":"hello-ok"}`)
	var payload HelloOkPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.Auth != nil {
		t.Fatalf("expected no auth data")
	}
}

func TestEventFrame_JSONRoundtrip(t *testing.T) {
	frame := EventFrame{
		Type:    "event",
		Event:   "node.event",
		Payload: json.RawMessage(`{"value":1}`),
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	var decoded EventFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if decoded.Type != frame.Type || decoded.Event != frame.Event || string(decoded.Payload) != string(frame.Payload) {
		t.Fatalf("event frame mismatch")
	}
}

func TestRequestFrame_JSONRoundtrip(t *testing.T) {
	frame := RequestFrame{
		Type:   "req",
		ID:     "id-1",
		Method: "node.invoke.request",
		Params: json.RawMessage(`{"id":"req-1"}`),
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	var decoded RequestFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if decoded.Type != frame.Type || decoded.ID != frame.ID || decoded.Method != frame.Method || string(decoded.Params) != string(frame.Params) {
		t.Fatalf("request frame mismatch")
	}
}

func TestResponseFrame_WithError(t *testing.T) {
	frame := ResponseFrame{
		Type: "res",
		ID:   "id-1",
		OK:   false,
		Error: &GatewayError{
			Code:    "bad_request",
			Message: "nope",
		},
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	var decoded ResponseFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if decoded.Error == nil || decoded.Error.Code != "bad_request" {
		t.Fatalf("expected error payload")
	}
}

func TestResponseFrame_WithPayload(t *testing.T) {
	frame := ResponseFrame{
		Type:    "res",
		ID:      "id-2",
		OK:      true,
		Payload: json.RawMessage(`{"type":"hello-ok"}`),
	}
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	var decoded ResponseFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if !decoded.OK || string(decoded.Payload) != string(frame.Payload) {
		t.Fatalf("expected ok payload")
	}
}

func TestInvokeRequestParams_ParamsJSON(t *testing.T) {
	raw := []byte(`{"id":"req-1","nodeId":"node-1","command":"canvas.present","args":{"value":true}}`)
	var params InvokeRequestParams
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("unmarshal invoke params: %v", err)
	}
	if params.RequestID != "req-1" || params.NodeID != "node-1" || params.Command != "canvas.present" {
		t.Fatalf("unexpected invoke params")
	}
	if string(params.Args) != `{"value":true}` {
		t.Fatalf("unexpected args: %s", string(params.Args))
	}
}

func TestNodeEventParams_JSON(t *testing.T) {
	payloadJSON := `{"ok":true}`
	params := NodeEventParams{
		Event:       "node.event",
		PayloadJSON: &payloadJSON,
	}
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var decoded NodeEventParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if decoded.Event != "node.event" || decoded.PayloadJSON == nil || *decoded.PayloadJSON != payloadJSON {
		t.Fatalf("unexpected node event params")
	}
}

func TestDefaultRegistration_DeviceFamily(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Client.DeviceFamily != "kobo" {
		t.Fatalf("expected deviceFamily kobo")
	}
}

func TestDefaultRegistration_DeviceFamily_Kobo(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Client.DeviceFamily != "kobo" {
		t.Fatalf("expected deviceFamily kobo")
	}
}

func TestDefaultRegistration_AllFields(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Role != "node" {
		t.Fatalf("expected role node")
	}
	if reg.Client.ID != "node-host" {
		t.Fatalf("expected client id node-host")
	}
	if reg.Client.DisplayName != "Kobo" {
		t.Fatalf("expected display name Kobo")
	}
	if reg.Client.Version != "0.1" {
		t.Fatalf("expected version 0.1")
	}
	if reg.Client.Platform != "linux" {
		t.Fatalf("expected platform linux")
	}
	if reg.Client.DeviceFamily != "kobo" {
		t.Fatalf("expected deviceFamily kobo")
	}
	if reg.Client.Mode != "node" {
		t.Fatalf("expected client mode node")
	}
	if len(reg.Caps) != 1 || reg.Caps[0] != "canvas" {
		t.Fatalf("expected canvas cap")
	}
	if len(reg.Commands) == 0 {
		t.Fatalf("expected commands")
	}
}

func TestDefaultRegistration_Commands(t *testing.T) {
	reg := DefaultRegistration()
	expected := []string{
		"canvas.present",
		"canvas.hide",
		"canvas.navigate",
		"canvas.eval",
		"canvas.snapshot",
		"canvas.a2ui.push",
		"canvas.a2ui.pushJSONL",
		"canvas.a2ui.reset",
	}
	if !reflect.DeepEqual(reg.Commands, expected) {
		t.Fatalf("unexpected commands")
	}
}

func TestDefaultRegistration_ClientID(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Client.ID != "node-host" {
		t.Fatalf("expected client ID node-host")
	}
}

func TestDefaultRegistration_InstanceID_Empty(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Client.InstanceID != "" {
		t.Fatalf("expected instanceId to be empty")
	}
}
