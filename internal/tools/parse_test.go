package tools

import (
	"bufio"
	"bytes"
	"encoding/json"
	"image"
	"image/png"
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
	text := strings.Join([]string{"spotify", "Playing", "Artist", "Song", "Album", "162000000", "2235000000", "0.5"}, sep) + "\n" +
		strings.Join([]string{"firefox", "Paused", "", "Video", "", "", "", ""}, sep)
	want := []player{
		{Name: "spotify", Status: "Playing", Artist: "Artist", Title: "Song", Album: "Album", Position: new(162.0), Length: new(2235.0), Volume: new(50)},
		{Name: "firefox", Status: "Paused", Title: "Video"},
	}
	if got := parsePlayers(text); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestPlayerctlArgs(t *testing.T) {
	cases := []struct {
		in   mediaArgs
		want []string
	}{
		{mediaArgs{Action: "next"}, []string{"next"}},
		{mediaArgs{Action: "seek", Value: 90}, []string{"position", "90"}},
		{mediaArgs{Action: "seek", Value: -10, Relative: true, Player: "mpv"}, []string{"--player", "mpv", "position", "10-"}},
		{mediaArgs{Action: "volume", Value: 40}, []string{"volume", "0.4"}},
		{mediaArgs{Action: "volume", Value: 5, Relative: true}, []string{"volume", "0.05+"}},
	}
	for _, c := range cases {
		if got, err := playerctlArgs(c.in); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v, %v", c.in, got, err)
		}
	}
	for _, bad := range []mediaArgs{{Action: "rewind"}, {Action: "seek", Value: -5}} {
		if _, err := playerctlArgs(bad); err == nil {
			t.Errorf("%+v should fail", bad)
		}
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

func TestShortcut(t *testing.T) {
	cases := map[string]string{
		"ctrl+t":           "CTRL, t",
		"Ctrl+Shift+Tab":   "CTRL SHIFT, Tab",
		"super + Return":   "SUPER, Return",
		"F5":               ", F5",
		"win+alt+Print":    "SUPER ALT, Print",
		"control+plus":     "CTRL, plus",
		"shift+mod1+space": "SHIFT ALT, space",
	}
	for in, want := range cases {
		if got, err := shortcut(in); err != nil || got != want {
			t.Errorf("%s: got %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"ctrl+", "hyper+t", ""} {
		if _, err := shortcut(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}

func TestToBind(t *testing.T) {
	got := toBind(rawBind{Modmask: 64 + 1, Key: "Q", Dispatcher: "killactive"})
	if got.Keys != "SUPER+SHIFT+Q" || got.Dispatcher != "killactive" {
		t.Errorf("got %+v", got)
	}
	if got := toBind(rawBind{Keycode: 49}); got.Keys != "code:49" {
		t.Errorf("keycode bind: %+v", got)
	}
}

func TestParseLayers(t *testing.T) {
	var raw rawLayers
	data := `{"eDP-1":{"levels":{"0":[{"x":0,"y":0,"w":1728,"h":1152,"namespace":"awww-daemon","pid":2145}],"1":[],"2":[{"x":0,"y":0,"w":1728,"h":36,"namespace":"waybar","pid":7}]}}}`
	if err := json.Unmarshal([]byte(data), &raw); err != nil {
		t.Fatal(err)
	}
	want := []layer{
		{Monitor: "eDP-1", Level: "background", Namespace: "awww-daemon", PID: 2145, Size: [2]int{1728, 1152}},
		{Monitor: "eDP-1", Level: "top", Namespace: "waybar", PID: 7, Size: [2]int{1728, 36}},
	}
	if got := parseLayers(raw); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}

func TestParseWallpapers(t *testing.T) {
	www := ": eDP-1: 1728x1152, scale: 1.6666666, currently displaying: image: /w/a.jpg\n" +
		": DP-1: 2304x1296, scale: 1, currently displaying: color: 000000"
	want := []wallpaper{{"eDP-1", "/w/a.jpg"}, {"DP-1", "color: 000000"}}
	if got := parseWallpapers("awww", www); !reflect.DeepEqual(got, want) {
		t.Errorf("awww: got %+v", got)
	}
	paper := "eDP-1 = /w/a.jpg\nDP-1 = /w/b.png"
	want = []wallpaper{{"eDP-1", "/w/a.jpg"}, {"DP-1", "/w/b.png"}}
	if got := parseWallpapers("hyprpaper", paper); !reflect.DeepEqual(got, want) {
		t.Errorf("hyprpaper: got %+v", got)
	}
}

func TestMonitorRule(t *testing.T) {
	m := hypr.Monitor{Name: "DP-1", Width: 3840, Height: 2160, RefreshRate: 59.997, X: 1728, Y: 0}
	if got := monitorRule(m, 1.5); got != "DP-1,3840x2160@59.997,1728x0,1.5" {
		t.Errorf("got %s", got)
	}
	m.Transform = 1
	if got := monitorRule(m, 2); got != "DP-1,3840x2160@59.997,1728x0,2,transform,1" {
		t.Errorf("transform: got %s", got)
	}
}

func TestClipboardImage(t *testing.T) {
	cases := []struct {
		types []string
		want  string
	}{
		{[]string{"image/png"}, "image/png"},
		{[]string{"text/html", "image/jpeg", "image/png"}, "image/png"},
		{[]string{"image/bmp"}, "image/bmp"},
		{[]string{"text/plain;charset=utf-8", "image/png"}, ""},
		{[]string{"UTF8_STRING", "TEXT"}, ""},
		{[]string{"text/uri-list"}, ""},
	}
	for _, c := range cases {
		if got := clipboardImage(c.types); got != c.want {
			t.Errorf("%v: got %q", c.types, got)
		}
	}
}

func TestShrinkImage(t *testing.T) {
	encode := func(w, h int) []byte {
		var buf bytes.Buffer
		if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	small := encode(100, 50)
	if got, mime, err := shrinkImage(small, "image/png"); err != nil || mime != "image/png" || !bytes.Equal(got, small) {
		t.Errorf("small image should pass through: %s %v", mime, err)
	}
	got, mime, err := shrinkImage(encode(4000, 1000), "image/png")
	if err != nil || mime != "image/jpeg" {
		t.Fatalf("large image: %s %v", mime, err)
	}
	img, _, err := image.Decode(bytes.NewReader(got))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 1000 || b.Dy() != 250 {
		t.Errorf("large image resized to %v", b)
	}
}
