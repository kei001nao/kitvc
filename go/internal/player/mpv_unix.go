//go:build !windows

package player

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"syscall"
)

func mpvDial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

func mpvIsRunning(cmd *exec.Cmd) bool {
	err := cmd.Process.Signal(os.Signal(syscall.Signal(0)))
	return err == nil
}

func mpvCleanupSocket(path string) {
	os.Remove(path)
}

func MpvSocketPath(pid int) string {
	return fmt.Sprintf("/tmp/kitvc-mpv-%d.sock", pid)
}
