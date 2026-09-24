package tools

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

const (
	pollEvery   = 150 * time.Millisecond
	defaultWait = 10
	maxWait     = 60
)

type waitArgs struct {
	Class   string `json:"class,omitempty" jsonschema:"case-insensitive regex on the window class, e.g. firefox"`
	Title   string `json:"title,omitempty" jsonschema:"case-insensitive regex on the window title"`
	NewOnly bool   `json:"new_only,omitempty" jsonschema:"ignore windows that already exist when the call starts"`
	Timeout int    `json:"timeout,omitempty" jsonschema:"seconds to wait, default 10, max 60"`
}

type windowMatcher struct {
	class, title *regexp.Regexp
}

func newMatcher(class, title string) (windowMatcher, error) {
	var m windowMatcher
	var err error
	if class == "" && title == "" {
		return m, fmt.Errorf("give a class or a title to wait for")
	}
	if class != "" {
		if m.class, err = regexp.Compile("(?i)" + class); err != nil {
			return m, fmt.Errorf("class: %w", err)
		}
	}
	if title != "" {
		if m.title, err = regexp.Compile("(?i)" + title); err != nil {
			return m, fmt.Errorf("title: %w", err)
		}
	}
	return m, nil
}

func (m windowMatcher) match(c hypr.Client) bool {
	if !c.Mapped || c.Hidden {
		return false
	}
	if m.class != nil && !m.class.MatchString(c.Class) && !m.class.MatchString(c.InitialClass) {
		return false
	}
	return m.title == nil || m.title.MatchString(c.Title)
}

func windowAddresses() (map[string]bool, error) {
	var clients []hypr.Client
	if err := hypr.Query("clients", &clients); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(clients))
	for _, c := range clients {
		seen[c.Address] = true
	}
	return seen, nil
}

// waitForWindow polls until a window matches, skipping addresses in exclude.
func waitForWindow(ctx context.Context, m windowMatcher, exclude map[string]bool, seconds int) (window, error) {
	if seconds <= 0 {
		seconds = defaultWait
	}
	deadline := time.Now().Add(time.Duration(min(seconds, maxWait)) * time.Second)
	for {
		var clients []hypr.Client
		if err := hypr.Query("clients", &clients); err != nil {
			return window{}, err
		}
		for _, c := range clients {
			if !exclude[c.Address] && m.match(c) {
				return toWindow(c), nil
			}
		}
		if time.Now().After(deadline) {
			return window{}, fmt.Errorf("no matching window appeared within %ds", seconds)
		}
		select {
		case <-ctx.Done():
			return window{}, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

func registerWait(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "wait_for_window",
		Description: "Wait until a window matching a class or title is open, then describe it. Use after starting something that opens a window.",
		Annotations: readOnly("Wait for window"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in waitArgs) (*mcp.CallToolResult, window, error) {
		m, err := newMatcher(in.Class, in.Title)
		if err != nil {
			return nil, window{}, err
		}
		exclude := map[string]bool{}
		if in.NewOnly {
			if exclude, err = windowAddresses(); err != nil {
				return nil, window{}, err
			}
		}
		w, err := waitForWindow(ctx, m, exclude, in.Timeout)
		return nil, w, err
	})
}
