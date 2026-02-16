package canvas

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

type A2UIAction struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type A2UIStyle struct {
	FillGray   *uint8 `json:"fillGray,omitempty"`
	StrokeGray *uint8 `json:"strokeGray,omitempty"`
}

type A2UIComponent struct {
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type"`
	X        int             `json:"x,omitempty"`
	Y        int             `json:"y,omitempty"`
	Width    int             `json:"width,omitempty"`
	Height   int             `json:"height,omitempty"`
	Text     string          `json:"text,omitempty"`
	FontSize float64         `json:"fontSize,omitempty"`
	Align    string          `json:"align,omitempty"`
	Padding  int             `json:"padding,omitempty"`
	Action   *A2UIAction      `json:"action,omitempty"`
	Style    *A2UIStyle       `json:"style,omitempty"`
	Children []A2UIComponent `json:"children,omitempty"`
}

type A2UIPush struct {
	Components []A2UIComponent `json:"components"`
	Replace    bool            `json:"replace,omitempty"`
}

type A2UIState struct {
	mu         sync.Mutex
	components []A2UIComponent
}

func NewA2UIState() *A2UIState {
	return &A2UIState{}
}

func (s *A2UIState) Reset() {
	s.mu.Lock()
	s.components = nil
	s.mu.Unlock()
}

func (s *A2UIState) ApplyPush(push A2UIPush) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if push.Replace {
		s.components = append([]A2UIComponent{}, push.Components...)
		return
	}
	s.components = append(s.components, push.Components...)
}

func (s *A2UIState) Components() []A2UIComponent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]A2UIComponent, len(s.components))
	copy(out, s.components)
	return out
}

func DecodeA2UIPush(data []byte) (A2UIPush, error) {
	var push A2UIPush
	if err := json.Unmarshal(data, &push); err == nil && len(push.Components) > 0 {
		return push, nil
	}
	var comp A2UIComponent
	if err := json.Unmarshal(data, &comp); err == nil && comp.Type != "" {
		return A2UIPush{Components: []A2UIComponent{comp}}, nil
	}
	return A2UIPush{}, errors.New("invalid A2UI payload")
}

func DecodeA2UIJSONL(data []byte) ([]A2UIPush, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var pushes []A2UIPush
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		push, err := DecodeA2UIPush([]byte(line))
		if err != nil {
			return nil, err
		}
		pushes = append(pushes, push)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pushes, nil
}
