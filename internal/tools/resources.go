package tools

import (
	"context"
	"encoding/json"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

// Resources expose the desktop's state for clients to read or subscribe to.
// Hyprland's event socket drives the update notifications.
func init() {
	add("resources", registerResources)
}

const (
	windowsURI    = "hyprland://windows"
	workspacesURI = "hyprland://workspaces"
	monitorsURI   = "hyprland://monitors"
)

// eventResources maps each Hyprland event to the resources it changes.
var eventResources = map[string][]string{
	"openwindow":         {windowsURI, workspacesURI},
	"closewindow":        {windowsURI, workspacesURI},
	"movewindow":         {windowsURI, workspacesURI},
	"movewindowv2":       {windowsURI, workspacesURI},
	"windowtitlev2":      {windowsURI},
	"activewindowv2":     {windowsURI},
	"changefloatingmode": {windowsURI},
	"fullscreen":         {windowsURI, workspacesURI},
	"pin":                {windowsURI},
	"workspacev2":        {workspacesURI, monitorsURI},
	"createworkspacev2":  {workspacesURI},
	"destroyworkspacev2": {workspacesURI},
	"renameworkspace":    {workspacesURI},
	"moveworkspacev2":    {workspacesURI, monitorsURI},
	"activespecialv2":    {workspacesURI, monitorsURI},
	"focusedmonv2":       {workspacesURI, monitorsURI},
	"monitoraddedv2":     {monitorsURI, workspacesURI},
	"monitorremovedv2":   {monitorsURI, workspacesURI},
	"configreloaded":     {monitorsURI},
}

func jsonResource[T any](uri string, list func() (T, error)) mcp.ResourceHandler {
	return func(ctx context.Context, _ *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		v, err := list()
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(data)}}}, nil
	}
}

func registerResources(s *mcp.Server) error {
	s.AddResource(&mcp.Resource{
		URI: windowsURI, Name: "windows", MIMEType: "application/json",
		Description: "Every mapped window with its class, title, workspace and state. Updates when windows open, close, move or change focus.",
	}, jsonResource(windowsURI, listWindows))
	s.AddResource(&mcp.Resource{
		URI: workspacesURI, Name: "workspaces", MIMEType: "application/json",
		Description: "Workspaces with their monitor and window count, plus the active one.",
	}, jsonResource(workspacesURI, listWorkspaces))
	s.AddResource(&mcp.Resource{
		URI: monitorsURI, Name: "monitors", MIMEType: "application/json",
		Description: "Monitors with resolution, scale, position and active workspace.",
	}, jsonResource(monitorsURI, listMonitors))

	watchers = append(watchers, notifyResources)
	return nil
}

// notifyResources tells subscribers when a resource changes, batching bursts of
// events, and reconnects if Hyprland restarts.
func notifyResources(ctx context.Context, s *mcp.Server) {
	for ctx.Err() == nil {
		events, err := hypr.Events(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			continue
		}
		dirty := map[string]bool{}
		var flush <-chan time.Time
		for events != nil {
			select {
			case e, open := <-events:
				if !open {
					events = nil
					break
				}
				for _, uri := range eventResources[e.Name] {
					dirty[uri] = true
				}
				if len(dirty) > 0 && flush == nil {
					flush = time.After(250 * time.Millisecond)
				}
			case <-flush:
				for uri := range dirty {
					_ = s.ResourceUpdated(ctx, &mcp.ResourceUpdatedNotificationParams{URI: uri})
				}
				clear(dirty)
				flush = nil
			}
		}
	}
}
