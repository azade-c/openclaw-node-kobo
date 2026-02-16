package canvas

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/openclaw/openclaw-node-kobo/internal/eink"
)

type ActionSender interface {
	SendEvent(ctx context.Context, method string, params interface{}) error
}

type Handler struct {
	fb       *eink.Framebuffer
	renderer *Renderer
	state    *A2UIState
	logger   zerolog.Logger
	sender   ActionSender
}

func NewHandler(fb *eink.Framebuffer, renderer *Renderer, sender ActionSender, logger zerolog.Logger) *Handler {
	return &Handler{
		fb:       fb,
		renderer: renderer,
		state:    NewA2UIState(),
		logger:   logger,
		sender:   sender,
	}
}

func (h *Handler) HandleInvoke(ctx context.Context, req InvokeRequest) (interface{}, error) {
	switch req.Command {
	case "canvas.present":
		return h.present(false)
	case "canvas.hide":
		h.renderer.Clear()
		if err := h.fb.WriteGray(h.renderer.Image); err != nil {
			return nil, err
		}
		return nil, h.fb.Refresh(eink.Update{Full: true})
	case "canvas.navigate":
		return nil, errors.New("canvas.navigate not supported on Kobo")
	case "canvas.eval":
		return nil, errors.New("canvas.eval not supported on Kobo")
	case "canvas.snapshot":
		return SnapshotBase64(h.renderer.Image)
	case "canvas.a2ui.push":
		return h.handleA2UIPush(req.Args)
	case "canvas.a2ui.pushJSONL":
		return h.handleA2UIPushJSONL(req.Args)
	case "canvas.a2ui.reset":
		h.state.Reset()
		h.renderer.Clear()
		if err := h.fb.WriteGray(h.renderer.Image); err != nil {
			return nil, err
		}
		return nil, h.fb.Refresh(eink.Update{Full: true})
	default:
		return nil, errors.New("unknown canvas command")
	}
}

type InvokeRequest struct {
	Command string
	Args    json.RawMessage
}

func (h *Handler) handleA2UIPush(args json.RawMessage) (interface{}, error) {
	push, err := DecodeA2UIPush(args)
	if err != nil {
		return nil, err
	}
	h.state.ApplyPush(push)
	return h.present(true)
}

func (h *Handler) handleA2UIPushJSONL(args json.RawMessage) (interface{}, error) {
	jsonl, err := unwrapStringArgs(args)
	if err != nil {
		return nil, err
	}
	pushes, err := DecodeA2UIJSONL([]byte(jsonl))
	if err != nil {
		return nil, err
	}
	for _, push := range pushes {
		h.state.ApplyPush(push)
	}
	return h.present(true)
}

func (h *Handler) present(partial bool) (interface{}, error) {
	h.renderer.Render(h.state.Components())
	if err := h.fb.WriteGray(h.renderer.Image); err != nil {
		return nil, err
	}
	update := eink.Update{Full: !partial}
	if partial {
		update.Fast = true
	}
	return nil, h.fb.Refresh(update)
}

func (h *Handler) HandleTouch(ctx context.Context, x, y int) {
	action := h.renderer.HitTest(x, y)
	if action == nil || h.sender == nil {
		return
	}
	payload := map[string]interface{}{
		"type":    action.Type,
		"payload": json.RawMessage(action.Payload),
		"x":       x,
		"y":       y,
		"time":    time.Now().UnixMilli(),
	}
	if err := h.sender.SendEvent(ctx, "canvas.a2ui.action", payload); err != nil {
		h.logger.Warn().Err(err).Msg("failed to send A2UI action")
	}
}

func unwrapStringArgs(args json.RawMessage) (string, error) {
	var asString string
	if err := json.Unmarshal(args, &asString); err == nil {
		return asString, nil
	}
	var obj map[string]string
	if err := json.Unmarshal(args, &obj); err == nil {
		if val, ok := obj["jsonl"]; ok {
			return val, nil
		}
	}
	return "", errors.New("invalid JSONL args")
}

func sanitizeCommand(cmd string) string {
	return strings.TrimSpace(cmd)
}

func (h *Handler) HandleInvokeRequest(ctx context.Context, req InvokeRequest) (interface{}, error) {
	req.Command = sanitizeCommand(req.Command)
	return h.HandleInvoke(ctx, req)
}
