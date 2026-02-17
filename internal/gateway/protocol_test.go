package gateway

import (
	"encoding/json"
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
