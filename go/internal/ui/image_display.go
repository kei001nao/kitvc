package ui

type imageDisplayer interface {
	Render(path string, cols, rows int) ([]byte, error)
}

func newImageDisplayer() imageDisplayer {
	return newPlatformDisplayer()
}
