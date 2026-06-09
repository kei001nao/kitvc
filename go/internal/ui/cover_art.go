package ui

import (
	"image"
	_ "image/jpeg"
	"os"
	"strings"
)

type coverArt struct {
	path   string
	cols   int
	rows   int
	art    string
	cached bool
}

func (c *coverArt) load(path string, cols, maxRows int) bool {
	if c.path == path && c.cols == cols && c.cached {
		return false
	}
	c.path = path
	c.cols = cols
	c.rows = 0
	c.art = ""
	c.cached = false

	if path == "" || cols <= 0 || maxRows <= 0 {
		return false
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return false
	}

	b := img.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return false
	}

	rows := int(float64(srcH) * float64(cols) / float64(srcW) / 2.0)
	if rows < 6 {
		rows = 6
	}
	if rows > maxRows {
		rows = maxRows
	}

	c.rows = rows

	// Blank lines for sidebar spacing
	var sb strings.Builder
	for i := 0; i < rows; i++ {
		sb.WriteString(strings.Repeat(" ", cols))
		if i < rows-1 {
			sb.WriteString("\n")
		}
	}
	c.art = sb.String()
	c.cached = true
	return true
}
