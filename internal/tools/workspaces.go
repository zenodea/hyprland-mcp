package tools

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

func init() {
	add("workspaces", registerWorkspaces)
}

type workspaceList struct {
	Active     string           `json:"active"`
	Workspaces []hypr.Workspace `json:"workspaces"`
}

type monitorList struct {
	Monitors []hypr.Monitor `json:"monitors"`
}

type switchArgs struct {
	Workspace string `json:"workspace" jsonschema:"a number, a name, +1/-1 for relative, or special:<name> to toggle a scratchpad"`
}

func registerWorkspaces(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List workspaces with their monitor and window count, plus which one is active.",
		Annotations: readOnly("List workspaces"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, workspaceList, error) {
		var out workspaceList
		if err := hypr.Query("workspaces", &out.Workspaces); err != nil {
			return nil, out, err
		}
		var active hypr.Ref
		if err := hypr.Query("activeworkspace", &active); err != nil {
			return nil, out, err
		}
		out.Active = active.Name
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_monitors",
		Description: "List monitors with resolution, scale, position and active workspace.",
		Annotations: readOnly("List monitors"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, monitorList, error) {
		out := monitorList{}
		return nil, out, hypr.Query("monitors", &out.Monitors)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "switch_workspace",
		Description: "Switch to a workspace, or toggle a special (scratchpad) workspace.",
		Annotations: action("Switch workspace", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in switchArgs) (*mcp.CallToolResult, ok, error) {
		if name, found := strings.CutPrefix(in.Workspace, "special:"); found {
			return nil, done, hypr.Dispatch("togglespecialworkspace", name)
		}
		return nil, done, hypr.Dispatch("workspace", in.Workspace)
	})

	return nil
}
