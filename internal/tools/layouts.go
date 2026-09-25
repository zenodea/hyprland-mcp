package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

func init() {
	add("layouts", registerLayouts)
}

type savedWindow struct {
	Class     string `json:"class"`
	Title     string `json:"title"`
	Workspace string `json:"workspace"`
	Floating  bool   `json:"floating"`
	Position  [2]int `json:"position"`
	Size      [2]int `json:"size"`
	Command   string `json:"command,omitempty"`
}

type layout struct {
	Saved   time.Time     `json:"saved"`
	Windows []savedWindow `json:"windows"`
}

type layoutSummary struct {
	Name       string    `json:"name"`
	Saved      time.Time `json:"saved"`
	Windows    int       `json:"windows"`
	Workspaces []string  `json:"workspaces"`
}

type layoutList struct {
	Layouts []layoutSummary `json:"layouts"`
}

type saveLayoutArgs struct {
	Name       string   `json:"name"`
	Workspaces []string `json:"workspaces,omitempty" jsonschema:"only save these workspaces; omit for all"`
}

type restoreLayoutArgs struct {
	Name          string `json:"name"`
	LaunchMissing *bool  `json:"launch_missing,omitempty" jsonschema:"start saved windows that aren't open, default true"`
}

type nameArgs struct {
	Name string `json:"name"`
}

type restoreResult struct {
	Moved    int      `json:"moved"`
	Launched int      `json:"launched"`
	Skipped  []string `json:"skipped,omitempty"`
}

var safeArg = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func layoutsPath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "hyprland-mcp", "layouts.json")
}

func loadLayouts() (map[string]layout, error) {
	out := map[string]layout{}
	data, err := os.ReadFile(layoutsPath())
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return out, json.Unmarshal(data, &out)
}

func storeLayouts(layouts map[string]layout) error {
	path := layoutsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(layouts, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func shellQuote(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		if safeArg.MatchString(a) {
			quoted[i] = a
		} else {
			quoted[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}

func commandLine(pid int) string {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil || len(raw) == 0 {
		return ""
	}
	return shellQuote(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00"))
}

// execRules builds the [rules] prefix so a launched window lands where it was saved.
func execRules(w savedWindow) string {
	rules := []string{"workspace " + w.Workspace + " silent"}
	if w.Floating {
		rules = append(rules, "float", fmt.Sprintf("move %d %d", w.Position[0], w.Position[1]), fmt.Sprintf("size %d %d", w.Size[0], w.Size[1]))
	}
	return "[" + strings.Join(rules, "; ") + "] "
}

func placeWindow(address string, w savedWindow, floating bool) error {
	target := "address:" + address
	if err := hypr.Dispatch("movetoworkspacesilent", w.Workspace+","+target); err != nil {
		return err
	}
	if w.Floating != floating {
		if err := hypr.Dispatch("togglefloating", target); err != nil {
			return err
		}
	}
	if !w.Floating {
		return nil
	}
	if err := hypr.Dispatch("resizewindowpixel", fmt.Sprintf("exact %d %d,%s", w.Size[0], w.Size[1], target)); err != nil {
		return err
	}
	return hypr.Dispatch("movewindowpixel", fmt.Sprintf("exact %d %d,%s", w.Position[0], w.Position[1], target))
}

// pickWindow prefers an unused window with the same class and title, then any with the same class.
func pickWindow(clients []hypr.Client, used map[string]bool, w savedWindow) int {
	best := -1
	for i, c := range clients {
		if used[c.Address] || !c.Mapped || c.Hidden || c.Class != w.Class {
			continue
		}
		if c.Title == w.Title {
			return i
		}
		if best < 0 {
			best = i
		}
	}
	return best
}

func registerLayouts(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "save_layout",
		Description: "Save which windows are open on which workspaces (and floating geometry) under a name, to restore later.",
		Annotations: action("Save layout", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in saveLayoutArgs) (*mcp.CallToolResult, layoutSummary, error) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, layoutSummary{}, fmt.Errorf("name is required")
		}
		var clients []hypr.Client
		if err := hypr.Query("clients", &clients); err != nil {
			return nil, layoutSummary{}, err
		}
		l := layout{Saved: time.Now().Round(time.Second)}
		for _, c := range clients {
			if !c.Mapped || c.Hidden || strings.HasPrefix(c.Workspace.Name, "special:") {
				continue
			}
			if len(in.Workspaces) > 0 && !slices.Contains(in.Workspaces, c.Workspace.Name) {
				continue
			}
			l.Windows = append(l.Windows, savedWindow{
				Class: c.Class, Title: c.Title, Workspace: c.Workspace.Name,
				Floating: c.Floating, Position: c.At, Size: c.Size, Command: commandLine(c.PID),
			})
		}
		if len(l.Windows) == 0 {
			return nil, layoutSummary{}, fmt.Errorf("no windows to save")
		}
		layouts, err := loadLayouts()
		if err != nil {
			return nil, layoutSummary{}, err
		}
		layouts[in.Name] = l
		return nil, summarize(in.Name, l), storeLayouts(layouts)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_layouts",
		Description: "List saved window layouts.",
		Annotations: readOnly("List layouts"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, layoutList, error) {
		layouts, err := loadLayouts()
		out := layoutList{Layouts: []layoutSummary{}}
		for name, l := range layouts {
			out.Layouts = append(out.Layouts, summarize(name, l))
		}
		slices.SortFunc(out.Layouts, func(a, b layoutSummary) int { return strings.Compare(a.Name, b.Name) })
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "restore_layout",
		Description: "Move open windows back to their saved workspaces and floating geometry, launching saved windows that aren't open. Tiled windows get their workspace back but not their exact tile.",
		Annotations: action("Restore layout", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in restoreLayoutArgs) (*mcp.CallToolResult, restoreResult, error) {
		layouts, err := loadLayouts()
		if err != nil {
			return nil, restoreResult{}, err
		}
		l, found := layouts[in.Name]
		if !found {
			return nil, restoreResult{}, fmt.Errorf("no layout named %q", in.Name)
		}
		var clients []hypr.Client
		if err := hypr.Query("clients", &clients); err != nil {
			return nil, restoreResult{}, err
		}
		launch := in.LaunchMissing == nil || *in.LaunchMissing
		used := map[string]bool{}
		var out restoreResult
		for _, w := range l.Windows {
			if i := pickWindow(clients, used, w); i >= 0 {
				used[clients[i].Address] = true
				if err := placeWindow(clients[i].Address, w, clients[i].Floating); err != nil {
					out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", w.Class, err))
					continue
				}
				out.Moved++
				continue
			}
			if !launch || w.Command == "" {
				out.Skipped = append(out.Skipped, w.Class+": not open")
				continue
			}
			if err := hypr.Dispatch("exec", execRules(w)+w.Command); err != nil {
				out.Skipped = append(out.Skipped, fmt.Sprintf("%s: %v", w.Class, err))
				continue
			}
			out.Launched++
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "delete_layout",
		Description: "Delete a saved layout.",
		Annotations: action("Delete layout", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in nameArgs) (*mcp.CallToolResult, ok, error) {
		layouts, err := loadLayouts()
		if err != nil {
			return nil, done, err
		}
		if _, found := layouts[in.Name]; !found {
			return nil, done, fmt.Errorf("no layout named %q", in.Name)
		}
		delete(layouts, in.Name)
		return nil, done, storeLayouts(layouts)
	})

	return nil
}

func summarize(name string, l layout) layoutSummary {
	s := layoutSummary{Name: name, Saved: l.Saved, Windows: len(l.Windows), Workspaces: []string{}}
	for _, w := range l.Windows {
		if !slices.Contains(s.Workspaces, w.Workspace) {
			s.Workspaces = append(s.Workspaces, w.Workspace)
		}
	}
	slices.Sort(s.Workspaces)
	return s
}
