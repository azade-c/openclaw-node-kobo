package eink

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestReadInputEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	ev := InputEvent{Sec: 1, Usec: 2, Type: EVAbs, Code: ABSX, Value: 123}
	if err := binary.Write(buf, binary.LittleEndian, ev); err != nil {
		t.Fatalf("binary write: %v", err)
	}
	read, err := readInputEvent(buf)
	if err != nil {
		t.Fatalf("readInputEvent: %v", err)
	}
	if read.Code != ABSX || read.Value != 123 {
		t.Fatalf("unexpected event")
	}
}
