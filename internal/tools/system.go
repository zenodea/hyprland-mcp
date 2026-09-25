package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
	"github.com/zenodea/hyprland-mcp/internal/run"
)

func init() {
	add("system", registerSystem)
}

const powerSupply = "/sys/class/power_supply"

type battery struct {
	Name           string  `json:"name"`
	Percent        int     `json:"percent"`
	Status         string  `json:"status"`
	Watts          float64 `json:"watts,omitempty"`
	HoursRemaining float64 `json:"hours_remaining,omitempty"`
	HealthPercent  int     `json:"health_percent,omitempty"`
}

type batteryStatus struct {
	OnAC      bool      `json:"on_ac"`
	Batteries []battery `json:"batteries"`
}

type disk struct {
	Path   string  `json:"path"`
	UsedGB float64 `json:"used_gb"`
	FreeGB float64 `json:"free_gb"`
}

type systemInfo struct {
	Hostname      string   `json:"hostname"`
	Kernel        string   `json:"kernel"`
	UptimeHours   float64  `json:"uptime_hours"`
	CPUs          int      `json:"cpus"`
	Load          []string `json:"load_1_5_15"`
	MemoryTotalGB float64  `json:"memory_total_gb"`
	MemoryUsedGB  float64  `json:"memory_used_gb"`
	SwapUsedGB    float64  `json:"swap_used_gb"`
	Disks         []disk   `json:"disks"`
}

type process struct {
	PID     int     `json:"pid"`
	Command string  `json:"command"`
	CPU     float64 `json:"cpu_percent"`
	Memory  float64 `json:"memory_percent"`
	RSSMB   float64 `json:"rss_mb"`
}

type processList struct {
	Processes []process `json:"processes"`
}

type processArgs struct {
	Sort  string `json:"sort,omitempty" jsonschema:"cpu (default) or memory"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many to return, default 10"`
}

type killArgs struct {
	PID   int  `json:"pid"`
	Force bool `json:"force,omitempty" jsonschema:"send SIGKILL instead of SIGTERM"`
}

type profile struct {
	Active    string   `json:"active"`
	Available []string `json:"available"`
}

type profileArgs struct {
	Profile string `json:"profile" jsonschema:"one of the available profiles, usually power-saver, balanced or performance"`
}

type sessionArgs struct {
	Action string `json:"action" jsonschema:"lock, suspend, hibernate, logout, reboot or poweroff"`
}

func readSys(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

func readSysInt(path string) float64 {
	v, _ := strconv.ParseFloat(readSys(path), 64)
	return v
}

func readBattery(dir string) battery {
	b := battery{
		Name:    filepath.Base(dir),
		Percent: int(readSysInt(filepath.Join(dir, "capacity"))),
		Status:  readSys(filepath.Join(dir, "status")),
	}
	now, full, design := readSysInt(filepath.Join(dir, "energy_now")), readSysInt(filepath.Join(dir, "energy_full")), readSysInt(filepath.Join(dir, "energy_full_design"))
	power := readSysInt(filepath.Join(dir, "power_now"))
	if now == 0 {
		now, full, design = readSysInt(filepath.Join(dir, "charge_now")), readSysInt(filepath.Join(dir, "charge_full")), readSysInt(filepath.Join(dir, "charge_full_design"))
		power = readSysInt(filepath.Join(dir, "current_now")) * readSysInt(filepath.Join(dir, "voltage_now")) / 1e6
	}
	if power > 0 {
		b.Watts = round(power/1e6, 1)
		switch b.Status {
		case "Discharging":
			b.HoursRemaining = round(now/power, 1)
		case "Charging":
			b.HoursRemaining = round((full-now)/power, 1)
		}
	}
	if design > 0 {
		b.HealthPercent = int(full / design * 100)
	}
	return b
}

func batteries() batteryStatus {
	out := batteryStatus{Batteries: []battery{}}
	entries, _ := os.ReadDir(powerSupply)
	for _, e := range entries {
		dir := filepath.Join(powerSupply, e.Name())
		switch readSys(filepath.Join(dir, "type")) {
		case "Battery":
			if readSys(filepath.Join(dir, "scope")) != "Device" {
				out.Batteries = append(out.Batteries, readBattery(dir))
			}
		case "Mains":
			out.OnAC = out.OnAC || readSys(filepath.Join(dir, "online")) == "1"
		}
	}
	return out
}

func round(v float64, places int) float64 {
	p, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', places, 64), 64)
	return p
}

func parseMeminfo(text string) map[string]float64 {
	out := map[string]float64{}
	for _, line := range strings.Split(text, "\n") {
		key, rest, found := strings.Cut(line, ":")
		if f := strings.Fields(rest); found && len(f) > 0 {
			kb, _ := strconv.ParseFloat(f[0], 64)
			out[key] = kb
		}
	}
	return out
}

func diskUsage(path string) (disk, bool) {
	var st syscall.Statfs_t
	if syscall.Statfs(path, &st) != nil {
		return disk{}, false
	}
	gb := func(blocks uint64) float64 { return round(float64(blocks)*float64(st.Bsize)/1e9, 1) }
	return disk{Path: path, UsedGB: gb(st.Blocks - st.Bfree), FreeGB: gb(st.Bavail)}, true
}

const clockTicks = 100

type procSample struct {
	command string
	ticks   float64
	rssKB   float64
}

// parseStat reads the command and utime+stime ticks from /proc/<pid>/stat.
func parseStat(stat string) (string, float64, bool) {
	open, close := strings.IndexByte(stat, '('), strings.LastIndexByte(stat, ')')
	if open < 0 || close < open {
		return "", 0, false
	}
	f := strings.Fields(stat[close+1:])
	if len(f) < 13 {
		return "", 0, false
	}
	utime, _ := strconv.ParseFloat(f[11], 64)
	stime, _ := strconv.ParseFloat(f[12], 64)
	return stat[open+1 : close], utime + stime, true
}

func sampleProcesses() map[int]procSample {
	out := map[int]procSample{}
	entries, _ := os.ReadDir("/proc")
	page := float64(os.Getpagesize()) / 1024
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		command, ticks, ok := parseStat(readSys(filepath.Join("/proc", e.Name(), "stat")))
		if !ok {
			continue
		}
		var rss float64
		if f := strings.Fields(readSys(filepath.Join("/proc", e.Name(), "statm"))); len(f) > 1 {
			rss, _ = strconv.ParseFloat(f[1], 64)
		}
		out[pid] = procSample{command, ticks, rss * page}
	}
	return out
}

// topProcesses samples CPU time over the interval, so the figures are current rather than lifetime averages.
func topProcesses(ctx context.Context, byMemory bool, limit int, interval time.Duration) ([]process, error) {
	before := sampleProcesses()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(interval):
	}
	after := sampleProcesses()
	memTotal := parseMeminfo(readSys("/proc/meminfo"))["MemTotal"]
	self := os.Getpid()
	var out []process
	for pid, a := range after {
		b, seen := before[pid]
		if !seen || pid == self {
			continue
		}
		p := process{PID: pid, Command: a.command, RSSMB: round(a.rssKB/1024, 1)}
		p.CPU = round((a.ticks-b.ticks)/clockTicks/interval.Seconds()*100, 1)
		if memTotal > 0 {
			p.Memory = round(a.rssKB/memTotal*100, 1)
		}
		out = append(out, p)
	}
	slices.SortFunc(out, func(x, y process) int {
		if byMemory {
			return cmpDesc(x.RSSMB, y.RSSMB)
		}
		if c := cmpDesc(x.CPU, y.CPU); c != 0 {
			return c
		}
		return cmpDesc(x.RSSMB, y.RSSMB)
	})
	return out[:min(limit, len(out))], nil
}

func cmpDesc(a, b float64) int {
	switch {
	case a > b:
		return -1
	case a < b:
		return 1
	}
	return 0
}

func parseProfiles(text string) profile {
	p := profile{Available: []string{}}
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasSuffix(line, ":") || strings.HasPrefix(strings.TrimLeft(line, " *"), " ") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*")), ":")
		if strings.Contains(name, " ") {
			continue
		}
		p.Available = append(p.Available, name)
		if strings.HasPrefix(strings.TrimSpace(line), "*") {
			p.Active = name
		}
	}
	return p
}

var sessionCommands = map[string][]string{
	"lock":      {"loginctl", "lock-session"},
	"suspend":   {"systemctl", "suspend"},
	"hibernate": {"systemctl", "hibernate"},
	"reboot":    {"systemctl", "reboot"},
	"poweroff":  {"systemctl", "poweroff"},
}

func registerSystem(s *mcp.Server) error {
	mcp.AddTool(s, &mcp.Tool{
		Name:        "system_info",
		Description: "Summarise the machine: kernel, uptime, load, memory, swap and disk space.",
		Annotations: readOnly("System info"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, systemInfo, error) {
		host, _ := os.Hostname()
		info := systemInfo{Hostname: host, Kernel: readSys("/proc/sys/kernel/osrelease"), CPUs: runtime.NumCPU(), Disks: []disk{}}
		if f := strings.Fields(readSys("/proc/uptime")); len(f) > 0 {
			secs, _ := strconv.ParseFloat(f[0], 64)
			info.UptimeHours = round(secs/3600, 1)
		}
		if f := strings.Fields(readSys("/proc/loadavg")); len(f) >= 3 {
			info.Load = f[:3]
		}
		mem := parseMeminfo(readSys("/proc/meminfo"))
		info.MemoryTotalGB = round(mem["MemTotal"]/1e6, 1)
		info.MemoryUsedGB = round((mem["MemTotal"]-mem["MemAvailable"])/1e6, 1)
		info.SwapUsedGB = round((mem["SwapTotal"]-mem["SwapFree"])/1e6, 1)
		home, _ := os.UserHomeDir()
		for _, p := range []string{"/", home} {
			if d, ok := diskUsage(p); ok && !slices.ContainsFunc(info.Disks, func(x disk) bool { return x.UsedGB == d.UsedGB && x.FreeGB == d.FreeGB }) {
				info.Disks = append(info.Disks, d)
			}
		}
		return nil, info, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "battery_status",
		Description: "Battery charge, charging state, power draw, time remaining and health, plus whether AC power is connected.",
		Annotations: readOnly("Battery status"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, batteryStatus, error) {
		return nil, batteries(), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "top_processes",
		Description: "List the processes using the most CPU (sampled over half a second) or memory.",
		Annotations: readOnly("Top processes"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in processArgs) (*mcp.CallToolResult, processList, error) {
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		procs, err := topProcesses(ctx, in.Sort == "memory", limit, 500*time.Millisecond)
		return nil, processList{Processes: procs}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "kill_process",
		Description: "Stop a process by pid, politely (SIGTERM) or forcibly (SIGKILL).",
		Annotations: action("Kill process", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in killArgs) (*mcp.CallToolResult, ok, error) {
		if in.PID <= 1 {
			return nil, done, fmt.Errorf("refusing to signal pid %d", in.PID)
		}
		sig := syscall.SIGTERM
		if in.Force {
			sig = syscall.SIGKILL
		}
		return nil, done, syscall.Kill(in.PID, sig)
	})

	if run.Has("powerprofilesctl") {
		mcp.AddTool(s, &mcp.Tool{
			Name:        "power_profile",
			Description: "Show the active power profile and the ones available.",
			Annotations: readOnly("Power profile"),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, profile, error) {
			text, err := run.Output(ctx, "powerprofilesctl", "list")
			return nil, parseProfiles(text), err
		})

		mcp.AddTool(s, &mcp.Tool{
			Name:        "set_power_profile",
			Description: "Switch the power profile, e.g. to power-saver on battery or performance for heavy work.",
			Annotations: action("Set power profile", false),
		}, func(ctx context.Context, _ *mcp.CallToolRequest, in profileArgs) (*mcp.CallToolResult, ok, error) {
			_, err := run.Output(ctx, "powerprofilesctl", "set", in.Profile)
			return nil, done, err
		})
	}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "session_action",
		Description: "Lock, suspend, hibernate, log out of Hyprland, reboot or power off. Everything but lock ends or pauses the user's work.",
		Annotations: action("Session action", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sessionArgs) (*mcp.CallToolResult, ok, error) {
		if in.Action == "logout" {
			return nil, done, hypr.Dispatch("exit", "")
		}
		argv, known := sessionCommands[in.Action]
		if !known {
			return nil, done, fmt.Errorf("action must be lock, suspend, hibernate, logout, reboot or poweroff")
		}
		_, err := run.Output(ctx, argv[0], argv[1:]...)
		return nil, done, err
	})

	return nil
}
