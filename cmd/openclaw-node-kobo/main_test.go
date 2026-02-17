package main

import (
	"testing"

	"github.com/openclaw/openclaw-node-kobo/internal/gateway"
)

func TestDefaultRegistration_InstanceIDSetFromIdentity(t *testing.T) {
	identity := &gateway.DeviceIdentity{DeviceID: "device-123"}
	reg := buildRegistration("node-name", identity)
	if reg.Client.InstanceID != "device-123" {
		t.Fatalf("expected instance id from identity, got %q", reg.Client.InstanceID)
	}
}
