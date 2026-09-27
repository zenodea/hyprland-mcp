package tools

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
	"github.com/zenodea/hyprland-mcp/internal/run"
)

// Input tools act as the user's keyboard and mouse, so they are opt-in: -enable input.
func init() {
	addOptIn("input", registerInput)
}

const typeLimit = 5000

type typeArgs struct {
	Text string `json:"text" jsonschema:"text to type into the focused window, as if typed on the keyboard"`
}

type shortcutArgs struct {
	Keys   string `json:"keys" jsonschema:"modifiers and one key joined by +, e.g. ctrl+t, ctrl+shift+Tab, super+Return or F5"`
	Window string `json:"window,omitempty" jsonschema:"Hyprland window selector to send it to without focusing; omit for the focused window"`
}

type cursorArgs struct {
	X int `json:"x" jsonschema:"layout x coordinate (see list_monitors for positions)"`
	Y int `json:"y" jsonschema:"layout y coordinate"`
}

var modifiers = map[string]string{
	"super": "SUPER", "win": "SUPER", "meta": "SUPER", "mod4": "SUPER",
	"ctrl": "CTRL", "control": "CTRL",
	"alt": "ALT", "mod1": "ALT",
	"shift": "SHIFT",
}

// shortcut turns ctrl+shift+t into Hyprland's "CTRL SHIFT, t".
func shortcut(keys string) (string, error) {
	parts := strings.Split(keys, "+")
	key := strings.TrimSpace(parts[len(parts)-1])
	if key == "" {
		return "", fmt.Errorf("keys must end in a key, e.g. ctrl+t")
	}
	var mods []string
	for _, p := range parts[:len(parts)-1] {
		mod, known := modifiers[strings.ToLower(strings.TrimSpace(p))]
		if !known {
			return "", fmt.Errorf("unknown modifier %q: use super, ctrl, alt or shift", p)
		}
		mods = append(mods, mod)
	}
	return strings.Join(mods, " ") + ", " + key, nil
}

func registerInput(s *mcp.Server) error {
	if run.Has("wtype") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "type_text",
			Description: "Type text into the focused window as if on the keyboard. Check which window is focused first: the text goes wherever focus is.",
			Annotations: action("Type text", true),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in typeArgs) (*mcp.CallToolResult, ok, error) {
			if in.Text == "" {
				return nil, done, fmt.Errorf("text is required")
			}
			if utf8.RuneCountInString(in.Text) > typeLimit {
				return nil, done, fmt.Errorf("text is longer than %d characters; use set_clipboard and paste instead", typeLimit)
			}
			_, err := run.Bytes(ctx, []byte(in.Text), "wtype", "-")
			return nil, done, err
		})
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "send_shortcut",
		Description: "Press a key or key combination, e.g. ctrl+t for a new tab, in the focused window or a given one.",
		Annotations: action("Send shortcut", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in shortcutArgs) (*mcp.CallToolResult, ok, error) {
		arg, err := shortcut(in.Keys)
		if err != nil {
			return nil, done, err
		}
		if in.Window != "" {
			arg += ", " + in.Window
		}
		return nil, done, hypr.Dispatch("sendshortcut", arg)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_cursor",
		Description: "Move the mouse pointer to a position in layout coordinates.",
		Annotations: action("Move cursor", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in cursorArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch("movecursor", fmt.Sprintf("%d %d", in.X, in.Y))
	})

	return nil
}
