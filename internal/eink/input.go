package eink

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

const (
	EVSyn = 0
	EVKey = 1
	EVAbs = 3

	ABSX = 0
	ABSY = 1

	BTNToolFinger = 325
	BTNTouch      = 330

	KEYPower = 116
)

type InputEvent struct {
	Sec   int32
	Usec  int32
	Type  uint16
	Code  uint16
	Value int32
}

type TouchEvent struct {
	X     int
	Y     int
	Down  bool
	At    time.Time
	Dirty bool
}

type PowerEvent struct {
	Pressed bool
	At      time.Time
}

type InputDevice struct {
	file *os.File
}

func OpenInputDevice(path string) (*InputDevice, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	return &InputDevice{file: file}, nil
}

func (d *InputDevice) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	return d.file.Close()
}

func (d *InputDevice) ReadEvents() (<-chan TouchEvent, <-chan PowerEvent, <-chan error) {
	touchCh := make(chan TouchEvent, 16)
	powerCh := make(chan PowerEvent, 4)
	errCh := make(chan error, 1)

	go func() {
		defer close(touchCh)
		defer close(powerCh)
		defer close(errCh)

		var (
			currentX   = 0
			currentY   = 0
			isTouching = false
			dirty      = false
		)
		for {
			event, err := readInputEvent(d.file)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				errCh <- err
				return
			}
			switch event.Type {
			case EVAbs:
				switch event.Code {
				case ABSX:
					currentX = int(event.Value)
					dirty = true
				case ABSY:
					currentY = int(event.Value)
					dirty = true
				}
			case EVKey:
				switch event.Code {
				case BTNTouch, BTNToolFinger:
					isTouching = event.Value != 0
					dirty = true
				case KEYPower:
					powerCh <- PowerEvent{Pressed: event.Value != 0, At: eventTime(event)}
				}
			case EVSyn:
				if dirty {
					touchCh <- TouchEvent{X: currentX, Y: currentY, Down: isTouching, At: eventTime(event), Dirty: true}
					dirty = false
				}
			}
		}
	}()

	return touchCh, powerCh, errCh
}

func readInputEvent(r io.Reader) (InputEvent, error) {
	var ev InputEvent
	if err := binary.Read(r, binary.LittleEndian, &ev); err != nil {
		return InputEvent{}, err
	}
	return ev, nil
}

func eventTime(ev InputEvent) time.Time {
	return time.Unix(int64(ev.Sec), int64(ev.Usec)*1000)
}
