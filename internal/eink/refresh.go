package eink

import (
	"image"
	"syscall"
	"unsafe"
)

type Update struct {
	Region   image.Rectangle
	Full     bool
	Fast     bool
	Waveform int
}

const (
	UpdateModePartial = 0
	UpdateModeFull    = 1
)

const (
	WaveformModeInit = 0
	WaveformModeDU   = 1
	WaveformModeGC16 = 2
	WaveformModeGC4  = 3
	WaveformModeA2   = 4
	WaveformModeAuto = 257
)

type mxcfbRect struct {
	Top    uint32
	Left   uint32
	Width  uint32
	Height uint32
}

type mxcfbUpdateData struct {
	UpdateRegion mxcfbRect
	WaveformMode uint32
	UpdateMode   uint32
	UpdateMarker uint32
	Temp         int32
	Flags        uint32
	AltBuffer    uint32
	AltStride    uint32
}

func (fb *Framebuffer) Refresh(update Update) error {
	if fb == nil || fb.file == nil {
		return nil
	}
	region := update.Region
	if region.Empty() {
		region = image.Rect(0, 0, fb.Width, fb.Height)
	}
	updateMode := UpdateModeFull
	if !update.Full {
		updateMode = UpdateModePartial
	}
	waveform := WaveformModeAuto
	if update.Fast {
		waveform = WaveformModeA2
	}
	if update.Waveform != 0 {
		waveform = update.Waveform
	}
	data := mxcfbUpdateData{
		UpdateRegion: mxcfbRect{
			Top:    uint32(region.Min.Y),
			Left:   uint32(region.Min.X),
			Width:  uint32(region.Dx()),
			Height: uint32(region.Dy()),
		},
		WaveformMode: uint32(waveform),
		UpdateMode:   uint32(updateMode),
		Temp:         -1,
	}
	req := ioc(iocRead|iocWrite, 'F', 0x2E, unsafe.Sizeof(data))
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fb.file.Fd(), req, uintptr(unsafe.Pointer(&data)))
	if errno != 0 {
		return errno
	}
	return nil
}
