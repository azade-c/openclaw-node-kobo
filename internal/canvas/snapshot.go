package canvas

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
)

func SnapshotBase64(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}
