//go:build !windows

package player

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"syscall"
)

func mpvDial(path string) (net.Conn, error) {
	return net.Dial("unix", path)
}

func mpvIsRunning(cmd *exec.Cmd) bool {
	err := cmd.Process.Signal(os.Signal(syscall.Signal(0)))
	return err == nil
}

func mpvSetSysProcAttr(cmd *exec.Cmd) {
}

func mpvCleanupSocket(path string) {
	os.Remove(path)
}

func MpvSocketPath(pid int) string {
	return fmt.Sprintf("/tmp/kitvc-mpv-%d.sock", pid)
}

func mpvKill(cmd *exec.Cmd) {
	cmd.Process.Kill()
	cmd.Wait()
}

func (p *MpvPlayer) startIPC() {
	go p.readLoop()
}

func (p *MpvPlayer) readLoop() {
	log.Printf("[DEBUG] MpvPlayer.readLoop: started")
	r := bufio.NewReader(p.conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			log.Printf("[DEBUG] readLoop: ReadBytes error: %v", err)
			return
		}
		p.processLine(line)
	}
}

func (p *MpvPlayer) writeToPipe(data []byte) error {
	p.mu.Lock()
	if p.conn == nil {
		p.mu.Unlock()
		return fmt.Errorf("mpv not connected")
	}
	conn := p.conn
	p.mu.Unlock()

	_, err := conn.Write(data)
	log.Printf("[DEBUG] sendCommand: write done err=%v", err)
	if err != nil {
		p.mu.Lock()
		p.conn.Close()
		p.conn = nil
		p.mu.Unlock()
		return fmt.Errorf("failed to send command to mpv: %w", err)
	}
	return nil
}
