//go:build windows

package ui

import "os"

func openTTY() ttyWriter {
	return os.Stdout
}
