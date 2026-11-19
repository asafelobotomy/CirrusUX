//go:build linux

package gateway

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"
)

// startPTY starts cmd on a new pseudo-terminal of the given size and
// returns its controlling side. Linux only, with the standard library:
// /dev/ptmx, unlock, find the pts number, open it as the child's terminal.
func startPTY(cmd *exec.Cmd, cols, rows int) (*os.File, error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var n uint32
	if err := ioctl(m, syscall.TIOCSPTLCK, unsafe.Pointer(new(int32))); err != nil {
		m.Close()
		return nil, err
	}
	if err := ioctl(m, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		m.Close()
		return nil, err
	}
	sl, err := os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		return nil, err
	}
	defer sl.Close()
	if err := setPTYSize(m, cols, rows); err != nil {
		m.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = sl, sl, sl
	// A new session with the terminal as its controlling one (fd 0).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

// setPTYSize tells the terminal its size, which the program reads.
func setPTYSize(m *os.File, cols, rows int) error {
	ws := struct{ Row, Col, X, Y uint16 }{uint16(rows), uint16(cols), 0, 0}
	return ioctl(m, syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}
