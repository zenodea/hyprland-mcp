package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("extras", registerExtras)
}

type keepAwake struct {
	Enabled bool   `json:"enabled"`
	Until   string `json:"until,omitempty"`
}

type keepAwakeArgs struct {
	Enabled bool `json:"enabled" jsonschema:"true to stop the screen idling, locking or suspending; false to allow it again"`
	Minutes int  `json:"minutes,omitempty" jsonschema:"stop after this many minutes; omit to stay awake until turned off or this server exits"`
}

type nightLight struct {
	Temperature int `json:"temperature_kelvin"`
	Gamma       int `json:"gamma_percent,omitempty"`
}

type nightLightArgs struct {
	Temperature int  `json:"temperature,omitempty" jsonschema:"colour temperature in kelvin, 1000 to 20000; about 6500 is neutral, 4000 warm, 2500 very warm"`
	Off         bool `json:"off,omitempty" jsonschema:"turn the filter off (neutral colours)"`
}

type wallpaper struct {
	Monitor string `json:"monitor"`
	Image   string `json:"image"`
}

type wallpaperList struct {
	Backend    string      `json:"backend"`
	Wallpapers []wallpaper `json:"wallpapers"`
}

type wallpaperArgs struct {
	Image   string `json:"image" jsonschema:"path to an image file"`
	Monitor string `json:"monitor,omitempty" jsonschema:"monitor name from list_monitors; omit for every monitor"`
}

type color struct {
	Hex string `json:"hex"`
}

var (
	awake      run.Background
	awakeMu    sync.Mutex
	awakeUntil time.Time
)

func readKeepAwake() keepAwake {
	awakeMu.Lock()
	defer awakeMu.Unlock()
	if !awake.Running() {
		return keepAwake{}
	}
	k := keepAwake{Enabled: true}
	if !awakeUntil.IsZero() {
		k.Until = awakeUntil.Format(time.Kitchen)
	}
	return k
}

func readNightLight(ctx context.Context) (nightLight, error) {
	text, err := run.Output(ctx, "hyprctl", "hyprsunset", "temperature")
	if err != nil {
		return nightLight{}, err
	}
	temp, err := strconv.Atoi(text)
	if err != nil {
		return nightLight{}, fmt.Errorf("hyprsunset: %s", text)
	}
	gamma, _ := run.Output(ctx, "hyprctl", "hyprsunset", "gamma")
	g, _ := strconv.Atoi(gamma)
	return nightLight{Temperature: temp, Gamma: g}, nil
}

// wallpaperBackend is the running wallpaper daemon's client: awww (formerly swww) or hyprpaper.
func wallpaperBackend() string {
	for _, b := range []string{"awww", "swww"} {
		if run.Has(b) {
			return b
		}
	}
	if run.Has("hyprpaper") {
		return "hyprpaper"
	}
	return ""
}

var wwwQueryLine = regexp.MustCompile(`^[^:]*: ([^:]+): .*currently displaying: (?:image: )?(.*)$`)

// parseWallpapers reads `awww query` or `hyprctl hyprpaper listactive` output.
func parseWallpapers(backend, text string) []wallpaper {
	out := []wallpaper{}
	for _, line := range strings.Split(text, "\n") {
		if backend == "hyprpaper" {
			if monitor, image, found := strings.Cut(line, " = "); found {
				out = append(out, wallpaper{strings.TrimSpace(monitor), strings.TrimSpace(image)})
			}
		} else if m := wwwQueryLine.FindStringSubmatch(line); m != nil {
			out = append(out, wallpaper{m[1], m[2]})
		}
	}
	return out
}

func expandHome(path string) string {
	if rest, found := strings.CutPrefix(path, "~/"); found {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}

func registerExtras(s *mcp.Server) error {
	if run.Has("systemd-inhibit") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_keep_awake",
			Description: "Report whether this server is keeping the screen awake.",
			Annotations: readOnly("Keep awake status"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, keepAwake, error) {
			return nil, readKeepAwake(), nil
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_keep_awake",
			Description: "Stop the screen from idling, locking or suspending, e.g. during a long task or a presentation, or allow it again. Uses a systemd idle inhibitor, which hypridle honours.",
			Annotations: action("Keep awake", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in keepAwakeArgs) (*mcp.CallToolResult, keepAwake, error) {
			awakeMu.Lock()
			awake.Stop(syscall.SIGTERM, time.Second)
			awakeUntil = time.Time{}
			var err error
			if in.Enabled {
				// The inhibitor holds for as long as its command runs: here, until this server exits.
				argv := []string{"--what=idle", "--who=hyprland-mcp", "--why=Kept awake by an agent", "--mode=block"}
				if in.Minutes > 0 {
					argv = append(argv, "timeout", strconv.Itoa(in.Minutes*60))
					awakeUntil = time.Now().Add(time.Duration(in.Minutes) * time.Minute)
				}
				argv = append(argv, "tail", "--pid="+strconv.Itoa(os.Getpid()), "-f", "/dev/null")
				err = awake.Start(syscall.SIGTERM, "systemd-inhibit", argv...)
			}
			awakeMu.Unlock()
			return nil, readKeepAwake(), err
		})
	}

	if run.Has("hyprsunset") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_night_light",
			Description: "Read hyprsunset's colour temperature. It can't report whether the filter was switched off, only the temperature it would use.",
			Annotations: readOnly("Night light"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, nightLight, error) {
			n, err := readNightLight(ctx)
			return nil, n, err
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_night_light",
			Description: "Warm the screen colours with hyprsunset (which must be running), or turn the filter off.",
			Annotations: action("Set night light", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in nightLightArgs) (*mcp.CallToolResult, nightLight, error) {
			if in.Off {
				_, err := run.Output(ctx, "hyprctl", "hyprsunset", "identity")
				return nil, nightLight{Temperature: 6500}, err
			}
			if in.Temperature < 1000 || in.Temperature > 20000 {
				return nil, nightLight{}, fmt.Errorf("temperature must be between 1000 and 20000 kelvin, or set off")
			}
			if _, err := run.Output(ctx, "hyprctl", "hyprsunset", "temperature", strconv.Itoa(in.Temperature)); err != nil {
				return nil, nightLight{}, err
			}
			n, err := readNightLight(ctx)
			return nil, n, err
		})
	}

	if backend := wallpaperBackend(); backend != "" {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "get_wallpaper",
			Description: "Show the wallpaper image on each monitor.",
			Annotations: readOnly("Get wallpaper"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, wallpaperList, error) {
			var text string
			var err error
			if backend == "hyprpaper" {
				text, err = run.Output(ctx, "hyprctl", "hyprpaper", "listactive")
			} else {
				text, err = run.Output(ctx, backend, "query")
			}
			return nil, wallpaperList{Backend: backend, Wallpapers: parseWallpapers(backend, text)}, err
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_wallpaper",
			Description: "Set the wallpaper on one monitor or all of them. Lasts until the wallpaper daemon restarts; the user's config decides the one at login.",
			Annotations: action("Set wallpaper", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in wallpaperArgs) (*mcp.CallToolResult, ok, error) {
			path, err := filepath.Abs(expandHome(in.Image))
			if err != nil {
				return nil, done, err
			}
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				return nil, done, fmt.Errorf("no image file at %s", path)
			}
			if backend != "hyprpaper" {
				args := []string{"img", path}
				if in.Monitor != "" {
					args = append(args, "--outputs", in.Monitor)
				}
				_, err := run.Output(ctx, backend, args...)
				return nil, done, err
			}
			// Older hyprpaper needs the image preloaded first; newer ones load it on demand.
			_, _ = run.Output(ctx, "hyprctl", "hyprpaper", "preload", path)
			reply, err := run.Output(ctx, "hyprctl", "hyprpaper", "wallpaper", in.Monitor+","+path)
			if err == nil && reply != "ok" {
				err = fmt.Errorf("hyprpaper: %s", reply)
			}
			return nil, done, err
		})
	}

	if run.Has("hyprpicker") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "pick_color",
			Description: "Let the user click anywhere on screen and return that pixel's colour. Waits up to a minute for the click.",
			Annotations: readOnly("Pick colour"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, color, error) {
			out, err := run.Exec(ctx, time.Minute, nil, "hyprpicker", "--format=hex", "--no-fancy")
			if err != nil {
				return nil, color{}, err
			}
			hex := strings.TrimSpace(string(out))
			if hex == "" {
				return nil, color{}, fmt.Errorf("no colour picked")
			}
			return nil, color{Hex: hex}, nil
		})
	}

	return nil
}
