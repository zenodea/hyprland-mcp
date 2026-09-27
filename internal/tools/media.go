package tools

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("media", registerMedia)
}

type player struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Artist string `json:"artist,omitempty"`
	Title  string `json:"title,omitempty"`
	Album  string `json:"album,omitempty"`
	// Position and Length are in seconds; Volume in percent. Players that don't report them leave them out.
	Position *float64 `json:"position_seconds,omitempty"`
	Length   *float64 `json:"length_seconds,omitempty"`
	Volume   *int     `json:"volume_percent,omitempty"`
}

type playerList struct {
	Players []player `json:"players"`
}

type mediaArgs struct {
	Action   string  `json:"action" jsonschema:"play, pause, play-pause, next, previous, stop, seek or volume"`
	Player   string  `json:"player,omitempty" jsonschema:"player name from media_status; omit for the most recently active one"`
	Value    float64 `json:"value,omitempty" jsonschema:"for seek: position in seconds; for volume: percent"`
	Relative bool    `json:"relative,omitempty" jsonschema:"for seek and volume: treat value as a change (negative goes back or lowers)"`
}

var mediaActions = []string{"play", "pause", "play-pause", "next", "previous", "stop", "seek", "volume"}

// playerctlArgs turns a media_control call into playerctl's command, e.g. position 10+ or volume 0.5.
func playerctlArgs(in mediaArgs) ([]string, error) {
	if !slices.Contains(mediaActions, in.Action) {
		return nil, fmt.Errorf("action must be one of %s", strings.Join(mediaActions, ", "))
	}
	var args []string
	if in.Player != "" {
		args = []string{"--player", in.Player}
	}
	var value float64
	switch in.Action {
	case "seek":
		value = in.Value
	case "volume":
		value = in.Value / 100
	default:
		return append(args, in.Action), nil
	}
	if !in.Relative {
		if value < 0 {
			return nil, fmt.Errorf("value can't be negative unless relative is set")
		}
		return append(args, playerctlCommand(in.Action), strconv.FormatFloat(value, 'f', -1, 64)), nil
	}
	sign := "+"
	if value < 0 {
		sign, value = "-", -value
	}
	return append(args, playerctlCommand(in.Action), strconv.FormatFloat(value, 'f', -1, 64)+sign), nil
}

func playerctlCommand(action string) string {
	if action == "seek" {
		return "position"
	}
	return action
}

const sep = "\x1f"

func parsePlayers(text string) []player {
	out := []player{}
	for _, line := range strings.Split(text, "\n") {
		if f := strings.Split(line, sep); len(f) == 8 {
			p := player{Name: f[0], Status: f[1], Artist: f[2], Title: f[3], Album: f[4]}
			if us, err := strconv.ParseFloat(f[5], 64); err == nil {
				p.Position = new(round(us/1e6, 1))
			}
			if us, err := strconv.ParseFloat(f[6], 64); err == nil {
				p.Length = new(round(us/1e6, 1))
			}
			if v, err := strconv.ParseFloat(f[7], 64); err == nil {
				p.Volume = new(int(v*100 + 0.5))
			}
			out = append(out, p)
		}
	}
	return out
}

func registerMedia(s *mcp.Server) error {
	if !run.Has("playerctl") {
		return nil
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "media_status",
		Description: "List MPRIS media players (Spotify, browsers, mpv…) with playback status, the current track, position and volume.",
		Annotations: readOnly("Media status"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, playerList, error) {
		out := playerList{Players: []player{}}
		format := strings.Join([]string{"{{playerName}}", "{{status}}", "{{artist}}", "{{title}}", "{{album}}", "{{position}}", "{{mpris:length}}", "{{volume}}"}, sep)
		text, err := run.Output(ctx, "playerctl", "--all-players", "metadata", "--format", format)
		if err != nil {
			if strings.Contains(err.Error(), "No players found") {
				return nil, out, nil
			}
			return nil, out, err
		}
		out.Players = parsePlayers(text)
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "media_control",
		Description: "Play, pause, skip or stop a media player, seek within the track, or set the player's own volume.",
		Annotations: action("Media control", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mediaArgs) (*mcp.CallToolResult, ok, error) {
		args, err := playerctlArgs(in)
		if err != nil {
			return nil, done, err
		}
		_, err = run.Output(ctx, "playerctl", args...)
		return nil, done, err
	})

	return nil
}
