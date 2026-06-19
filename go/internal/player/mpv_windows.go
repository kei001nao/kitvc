//go:build windows

package player

import (
	"fmt"
	"net"
	"os/exec"
)

func mpvDial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

func mpvIsRunning(cmd *exec.Cmd) bool {
	return cmd.ProcessState == nil || !cmd.ProcessState.Exited()
}

func mpvCleanupSocket(path string) {
}

func MpvSocketPath(pid int) string {
	return fmt.Sprintf("\\\\.\\pipe\\kitvc-mpv-%d", pid)
}
