package canvas

import "testing"

func TestRendererHitTest(t *testing.T) {
	r := NewRenderer(200, 100)
	action := A2UIAction{Type: "tap"}
	comp := A2UIComponent{
		Type:   "button",
		X:      10,
		Y:      10,
		Width:  80,
		Height: 30,
		Action: &action,
	}
	r.Render([]A2UIComponent{comp})
	if got := r.HitTest(20, 20); got == nil || got.Type != "tap" {
		t.Fatalf("expected hit action")
	}
	if got := r.HitTest(150, 20); got != nil {
		t.Fatalf("expected no hit")
	}
}
