package hypr

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func socket() (string, error) {
	sig := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if sig == "" {
		return "", errors.New("not running under Hyprland (HYPRLAND_INSTANCE_SIGNATURE is unset)")
	}
	runtime := os.Getenv("XDG_RUNTIME_DIR")
	if runtime == "" {
		runtime = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(runtime, "hypr", sig, ".socket.sock"), nil
}

func Request(cmd string) ([]byte, error) {
	path, err := socket()
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connecting to Hyprland: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := conn.Write([]byte(cmd)); err != nil {
		return nil, err
	}
	return io.ReadAll(conn)
}

func Query(cmd string, v any) error {
	out, err := Request("j/" + cmd)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("hyprland %s: %s", cmd, strings.TrimSpace(string(out)))
	}
	return nil
}

func Dispatch(dispatcher, args string) error {
	return expectOK(Request(strings.TrimSpace("dispatch " + dispatcher + " " + args)))
}

func Keyword(name, value string) error {
	return expectOK(Request("keyword " + name + " " + value))
}

func expectOK(out []byte, err error) error {
	if err != nil {
		return err
	}
	if reply := strings.TrimSpace(string(out)); reply != "ok" {
		return errors.New(reply)
	}
	return nil
}

type Ref struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Client struct {
	Address        string `json:"address"`
	Mapped         bool   `json:"mapped"`
	Hidden         bool   `json:"hidden"`
	At             [2]int `json:"at"`
	Size           [2]int `json:"size"`
	Workspace      Ref    `json:"workspace"`
	Floating       bool   `json:"floating"`
	Pinned         bool   `json:"pinned"`
	Monitor        int    `json:"monitor"`
	Class          string `json:"class"`
	Title          string `json:"title"`
	InitialClass   string `json:"initialClass"`
	PID            int    `json:"pid"`
	XWayland       bool   `json:"xwayland"`
	Fullscreen     int    `json:"fullscreen"`
	FocusHistoryID int    `json:"focusHistoryID"`
}

type Workspace struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Monitor         string `json:"monitor"`
	Windows         int    `json:"windows"`
	HasFullscreen   bool   `json:"hasfullscreen"`
	LastWindowTitle string `json:"lastwindowtitle"`
}

type Monitor struct {
	ID              int     `json:"id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	Width           int     `json:"width"`
	Height          int     `json:"height"`
	RefreshRate     float64 `json:"refreshRate"`
	X               int     `json:"x"`
	Y               int     `json:"y"`
	Scale           float64 `json:"scale"`
	ActiveWorkspace Ref     `json:"activeWorkspace"`
	Focused         bool    `json:"focused"`
	Disabled        bool    `json:"disabled"`
}
