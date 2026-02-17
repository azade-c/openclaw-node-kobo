package canvas

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/openclaw/openclaw-node-kobo/internal/eink"
	"github.com/rs/zerolog"
)

type mockSender struct {
	called bool
}

func (m *mockSender) SendEvent(ctx context.Context, method string, params interface{}) error {
	m.called = true
	return nil
}

func TestHandlerA2UIPush(t *testing.T) {
	fb := eink.NewFramebufferFromBuffer(100, 50)
	renderer := NewRenderer(100, 50)
	sender := &mockSender{}
	h := NewHandler(fb, renderer, sender, zerolog.Nop())

	fill := 100
	payload := map[string]interface{}{
		"components": []map[string]interface{}{
			{
				"type":   "box",
				"x":      0,
				"y":      0,
				"width":  10,
				"height": 10,
				"style": map[string]interface{}{
					"fillGray": fill,
				},
			},
		},
	}
	args, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	_, err = h.HandleInvokeRequest(context.Background(), InvokeRequest{Command: "canvas.a2ui.push", Args: args})
	if err != nil {
		t.Fatalf("handle invoke: %v", err)
	}
	if got := renderer.Image.GrayAt(1, 1).Y; got != uint8(fill) {
		t.Fatalf("expected pixel fill %d, got %d", fill, got)
	}

	h.HandleTouch(context.Background(), 3, 3)
	if sender.called {
		// no action registered, should not send
		t.Fatalf("unexpected action send")
	}
}

func TestHandlerConcurrentRenderHitTest(t *testing.T) {
	fb := eink.NewFramebufferFromBuffer(100, 50)
	renderer := NewRenderer(100, 50)
	h := NewHandler(fb, renderer, nil, zerolog.Nop())

	payload := map[string]interface{}{
		"components": []map[string]interface{}{
			{
				"type":   "box",
				"x":      0,
				"y":      0,
				"width":  10,
				"height": 10,
			},
		},
	}
	args, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := h.HandleInvokeRequest(context.Background(), InvokeRequest{Command: "canvas.a2ui.push", Args: args}); err != nil {
		t.Fatalf("handle invoke: %v", err)
	}

	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_, _ = h.HandleInvokeRequest(context.Background(), InvokeRequest{Command: "canvas.present"})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			h.HandleTouch(context.Background(), 1, 1)
		}
	}()
	wg.Wait()
}
