package player

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type MpvPlayer struct {
	cmd        *exec.Cmd
	socketPath string
	args       []string
	conn       net.Conn
	queue      []string
	currentIdx int
	pending    map[uint32]chan interface{}
	mu         sync.Mutex
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
	// Clean up existing socket
	os.Remove(p.socketPath)

	fullArgs := append([]string{
		"--idle",
		"--input-ipc-server=" + p.socketPath,
		"--no-video",
		"--gapless-audio=yes",
		"--prefetch-playlist=yes",
	}, p.args...)

	p.cmd = exec.Command("mpv", fullArgs...)

	// Capture stderr for debugging
	stderr, err := p.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start mpv: %w", err)
	}

	// Read stderr in a goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// log.Printf("mpv stderr: %s", scanner.Text())
		}
	}()

	// Wait for socket to be ready
	var conn net.Conn
	var dialErr error
	for i := 0; i < 30; i++ {
		conn, dialErr = net.Dial("unix", p.socketPath)
		if dialErr == nil {
			break
		}
		
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			return fmt.Errorf("mpv exited immediately")
		}
		
		time.Sleep(200 * time.Millisecond)
	}

	if dialErr != nil {
		return fmt.Errorf("failed to connect to mpv ipc after 6s: %w", dialErr)
	}

	p.conn = conn
	
	// Start event loop
	go p.readLoop()
	
	return nil
}

func (p *MpvPlayer) readLoop() {
	scanner := bufio.NewScanner(p.conn)
	for scanner.Scan() {
		var resp map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			continue
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
}

func (p *MpvPlayer) handleEvent(event map[string]interface{}) {
	evtName, _ := event["event"].(string)
	
	switch evtName {
	case "file-loaded":
		// Auto prefetch next track for gapless
		p.mu.Lock()
		if p.currentIdx >= 0 && p.currentIdx+1 < len(p.queue) {
			nextPath := p.queue[p.currentIdx+1]
			p.mu.Unlock()
			p.sendCommand(map[string]interface{}{
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
			if p.currentIdx >= 0 && p.currentIdx+1 < len(p.queue) {
				p.currentIdx++
			}
			p.mu.Unlock()
		case "quit", "stop", "error":
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
	p.mu.Unlock()
	return p.LoadFile(path)
}

func (p *MpvPlayer) GetCurrentTrackPath() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.currentIdx >= 0 && p.currentIdx < len(p.queue) {
		return p.queue[p.currentIdx]
	}
	return ""
}

func (p *MpvPlayer) IsRunning() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	err := p.cmd.Process.Signal(os.Signal(syscall.Signal(0)))
	return err == nil
}

func (p *MpvPlayer) EnsureRunning() error {
	if p.conn != nil && p.IsRunning() {
		return nil
	}
	p.Stop()
	return p.Start()
}

func (p *MpvPlayer) Stop() {
	p.mu.Lock()
	if p.conn != nil {
		p.conn.Close()
		p.conn = nil
	}
	p.mu.Unlock()
	if p.cmd != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
		p.cmd.Wait()
		p.cmd = nil
	}
}

func (p *MpvPlayer) LoadFile(path string) error {
	if err := p.EnsureRunning(); err != nil {
		return err
	}
	p.SetProperty("pause", false)

	// Use 'replace' to clear current playlist and start new one
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

	select {
	case res := <-ch:
		return res, nil
	case <-time.After(500 * time.Millisecond):
		p.mu.Lock()
		delete(p.pending, requestID)
		p.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for property %s", name)
	}
}

func (p *MpvPlayer) sendCommand(cmd interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn == nil {
		return fmt.Errorf("mpv not connected")
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}
	_, err = p.conn.Write(append(data, '\n'))
	if err != nil {
		p.conn.Close()
		p.conn = nil
		return fmt.Errorf("failed to send command to mpv: %w", err)
	}
	return nil
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
