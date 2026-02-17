package gateway

import "testing"

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
