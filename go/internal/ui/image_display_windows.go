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
	out, err := img.Width(cols).Height(rows).Render()
	if err != nil {
		return nil, fmt.Errorf("render image: %w", err)
	}
	return []byte(out), nil
}
