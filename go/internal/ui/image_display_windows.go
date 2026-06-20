//go:build windows

package ui

import (
	"fmt"
	"io"
	"strings"

	"github.com/blacktop/go-termimg"
)

type termimgDisplayer struct{}

func newPlatformDisplayer() imageDisplayer {
	return &termimgDisplayer{}
}

func (d *termimgDisplayer) Render(path string, cols, rows int) ([]byte, error) {
	img, err := termimg.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open image: %w", err)
	}
	// Try Sixel for high-resolution rendering
	out, err := img.Width(cols).Height(rows).Protocol(termimg.Sixel).Render()
	if err != nil {
		// Fallback to auto-detection (which might fall back to halfblocks)
		out, err = img.Width(cols).Height(rows).Protocol(termimg.Auto).Render()
		if err != nil {
			return nil, fmt.Errorf("render image: %w", err)
		}
	}
	return []byte(out), nil
}

func (d *termimgDisplayer) Draw(w io.Writer, path string, startRow, startCol, cols, rows int, imageID int) error {
	data, err := d.Render(path, cols, rows)
	if err != nil {
		return err
	}
	return d.DrawCached(w, string(data), startRow, startCol, cols, rows, imageID)
}

func (d *termimgDisplayer) Clear(w io.Writer, startRow, startCol, cols, rows int, imageID int) error {
	// Hide cursor and save position using DECSC
	fmt.Fprint(w, "\x1b[?25l\x1b7")

	// Overwrite image area with spaces to clear Sixel residue
	for i := 0; i < rows; i++ {
		fmt.Fprintf(w, "\x1b[%d;%dH%s", startRow+i, startCol, strings.Repeat(" ", cols))
	}

	fmt.Fprint(w, "\x1b8\x1b[?25l")
	return nil
}

func (d *termimgDisplayer) DrawCached(w io.Writer, data string, startRow, startCol, cols, rows int, imageID int) error {
	isSixel := strings.Contains(data, "\x1bP")

	// Hide cursor and save position using DECSC
	fmt.Fprint(w, "\x1b[?25l\x1b7")

	// 1. Clear the area first to prevent residue/artifacts
	for i := 0; i < rows; i++ {
		fmt.Fprintf(w, "\x1b[%d;%dH%s", startRow+i, startCol, strings.Repeat(" ", cols))
	}

	// 2. Adjust Sixel header for background transparency (P2=1)
	if isSixel {
		if idx := strings.Index(data, "q"); idx != -1 && strings.HasPrefix(data, "\x1bP") {
			data = "\x1bP;1;q" + data[idx+1:]
		}
		// Move cursor back to starting coordinate and output Sixel
		fmt.Fprintf(w, "\x1b[%d;%dH", startRow, startCol)
		fmt.Fprint(w, data)
		// Reset character attributes and palette
		fmt.Fprint(w, "\x1b[0m\x1b]104\x1b\\")
	} else {
		// Halfblocks (ANSI fallback)
		lines := strings.Split(data, "\n")
		for i, line := range lines {
			if line == "" && i == len(lines)-1 {
				break
			}
			fmt.Fprintf(w, "\x1b[%d;%dH%s", startRow+i, startCol, line)
		}
	}

	// Restore cursor using DECRC
	fmt.Fprint(w, "\x1b8\x1b[?25l")
	return nil
}

