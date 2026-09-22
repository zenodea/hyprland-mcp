package tools

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("audio", registerAudio)
}

type audioNode struct {
	ID      int     `json:"id"`
	Name    string  `json:"name"`
	Default bool    `json:"default,omitempty"`
	Volume  float64 `json:"volume"`
	Muted   bool    `json:"muted"`
}

type audioStatus struct {
	Outputs []audioNode `json:"outputs"`
	Inputs  []audioNode `json:"inputs"`
	Streams []audioNode `json:"streams"`
}

type volumeArgs struct {
	Target   string `json:"target" jsonschema:"output, input, or a numeric id from audio_status (devices or per-app streams)"`
	Percent  int    `json:"percent" jsonschema:"volume in percent, or the change when relative is true (negative lowers it)"`
	Relative bool   `json:"relative,omitempty" jsonschema:"treat percent as a change instead of an absolute level"`
}

type muteArgs struct {
	Target string `json:"target" jsonschema:"output, input, or a numeric id from audio_status"`
	Muted  *bool  `json:"muted,omitempty" jsonschema:"true to mute, false to unmute, omit to toggle"`
}

type defaultArgs struct {
	ID int `json:"id" jsonschema:"device id from audio_status"`
}

var (
	nodeLine   = regexp.MustCompile(`^[\s│├└─]*(\*)?\s*(\d+)\.\s+(.+?)\s*(?:\[vol: ([\d.]+)( MUTED)?\])?\s*$`)
	volumeLine = regexp.MustCompile(`Volume: ([\d.]+)( \[MUTED\])?`)
)

func wpTarget(target string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(target)) {
	case "output", "sink", "speaker", "speakers":
		return "@DEFAULT_AUDIO_SINK@", nil
	case "input", "source", "mic", "microphone":
		return "@DEFAULT_AUDIO_SOURCE@", nil
	}
	if _, err := strconv.Atoi(target); err != nil {
		return "", fmt.Errorf("target must be output, input or a numeric id, got %q", target)
	}
	return target, nil
}

func audioState(ctx context.Context) (audioStatus, error) {
	text, err := run.Output(ctx, "wpctl", "status")
	if err != nil {
		return audioStatus{}, err
	}
	out := audioStatus{Outputs: []audioNode{}, Inputs: []audioNode{}, Streams: []audioNode{}}
	inAudio, section := false, ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.Trim(line, " │├└─")
		switch {
		case !strings.HasPrefix(line, " ") && trimmed != "":
			inAudio = trimmed == "Audio"
			continue
		case strings.HasSuffix(trimmed, ":"):
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if !inAudio {
			continue
		}
		m := nodeLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[2])
		node := audioNode{ID: id, Name: m[3], Default: m[1] == "*", Muted: m[5] != ""}
		node.Volume, _ = strconv.ParseFloat(m[4], 64)
		switch section {
		case "Sinks":
			out.Outputs = append(out.Outputs, node)
		case "Sources":
			out.Inputs = append(out.Inputs, node)
		case "Streams":
			if len(line)-len(strings.TrimLeft(line, " ")) > 10 {
				continue
			}
			if v, err := run.Output(ctx, "wpctl", "get-volume", m[2]); err == nil {
				if vm := volumeLine.FindStringSubmatch(v); vm != nil {
					node.Volume, _ = strconv.ParseFloat(vm[1], 64)
					node.Muted = vm[2] != ""
				}
			}
			out.Streams = append(out.Streams, node)
		}
	}
	return out, nil
}

func registerAudio(s *mcp.Server) error {
	if !run.Has("wpctl") {
		return nil
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "audio_status",
		Description: "List audio outputs, inputs and per-app streams with volume (1.0 = 100%) and mute state. The default device is marked.",
		Annotations: readOnly("Audio status"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, audioStatus, error) {
		out, err := audioState(ctx)
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_volume",
		Description: "Set or change the volume of the default output, default input, or any device or app stream by id. Capped at 150%.",
		Annotations: action("Set volume", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in volumeArgs) (*mcp.CallToolResult, ok, error) {
		target, err := wpTarget(in.Target)
		if err != nil {
			return nil, done, err
		}
		level := fmt.Sprintf("%d%%", in.Percent)
		if in.Relative {
			if in.Percent < 0 {
				level = fmt.Sprintf("%d%%-", -in.Percent)
			} else {
				level += "+"
			}
		} else if in.Percent < 0 {
			return nil, done, fmt.Errorf("percent must be positive unless relative is set")
		}
		_, err = run.Output(ctx, "wpctl", "set-volume", "-l", "1.5", target, level)
		return nil, done, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_mute",
		Description: "Mute, unmute or toggle the default output, default input, or any device or app stream by id.",
		Annotations: action("Set mute", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in muteArgs) (*mcp.CallToolResult, ok, error) {
		target, err := wpTarget(in.Target)
		if err != nil {
			return nil, done, err
		}
		state := "toggle"
		if in.Muted != nil {
			state = strconv.Itoa(map[bool]int{false: 0, true: 1}[*in.Muted])
		}
		_, err = run.Output(ctx, "wpctl", "set-mute", target, state)
		return nil, done, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_default_audio",
		Description: "Make an output or input device the default, moving new audio to it.",
		Annotations: action("Set default audio device", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in defaultArgs) (*mcp.CallToolResult, ok, error) {
		_, err := run.Output(ctx, "wpctl", "set-default", strconv.Itoa(in.ID))
		return nil, done, err
	})

	return nil
}
