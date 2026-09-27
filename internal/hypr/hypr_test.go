package hypr

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeHyprland points the package at a temporary runtime dir and listens on one of its sockets.
func fakeHyprland(t *testing.T, name string) net.Listener {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "test")
	if err := os.MkdirAll(filepath.Join(dir, "hypr", "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("unix", filepath.Join(dir, "hypr", "test", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestDispatch(t *testing.T) {
	l := fakeHyprland(t, ".socket.sock")
	replies := []string{"ok", "Invalid dispatcher"}
	go func() {
		for _, reply := range replies {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			buf := make([]byte, 256)
			n, _ := conn.Read(buf)
			if string(buf[:n]) != "dispatch workspace 3" {
				conn.Write([]byte("unexpected " + string(buf[:n])))
			} else {
				conn.Write([]byte(reply))
			}
			conn.Close()
		}
	}()
	if err := Dispatch("workspace", "3"); err != nil {
		t.Errorf("ok reply: %v", err)
	}
	if err := Dispatch("workspace", "3"); err == nil || err.Error() != "Invalid dispatcher" {
		t.Errorf("error reply: %v", err)
	}
}

func TestEvents(t *testing.T) {
	l := fakeHyprland(t, ".socket2.sock")
	go func() {
		conn, err := l.Accept()
		if err != nil {
			return
		}
		conn.Write([]byte("openwindow>>55,2,kitty,kitty\nworkspacev2>>3,3\n"))
		conn.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	events, err := Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	for e := range events {
		got = append(got, e)
	}
	if len(got) != 2 || got[0] != (Event{"openwindow", "55,2,kitty,kitty"}) || got[1].Name != "workspacev2" {
		t.Errorf("got %+v", got)
	}
}

func TestNotUnderHyprland(t *testing.T) {
	t.Setenv("HYPRLAND_INSTANCE_SIGNATURE", "")
	if _, err := Request("version"); err == nil {
		t.Error("should fail without an instance signature")
	}
}
