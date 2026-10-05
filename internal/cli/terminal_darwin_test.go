package cli_test

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// openTerminal opens a terminal pair, as a terminal window does, and returns
// the path of its terminal end, such as /dev/ttys003. macOS names that end
// after the minor device number of the other end.
func openTerminal(t *testing.T) string {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skip("no /dev/ptmx: " + err.Error())
	}

	t.Cleanup(func() { master.Close() })

	fd := int(master.Fd())
	must(t, unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0))
	must(t, unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0))

	var stat unix.Stat_t
	must(t, unix.Fstat(fd, &stat))

	return fmt.Sprintf("/dev/ttys%03d", unix.Minor(uint64(stat.Rdev)))
}
