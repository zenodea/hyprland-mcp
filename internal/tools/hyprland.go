package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

func init() {
	add("hyprland", registerHyprland)
}

type launchArgs struct {
	Command string `json:"command" jsonschema:"shell command to start detached under Hyprland, e.g. firefox or [workspace 3 silent] kitty"`
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

func registerHyprland(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "launch_app",
		Description: "Start a program detached under Hyprland, so it outlives this server. Supports Hyprland exec rules in brackets.",
		Annotations: action("Launch app", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in launchArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch("exec", in.Command)
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

	return nil
}
