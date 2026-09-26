package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("toggles", registerToggles)
}

type keyboard struct {
	Name         string   `json:"name"`
	ActiveLayout string   `json:"active_layout"`
	Layouts      []string `json:"layouts"`
	Main         bool     `json:"main"`
}

type keyboardList struct {
	Keyboards []keyboard `json:"keyboards"`
}

type layoutSwitchArgs struct {
	Layout   string `json:"layout" jsonschema:"next, prev, or the index of a layout in the keyboard's list"`
	Keyboard string `json:"keyboard,omitempty" jsonschema:"keyboard name from keyboard_layouts; omit for all keyboards"`
}

type openArgs struct {
	Target string `json:"target" jsonschema:"a file path or URL to open with its default application"`
}

type dndState struct {
	Enabled bool   `json:"enabled"`
	Daemon  string `json:"daemon"`
}

type dndBackend struct {
	name     string
	get      []string
	on, off  []string
	parseGet func(string) bool
}

var dndBackends = []dndBackend{
	{"dunst", []string{"dunstctl", "is-paused"}, []string{"dunstctl", "set-paused", "true"}, []string{"dunstctl", "set-paused", "false"},
		func(s string) bool { return s == "true" }},
	{"swaync", []string{"swaync-client", "-D"}, []string{"swaync-client", "-dn"}, []string{"swaync-client", "-df"},
		func(s string) bool { return s == "true" }},
	{"mako", []string{"makoctl", "mode"}, []string{"makoctl", "mode", "-a", "do-not-disturb"}, []string{"makoctl", "mode", "-r", "do-not-disturb"},
		func(s string) bool { return strings.Contains(s, "do-not-disturb") }},
}

func registerToggles(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "keyboard_layouts",
		Description: "List keyboards with their configured layouts and the active one.",
		Annotations: readOnly("Keyboard layouts"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, keyboardList, error) {
		var devices struct {
			Keyboards []struct {
				Name         string `json:"name"`
				ActiveKeymap string `json:"active_keymap"`
				Layout       string `json:"layout"`
				Main         bool   `json:"main"`
			} `json:"keyboards"`
		}
		out := keyboardList{Keyboards: []keyboard{}}
		if err := hypr.Query("devices", &devices); err != nil {
			return nil, out, err
		}
		for _, k := range devices.Keyboards {
			out.Keyboards = append(out.Keyboards, keyboard{k.Name, k.ActiveKeymap, strings.Split(k.Layout, ","), k.Main})
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "switch_keyboard_layout",
		Description: "Switch the keyboard layout to the next, previous or a specific configured one.",
		Annotations: action("Switch keyboard layout", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in layoutSwitchArgs) (*mcp.CallToolResult, ok, error) {
		if _, err := strconv.Atoi(in.Layout); err != nil && in.Layout != "next" && in.Layout != "prev" {
			return nil, done, fmt.Errorf("layout must be next, prev or an index")
		}
		device := in.Keyboard
		if device == "" {
			device = "all"
		}
		out, err := hypr.Request("switchxkblayout " + device + " " + in.Layout)
		if err == nil && strings.TrimSpace(string(out)) != "ok" {
			err = fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return nil, done, err
	})

	if run.Has("xdg-open") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "open",
			Description: "Open a file or URL with its default application, like double-clicking it.",
			Annotations: action("Open", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in openArgs) (*mcp.CallToolResult, ok, error) {
			if strings.TrimSpace(in.Target) == "" {
				return nil, done, fmt.Errorf("target is required")
			}
			return nil, done, hypr.Dispatch("exec", shellQuote([]string{"xdg-open", in.Target}))
		})
	}

	for _, b := range dndBackends {
		if !run.Has(b.get[0]) {
			continue
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_do_not_disturb",
			Description: "Report whether notifications are silenced.",
			Annotations: readOnly("Do not disturb"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, dndState, error) {
			text, err := run.Output(ctx, b.get[0], b.get[1:]...)
			return nil, dndState{Enabled: b.parseGet(text), Daemon: b.name}, err
		})
		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_do_not_disturb",
			Description: "Silence notifications, or let them through again.",
			Annotations: action("Set do not disturb", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in enabledArgs) (*mcp.CallToolResult, ok, error) {
			argv := b.off
			if in.Enabled {
				argv = b.on
			}
			_, err := run.Output(ctx, argv[0], argv[1:]...)
			return nil, done, err
		})
		break
	}

	return nil
}
