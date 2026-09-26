package tools

import (
	"bufio"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/zenodea/hyprland-mcp/internal/hypr"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseWpctlStatus(t *testing.T) {
	got := parseWpctlStatus(fixture(t, "wpctl-status.txt"))
	want := audioStatus{
		Outputs: []audioNode{{ID: 46, Name: "Built-in Audio Analog Stereo", Default: true, Volume: 1.05}},
		Inputs:  []audioNode{{ID: 56, Name: "Built-in Audio Analog Stereo", Default: true, Volume: 1}},
		Streams: []audioNode{{ID: 67, Name: "speech-dispatcher-dummy"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseWpctlStatusBusy(t *testing.T) {
	got := parseWpctlStatus(fixture(t, "wpctl-status-busy.txt"))
	want := audioStatus{
		Outputs: []audioNode{
			{ID: 46, Name: "Built-in Audio Analog Stereo", Volume: 0.4, Muted: true},
			{ID: 92, Name: "Nothing Headphone (1)", Default: true, Volume: 0.75},
		},
		Inputs:  []audioNode{{ID: 56, Name: "Built-in Audio Analog Stereo", Default: true, Volume: 1}},
		Streams: []audioNode{{ID: 101, Name: "spotify"}, {ID: 110, Name: "Firefox"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseVolume(t *testing.T) {
	for _, tc := range []struct {
		in    string
		vol   float64
		muted bool
		ok    bool
	}{
		{"Volume: 0.45", 0.45, false, true},
		{"Volume: 1.20 [MUTED]", 1.2, true, true},
		{"garbage", 0, false, false},
	} {
		vol, muted, ok := parseVolume(tc.in)
		if vol != tc.vol || muted != tc.muted || ok != tc.ok {
			t.Errorf("parseVolume(%q) = %v, %v, %v", tc.in, vol, muted, ok)
		}
	}
}

func TestLevels(t *testing.T) {
	for _, tc := range []struct {
		percent  int
		relative bool
		want     string
	}{
		{40, false, "40%"},
		{5, true, "5%+"},
		{-5, true, "5%-"},
	} {
		if got, _ := volumeLevel(tc.percent, tc.relative); got != tc.want {
			t.Errorf("volumeLevel(%d, %v) = %q, want %q", tc.percent, tc.relative, got, tc.want)
		}
		if got := brightnessLevel(tc.percent, tc.relative); got != tc.want {
			t.Errorf("brightnessLevel(%d, %v) = %q, want %q", tc.percent, tc.relative, got, tc.want)
		}
	}
	if _, err := volumeLevel(-5, false); err == nil {
		t.Error("negative absolute volume should fail")
	}
	if got := brightnessLevel(0, false); got != "1%" {
		t.Errorf("brightness floor: got %q", got)
	}
}

func TestTerse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"wlan0:wifi:connected:Home", []string{"wlan0", "wifi", "connected", "Home"}},
		{`*:Cafe\: Free:70:WPA2`, []string{"*", "Cafe: Free", "70", "WPA2"}},
		{`back\\slash::`, []string{`back\slash`, "", ""}},
	} {
		if got := terse(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("terse(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseWifi(t *testing.T) {
	text := " :Home:100:WPA2\n::90:WPA2\n*:Home:69:WPA2\n :Cafe\\: Free:37:\n"
	got := parseWifi(parseTerseRows(text, 4))
	want := []wifiNetwork{
		{SSID: "Home", Signal: 100, Security: "WPA2", InUse: true},
		{SSID: "Cafe: Free", Signal: 37},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestParseBtDevices(t *testing.T) {
	text := "Device 3C:00:00:00:00:00 Nothing Headphone (1)\nDevice AA:BB:CC:DD:EE:FF Keyboard K2\nnoise line\n"
	want := map[string]string{"3C:00:00:00:00:00": "Nothing Headphone (1)", "AA:BB:CC:DD:EE:FF": "Keyboard K2"}
	if got := parseBtDevices(text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestParsePlayers(t *testing.T) {
	text := strings.Join([]string{"spotify", "Playing", "Artist", "Song", "Album"}, sep) + "\n" +
		strings.Join([]string{"firefox", "Paused", "", "Video", ""}, sep)
	want := []player{
		{Name: "spotify", Status: "Playing", Artist: "Artist", Title: "Song", Album: "Album"},
		{Name: "firefox", Status: "Paused", Title: "Video"},
	}
	if got := parsePlayers(text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestParseBrightness(t *testing.T) {
	got, err := parseBrightness("intel_backlight,backlight,115200,60%,192000\n")
	if err != nil || got != (brightness{Device: "intel_backlight", Percent: 60}) {
		t.Errorf("got %+v, %v", got, err)
	}
	if _, err := parseBrightness(""); err == nil {
		t.Error("empty output should fail")
	}
}

func TestParseProfiles(t *testing.T) {
	got := parseProfiles(fixture(t, "powerprofilesctl-list.txt"))
	want := profile{Active: "performance", Available: []string{"performance", "balanced", "power-saver"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestParseStat(t *testing.T) {
	stat := "1234 (Web Content (x)) S 1 2 3 4 5 6 7 8 9 10 150 50 0 0 20 0"
	cmd, ticks, ok := parseStat(stat)
	if !ok || cmd != "Web Content (x)" || ticks != 200 {
		t.Errorf("got %q, %v, %v", cmd, ticks, ok)
	}
	if _, _, ok := parseStat("broken"); ok {
		t.Error("broken stat should fail")
	}
}

func TestParseMeminfo(t *testing.T) {
	got := parseMeminfo("MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\nHugePages_Total:       0\n")
	if got["MemTotal"] != 32000000 || got["MemAvailable"] != 20000000 || got["HugePages_Total"] != 0 {
		t.Errorf("got %v", got)
	}
}

func TestDesktopEntry(t *testing.T) {
	entry := `[Desktop Entry]
Type=Application
Name=Firefox
Comment=Browse the web
Exec=/usr/lib/firefox/firefox %u --flag=100%%
Categories=Network;WebBrowser;

[Desktop Action new-window]
Name=New Window
Exec=/usr/lib/firefox/firefox --new-window %u
`
	got, ok := parseDesktopEntry("firefox", bufio.NewScanner(strings.NewReader(entry)))
	want := app{ID: "firefox", Name: "Firefox", Command: "/usr/lib/firefox/firefox --flag=100%", Description: "Browse the web", Categories: []string{"Network", "WebBrowser"}}
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v", got, ok)
	}
	for _, swap := range [][2]string{
		{"Comment=Browse the web", "NoDisplay=true"},
		{"Comment=Browse the web", "Hidden=true"},
		{"Type=Application", "Type=Link"},
	} {
		e := strings.Replace(entry, swap[0], swap[1], 1)
		if _, ok := parseDesktopEntry("x", bufio.NewScanner(strings.NewReader(e))); ok {
			t.Errorf("entry with %s should be skipped", swap[1])
		}
	}
}

func TestCustomArgv(t *testing.T) {
	c := customCommand{Command: []string{"greet", "{who}", "--from={place}", "{extra}"}}
	got := c.argv(map[string]string{"who": "a b; rm -rf ~", "place": "home"})
	want := []string{"greet", "a b; rm -rf ~", "--from=home"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestCustomValidate(t *testing.T) {
	good := customCommand{Name: "ok_1", Command: []string{"x", "{a}"}, Args: map[string]customArg{"a": {}}}
	if err := good.validate(); err != nil {
		t.Error(err)
	}
	for _, bad := range []customCommand{
		{Name: "has space", Command: []string{"x"}},
		{Name: "empty"},
		{Name: "undeclared", Command: []string{"x", "{nope}"}},
	} {
		if bad.validate() == nil {
			t.Errorf("%q should fail validation", bad.Name)
		}
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote([]string{"ghostty", "--gtk-single-instance=true", "it's", "a b"})
	want := `ghostty --gtk-single-instance=true 'it'\''s' 'a b'`
	if got != want {
		t.Errorf("got %s", got)
	}
}

func TestExecRules(t *testing.T) {
	tiled := savedWindow{Workspace: "3"}
	if got := execRules(tiled); got != "[workspace 3 silent] " {
		t.Errorf("tiled: %q", got)
	}
	floating := savedWindow{Workspace: "2", Floating: true, Position: [2]int{10, 20}, Size: [2]int{800, 600}}
	if got := execRules(floating); got != "[workspace 2 silent; float; move 10 20; size 800 600] " {
		t.Errorf("floating: %q", got)
	}
}

func TestPickWindow(t *testing.T) {
	clients := []hypr.Client{
		{Address: "a", Class: "kitty", Title: "btop", Mapped: true},
		{Address: "b", Class: "kitty", Title: "nvim", Mapped: true},
		{Address: "c", Class: "firefox", Title: "x", Mapped: true},
	}
	used := map[string]bool{}
	if i := pickWindow(clients, used, savedWindow{Class: "kitty", Title: "nvim"}); i != 1 {
		t.Errorf("same title should win, got %d", i)
	}
	used["b"] = true
	if i := pickWindow(clients, used, savedWindow{Class: "kitty", Title: "nvim"}); i != 0 {
		t.Errorf("fall back to same class, got %d", i)
	}
	if i := pickWindow(clients, used, savedWindow{Class: "slack"}); i != -1 {
		t.Errorf("no match should be -1, got %d", i)
	}
}

func TestWindowMatcher(t *testing.T) {
	m, err := newMatcher("FireFox", "")
	if err != nil {
		t.Fatal(err)
	}
	if !m.match(hypr.Client{Class: "firefox", Mapped: true}) {
		t.Error("class match should be case-insensitive")
	}
	if m.match(hypr.Client{Class: "firefox", Mapped: true, Hidden: true}) {
		t.Error("hidden windows should not match")
	}
	if _, err := newMatcher("", ""); err == nil {
		t.Error("empty matcher should fail")
	}
}
