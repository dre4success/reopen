package term

import (
	"os"
	"syscall"
	"unsafe"
)

func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// MakeRaw puts the terminal f in raw mode: keys arrive one by one, unechoed,
// and Ctrl-C arrives as a key rather than a signal. Output is still
// processed, so "\n" starts a line. restore puts back what was there; call it
// on every way out.
func MakeRaw(f *os.File) (restore func(), err error) {
	var old syscall.Termios
	if err := ioctl(f, ioctlGetTermios, unsafe.Pointer(&old)); err != nil {
		return nil, err
	}
	raw := old
	raw.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP | syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	raw.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	raw.Cflag &^= syscall.CSIZE | syscall.PARENB
	raw.Cflag |= syscall.CS8
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0
	if err := ioctl(f, ioctlSetTermios, unsafe.Pointer(&raw)); err != nil {
		return nil, err
	}
	return func() { ioctl(f, ioctlSetTermios, unsafe.Pointer(&old)) }, nil
}

// Size is the terminal f's width in columns and height in lines, or 0 and
// 0 if it can't be told.
func Size(f *os.File) (cols, lines int) {
	var ws struct{ Row, Col, X, Y uint16 }
	if ioctl(f, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)) != nil {
		return 0, 0
	}
	return int(ws.Col), int(ws.Row)
}
