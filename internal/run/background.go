package run

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Background is a long-running helper process owned by this server, such as a
// screen recorder. It dies with the server, getting stopSignal so it can finish up.
type Background struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	exited  chan struct{}
	stderr  tail
	Started time.Time
}

// tail keeps the last few KB written to it, enough for an error message.
type tail struct{ bytes.Buffer }

func (t *tail) Write(p []byte) (int, error) {
	if t.Len()+len(p) > 4096 {
		t.Next(t.Len() + len(p) - 4096)
	}
	return t.Buffer.Write(p)
}

// Start launches the process and waits briefly so an immediate failure is reported.
func (b *Background) Start(stopSignal syscall.Signal, name string, args ...string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running() {
		return errors.New("already running")
	}
	cmd := exec.Command(name, args...)
	// Its own process group, so Stop reaches its children too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: stopSignal, Setpgid: true}
	b.stderr.Reset()
	cmd.Stderr = &b.stderr
	if err := cmd.Start(); err != nil {
		var missing *exec.Error
		if errors.As(err, &missing) {
			return fmt.Errorf("%s is not installed", name)
		}
		return err
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
		msg := strings.TrimSpace(b.stderr.String())
		if msg == "" {
			msg = cmd.ProcessState.String()
		}
		return fmt.Errorf("%s: %s", name, msg)
	case <-time.After(500 * time.Millisecond):
	}
	b.cmd, b.exited, b.Started = cmd, exited, time.Now()
	return nil
}

// Running reports whether the process is still going.
func (b *Background) Running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running()
}

func (b *Background) running() bool {
	if b.cmd == nil {
		return false
	}
	select {
	case <-b.exited:
		return false
	default:
		return true
	}
}

// Stop signals the process and waits up to limit for it to exit, then kills it.
// It reports false if nothing was running.
func (b *Background) Stop(sig syscall.Signal, limit time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running() {
		b.cmd = nil
		return false
	}
	pgid := b.cmd.Process.Pid
	_ = syscall.Kill(-pgid, sig)
	select {
	case <-b.exited:
	case <-time.After(limit):
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-b.exited
	}
	b.cmd = nil
	return true
}
