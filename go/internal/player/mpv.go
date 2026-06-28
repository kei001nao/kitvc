package player

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sync"
	"time"
)

type cmdRequest struct {
	data   []byte
	result chan<- error
}

type MpvPlayer struct {
	cmd           *exec.Cmd
	socketPath    string
	args          []string
	conn          net.Conn
	queue         []string
	currentIdx    int
	pending       map[uint32]chan interface{}
	mu            sync.Mutex
	completedPath string
	cmdCh         chan cmdRequest
}

func NewMpvPlayer(socketPath string, args []string) *MpvPlayer {
	return &MpvPlayer{
		socketPath: socketPath,
		args:       args,
		currentIdx: -1,
		pending:    make(map[uint32]chan interface{}),
	}
}

func (p *MpvPlayer) Start() error {
	log.Printf("[DEBUG] MpvPlayer.Start: socketPath=%s", p.socketPath)
	mpvCleanupSocket(p.socketPath)

	fullArgs := append([]string{
		"--idle",
		"--input-ipc-server=" + p.socketPath,
		"--no-video",
		"--gapless-audio=yes",
		"--prefetch-playlist=yes",
	}, p.args...)

	p.cmd = exec.Command("mpv", fullArgs...)

	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	mpvSetSysProcAttr(p.cmd)

	if err := p.cmd.Start(); err != nil {
		log.Printf("[DEBUG] MpvPlayer.Start: failed: %v", err)
		return fmt.Errorf("failed to start mpv: %w", err)
	}
	log.Printf("[DEBUG] MpvPlayer.Start: mpv started, pid=%d", p.cmd.Process.Pid)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
		}
	}()

	return nil
}

func (p *MpvPlayer) ensureIPC() error {
	if p.conn != nil {
		return nil
	}
	log.Printf("[DEBUG] MpvPlayer.ensureIPC: connecting to %s", p.socketPath)

	var conn net.Conn
	var dialErr error
	for i := 0; i < 30; i++ {
		conn, dialErr = mpvDial(p.socketPath)
		if dialErr == nil {
			log.Printf("[DEBUG] MpvPlayer.ensureIPC: connected on attempt %d", i+1)
			break
		}

		if p.cmd != nil && p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			log.Printf("[DEBUG] MpvPlayer.ensureIPC: mpv exited during connect")
			return fmt.Errorf("mpv exited before ipc connect")
		}

		time.Sleep(200 * time.Millisecond)
	}

	if dialErr != nil {
		log.Printf("[DEBUG] MpvPlayer.ensureIPC: failed after 30 attempts: %v", dialErr)
		return fmt.Errorf("failed to connect to mpv ipc after 6s: %w", dialErr)
	}

	p.conn = conn
	log.Printf("[DEBUG] MpvPlayer.ensureIPC: spawning ipc handler")
	p.startIPC()
	return nil
}

func (p *MpvPlayer) processLine(line []byte) {
	line = bytes.TrimRight(line, "\r\n")
	if len(line) == 0 {
		return
	}
	log.Printf("[DEBUG] processLine: got %d bytes: %s", len(line), string(line))

	var resp map[string]interface{}
	if err := json.Unmarshal(line, &resp); err != nil {
		log.Printf("[DEBUG] processLine: json error: %v", err)
		return
	}

	if _, ok := resp["event"]; ok {
		p.handleEvent(resp)
	} else if id, ok := resp["request_id"].(float64); ok {
		p.mu.Lock()
		ch, exists := p.pending[uint32(id)]
		if exists {
			delete(p.pending, uint32(id))
			ch <- resp["data"]
		}
		p.mu.Unlock()
	}
}

func (p *MpvPlayer) handleEvent(event map[string]interface{}) {
	evtName, _ := event["event"].(string)

	switch evtName {
	case "file-loaded":
		p.mu.Lock()
		if p.currentIdx >= 0 && p.currentIdx+1 < len(p.queue) {
			nextPath := p.queue[p.currentIdx+1]
			p.mu.Unlock()
			p.sendCommandAsync(map[string]interface{}{
				"command": []interface{}{"loadfile", nextPath, "append"},
			})
		} else {
			p.mu.Unlock()
		}
	case "end-file":
		reason, _ := event["reason"].(string)
		switch reason {
		case "eof":
			p.mu.Lock()
			completed := ""
			if p.currentIdx >= 0 && p.currentIdx < len(p.queue) {
				completed = p.queue[p.currentIdx]
			}
			if p.currentIdx >= 0 && p.currentIdx+1 < len(p.queue) {
				p.currentIdx++
			}
			p.completedPath = completed
			p.mu.Unlock()
		case "quit", "error":
			p.mu.Lock()
			p.currentIdx = -1
			p.queue = nil
			p.mu.Unlock()
		}
	}
}

func (p *MpvPlayer) AddTracks(paths []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.queue = append(p.queue, paths...)
}

func (p *MpvPlayer) PlayQueue(paths []string, startIdx int) error {
	p.mu.Lock()
	p.queue = paths
	p.currentIdx = startIdx
	if startIdx < 0 || startIdx >= len(paths) {
		p.mu.Unlock()
		return nil
	}
	path := paths[startIdx]
	var nextPath string
	if startIdx+1 < len(paths) {
		nextPath = paths[startIdx+1]
	}
	p.mu.Unlock()

	if err := p.LoadFile(path); err != nil {
		return err
	}
	if nextPath != "" {
		p.sendCommandAsync(map[string]interface{}{
			"command": []interface{}{"loadfile", nextPath, "append"},
		})
	}
	return nil
}

func (p *MpvPlayer) GetCurrentTrackPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.currentIdx >= 0 && p.currentIdx < len(p.queue) {
		return p.queue[p.currentIdx]
	}
	return ""
}

func (p *MpvPlayer) ConsumeCompletedPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	path := p.completedPath
	p.completedPath = ""
	return path
}

func (p *MpvPlayer) IsRunning() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	return mpvIsRunning(p.cmd)
}

func (p *MpvPlayer) EnsureRunning() error {
	if p.conn != nil && p.IsRunning() {
		return nil
	}
	log.Printf("[DEBUG] EnsureRunning: conn=%v IsRunning=%v", p.conn != nil, p.IsRunning())
	if !p.IsRunning() {
		log.Printf("[DEBUG] EnsureRunning: mpv not running, restarting")
		p.Stop()
		if err := p.Start(); err != nil {
			log.Printf("[DEBUG] EnsureRunning: restart failed: %v", err)
			return err
		}
	}
	return p.ensureIPC()
}

func (p *MpvPlayer) Stop() {
	if p.cmd != nil && p.cmd.Process != nil {
		mpvKill(p.cmd)
		p.cmd = nil
	}
	p.mu.Lock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	p.mu.Unlock()
}

func (p *MpvPlayer) LoadFile(path string) error {
	if err := p.EnsureRunning(); err != nil {
		return err
	}
	p.SetProperty("pause", false)

	cmd := map[string]interface{}{
		"command": []interface{}{"loadfile", path, "replace"},
	}
	return p.sendCommand(cmd)
}

func (p *MpvPlayer) Play() error {
	return p.SetProperty("pause", false)
}

func (p *MpvPlayer) Pause() error {
	return p.SetProperty("pause", true)
}

func (p *MpvPlayer) CyclePause() error {
	if err := p.EnsureRunning(); err != nil {
		return err
	}
	cmd := map[string]interface{}{
		"command": []interface{}{"cycle", "pause"},
	}
	return p.sendCommand(cmd)
}

func (p *MpvPlayer) Seek(seconds float64) error {
	if err := p.EnsureRunning(); err != nil {
		return err
	}
	cmd := map[string]interface{}{
		"command": []interface{}{"seek", seconds, "relative"},
	}
	return p.sendCommand(cmd)
}

func (p *MpvPlayer) AdjustVolume(delta float64) error {
	if err := p.EnsureRunning(); err != nil {
		return err
	}
	cmd := map[string]interface{}{
		"command": []interface{}{"add", "volume", delta},
	}
	return p.sendCommand(cmd)
}

func (p *MpvPlayer) GetVolume() (float64, error) {
	val, err := p.GetProperty("volume")
	if err != nil {
		return 0, err
	}
	if v, ok := val.(float64); ok {
		return v, nil
	}
	return 0, fmt.Errorf("volume property is not a number")
}

func (p *MpvPlayer) GetProperty(name string) (interface{}, error) {
	if err := p.EnsureRunning(); err != nil {
		return nil, err
	}

	return p.getProperty(name)
}

func (p *MpvPlayer) TryGetProperty(name string) (interface{}, error) {
	p.mu.Lock()
	conn := p.conn
	running := p.IsRunning()
	p.mu.Unlock()
	if conn == nil || !running {
		return nil, fmt.Errorf("player not running")
	}

	return p.getProperty(name)
}

func (p *MpvPlayer) getProperty(name string) (interface{}, error) {
	requestID := uint32(time.Now().UnixNano())
	ch := make(chan interface{}, 1)

	p.mu.Lock()
	p.pending[requestID] = ch
	p.mu.Unlock()

	cmd := map[string]interface{}{
		"command":    []interface{}{"get_property", name},
		"request_id": requestID,
	}
	if err := p.sendCommand(cmd); err != nil {
		p.mu.Lock()
		delete(p.pending, requestID)
		p.mu.Unlock()
		return nil, err
	}

	log.Printf("[DEBUG] GetProperty(%s): waiting for response", name)
	select {
	case res := <-ch:
		log.Printf("[DEBUG] GetProperty(%s): got response", name)
		return res, nil
	case <-time.After(500 * time.Millisecond):
		log.Printf("[DEBUG] GetProperty(%s): timeout", name)
		p.mu.Lock()
		delete(p.pending, requestID)
		p.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for property %s", name)
	}
}

func (p *MpvPlayer) sendCommand(cmd interface{}) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	log.Printf("[DEBUG] sendCommand: writing %d bytes", len(data))
	return p.writeToPipe(data)
}

func (p *MpvPlayer) sendCommandAsync(cmd interface{}) {
	data, err := json.Marshal(cmd)
	if err != nil {
		return
	}
	data = append(data, '\n')
	log.Printf("[DEBUG] sendCommandAsync: writing %d bytes", len(data))
	select {
	case p.cmdCh <- cmdRequest{data: data, result: nil}:
	default:
		log.Printf("[DEBUG] sendCommandAsync: cmdCh full, dropping command")
	}
}

func (p *MpvPlayer) SetProperty(name string, value interface{}) error {
	if err := p.EnsureRunning(); err != nil {
		return err
	}
	cmd := map[string]interface{}{
		"command": []interface{}{"set_property", name, value},
	}
	return p.sendCommand(cmd)
}
