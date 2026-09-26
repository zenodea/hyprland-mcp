package tools

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("network", registerNetwork)
}

type netDevice struct {
	Device     string `json:"device"`
	Type       string `json:"type"`
	State      string `json:"state"`
	Connection string `json:"connection,omitempty"`
}

type netConnection struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Device string `json:"device,omitempty"`
	Active bool   `json:"active"`
}

type networkStatus struct {
	Wifi        bool            `json:"wifi_enabled"`
	Devices     []netDevice     `json:"devices"`
	Connections []netConnection `json:"connections"`
}

type wifiNetwork struct {
	SSID     string `json:"ssid"`
	Signal   int    `json:"signal"`
	Security string `json:"security,omitempty"`
	InUse    bool   `json:"in_use"`
}

type wifiList struct {
	Networks []wifiNetwork `json:"networks"`
}

type wifiListArgs struct {
	Rescan bool `json:"rescan,omitempty" jsonschema:"scan again first (takes a few seconds)"`
}

type connectWifiArgs struct {
	SSID     string `json:"ssid" jsonschema:"network name from list_wifi"`
	Password string `json:"password,omitempty" jsonschema:"only needed for a network that isn't saved yet"`
}

type connectionArgs struct {
	Name string `json:"name" jsonschema:"saved connection name from network_status, e.g. a VPN or wired profile"`
	Up   bool   `json:"up" jsonschema:"true to activate, false to deactivate"`
}

type enabledArgs struct {
	Enabled bool `json:"enabled"`
}

// terse splits an nmcli -t line, honouring its \: escapes.
func terse(line string) []string {
	var fields []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line):
			i++
			cur.WriteByte(line[i])
		case line[i] == ':':
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(fields, cur.String())
}

func nmRows(ctx context.Context, fields int, args ...string) ([][]string, error) {
	text, err := run.Output(ctx, "nmcli", append([]string{"-t"}, args...)...)
	if err != nil {
		return nil, err
	}
	return parseTerseRows(text, fields), nil
}

// parseWifi merges access points that share an SSID, keeping the strongest signal.
func parseWifi(rows [][]string) []wifiNetwork {
	out := []wifiNetwork{}
	seen := map[string]int{}
	for _, f := range rows {
		if f[1] == "" {
			continue
		}
		signal, _ := strconv.Atoi(f[2])
		n := wifiNetwork{SSID: f[1], Signal: signal, Security: f[3], InUse: f[0] == "*"}
		if i, dup := seen[n.SSID]; dup {
			prev := &out[i]
			prev.InUse = prev.InUse || n.InUse
			prev.Signal = max(prev.Signal, n.Signal)
			continue
		}
		seen[n.SSID] = len(out)
		out = append(out, n)
	}
	return out
}

func parseTerseRows(text string, fields int) [][]string {
	var rows [][]string
	for _, line := range strings.Split(text, "\n") {
		if f := terse(line); line != "" && len(f) == fields {
			rows = append(rows, f)
		}
	}
	return rows
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func registerNetwork(s *mcp.Server) error {
	if !run.Has("nmcli") {
		return nil
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "network_status",
		Description: "Show network devices, whether wifi is on, and saved connections (wifi, wired, VPN, WireGuard) with which are active.",
		Annotations: readOnly("Network status"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, networkStatus, error) {
		out := networkStatus{Devices: []netDevice{}, Connections: []netConnection{}}
		radio, err := run.Output(ctx, "nmcli", "radio", "wifi")
		if err != nil {
			return nil, out, err
		}
		out.Wifi = radio == "enabled"
		devices, err := nmRows(ctx, 4, "-f", "DEVICE,TYPE,STATE,CONNECTION", "device")
		if err != nil {
			return nil, out, err
		}
		for _, f := range devices {
			if f[1] != "loopback" && f[1] != "wifi-p2p" {
				out.Devices = append(out.Devices, netDevice{f[0], f[1], f[2], f[3]})
			}
		}
		conns, err := nmRows(ctx, 4, "-f", "NAME,TYPE,DEVICE,ACTIVE", "connection", "show")
		if err != nil {
			return nil, out, err
		}
		for _, f := range conns {
			if f[1] != "loopback" {
				out.Connections = append(out.Connections, netConnection{f[0], f[1], f[2], f[3] == "yes"})
			}
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_wifi",
		Description: "List visible wifi networks, strongest first, marking the one in use.",
		Annotations: readOnly("List wifi"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in wifiListArgs) (*mcp.CallToolResult, wifiList, error) {
		out := wifiList{Networks: []wifiNetwork{}}
		rows, err := nmRows(ctx, 4, "-f", "IN-USE,SSID,SIGNAL,SECURITY", "device", "wifi", "list", "--rescan", map[bool]string{true: "yes", false: "auto"}[in.Rescan])
		if err != nil {
			return nil, out, err
		}
		out.Networks = parseWifi(rows)
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "connect_wifi",
		Description: "Connect to a wifi network. Saved networks need no password.",
		Annotations: action("Connect to wifi", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in connectWifiArgs) (*mcp.CallToolResult, ok, error) {
		args := []string{"device", "wifi", "connect", in.SSID}
		if in.Password != "" {
			args = append(args, "password", in.Password)
		}
		_, err := run.Output(ctx, "nmcli", args...)
		return nil, done, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_connection",
		Description: "Bring a saved NetworkManager connection (VPN, WireGuard, wired, wifi) up or down.",
		Annotations: action("Toggle connection", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in connectionArgs) (*mcp.CallToolResult, ok, error) {
		_, err := run.Output(ctx, "nmcli", "connection", map[bool]string{true: "up", false: "down"}[in.Up], "id", in.Name)
		return nil, done, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_wifi",
		Description: "Turn the wifi radio on or off. Turning it off drops any wifi connection.",
		Annotations: action("Toggle wifi", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in enabledArgs) (*mcp.CallToolResult, ok, error) {
		_, err := run.Output(ctx, "nmcli", "radio", "wifi", onOff(in.Enabled))
		return nil, done, err
	})

	return nil
}
