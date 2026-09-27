# hyprland-mcp

An [MCP](https://modelcontextprotocol.io) server that lets an agent drive a
Hyprland desktop: windows, workspaces, monitors, audio, media, network,
Bluetooth, brightness, notifications, screenshots, screen recording, the
clipboard, wallpaper and night light. It is a single static Go binary that
speaks MCP over stdio.

Hyprland is reached through its IPC socket directly; everything else goes
through the usual command-line tools, and a group of tools is skipped when
its tool isn't installed.

## Install

```sh
go install github.com/zenodea/hyprland-mcp@latest
```

or from a checkout:

```sh
make install   # builds with the git version into ~/.local/bin
make test
```

## Use with Claude Code

```sh
claude mcp add hyprland -- hyprland-mcp
```

or pass a config file to a single run:

```json
{ "mcpServers": { "hyprland": { "command": "hyprland-mcp" } } }
```

```sh
claude --mcp-config hyprland.json
```

Every tool carries MCP annotations (`readOnlyHint`, `destructiveHint`), and
Claude Code permissions can be set per tool. A reasonable starting point is
to allow the read-only ones and keep prompting for the rest:

```json
{
  "permissions": {
    "allow": [
      "mcp__hyprland__list_windows",
      "mcp__hyprland__active_window",
      "mcp__hyprland__list_workspaces",
      "mcp__hyprland__list_monitors",
      "mcp__hyprland__audio_status",
      "mcp__hyprland__media_status",
      "mcp__hyprland__network_status",
      "mcp__hyprland__bluetooth_status",
      "mcp__hyprland__get_brightness",
      "mcp__hyprland__list_binds",
      "mcp__hyprland__cursor_position"
    ]
  }
}
```

`screenshot` and `get_clipboard` are read-only too, but they can expose
private content, so think before allowing them without a prompt. The same
goes for `start_recording`.

## Tools

| Group | Tools | Needs |
|---|---|---|
| `windows` | `list_windows`, `active_window`, `focus_window`, `close_window`, `move_window`, `toggle_floating`, `toggle_fullscreen`, `set_window_geometry`, `pin_window`, `swap_window`, `move_focus`, `wait_for_window` | Hyprland |
| `workspaces` | `list_workspaces`, `list_monitors`, `switch_workspace`, `move_workspace_to_monitor`, `rename_workspace`, `set_monitor` | Hyprland |
| `hyprland` | `launch_app`, `get_option`, `set_option`, `hypr_dispatch`, `list_binds`, `list_layers`, `list_devices`, `cursor_position`, `reload_config` | Hyprland |
| `apps` | `list_apps` | — |
| `layouts` | `save_layout`, `list_layouts`, `restore_layout`, `delete_layout` | Hyprland |
| `audio` | `audio_status`, `set_volume`, `set_mute`, `set_default_audio` | `wpctl` |
| `media` | `media_status`, `media_control` | `playerctl` |
| `network` | `network_status`, `list_wifi`, `connect_wifi`, `set_connection`, `set_wifi` | `nmcli` |
| `bluetooth` | `bluetooth_status`, `bluetooth_connect`, `set_bluetooth` | `bluetoothctl` |
| `desktop` | `notify`, `screenshot`, `get_brightness`, `set_brightness`, `get_clipboard`, `set_clipboard` | `notify-send`, `grim` (+ `slurp`), `brightnessctl`, `wl-clipboard` |
| `recording` | `start_recording`, `stop_recording` | `wf-recorder` (+ `slurp`) |
| `system` | `system_info`, `battery_status`, `top_processes`, `kill_process`, `power_profile`, `set_power_profile`, `session_action` | `powerprofilesctl` for profiles |
| `toggles` | `keyboard_layouts`, `switch_keyboard_layout`, `open`, `get_do_not_disturb`, `set_do_not_disturb` | `xdg-open`; dunst, SwayNC or mako for do-not-disturb |
| `extras` | `get_keep_awake`, `set_keep_awake`, `get_night_light`, `set_night_light`, `get_wallpaper`, `set_wallpaper`, `pick_color` | `systemd-inhibit`, `hyprsunset`, `awww`/`swww` or `hyprpaper`, `hyprpicker` |
| `resources` | the `hyprland://windows`, `hyprland://workspaces` and `hyprland://monitors` resources | Hyprland |
| `custom` | anything in the config file | — |
| `input` (opt-in) | `type_text`, `send_shortcut`, `move_cursor` | `wtype` for `type_text` |

Leave groups out with `-disable`, e.g. `hyprland-mcp -disable network,bluetooth`.

### Typing and key presses

The `input` group acts as the user's keyboard, so it is off unless asked for:

```sh
claude mcp add hyprland -- hyprland-mcp -enable input
```

`type_text` types into whatever window has focus, and `send_shortcut` presses
a combination such as `ctrl+shift+t`, in the focused window or one picked by
selector. Neither can see where the input lands, so an agent should check
`active_window` first. Keep both behind a permission prompt. Clicking isn't
supported, since Hyprland has no dispatcher for it.

### Resources

`hyprland://windows`, `hyprland://workspaces` and `hyprland://monitors` hold
the same JSON as the matching list tools. Clients that subscribe get an update
notification when Hyprland reports a change, such as a window opening or a
workspace switch. `wait_for_window` and `launch_app` listen for the same
events instead of polling.

### Notes

- `set_monitor` and `set_option` last until Hyprland reloads its config.
  `set_monitor` refuses to turn off or blank the last screen that is on.
- `set_keep_awake` holds a systemd idle inhibitor, which hypridle honours,
  until it is turned off, its minutes run out, or the server exits.
- `start_recording` writes to `~/Videos` by default. The recording stops and
  is finalised if the server exits.
- `get_clipboard` returns images as image content. Large ones are scaled down
  to at most 1600 pixels on a side.
- `connect_wifi` gives the password to `nmcli` on stdin, so it never appears
  in the process list.

`launch_app` takes `wait_class` to return the window it opened, so an agent
can start an app and then move or resize it in the next step.

Layouts are stored in `~/.local/state/hyprland-mcp/layouts.json`. Restoring
one moves open windows back to their saved workspace (and floating geometry)
and launches the ones that aren't open from their saved command line. Tiled
windows get their workspace back, but not their exact tile.

Windows are picked with Hyprland's own selectors: `address:0x…`,
`class:^(firefox)$`, `title:…` or `pid:…`. Most window tools act on the
focused window when none is given.

## Custom commands

Any command can become a tool by listing it in
`~/.config/hyprland-mcp/config.json` (or the file given with `-config`):

```json
{
  "commands": [
    {
      "name": "switch_theme",
      "description": "Switch the desktop colour theme.",
      "command": ["switch-theme", "{theme}"],
      "args": { "theme": { "description": "theme name", "enum": ["rose-pine", "gruvbox"] } },
      "timeout": 60
    }
  ]
}
```

| Field | Meaning |
|---|---|
| `name` | tool name (`[a-zA-Z0-9_-]`, up to 64 characters) |
| `description` | what the agent sees |
| `command` | argv; `{arg}` placeholders are filled in without a shell |
| `args` | each argument's `description`, optional `enum`, and `optional` |
| `read_only` | mark the tool read-only (default false) |
| `destructive` | mark it destructive (default false) |
| `timeout` | seconds before the command is killed (default 30) |

Arguments are validated against their declared set before anything runs,
and an optional argument left empty drops its argv element. See
[`examples/config.json`](examples/config.json).

## License

MIT
