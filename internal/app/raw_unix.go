//go:build !windows

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// rawState is the terminal mode to restore.
type rawState struct{ termios unix.Termios }

// makeRaw is libuv's raw mode (what ink's setRawMode asks for): no echo, no
// line buffering, no signals — but output post-processing stays on, so "\n"
// still moves to the start of the next line.
func makeRaw() (*rawState, error) {
	fd := int(os.Stdin.Fd())
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	saved := *t
	t.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	t.Cflag |= unix.CS8
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, t); err != nil {
		return nil, err
	}
	return &rawState{saved}, nil
}

func (s *rawState) restore() {
	unix.IoctlSetTermios(int(os.Stdin.Fd()), ioctlSetTermios, &s.termios)
}
