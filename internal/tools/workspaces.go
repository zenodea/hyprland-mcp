package tools

import (
	"context"
	"fmt"
	"strconv"
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

type workspaceMonitorArgs struct {
	Workspace string `json:"workspace" jsonschema:"workspace number or name"`
	Monitor   string `json:"monitor" jsonschema:"monitor name from list_monitors, e.g. DP-1, or a direction: l, r, u, d, +1, -1"`
}

type renameArgs struct {
	Workspace string `json:"workspace" jsonschema:"workspace number or current name"`
	Name      string `json:"name,omitempty" jsonschema:"new name; empty resets it to the number"`
}

type monitorArgs struct {
	Monitor string   `json:"monitor" jsonschema:"monitor name from list_monitors, e.g. DP-1"`
	Enabled *bool    `json:"enabled,omitempty" jsonschema:"false turns the output off and moves its workspaces away; true turns it back on"`
	Scale   *float64 `json:"scale,omitempty" jsonschema:"new scale, e.g. 1, 1.25, 1.5, 2; Hyprland picks the nearest valid one"`
	Screen  *bool    `json:"screen_on,omitempty" jsonschema:"false blanks the screen (DPMS) but keeps the output and its workspaces; true wakes it"`
}

func listWorkspaces() (workspaceList, error) {
	var out workspaceList
	if err := hypr.Query("workspaces", &out.Workspaces); err != nil {
		return out, err
	}
	var active hypr.Ref
	if err := hypr.Query("activeworkspace", &active); err != nil {
		return out, err
	}
	out.Active = active.Name
	return out, nil
}

func listMonitors() (monitorList, error) {
	var out monitorList
	err := hypr.Query("monitors", &out.Monitors)
	return out, err
}

// workspaceID resolves a workspace number or name to its id.
func workspaceID(ref string) (int, error) {
	var workspaces []hypr.Workspace
	if err := hypr.Query("workspaces", &workspaces); err != nil {
		return 0, err
	}
	for _, w := range workspaces {
		if w.Name == ref || strconv.Itoa(w.ID) == ref {
			return w.ID, nil
		}
	}
	return 0, fmt.Errorf("no workspace %q", ref)
}

// monitorRule rebuilds a monitor's current mode as a Hyprland monitor= rule with a new scale.
// Settings the JSON doesn't report (mirroring, bit depth, VRR) fall back to their defaults.
func monitorRule(m hypr.Monitor, scale float64) string {
	rule := fmt.Sprintf("%s,%dx%d@%.3f,%dx%d,%s", m.Name, m.Width, m.Height, m.RefreshRate, m.X, m.Y, strconv.FormatFloat(scale, 'f', -1, 64))
	if m.Transform != 0 {
		rule += fmt.Sprintf(",transform,%d", m.Transform)
	}
	return rule
}

func registerWorkspaces(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_workspaces",
		Description: "List workspaces with their monitor and window count, plus which one is active.",
		Annotations: readOnly("List workspaces"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, workspaceList, error) {
		out, err := listWorkspaces()
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_monitors",
		Description: "List monitors with resolution, scale, position and active workspace.",
		Annotations: readOnly("List monitors"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, monitorList, error) {
		out, err := listMonitors()
		return nil, out, err
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

	mcp.AddTool(s, &mcp.Tool{
		Name:        "move_workspace_to_monitor",
		Description: "Move a workspace, with its windows, to another monitor.",
		Annotations: action("Move workspace to monitor", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in workspaceMonitorArgs) (*mcp.CallToolResult, ok, error) {
		return nil, done, hypr.Dispatch("moveworkspacetomonitor", in.Workspace+" "+in.Monitor)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "rename_workspace",
		Description: "Give a workspace a name, shown by bars instead of its number.",
		Annotations: action("Rename workspace", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in renameArgs) (*mcp.CallToolResult, ok, error) {
		id, err := workspaceID(in.Workspace)
		if err != nil {
			return nil, done, err
		}
		return nil, done, hypr.Dispatch("renameworkspace", strings.TrimSpace(fmt.Sprintf("%d %s", id, in.Name)))
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_monitor",
		Description: "Turn a monitor on or off, blank or wake its screen, or change its scale. Changes last until Hyprland reloads its config. Won't turn off the last active monitor.",
		Annotations: action("Set monitor", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in monitorArgs) (*mcp.CallToolResult, monitorList, error) {
		out := monitorList{}
		if in.Enabled == nil && in.Scale == nil && in.Screen == nil {
			return nil, out, fmt.Errorf("give at least one of enabled, scale or screen_on")
		}
		var monitors []hypr.Monitor
		if err := hypr.Query("monitors all", &monitors); err != nil {
			return nil, out, err
		}
		var target *hypr.Monitor
		active, lit := 0, 0
		for i, m := range monitors {
			if m.Name == in.Monitor {
				target = &monitors[i]
			}
			if !m.Disabled {
				active++
				if m.DPMS {
					lit++
				}
			}
		}
		if target == nil {
			return nil, out, fmt.Errorf("no monitor %q", in.Monitor)
		}
		if in.Screen != nil && !*in.Screen && target.DPMS && lit <= 1 {
			return nil, out, fmt.Errorf("refusing to blank the only screen that is on")
		}

		switch {
		case in.Enabled != nil && !*in.Enabled:
			if !target.Disabled && active <= 1 {
				return nil, out, fmt.Errorf("refusing to turn off the only active monitor")
			}
			if err := hypr.Keyword("monitor", target.Name+",disable"); err != nil {
				return nil, out, err
			}
		case target.Disabled && (in.Enabled != nil || in.Scale != nil):
			scale := 1.0
			if in.Scale != nil {
				scale = *in.Scale
			}
			if err := hypr.Keyword("monitor", fmt.Sprintf("%s,preferred,auto,%s", target.Name, strconv.FormatFloat(scale, 'f', -1, 64))); err != nil {
				return nil, out, err
			}
		case in.Scale != nil:
			if *in.Scale < 0.25 || *in.Scale > 4 {
				return nil, out, fmt.Errorf("scale must be between 0.25 and 4")
			}
			if err := hypr.Keyword("monitor", monitorRule(*target, *in.Scale)); err != nil {
				return nil, out, err
			}
		}

		if in.Screen != nil {
			state := "off"
			if *in.Screen {
				state = "on"
			}
			if err := hypr.Dispatch("dpms", state+" "+target.Name); err != nil {
				return nil, out, err
			}
		}
		err := hypr.Query("monitors all", &out.Monitors)
		return nil, out, err
	})

	return nil
}
