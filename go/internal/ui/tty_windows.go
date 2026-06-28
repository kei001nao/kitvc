//go:build windows

package ui

import "os"

func openTTY() ttyWriter {
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return os.Stdout
	}
	return f
}
