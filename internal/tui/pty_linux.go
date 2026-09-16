//go:build linux

package tui

import (
	"fmt"
	"os"
	"strconv"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY opens a new pseudo-terminal pair. The slave end is a genuine
// terminal device, so code that only activates its terminal behavior
// against an *os.File with working termios ioctls (like EnableRawInput)
// behaves exactly as it would against a real user terminal, unlike the
// strings.Reader/bufio.Reader pipes this package's other tests use.
//
// This uses the low-level TIOCSPTLCK/TIOCGPTN ioctls directly via
// unix.Syscall rather than the higher-level unix.IoctlSetInt/IoctlGetInt
// helpers, which did not work for TIOCSPTLCK in local testing; the direct
// syscall form does.
func openPTY() (master, slave *os.File, err error) {
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	master = os.NewFile(uintptr(fd), "/dev/ptmx")

	var unlock int32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCSPTLCK), uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		master.Close()
		return nil, nil, fmt.Errorf("unlock pty: %w", errno)
	}

	var n int32
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCGPTN), uintptr(unsafe.Pointer(&n))); errno != 0 {
		master.Close()
		return nil, nil, fmt.Errorf("get pty number: %w", errno)
	}

	slavePath := "/dev/pts/" + strconv.Itoa(int(n))
	slave, err = os.OpenFile(slavePath, os.O_RDWR, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("open %s: %w", slavePath, err)
	}
	return master, slave, nil
}
