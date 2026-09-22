package tools

import (
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type group struct {
	name     string
	register func(*mcp.Server) error
}

var groups []group

func add(name string, register func(*mcp.Server) error) {
	groups = append(groups, group{name, register})
}

func Groups() []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.name)
	}
	slices.Sort(names)
	return names
}

func Register(s *mcp.Server, skip map[string]bool) error {
	for name := range skip {
		if !slices.Contains(Groups(), name) {
			return fmt.Errorf("unknown tool group %q", name)
		}
	}
	for _, g := range groups {
		if skip[g.name] {
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
