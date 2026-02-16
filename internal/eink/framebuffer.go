package eink

import (
	"errors"
	"fmt"
	"image"
	"os"
	"syscall"
	"unsafe"
)

const (
	iocNRBits   = 8
	iocTypeBits = 8
	iocSizeBits = 14
	iocDirBits  = 2

	iocNone  = 0
	iocWrite = 1
	iocRead  = 2

	iocNRShift   = 0
	iocTypeShift = iocNRShift + iocNRBits
	iocSizeShift = iocTypeShift + iocTypeBits
	iocDirShift  = iocSizeShift + iocSizeBits
)

func ioc(dir, iocType, nr, size uintptr) uintptr {
	return (dir << iocDirShift) | (iocType << iocTypeShift) | (nr << iocNRShift) | (size << iocSizeShift)
}

func ior(iocType, nr, size uintptr) uintptr {
	return ioc(iocRead, iocType, nr, size)
}

const (
	fbIOGetVScreenInfo = 'F'
	fbIOGetFScreenInfo = 'F'
)

type fbFixScreeninfo struct {
	ID         [16]byte
	SMemStart  uint32
	SMemLen    uint32
	Type       uint32
	TypeAux    uint32
	Visual     uint32
	XPanStep   uint16
	YPanStep   uint16
	YWrapStep  uint16
	LineLength uint32
	MMIOStart  uint32
	MMIOLen    uint32
	Accel      uint32
	Cap        uint16
	Reserved   [2]uint16
}

type fbVarScreeninfo struct {
	XRes         uint32
	YRes         uint32
	XResVirtual  uint32
	YResVirtual  uint32
	XOffset      uint32
	YOffset      uint32
	BitsPerPixel uint32
	Grayscale    uint32
	Red          fbBitfield
	Green        fbBitfield
	Blue         fbBitfield
	Transp       fbBitfield
	NonStd       uint32
	Activate     uint32
	Height       uint32
	Width        uint32
	AccelFlags   uint32
	Pixclock     uint32
	LeftMargin   uint32
	RightMargin  uint32
	UpperMargin  uint32
	LowerMargin  uint32
	HsyncLen     uint32
	VsyncLen     uint32
	Sync         uint32
	Vmode        uint32
	Rotate       uint32
	Colorspace   uint32
	Reserved     [4]uint32
}

type fbBitfield struct {
	Offset   uint32
	Length   uint32
	MSBRight uint32
}

type Framebuffer struct {
	file   *os.File
	data   []byte
	Width  int
	Height int
	Stride int
	BPP    int
}

func Open(path string) (*Framebuffer, error) {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	var vinfo fbVarScreeninfo
	var finfo fbFixScreeninfo
	if err := ioctl(file.Fd(), ior(fbIOGetVScreenInfo, 0x00, unsafe.Sizeof(vinfo)), unsafe.Pointer(&vinfo)); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := ioctl(file.Fd(), ior(fbIOGetFScreenInfo, 0x02, unsafe.Sizeof(finfo)), unsafe.Pointer(&finfo)); err != nil {
		_ = file.Close()
		return nil, err
	}
	if vinfo.BitsPerPixel != 8 {
		_ = file.Close()
		return nil, fmt.Errorf("unsupported bpp: %d", vinfo.BitsPerPixel)
	}
	length := int(finfo.SMemLen)
	data, err := syscall.Mmap(int(file.Fd()), 0, length, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Framebuffer{
		file:   file,
		data:   data,
		Width:  int(vinfo.XRes),
		Height: int(vinfo.YRes),
		Stride: int(finfo.LineLength),
		BPP:    int(vinfo.BitsPerPixel),
	}, nil
}

func NewFramebufferFromBuffer(width, height int) *Framebuffer {
	return &Framebuffer{
		data:   make([]byte, width*height),
		Width:  width,
		Height: height,
		Stride: width,
		BPP:    8,
	}
}

func (fb *Framebuffer) Close() error {
	if fb == nil {
		return nil
	}
	if fb.data != nil {
		_ = syscall.Munmap(fb.data)
		fb.data = nil
	}
	if fb.file != nil {
		return fb.file.Close()
	}
	return nil
}

func (fb *Framebuffer) WriteGray(img *image.Gray) error {
	if fb == nil || fb.data == nil {
		return errors.New("framebuffer not initialized")
	}
	bounds := img.Bounds()
	if bounds.Dx() != fb.Width || bounds.Dy() != fb.Height {
		return fmt.Errorf("image size %dx%d does not match framebuffer %dx%d", bounds.Dx(), bounds.Dy(), fb.Width, fb.Height)
	}
	for y := 0; y < fb.Height; y++ {
		src := img.Pix[y*img.Stride : y*img.Stride+fb.Width]
		dst := fb.data[y*fb.Stride : y*fb.Stride+fb.Width]
		copy(dst, src)
	}
	return nil
}

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
