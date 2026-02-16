package canvas

import (
	"image"
	"testing"
)

func TestSnapshotBase64(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	out, err := SnapshotBase64(img)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if out == "" {
		t.Fatalf("expected base64 output")
	}
}
