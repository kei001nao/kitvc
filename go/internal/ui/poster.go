package ui

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

type posterState struct {
	lastRow     int
	lastCol     int
	lastPath    string
	displayed   bool
	cachedKitty []byte
}

func newPosterState() posterState {
	return posterState{
		lastRow:     -1,
		lastCol:     -1,
		lastPath:    "",
		displayed:   false,
		cachedKitty: nil,
	}
}

// --- videoFetchModal poster methods ---

func (m *videoFetchModal) PosterPos() (row, col, w, h int) {
	sl := m.OverlayStartLine()
	sc := m.OverlayStartCol()
	row = sl + 2 + m.HeaderHeight()
	col = sc + 2
	w = m.PosterCols()
	h = m.PosterRows()
	return
}

func (m *videoFetchModal) PosterClearCmd() tea.Cmd {
	row, col, w, h := m.PosterPos()
	if w <= 0 || h <= 0 {
		return nil
	}
	return func() tea.Msg {
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f.Close()
		fmt.Fprintf(f, "\x1b[%d;%dH", row+1, col+1)
		f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		f.WriteString("\x1b[?25l")
		f.Sync()
		return nil
	}
}

func (m *videoFetchModal) PosterDisplayCmd() tea.Cmd {
	if m.PosterPath() == "" {
		return nil
	}
	path := m.PosterPath()
	cols := m.PosterCols()
	rows := m.PosterRows()
	oldRow, oldCol := m.ps.lastRow, m.ps.lastCol

	return func() tea.Msg {
		row, col, _, _ := m.PosterPos()

		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		fmt.Fprintf(f, "\x1b[%d;%dH", row+1, col+1)
		f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		if oldRow >= 0 && (oldRow != row || oldCol != col) {
			fmt.Fprintf(f, "\x1b[%d;%dH", oldRow+1, oldCol+1)
			f.WriteString("\x1b_Ga=d,d=c\x1b\\")
		}
		f.Close()

		cmd := exec.Command("chafa", "-f", "kitty",
			"--symbols", "none",
			"--probe", "off",
			"--size", fmt.Sprintf("%dx%d", cols, rows),
			path)
		cmd.Env = append(os.Environ(), "TERM=xterm-kitty")
		out, err := cmd.Output()
		if err != nil {
			return nil
		}

		f2, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return nil
		}
		defer f2.Close()
		f2.WriteString("\x1b[?25l")
		fmt.Fprintf(f2, "\x1b[%d;%dH", row+1, col+1)
		f2.Write(out)
		f2.WriteString("\x1b[?25l")
		f2.Sync()

		m.ps.lastRow, m.ps.lastCol = row, col
		return nil
	}
}

func (m *videoFetchModal) TriggerPosterDisplay() tea.Cmd {
	path := m.PosterPath()
	if path == "" {
		m.ps.displayed = false
		m.ps.lastPath = ""
		return nil
	}
	row, col, _, _ := m.PosterPos()
	if row < 0 || col < 0 {
		return nil
	}
	needsDisplay := !m.ps.displayed
	needsDisplay = needsDisplay || path != m.ps.lastPath
	needsDisplay = needsDisplay || row != m.ps.lastRow || col != m.ps.lastCol
	if needsDisplay {
		m.ps.displayed = true
		m.ps.lastPath = path
		return m.PosterDisplayCmd()
	}
	return nil
}

func (m *videoFetchModal) ResetPoster() {
	m.ps = newPosterState()
}

// --- videoEditModal poster methods ---

func (m *videoEditModal) PosterPos() (row, col, w, h int) {
	sl := m.OverlayStartLine()
	sc := m.OverlayStartCol()
	w = m.PosterCols()
	h = m.PosterRows()
	dialogW := m.Width() - 4
	if dialogW < 40 {
		dialogW = 40
	}
	if dialogW > 80 {
		dialogW = 80
	}
	row = sl + 1 + m.HeaderHeight()
	col = sc + 2 + (dialogW-w)/2
	log.Printf("[poster-debug] PosterPos: sl=%d sc=%d dialogW=%d headerH=%d → row=%d col=%d", sl, sc, dialogW, m.HeaderHeight(), row, col)
	return
}

func (m *videoEditModal) PosterClearCmd() tea.Cmd {
	m.ps = newPosterState()
	return func() tea.Msg {
		var buf bytes.Buffer
		buf.WriteString("\x1b_Ga=d\x1b\\")
		buf.WriteString("\x1b[?25l")

		tmpFile, err := os.CreateTemp("", "poster-clear-*.bin")
		if err != nil {
			return nil
		}
		tmpFile.Write(buf.Bytes())
		tmpFile.Close()
		defer os.Remove(tmpFile.Name())

		cmd := exec.Command("sh", "-c",
			fmt.Sprintf("cat %s > /dev/tty", tmpFile.Name()))
		cmd.Run()
		return nil
	}
}

// PosterDisplayCmd draws the poster via a SEPARATE PROCESS.
// The kitty data is written to a temp file, then a shell process
// sleeps briefly (for Bubbletea to finish rendering) and writes
// the data to /dev/tty. This avoids interleaving with Bubbletea's
// stdout writes to the same terminal.
func (m *videoEditModal) PosterDisplayCmd() tea.Cmd {
	if m.PosterPath() == "" {
		return nil
	}
	path := m.PosterPath()
	cols := m.PosterCols()
	rows := m.PosterRows()
	useCache := m.ps.cachedKitty != nil && m.ps.lastPath == path
	log.Printf("poster PosterDisplayCmd: cache=%v path=%s", useCache, path)

	return func() tea.Msg {
		row, col, w, h := m.PosterPos()
		log.Printf("poster exec: pos=(%d,%d) size=(%d,%d) cache=%v", row, col, w, h, useCache)

		if w <= 0 || h <= 0 {
			log.Printf("poster exec: invalid size, skip")
			return nil
		}

		var out []byte
		if useCache {
			out = m.ps.cachedKitty
		} else {
			cmd := exec.Command("chafa", "-f", "kitty",
				"--symbols", "none",
				"--probe", "off",
				"--size", fmt.Sprintf("%dx%d", cols, rows),
				path)
			cmd.Env = append(os.Environ(), "TERM=xterm-kitty")
			var err error
			out, err = cmd.Output()
			if err != nil {
				log.Printf("poster chafa error: %v", err)
				return nil
			}
			m.ps.cachedKitty = out
			m.ps.lastPath = path
		}

		// Build complete terminal output
		var buf bytes.Buffer
		buf.WriteString("\x1b_Ga=d\x1b\\")
		fmt.Fprintf(&buf, "\x1b[%d;%dH", row+1, col+1)
		buf.Write(out)
		buf.WriteString("\x1b[?25l")

		// Write to temp file
		tmpFile, err := os.CreateTemp("", "poster-*.kitty")
		if err != nil {
			log.Printf("poster temp error: %v", err)
			return nil
		}
		if _, err := tmpFile.Write(buf.Bytes()); err != nil {
			tmpFile.Close()
			os.Remove(tmpFile.Name())
			return nil
		}
		tmpFile.Close()

		// Display via separate process with delay
		tmpPath := tmpFile.Name()
		script := fmt.Sprintf("sleep 0.1 && cat %s > /dev/tty && rm -f %s",
			tmpPath, tmpPath)
		dispCmd := exec.Command("sh", "-c", script)
		dispCmd.Run()

		log.Printf("poster DRAWN at (%d,%d) size=%d", row, col, len(out))
		m.ps.lastRow, m.ps.lastCol = row, col
		return nil
	}
}

func (m *videoEditModal) TriggerPosterDisplay() tea.Cmd {
	path := m.PosterPath()
	if path == "" {
		log.Printf("[poster-debug] TriggerPosterDisplay: path empty, skip")
		m.ps.displayed = false
		m.ps.lastPath = ""
		return nil
	}
	sl := m.OverlayStartLine()
	if sl < 0 {
		log.Printf("[poster-debug] TriggerPosterDisplay: sl=%d, skip", sl)
		return nil
	}

	row, col, _, _ := m.PosterPos()
	log.Printf("[poster-debug] TriggerPosterDisplay: path=%q sl=%d pos=(%d,%d) displayed=%v lastRow=%d lastCol=%d lastPath=%q",
		path, sl, row, col, m.ps.displayed, m.ps.lastRow, m.ps.lastCol, m.ps.lastPath)
	if row < 0 || col < 0 {
		log.Printf("[poster-debug] TriggerPosterDisplay: invalid pos, skip")
		return nil
	}

	needsDisplay := !m.ps.displayed
	needsDisplay = needsDisplay || path != m.ps.lastPath
	needsDisplay = needsDisplay || row != m.ps.lastRow || col != m.ps.lastCol

	if needsDisplay {
		log.Printf("[poster-debug] TriggerPosterDisplay: NEEDS display (displayed=%v pathChanged=%v posChanged=%v)",
			m.ps.displayed, path != m.ps.lastPath, row != m.ps.lastRow || col != m.ps.lastCol)
		m.ps.displayed = true
		m.ps.lastPath = path
		m.ps.lastRow = row
		m.ps.lastCol = col
		m.ps.cachedKitty = nil
		return m.PosterDisplayCmd()
	}
	log.Printf("[poster-debug] TriggerPosterDisplay: no change needed")
	return nil
}

func (m *videoEditModal) ResetPoster() {
	m.ps = newPosterState()
}
