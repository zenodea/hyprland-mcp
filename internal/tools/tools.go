package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type group struct {
	name     string
	register func(*mcp.Server) error
	optIn    bool
}

var groups []group

func add(name string, register func(*mcp.Server) error) {
	groups = append(groups, group{name, register, false})
}

// addOptIn adds a group that is only registered when named with -enable.
func addOptIn(name string, register func(*mcp.Server) error) {
	groups = append(groups, group{name, register, true})
}

// watchers run in the background for the life of the server, started by Watch.
var watchers []func(context.Context, *mcp.Server)

// Watch starts the background work registered groups need, such as resource
// update notifications. It returns immediately.
func Watch(ctx context.Context, s *mcp.Server) {
	for _, w := range watchers {
		go w(ctx, s)
	}
}

// ServerOptions accepts resource subscriptions; the SDK tracks who subscribed
// and Watch sends the updates.
func ServerOptions() *mcp.ServerOptions {
	return &mcp.ServerOptions{
		SubscribeHandler:   func(context.Context, *mcp.SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *mcp.UnsubscribeRequest) error { return nil },
	}
}

// Groups lists the tool groups, either the default ones or the opt-in ones.
func Groups(optIn bool) []string {
	names := []string{}
	for _, g := range groups {
		if g.optIn == optIn {
			names = append(names, g.name)
		}
	}
	slices.Sort(names)
	return names
}

func Register(s *mcp.Server, skip, enable map[string]bool) error {
	for name := range skip {
		if !slices.Contains(Groups(false), name) && !slices.Contains(Groups(true), name) {
			return fmt.Errorf("unknown tool group %q", name)
		}
	}
	for name := range enable {
		if !slices.Contains(Groups(true), name) {
			return fmt.Errorf("%q is not an opt-in tool group (those are: %s)", name, strings.Join(Groups(true), ", "))
		}
	}
	for _, g := range groups {
		if skip[g.name] || (g.optIn && !enable[g.name]) {
			continue
		}
		if err := g.register(s); err != nil {
			return fmt.Errorf("%s: %w", g.name, err)
		}
	}
	return nil
}

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: new(false)}
}

func action(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: new(destructive), OpenWorldHint: new(false)}
}

type ok struct {
	OK bool `json:"ok"`
}

var done = ok{OK: true}
