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
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

var ConfigPath string

const outputLimit = 20000

func init() {
	add("custom", registerCustom)
}

func DefaultConfig() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "hyprland-mcp", "config.json")
}

type customArg struct {
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
	Optional    bool     `json:"optional"`
}

type customCommand struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Command     []string             `json:"command"`
	Args        map[string]customArg `json:"args"`
	ReadOnly    bool                 `json:"read_only"`
	Destructive bool                 `json:"destructive"`
	Timeout     int                  `json:"timeout"`
}

type customConfig struct {
	Commands []customCommand `json:"commands"`
}

type commandOutput struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

var (
	toolName    = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	placeholder = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)
)

func loadConfig(path string) (customConfig, error) {
	var cfg customConfig
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

func (c customCommand) validate() error {
	if !toolName.MatchString(c.Name) {
		return fmt.Errorf("command name %q must match %s", c.Name, toolName)
	}
	if len(c.Command) == 0 {
		return fmt.Errorf("%s: command is empty", c.Name)
	}
	for _, part := range c.Command {
		for _, m := range placeholder.FindAllStringSubmatch(part, -1) {
			if _, ok := c.Args[m[1]]; !ok {
				return fmt.Errorf("%s: {%s} is used but not declared in args", c.Name, m[1])
			}
		}
	}
	return nil
}

func (c customCommand) schema() *jsonschema.Schema {
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}, AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}}}
	for name, arg := range c.Args {
		prop := &jsonschema.Schema{Type: "string", Description: arg.Description}
		for _, v := range arg.Enum {
			prop.Enum = append(prop.Enum, v)
		}
		schema.Properties[name] = prop
		if !arg.Optional {
			schema.Required = append(schema.Required, name)
		}
	}
	return schema
}

// argv fills placeholders; an element that is only an empty optional placeholder is dropped.
func (c customCommand) argv(values map[string]string) []string {
	var out []string
	for _, part := range c.Command {
		if m := placeholder.FindStringSubmatch(part); m != nil && m[0] == part && values[m[1]] == "" {
			continue
		}
		out = append(out, placeholder.ReplaceAllStringFunc(part, func(p string) string {
			return values[p[1:len(p)-1]]
		}))
	}
	return out
}

func (c customCommand) handler(schema *jsonschema.Resolved) mcp.ToolHandler {
	limit := 30 * time.Second
	if c.Timeout > 0 {
		limit = time.Duration(c.Timeout) * time.Second
	}
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		values := map[string]string{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &values); err != nil {
				return nil, fmt.Errorf("arguments must all be strings: %w", err)
			}
		}
		res := &mcp.CallToolResult{}
		asAny := map[string]any{}
		for k, v := range values {
			asAny[k] = v
		}
		if err := schema.Validate(asAny); err != nil {
			res.SetError(fmt.Errorf("invalid arguments: %w", err))
			return res, nil
		}
		argv := c.argv(values)
		out, err := run.Exec(ctx, limit, nil, argv[0], argv[1:]...)
		if err != nil {
			res.SetError(err)
			return res, nil
		}
		result := commandOutput{Output: strings.TrimSpace(string(out))}
		if len(result.Output) > outputLimit {
			result.Output, result.Truncated = result.Output[:outputLimit], true
		}
		text, _ := json.Marshal(result)
		res.Content = []mcp.Content{&mcp.TextContent{Text: string(text)}}
		return res, nil
	}
}

func registerCustom(s *mcp.Server) error {
	cfg, err := loadConfig(ConfigPath)
	if err != nil {
		return err
	}
	for _, c := range cfg.Commands {
		if err := c.validate(); err != nil {
			return err
		}
		annotations := readOnly(c.Name)
		if !c.ReadOnly {
			annotations = action(c.Name, c.Destructive)
		}
		schema := c.schema()
		resolved, err := schema.Resolve(nil)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		s.AddTool(&mcp.Tool{
			Name:        c.Name,
			Description: c.Description,
			InputSchema: schema,
			Annotations: annotations,
		}, c.handler(resolved))
	}
	return nil
}
