//go:build windows

package ui

import (
	"fmt"

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
