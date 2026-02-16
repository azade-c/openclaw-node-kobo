package eink

import (
	"image"
	"image/color"
	"testing"
)

func TestFramebufferWriteGray(t *testing.T) {
	fb := NewFramebufferFromBuffer(4, 3)
	img := image.NewGray(image.Rect(0, 0, 4, 3))
	img.SetGray(1, 1, color.Gray{Y: 50})
	if err := fb.WriteGray(img); err != nil {
		t.Fatalf("write gray: %v", err)
	}
	idx := 1 + 1*fb.Stride
	if fb.data[idx] != 50 {
		t.Fatalf("expected pixel value 50, got %d", fb.data[idx])
	}
}

func TestFramebufferRefreshNoFile(t *testing.T) {
	fb := NewFramebufferFromBuffer(1, 1)
	if err := fb.Refresh(Update{Full: true}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
}
