//go:build windows

package player

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modkernel32       = syscall.NewLazyDLL("kernel32.dll")
	procPeekNamedPipe = modkernel32.NewProc("PeekNamedPipe")
)

type namedPipeConn struct {
	f      *os.File
	mu     sync.Mutex
	closed bool
}

func (c *namedPipeConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.mu.Unlock()
	return c.f.Read(b)
}

func (c *namedPipeConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.mu.Unlock()
	return c.f.Write(b)
}

func (c *namedPipeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.f.Close()
}

func (c *namedPipeConn) LocalAddr() net.Addr                { return nil }
func (c *namedPipeConn) RemoteAddr() net.Addr               { return nil }
func (c *namedPipeConn) SetDeadline(t time.Time) error      { return nil }
func (c *namedPipeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *namedPipeConn) SetWriteDeadline(t time.Time) error { return nil }

func mpvSetSysProcAttr(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = new(syscall.SysProcAttr)
	}
	cmd.SysProcAttr.CreationFlags = 0x08000000 // CREATE_NO_WINDOW

	nul, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		nul, _ = os.Open(os.DevNull)
	}
	cmd.Stdin = nul
	cmd.Stdout = nul
}

func mpvDial(path string) (net.Conn, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &namedPipeConn{f: f}, nil
}

func mpvIsRunning(cmd *exec.Cmd) bool {
	return cmd.ProcessState == nil || !cmd.ProcessState.Exited()
}

func mpvCleanupSocket(path string) {
}

func MpvSocketPath(pid int) string {
	return fmt.Sprintf("\\\\.\\pipe\\kitvc-mpv-%d", pid)
}

func mpvKill(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	cmd.Process.Kill()
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
}

func (p *MpvPlayer) startIPC() {
	p.cmdCh = make(chan cmdRequest, 64)
	go p.ipcLoop()
}

func (p *MpvPlayer) ipcLoop() {
	log.Printf("[DEBUG] MpvPlayer.ipcLoop: started")
	rdBuf := make([]byte, 4096)
	var pending []byte
	for {
		lines := processPending(&pending)
		for _, line := range lines {
			p.processLine(line)
		}

		avail := peekPipeData(p.conn)
		if avail > 0 {
			n := len(rdBuf)
			if int(avail) < n {
				n = int(avail)
			}
			n, err := p.conn.Read(rdBuf[:n])
			if err != nil {
				log.Printf("[DEBUG] ipcLoop: read error: %v", err)
				return
			}
			if n > 0 {
				pending = append(pending, rdBuf[:n]...)
				continue
			}
		}

		select {
		case req := <-p.cmdCh:
			_, err := p.conn.Write(req.data)
			log.Printf("[DEBUG] ipcLoop: write done err=%v", err)
			if req.result != nil {
				if err != nil {
					req.result <- err
				} else {
					req.result <- nil
				}
			}
			if err != nil {
				log.Printf("[DEBUG] ipcLoop: write error: %v", err)
				p.mu.Lock()
				p.conn.Close()
				p.conn = nil
				p.mu.Unlock()
				return
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func processPending(pending *[]byte) [][]byte {
	var lines [][]byte
	for {
		idx := bytes.IndexByte(*pending, '\n')
		if idx < 0 {
			break
		}
		lines = append(lines, (*pending)[:idx])
		*pending = (*pending)[idx+1:]
	}
	return lines
}

func peekPipeData(conn net.Conn) uint32 {
	npc, ok := conn.(*namedPipeConn)
	if !ok {
		return 0
	}
	npc.mu.Lock()
	if npc.closed {
		npc.mu.Unlock()
		return 0
	}
	fd := npc.f.Fd()
	npc.mu.Unlock()

	var totalAvail uint32
	ret, _, _ := procPeekNamedPipe.Call(
		fd,
		0, 0, 0,
		uintptr(unsafe.Pointer(&totalAvail)),
		0,
	)
	runtime.KeepAlive(npc.f)
	if ret == 0 {
		return 0
	}
	return totalAvail
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
