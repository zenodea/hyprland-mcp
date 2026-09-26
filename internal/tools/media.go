package tools

import (
	"context"
	"fmt"
	"slices"
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
}

type playerList struct {
	Players []player `json:"players"`
}

type mediaArgs struct {
	Action string `json:"action" jsonschema:"play, pause, play-pause, next, previous or stop"`
	Player string `json:"player,omitempty" jsonschema:"player name from media_status; omit for the most recently active one"`
}

var mediaActions = []string{"play", "pause", "play-pause", "next", "previous", "stop"}

const sep = "\x1f"

func parsePlayers(text string) []player {
	out := []player{}
	for _, line := range strings.Split(text, "\n") {
		if f := strings.Split(line, sep); len(f) == 5 {
			out = append(out, player{Name: f[0], Status: f[1], Artist: f[2], Title: f[3], Album: f[4]})
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
		Description: "List MPRIS media players (Spotify, browsers, mpv…) with playback status and the current track.",
		Annotations: readOnly("Media status"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, playerList, error) {
		out := playerList{Players: []player{}}
		format := strings.Join([]string{"{{playerName}}", "{{status}}", "{{artist}}", "{{title}}", "{{album}}"}, sep)
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
		Description: "Play, pause, skip or stop a media player.",
		Annotations: action("Media control", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in mediaArgs) (*mcp.CallToolResult, ok, error) {
		if !slices.Contains(mediaActions, in.Action) {
			return nil, done, fmt.Errorf("action must be one of %s", strings.Join(mediaActions, ", "))
		}
		args := []string{in.Action}
		if in.Player != "" {
			args = append([]string{"--player", in.Player}, args...)
		}
		_, err := run.Output(ctx, "playerctl", args...)
		return nil, done, err
	})

	return nil
}
