package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("recording", registerRecording)
}

type recordArgs struct {
	Target string `json:"target,omitempty" jsonschema:"monitor (default, the focused one), window (the focused window's area) or region (the user drags a selection)"`
	Audio  bool   `json:"audio,omitempty" jsonschema:"also record the default audio source"`
	Path   string `json:"path,omitempty" jsonschema:"file to write, .mp4, .mkv or .webm; default a timestamped .mp4 in ~/Videos"`
}

type recording struct {
	Recording bool    `json:"recording"`
	Path      string  `json:"path,omitempty"`
	Seconds   float64 `json:"seconds,omitempty"`
	SizeMB    float64 `json:"size_mb,omitempty"`
}

const recordingName = "recording-2006-01-02_15-04-05.mp4"

var (
	recorder    run.Background
	recordMu    sync.Mutex
	recordingTo string
)

func videosDir() string {
	if dir := os.Getenv("XDG_VIDEOS_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Videos")
}

func recordingPath(path string) (string, error) {
	if path == "" {
		path = filepath.Join(videosDir(), time.Now().Format(recordingName))
	}
	path, err := filepath.Abs(expandHome(path))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	return path, os.MkdirAll(filepath.Dir(path), 0o755)
}

func recordingState() recording {
	if !recorder.Running() {
		return recording{}
	}
	return recording{Recording: true, Path: recordingTo, Seconds: round(time.Since(recorder.Started).Seconds(), 1)}
}

func registerRecording(s *mcp.Server) error {
	if !run.Has("wf-recorder") {
		return nil
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "start_recording",
		Description: "Start recording the screen to a video file with wf-recorder. Stop it with stop_recording.",
		Annotations: action("Start recording", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in recordArgs) (*mcp.CallToolResult, recording, error) {
		recordMu.Lock()
		defer recordMu.Unlock()
		if recorder.Running() {
			return nil, recordingState(), fmt.Errorf("already recording to %s", recordingTo)
		}
		if in.Target == "all" {
			return nil, recording{}, fmt.Errorf("wf-recorder records one monitor at a time; use monitor, window or region")
		}
		area, err := screenshotArea(ctx, in.Target)
		if err != nil {
			return nil, recording{}, err
		}
		path, err := recordingPath(in.Path)
		if err != nil {
			return nil, recording{}, err
		}
		args := append(area, "-f", path)
		if in.Audio {
			args = append(args, "--audio")
		}
		if err := recorder.Start(syscall.SIGINT, "wf-recorder", args...); err != nil {
			return nil, recording{}, err
		}
		recordingTo = path
		return nil, recordingState(), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "stop_recording",
		Description: "Stop the screen recording and report where the video was saved.",
		Annotations: action("Stop recording", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, recording, error) {
		recordMu.Lock()
		defer recordMu.Unlock()
		state := recordingState()
		if !recorder.Stop(syscall.SIGINT, 10*time.Second) {
			return nil, recording{}, fmt.Errorf("not recording")
		}
		state.Recording = false
		info, err := os.Stat(recordingTo)
		if err != nil {
			return nil, state, fmt.Errorf("wf-recorder stopped but %s wasn't written", recordingTo)
		}
		state.SizeMB = round(float64(info.Size())/1e6, 1)
		return nil, state, nil
	})

	return nil
}
