package ui

import (
	"image"
	_ "image/jpeg"
	"log"
	"os"
	"strings"
)

type coverArt struct {
	path    string
	cols    int // sidebar width input (for cache invalidation)
	imgCols int // actual image display width in character cells
	rows    int
	art     string
	cached  bool
}

func (c *coverArt) load(path string, cols, maxRows int) bool {
	if c.path == path && c.cols == cols && c.cached {
		return false
	}
	changed := (c.path != path) || (c.cols != cols)
	c.path = path
	c.cols = cols
	c.imgCols = 0
	c.rows = 0
	c.art = ""
	c.cached = false

	if path == "" || cols <= 0 || maxRows <= 0 {
		return changed
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

	// Calculate natural image width in character cells (fontWidth ≈ 7px)
	imgCols := srcW / 7
	if imgCols < 6 {
		imgCols = 6
	}
	if imgCols > cols {
		imgCols = cols
	}

	rows := int(float64(srcH) * float64(imgCols) / float64(srcW) / 2.0)
	if rows < 6 {
		rows = 6
	}
	if rows > maxRows {
		rows = maxRows
	}

	c.imgCols = imgCols
	c.rows = rows
	log.Printf("[DEBUG] coverImage: srcW=%d srcH=%d availCols=%d imgCols=%d rows=%d", srcW, srcH, cols, imgCols, rows)

	// Blank lines for sidebar spacing (use imgCols width)
	var sb strings.Builder
	for i := 0; i < rows; i++ {
		sb.WriteString(strings.Repeat(" ", imgCols))
		if i < rows-1 {
			sb.WriteString("\n")
		}
	}
	c.art = sb.String()
	c.cached = true
	return true
}
