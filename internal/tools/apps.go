package tools

import (
	"bufio"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func init() {
	add("apps", registerApps)
}

type app struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Command     string   `json:"command"`
	Description string   `json:"description,omitempty"`
	Categories  []string `json:"categories,omitempty"`
	Terminal    bool     `json:"terminal,omitempty"`
}

type appList struct {
	Apps []app `json:"apps"`
}

type appQuery struct {
	Query string `json:"query,omitempty" jsonschema:"case-insensitive filter on name, id, description and categories"`
}

func appDirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	dirs := []string{filepath.Join(dataHome, "applications")}
	for _, d := range strings.Split(dataDirs, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return dirs
}

// stripFieldCodes removes %f, %U and friends from an Exec line; %% becomes %.
func stripFieldCodes(exec string) string {
	var b strings.Builder
	for i := 0; i < len(exec); i++ {
		if exec[i] == '%' && i+1 < len(exec) {
			i++
			if exec[i] == '%' {
				b.WriteByte('%')
			}
			continue
		}
		b.WriteByte(exec[i])
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// parseDesktopEntry returns ok=false for entries that shouldn't be listed.
func parseDesktopEntry(id string, r *bufio.Scanner) (app, bool) {
	a := app{ID: id}
	inEntry, hidden, isApp := false, false, false
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if strings.HasPrefix(line, "[") {
			if inEntry {
				break
			}
			inEntry = line == "[Desktop Entry]"
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !inEntry || !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Type":
			isApp = value == "Application"
		case "Name":
			a.Name = value
		case "Comment":
			a.Description = value
		case "Exec":
			a.Command = stripFieldCodes(value)
		case "Categories":
			a.Categories = slices.DeleteFunc(strings.Split(value, ";"), func(s string) bool { return s == "" })
		case "Terminal":
			a.Terminal = value == "true"
		case "NoDisplay", "Hidden":
			hidden = hidden || value == "true"
		}
	}
	return a, isApp && !hidden && a.Name != "" && a.Command != ""
}

func installedApps() []app {
	seen := map[string]bool{}
	var apps []app
	for _, dir := range appDirs() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), string(filepath.Separator), "-")
			if seen[id] {
				return nil
			}
			seen[id] = true
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			defer f.Close()
			if a, ok := parseDesktopEntry(id, bufio.NewScanner(f)); ok {
				apps = append(apps, a)
			}
			return nil
		})
	}
	slices.SortFunc(apps, func(a, b app) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return apps
}

func (a app) matches(q string) bool {
	hay := strings.ToLower(strings.Join(append([]string{a.ID, a.Name, a.Description}, a.Categories...), " "))
	return strings.Contains(hay, strings.ToLower(q))
}

func registerApps(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_apps",
		Description: "List installed desktop applications with the command to start each one. Pass the command to launch_app.",
		Annotations: readOnly("List apps"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in appQuery) (*mcp.CallToolResult, appList, error) {
		out := appList{Apps: []app{}}
		for _, a := range installedApps() {
			if in.Query == "" || a.matches(in.Query) {
				out.Apps = append(out.Apps, a)
			}
		}
		return nil, out, nil
	})
	return nil
}
