package tools

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

func init() {
	add("hyprland", registerHyprland)
}

type launchArgs struct {
	Command   string `json:"command" jsonschema:"shell command to start detached under Hyprland, e.g. firefox or [workspace 3 silent] kitty"`
	WaitClass string `json:"wait_class,omitempty" jsonschema:"regex on the new window's class; when set, wait for that window and return it"`
	Timeout   int    `json:"timeout,omitempty" jsonschema:"seconds to wait for the window, default 10, max 60"`
}

type launchResult struct {
	OK     bool    `json:"ok"`
	Window *window `json:"window,omitempty"`
}

type dispatchArgs struct {
	Dispatcher string `json:"dispatcher" jsonschema:"any Hyprland dispatcher, e.g. resizeactive, swapwindow, pin"`
	Args       string `json:"args,omitempty" jsonschema:"the dispatcher's arguments"`
}

type optionArgs struct {
	Name string `json:"name" jsonschema:"config option, e.g. general:gaps_out or decoration:rounding"`
}

type setOptionArgs struct {
	Name  string `json:"name" jsonschema:"config option, e.g. general:gaps_out"`
	Value string `json:"value" jsonschema:"new value; lasts until Hyprland reloads its config"`
}

type option map[string]any

type bindArgs struct {
	Query string `json:"query,omitempty" jsonschema:"case-insensitive filter on keys, dispatcher, argument and description, e.g. firefox or screenshot"`
}

type bind struct {
	Keys        string `json:"keys"`
	Dispatcher  string `json:"dispatcher"`
	Arg         string `json:"arg,omitempty"`
	Description string `json:"description,omitempty"`
	Submap      string `json:"submap,omitempty"`
	Mouse       bool   `json:"mouse,omitempty"`
	Locked      bool   `json:"locked,omitempty"`
	Release     bool   `json:"release,omitempty"`
	Repeat      bool   `json:"repeat,omitempty"`
}

type bindList struct {
	Binds []bind `json:"binds"`
}

type rawBind struct {
	Locked      bool   `json:"locked"`
	Mouse       bool   `json:"mouse"`
	Release     bool   `json:"release"`
	Repeat      bool   `json:"repeat"`
	Modmask     int    `json:"modmask"`
	Submap      string `json:"submap"`
	Key         string `json:"key"`
	Keycode     int    `json:"keycode"`
	Description string `json:"description"`
	Dispatcher  string `json:"dispatcher"`
	Arg         string `json:"arg"`
}

// modNames follow Hyprland's modmask bits in the order people write them.
var modNames = []struct {
	bit  int
	name string
}{{64, "SUPER"}, {4, "CTRL"}, {8, "ALT"}, {1, "SHIFT"}, {2, "CAPS"}, {16, "MOD2"}, {32, "MOD3"}, {128, "MOD5"}}

func toBind(r rawBind) bind {
	var keys []string
	for _, m := range modNames {
		if r.Modmask&m.bit != 0 {
			keys = append(keys, m.name)
		}
	}
	key := r.Key
	if key == "" && r.Keycode != 0 {
		key = "code:" + strconv.Itoa(r.Keycode)
	}
	keys = append(keys, key)
	return bind{
		Keys: strings.Join(keys, "+"), Dispatcher: r.Dispatcher, Arg: r.Arg, Description: r.Description,
		Submap: r.Submap, Mouse: r.Mouse, Locked: r.Locked, Release: r.Release, Repeat: r.Repeat,
	}
}

type layer struct {
	Monitor   string `json:"monitor"`
	Level     string `json:"level"`
	Namespace string `json:"namespace"`
	PID       int    `json:"pid"`
	Position  [2]int `json:"position"`
	Size      [2]int `json:"size"`
}

type layerList struct {
	Layers []layer `json:"layers"`
}

var layerLevels = map[string]string{"0": "background", "1": "bottom", "2": "top", "3": "overlay"}

// rawLayers is hyprctl layers: monitor -> level number -> surfaces.
type rawLayers map[string]struct {
	Levels map[string][]struct {
		X, Y, W, H int
		Namespace  string `json:"namespace"`
		PID        int    `json:"pid"`
	} `json:"levels"`
}

func parseLayers(raw rawLayers) []layer {
	out := []layer{}
	for monitor, m := range raw {
		for level, surfaces := range m.Levels {
			for _, l := range surfaces {
				out = append(out, layer{monitor, layerLevels[level], l.Namespace, l.PID, [2]int{l.X, l.Y}, [2]int{l.W, l.H}})
			}
		}
	}
	slices.SortFunc(out, func(a, b layer) int {
		return strings.Compare(a.Monitor+a.Level+a.Namespace, b.Monitor+b.Level+b.Namespace)
	})
	return out
}

type cursor struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func registerHyprland(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "launch_app",
		Description: "Start a program detached under Hyprland, so it outlives this server. Supports Hyprland exec rules in brackets. Set wait_class to get the new window back once it opens.",
		Annotations: action("Launch app", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in launchArgs) (*mcp.CallToolResult, launchResult, error) {
		if in.WaitClass == "" {
			return nil, launchResult{OK: true}, hypr.Dispatch("exec", in.Command)
		}
		m, err := newMatcher(in.WaitClass, "")
		if err != nil {
			return nil, launchResult{}, err
		}
		before, err := windowAddresses()
		if err != nil {
			return nil, launchResult{}, err
		}
		if err := hypr.Dispatch("exec", in.Command); err != nil {
			return nil, launchResult{}, err
		}
		w, err := waitForWindow(ctx, m, before, in.Timeout)
		if err != nil {
			return nil, launchResult{OK: true}, fmt.Errorf("launched, but %w", err)
		}
		return nil, launchResult{OK: true, Window: &w}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_option",
		Description: "Read the current value of a Hyprland config option.",
		Annotations: readOnly("Get option"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in optionArgs) (*mcp.CallToolResult, option, error) {
		out := option{}
		return nil, out, hypr.Query("getoption "+in.Name, &out)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_option",
		Description: "Change a Hyprland config option at runtime. The change is lost when the config reloads.",
		Annotations: action("Set option", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in setOptionArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Keyword(in.Name, in.Value)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "hypr_dispatch",
		Description: "Run any Hyprland dispatcher. Prefer the dedicated tools; use this for anything they don't cover.",
		Annotations: action("Hyprland dispatch", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in dispatchArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch(in.Dispatcher, in.Args)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_binds",
		Description: "List Hyprland key bindings with what each one does, e.g. to answer \"what's my shortcut for X?\".",
		Annotations: readOnly("List key bindings"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in bindArgs) (*mcp.CallToolResult, bindList, error) {
		out := bindList{Binds: []bind{}}
		var raw []rawBind
		if err := hypr.Query("binds", &raw); err != nil {
			return nil, out, err
		}
		query := strings.ToLower(in.Query)
		for _, r := range raw {
			b := toBind(r)
			if query == "" || strings.Contains(strings.ToLower(strings.Join([]string{b.Keys, b.Dispatcher, b.Arg, b.Description}, " ")), query) {
				out.Binds = append(out.Binds, b)
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_layers",
		Description: "List layer-shell surfaces (bars, wallpapers, launchers, notifications) on each monitor.",
		Annotations: readOnly("List layers"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, layerList, error) {
		var raw rawLayers
		if err := hypr.Query("layers", &raw); err != nil {
			return nil, layerList{}, err
		}
		return nil, layerList{Layers: parseLayers(raw)}, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_devices",
		Description: "List input devices Hyprland sees: mice, keyboards (with layouts), tablets, touch devices and switches.",
		Annotations: readOnly("List devices"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, option, error) {
		out := option{}
		return nil, out, hypr.Query("devices", &out)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "cursor_position",
		Description: "Report where the mouse pointer is, in layout coordinates.",
		Annotations: readOnly("Cursor position"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, cursor, error) {
		var c cursor
		err := hypr.Query("cursorpos", &c)
		return nil, c, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "reload_config",
		Description: "Reload the Hyprland config from disk, applying edits and undoing runtime changes from set_option and set_monitor.",
		Annotations: action("Reload config", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, ok, error) {
		out, err := hypr.Request("reload")
		if err == nil && strings.TrimSpace(string(out)) != "ok" {
			err = fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return nil, done, err
	})

	return nil
}
