//go:build !windows

package ui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
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

func (d *chafaDisplayer) Draw(w io.Writer, path string, startRow, startCol, cols, rows int, imageID int) error {
	data, err := d.Render(path, cols, rows)
	if err != nil {
		return err
	}

	kittyOut := string(data)
	if strings.HasPrefix(kittyOut, "\x1b_G") {
		kittyOut = fmt.Sprintf("\x1b_Gi=%d,", imageID) + kittyOut[3:]
	}

	return d.DrawCached(w, kittyOut, startRow, startCol, cols, rows, imageID)
}

func (d *chafaDisplayer) Clear(w io.Writer, startRow, startCol, cols, rows int, imageID int) error {
	fmt.Fprintf(w, "\x1b_Ga=d,i=%d\x1b\\", imageID)
	return nil
}

func (d *chafaDisplayer) DrawCached(w io.Writer, data string, startRow, startCol, cols, rows int, imageID int) error {
	fmt.Fprint(w, "\x1b[?25l")
	fmt.Fprint(w, "\x1b[s") // Save cursor
	fmt.Fprintf(w, "\x1b_Ga=d,i=%d\x1b\\\x1b[%d;%dH", imageID, startRow, startCol)
	fmt.Fprint(w, data)
	fmt.Fprint(w, "\x1b[u") // Restore cursor
	fmt.Fprint(w, "\x1b[?25l")
	return nil
}

