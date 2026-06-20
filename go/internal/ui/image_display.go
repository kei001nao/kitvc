package ui

import "io"

type imageDisplayer interface {
	Render(path string, cols, rows int) ([]byte, error)
	Draw(w io.Writer, path string, startRow, startCol, cols, rows int, imageID int) error
	Clear(w io.Writer, startRow, startCol, cols, rows int, imageID int) error
	DrawCached(w io.Writer, data string, startRow, startCol, cols, rows int, imageID int) error
}

func newImageDisplayer() imageDisplayer {
	return newPlatformDisplayer()
}

