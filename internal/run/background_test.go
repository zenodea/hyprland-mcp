package run

import (
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBackgroundStartStop(t *testing.T) {
	var b Background
	if err := b.Start(syscall.SIGTERM, "sh", "-c", "sleep 30 & wait"); err != nil {
		t.Fatal(err)
	}
	if !b.Running() {
		t.Fatal("should be running")
	}
	if err := b.Start(syscall.SIGTERM, "sleep", "30"); err == nil {
		t.Error("second start should fail while running")
	}
	if !b.Stop(syscall.SIGTERM, 2*time.Second) {
		t.Error("stop should report it was running")
	}
	if b.Running() || b.Stop(syscall.SIGTERM, time.Second) {
		t.Error("should be stopped")
	}
}

func TestBackgroundImmediateFailure(t *testing.T) {
	var b Background
	err := b.Start(syscall.SIGTERM, "sh", "-c", "echo broken >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("got %v", err)
	}
	if err := b.Start(syscall.SIGTERM, "no-such-command-hyprland-mcp"); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("got %v", err)
	}
}

func TestTail(t *testing.T) {
	var tl tail
	tl.Write([]byte(strings.Repeat("a", 4000)))
	tl.Write([]byte(strings.Repeat("b", 200)))
	if tl.Len() != 4096 || !strings.HasSuffix(tl.String(), strings.Repeat("b", 200)) {
		t.Errorf("kept %d bytes", tl.Len())
	}
}
