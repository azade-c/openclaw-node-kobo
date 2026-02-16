package gateway

import "testing"

func TestDefaultRegistration(t *testing.T) {
	reg := DefaultRegistration()
	if reg.Role != "node" {
		t.Fatalf("expected role node")
	}
	if len(reg.Caps) == 0 || reg.Caps[0] != "canvas" {
		t.Fatalf("expected canvas cap")
	}
	if len(reg.Commands) == 0 {
		t.Fatalf("expected commands")
	}
}
