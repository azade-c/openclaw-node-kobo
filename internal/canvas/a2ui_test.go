package canvas

import "testing"

func TestDecodeA2UIPush(t *testing.T) {
	payload := []byte(`{"components":[{"type":"text","text":"hi"}]}`)
	push, err := DecodeA2UIPush(payload)
	if err != nil {
		t.Fatalf("decode push: %v", err)
	}
	if len(push.Components) != 1 {
		t.Fatalf("expected 1 component")
	}
}

func TestDecodeA2UIJSONL(t *testing.T) {
	payload := []byte("{\"type\":\"text\",\"text\":\"hi\"}\n{\"components\":[{\"type\":\"box\"}]}")
	pushes, err := DecodeA2UIJSONL(payload)
	if err != nil {
		t.Fatalf("decode jsonl: %v", err)
	}
	if len(pushes) != 2 {
		t.Fatalf("expected 2 pushes")
	}
}

func TestA2UIStateApply(t *testing.T) {
	state := NewA2UIState()
	state.ApplyPush(A2UIPush{Components: []A2UIComponent{{Type: "text", Text: "a"}}})
	state.ApplyPush(A2UIPush{Components: []A2UIComponent{{Type: "text", Text: "b"}}})
	comps := state.Components()
	if len(comps) != 2 {
		t.Fatalf("expected 2 components")
	}
	state.Reset()
	if len(state.Components()) != 0 {
		t.Fatalf("expected reset to clear")
	}
}
