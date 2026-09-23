package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("desktop", registerDesktop)
}

const clipboardLimit = 20000

type notifyArgs struct {
	Summary string `json:"summary" jsonschema:"notification title"`
	Body    string `json:"body,omitempty"`
	Urgency string `json:"urgency,omitempty" jsonschema:"low, normal (default) or critical"`
}

type screenshotArgs struct {
	Target string  `json:"target,omitempty" jsonschema:"monitor (default, the focused one), window (the focused window), all, or region (the user drags a selection)"`
	Scale  float64 `json:"scale,omitempty" jsonschema:"resize factor from 0.1 to 1; default 0.5 keeps the image small"`
}

type brightness struct {
	Device  string `json:"device"`
	Percent int    `json:"percent"`
}

type brightnessArgs struct {
	Percent  int  `json:"percent" jsonschema:"brightness in percent, or the change when relative is true (negative dims)"`
	Relative bool `json:"relative,omitempty"`
}

type clipboard struct {
	Text      string `json:"text"`
	Truncated bool   `json:"truncated,omitempty"`
}

type clipboardArgs struct {
	Text string `json:"text"`
}

func readBrightness(ctx context.Context) (brightness, error) {
	text, err := run.Output(ctx, "brightnessctl", "-m", "-c", "backlight")
	if err != nil {
		return brightness{}, err
	}
	f := strings.Split(strings.Split(text, "\n")[0], ",")
	if len(f) < 4 {
		return brightness{}, fmt.Errorf("no backlight found")
	}
	pct, _ := strconv.Atoi(strings.TrimSuffix(f[3], "%"))
	return brightness{Device: f[0], Percent: pct}, nil
}

func screenshotArea(target string) ([]string, error) {
	switch target {
	case "", "monitor":
		var monitors []hypr.Monitor
		if err := hypr.Query("monitors", &monitors); err != nil {
			return nil, err
		}
		for _, m := range monitors {
			if m.Focused {
				return []string{"-o", m.Name}, nil
			}
		}
		return nil, fmt.Errorf("no focused monitor")
	case "window":
		var c hypr.Client
		if err := hypr.Query("activewindow", &c); err != nil {
			return nil, err
		}
		if c.Address == "" {
			return nil, fmt.Errorf("no window is focused")
		}
		return []string{"-g", fmt.Sprintf("%d,%d %dx%d", c.At[0], c.At[1], c.Size[0], c.Size[1])}, nil
	case "all":
		return nil, nil
	case "region":
		geometry, err := run.Output(context.Background(), "slurp")
		if err != nil {
			return nil, fmt.Errorf("selection cancelled")
		}
		return []string{"-g", geometry}, nil
	}
	return nil, fmt.Errorf("target must be monitor, window, all or region")
}

func registerDesktop(s *mcp.Server) error {
	if run.Has("notify-send") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "notify",
			Description: "Show a desktop notification, e.g. to report that a long task finished.",
			Annotations: action("Notify", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in notifyArgs) (*mcp.CallToolResult, ok, error) {
			urgency := in.Urgency
			if urgency == "" {
				urgency = "normal"
			}
			_, err := run.Output(ctx, "notify-send", "-a", "Claude", "-u", urgency, in.Summary, in.Body)
			return nil, done, err
		})
	}

	if run.Has("grim") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "screenshot",
			Description: "Capture the screen and return it as an image, to see what the user sees.",
			Annotations: readOnly("Screenshot"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in screenshotArgs) (*mcp.CallToolResult, any, error) {
			area, err := screenshotArea(in.Target)
			if err != nil {
				return nil, nil, err
			}
			scale := in.Scale
			if scale <= 0 || scale > 1 {
				scale = 0.5
			}
			args := append([]string{"-t", "jpeg", "-q", "75", "-s", strconv.FormatFloat(scale, 'f', 2, 64)}, area...)
			img, err := run.Bytes(ctx, nil, "grim", append(args, "-")...)
			if err != nil {
				return nil, nil, err
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: img, MIMEType: "image/jpeg"}}}, nil, nil
		})
	}

	if run.Has("brightnessctl") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_brightness",
			Description: "Read the screen backlight level.",
			Annotations: readOnly("Get brightness"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, brightness, error) {
			b, err := readBrightness(ctx)
			return nil, b, err
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_brightness",
			Description: "Set or change the screen backlight. Never goes below 1% so the screen stays visible.",
			Annotations: action("Set brightness", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in brightnessArgs) (*mcp.CallToolResult, brightness, error) {
			level := fmt.Sprintf("%d%%", max(in.Percent, 1))
			if in.Relative {
				level = fmt.Sprintf("%+d%%", in.Percent)
				level = level[1:] + level[:1]
			}
			if _, err := run.Output(ctx, "brightnessctl", "-c", "backlight", "-n", "1%", "set", level); err != nil {
				return nil, brightness{}, err
			}
			b, err := readBrightness(ctx)
			return nil, b, err
		})
	}

	if run.Has("wl-paste") && run.Has("wl-copy") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_clipboard",
			Description: "Read the text currently on the clipboard.",
			Annotations: readOnly("Get clipboard"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, clipboard, error) {
			raw, err := run.Bytes(ctx, nil, "wl-paste", "--no-newline", "--type", "text")
			if err != nil {
				if strings.Contains(err.Error(), "Nothing is copied") || strings.Contains(err.Error(), "No selection") {
					return nil, clipboard{}, nil
				}
				return nil, clipboard{}, err
			}
			text := string(raw)
			if !utf8.ValidString(text) {
				return nil, clipboard{}, fmt.Errorf("the clipboard holds binary data")
			}
			if len(text) > clipboardLimit {
				cut := clipboardLimit
				for !utf8.RuneStart(text[cut]) {
					cut--
				}
				return nil, clipboard{Text: text[:cut], Truncated: true}, nil
			}
			return nil, clipboard{Text: text}, nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_clipboard",
			Description: "Put text on the clipboard, replacing what was there.",
			Annotations: action("Set clipboard", true),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in clipboardArgs) (*mcp.CallToolResult, ok, error) {
			_, err := run.Bytes(ctx, []byte(in.Text), "wl-copy")
			return nil, done, err
		})
	}

	return nil
}
