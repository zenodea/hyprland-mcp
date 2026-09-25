package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

func init() {
	add("windows", registerWindows)
}

type window struct {
	Address    string `json:"address"`
	Class      string `json:"class"`
	Title      string `json:"title"`
	Workspace  string `json:"workspace"`
	Monitor    int    `json:"monitor"`
	Focused    bool   `json:"focused"`
	Floating   bool   `json:"floating"`
	Fullscreen bool   `json:"fullscreen"`
	Pinned     bool   `json:"pinned"`
	XWayland   bool   `json:"xwayland"`
	PID        int    `json:"pid"`
	Position   [2]int `json:"position"`
	Size       [2]int `json:"size"`
}

func toWindow(c hypr.Client) window {
	return window{
		Address:    c.Address,
		Class:      c.Class,
		Title:      c.Title,
		Workspace:  c.Workspace.Name,
		Monitor:    c.Monitor,
		Focused:    c.FocusHistoryID == 0,
		Floating:   c.Floating,
		Fullscreen: c.Fullscreen != 0,
		Pinned:     c.Pinned,
		XWayland:   c.XWayland,
		PID:        c.PID,
		Position:   c.At,
		Size:       c.Size,
	}
}

type windowList struct {
	Windows []window `json:"windows"`
}

type windowArgs struct {
	Window string `json:"window,omitempty" jsonschema:"Hyprland window selector, e.g. address:0x55bf7704ef90, class:^(firefox)$, title:Spotify, pid:1234. Omit for the focused window."`
}

type requiredWindowArgs struct {
	Window string `json:"window" jsonschema:"Hyprland window selector, e.g. address:0x55bf7704ef90, class:^(firefox)$, title:Spotify, pid:1234"`
}

type moveArgs struct {
	Window    string `json:"window,omitempty" jsonschema:"Hyprland window selector; omit for the focused window"`
	Workspace string `json:"workspace" jsonschema:"target workspace: a number, a name, or special:<name>"`
	Follow    bool   `json:"follow,omitempty" jsonschema:"also switch to the target workspace"`
}

type geometryArgs struct {
	Window string `json:"window,omitempty" jsonschema:"Hyprland window selector; omit for the focused window"`
	X      *int   `json:"x,omitempty" jsonschema:"left edge in layout pixels (see list_monitors for positions)"`
	Y      *int   `json:"y,omitempty"`
	Width  *int   `json:"width,omitempty"`
	Height *int   `json:"height,omitempty"`
}

type directionArgs struct {
	Direction string `json:"direction" jsonschema:"left, right, up or down"`
}

var directions = map[string]string{"left": "l", "right": "r", "up": "u", "down": "d"}

type fullscreenArgs struct {
	Mode string `json:"mode,omitempty" jsonschema:"fullscreen (default) or maximize; toggles on the focused window"`
}

func registerWindows(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_windows",
		Description: "List every mapped window with its address, class, title, workspace and state.",
		Annotations: readOnly("List windows"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, windowList, error) {
		var clients []hypr.Client
		if err := hypr.Query("clients", &clients); err != nil {
			return nil, windowList{}, err
		}
		out := windowList{Windows: []window{}}
		for _, c := range clients {
			if c.Mapped && !c.Hidden {
				out.Windows = append(out.Windows, toWindow(c))
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "active_window",
		Description: "Describe the currently focused window.",
		Annotations: readOnly("Active window"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, window, error) {
		var c hypr.Client
		if err := hypr.Query("activewindow", &c); err != nil {
			return nil, window{}, err
		}
		if c.Address == "" {
			return nil, window{}, fmt.Errorf("no window is focused")
		}
		return nil, toWindow(c), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "focus_window",
		Description: "Focus a window, switching to its workspace if needed.",
		Annotations: action("Focus window", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in requiredWindowArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch("focuswindow", in.Window)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "close_window",
		Description: "Ask a window to close, like pressing its close button. Unsaved work may prompt or be lost.",
		Annotations: action("Close window", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in windowArgs) (*mcp.CallToolResult, ok, error) {
		if in.Window == "" {
			return nil, done, hypr.Dispatch("killactive", "")
		}
		return nil, done, hypr.Dispatch("closewindow", in.Window)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_window",
		Description: "Move a window to another workspace.",
		Annotations: action("Move window", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in moveArgs) (*mcp.CallToolResult, ok, error) {
		dispatcher := "movetoworkspacesilent"
		if in.Follow {
			dispatcher = "movetoworkspace"
		}
		target := strings.TrimSpace(in.Workspace)
		if in.Window != "" {
			target += "," + in.Window
		}
		return nil, done, hypr.Dispatch(dispatcher, target)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "toggle_floating",
		Description: "Toggle a window between tiled and floating.",
		Annotations: action("Toggle floating", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in windowArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch("togglefloating", in.Window)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "toggle_fullscreen",
		Description: "Toggle fullscreen or maximize on the focused window.",
		Annotations: action("Toggle fullscreen", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in fullscreenArgs) (*mcp.CallToolResult, ok, error) {
		switch in.Mode {
		case "", "fullscreen":
			return nil, done, hypr.Dispatch("fullscreen", "0")
		case "maximize":
			return nil, done, hypr.Dispatch("fullscreen", "1")
		}
		return nil, done, fmt.Errorf("unknown mode %q", in.Mode)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_window_geometry",
		Description: "Float a window and give it an exact position and/or size. Only the fields given change.",
		Annotations: action("Set window geometry", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in geometryArgs) (*mcp.CallToolResult, window, error) {
		target := in.Window
		if target == "" {
			var c hypr.Client
			if err := hypr.Query("activewindow", &c); err != nil {
				return nil, window{}, err
			}
			if c.Address == "" {
				return nil, window{}, fmt.Errorf("no window is focused")
			}
			target = "address:" + c.Address
		}
		if in.X == nil && in.Y == nil && in.Width == nil && in.Height == nil {
			return nil, window{}, fmt.Errorf("give at least one of x, y, width or height")
		}
		if err := hypr.Dispatch("setfloating", target); err != nil {
			return nil, window{}, err
		}
		current, err := findWindow(target)
		if err != nil {
			return nil, window{}, err
		}
		if in.Width != nil || in.Height != nil {
			w, h := pick(in.Width, current.Size[0]), pick(in.Height, current.Size[1])
			if err := hypr.Dispatch("resizewindowpixel", fmt.Sprintf("exact %d %d,%s", w, h, target)); err != nil {
				return nil, window{}, err
			}
		}
		if in.X != nil || in.Y != nil {
			x, y := pick(in.X, current.At[0]), pick(in.Y, current.At[1])
			if err := hypr.Dispatch("movewindowpixel", fmt.Sprintf("exact %d %d,%s", x, y, target)); err != nil {
				return nil, window{}, err
			}
		}
		updated, err := findWindow(target)
		return nil, toWindow(updated), err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "pin_window",
		Description: "Toggle pinning a floating window so it shows on every workspace.",
		Annotations: action("Pin window", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in windowArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch("pin", in.Window)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "swap_window",
		Description: "Swap the focused tiled window with its neighbour in a direction.",
		Annotations: action("Swap window", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in directionArgs) (*mcp.CallToolResult, ok, error) {
		d, known := directions[in.Direction]
		if !known {
			return nil, done, fmt.Errorf("direction must be left, right, up or down")
		}
		return nil, done, hypr.Dispatch("swapwindow", d)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_focus",
		Description: "Move keyboard focus to the neighbouring window in a direction.",
		Annotations: action("Move focus", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in directionArgs) (*mcp.CallToolResult, ok, error) {
		d, known := directions[in.Direction]
		if !known {
			return nil, done, fmt.Errorf("direction must be left, right, up or down")
		}
		return nil, done, hypr.Dispatch("movefocus", d)
	})

	registerWait(s)
	return nil
}

func pick(v *int, fallback int) int {
	if v != nil {
		return *v
	}
	return fallback
}

// findWindow resolves an address:0x… selector, or the first window matching class:/title: regexes.
func findWindow(selector string) (hypr.Client, error) {
	var clients []hypr.Client
	if err := hypr.Query("clients", &clients); err != nil {
		return hypr.Client{}, err
	}
	kind, value, _ := strings.Cut(selector, ":")
	for _, c := range clients {
		switch kind {
		case "address":
			if c.Address == value {
				return c, nil
			}
		case "pid":
			if strconv.Itoa(c.PID) == value {
				return c, nil
			}
		case "class", "initialclass", "title":
			field := map[string]string{"class": c.Class, "initialclass": c.InitialClass, "title": c.Title}[kind]
			if re, err := regexp.Compile(value); err == nil && re.MatchString(field) {
				return c, nil
			}
		}
	}
	return hypr.Client{}, fmt.Errorf("no window matches %s", selector)
}
