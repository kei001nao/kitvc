//go:build !windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
)

type chafaDisplayer struct{}

func newPlatformDisplayer() imageDisplayer {
	return &chafaDisplayer{}
}

func (d *chafaDisplayer) Render(path string, cols, rows int) ([]byte, error) {
	cmd := exec.Command("chafa", "-f", "kitty",
		"--symbols", "none",
		"--probe", "off",
		"--size", fmt.Sprintf("%dx%d", cols, rows),
		path)
	cmd.Env = append(os.Environ(), "TERM=xterm-kitty")
	return cmd.Output()
}
