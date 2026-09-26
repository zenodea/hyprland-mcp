package tools

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("bluetooth", registerBluetooth)
}

type btDevice struct {
	Address   string `json:"address"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

type btStatus struct {
	Powered bool       `json:"powered"`
	Devices []btDevice `json:"paired_devices"`
}

type btConnectArgs struct {
	Address string `json:"address" jsonschema:"device address from bluetooth_status, e.g. 3C:B0:ED:A7:78:00"`
	Connect bool   `json:"connect" jsonschema:"true to connect, false to disconnect"`
}

var btDeviceLine = regexp.MustCompile(`^Device ([0-9A-F:]{17}) (.*)$`)

func btDevices(ctx context.Context, filter string) (map[string]string, error) {
	text, err := run.Output(ctx, "bluetoothctl", "devices", filter)
	if err != nil {
		return nil, err
	}
	return parseBtDevices(text), nil
}

func parseBtDevices(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if m := btDeviceLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out[m[1]] = m[2]
		}
	}
	return out
}

func registerBluetooth(s *mcp.Server) error {
	if !run.Has("bluetoothctl") {
		return nil
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "bluetooth_status",
		Description: "Show whether Bluetooth is powered and list paired devices with their connection state.",
		Annotations: readOnly("Bluetooth status"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, btStatus, error) {
		out := btStatus{Devices: []btDevice{}}
		show, err := run.Output(ctx, "bluetoothctl", "show")
		if err != nil {
			return nil, out, err
		}
		out.Powered = strings.Contains(show, "Powered: yes")
		paired, err := btDevices(ctx, "Paired")
		if err != nil {
			return nil, out, err
		}
		connected, err := btDevices(ctx, "Connected")
		if err != nil {
			return nil, out, err
		}
		for addr, name := range paired {
			_, on := connected[addr]
			out.Devices = append(out.Devices, btDevice{addr, name, on})
		}
		slices.SortFunc(out.Devices, func(a, b btDevice) int { return strings.Compare(a.Name, b.Name) })
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "bluetooth_connect",
		Description: "Connect or disconnect a paired Bluetooth device.",
		Annotations: action("Bluetooth connect", false),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in btConnectArgs) (*mcp.CallToolResult, ok, error) {
		verb := "disconnect"
		if in.Connect {
			verb = "connect"
		}
		_, err := run.Output(ctx, "bluetoothctl", verb, in.Address)
		return nil, done, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_bluetooth",
		Description: "Power the Bluetooth adapter on or off. Turning it off disconnects every device.",
		Annotations: action("Toggle Bluetooth", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in enabledArgs) (*mcp.CallToolResult, ok, error) {
		_, err := run.Output(ctx, "bluetoothctl", "power", onOff(in.Enabled))
		return nil, done, err
	})

	return nil
}
