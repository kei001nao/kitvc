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
	"time"
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
	p.cmdCh = make(chan cmdRequest, 64)
	go p.readLoop()
	go p.writeLoop()
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

func (p *MpvPlayer) writeLoop() {
	log.Printf("[DEBUG] MpvPlayer.writeLoop: started")
	for req := range p.cmdCh {
		p.mu.Lock()
		conn := p.conn
		p.mu.Unlock()
		if conn == nil {
			if req.result != nil {
				req.result <- fmt.Errorf("mpv not connected")
			}
			continue
		}
		_, err := conn.Write(req.data)
		if req.result != nil {
			req.result <- err
		}
		if err != nil {
			log.Printf("[DEBUG] writeLoop: write error: %v", err)
			p.mu.Lock()
			if p.conn != nil {
				p.conn.Close()
				p.conn = nil
			}
			p.mu.Unlock()
			return
		}
	}
}

func (p *MpvPlayer) writeToPipe(data []byte) error {
	ch := make(chan error, 1)
	select {
	case p.cmdCh <- cmdRequest{data: data, result: ch}:
	case <-time.After(3 * time.Second):
		return fmt.Errorf("timeout sending command to mpv")
	}
	select {
	case err := <-ch:
		if err != nil {
			return fmt.Errorf("failed to send command to mpv: %w", err)
		}
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("timeout waiting for mpv command result")
	}
}

