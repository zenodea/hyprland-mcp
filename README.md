# hyprland-mcp

An [MCP](https://modelcontextprotocol.io) server that lets an agent drive a
Hyprland desktop: windows, workspaces, audio, media, network, Bluetooth,
brightness, notifications, screenshots and the clipboard. It is a single
static Go binary that speaks MCP over stdio.

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
      "mcp__hyprland__get_brightness"
    ]
  }
}
```

`screenshot` and `get_clipboard` are read-only too, but they can expose
private content, so think before allowing them without a prompt.

## Tools

| Group | Tools | Needs |
|---|---|---|
| `windows` | `list_windows`, `active_window`, `focus_window`, `close_window`, `move_window`, `toggle_floating`, `toggle_fullscreen`, `set_window_geometry`, `pin_window`, `swap_window`, `move_focus`, `wait_for_window` | Hyprland |
| `workspaces` | `list_workspaces`, `list_monitors`, `switch_workspace` | Hyprland |
| `hyprland` | `launch_app`, `get_option`, `set_option`, `hypr_dispatch` | Hyprland |
| `apps` | `list_apps` | — |
| `layouts` | `save_layout`, `list_layouts`, `restore_layout`, `delete_layout` | Hyprland |
| `audio` | `audio_status`, `set_volume`, `set_mute`, `set_default_audio` | `wpctl` |
| `media` | `media_status`, `media_control` | `playerctl` |
| `network` | `network_status`, `list_wifi`, `connect_wifi`, `set_connection`, `set_wifi` | `nmcli` |
| `bluetooth` | `bluetooth_status`, `bluetooth_connect`, `set_bluetooth` | `bluetoothctl` |
| `desktop` | `notify`, `screenshot`, `get_brightness`, `set_brightness`, `get_clipboard`, `set_clipboard` | `notify-send`, `grim` (+ `slurp`), `brightnessctl`, `wl-clipboard` |
| `system` | `system_info`, `battery_status`, `top_processes`, `kill_process`, `power_profile`, `set_power_profile`, `session_action` | `powerprofilesctl` for profiles |
| `toggles` | `keyboard_layouts`, `switch_keyboard_layout`, `open`, `get_do_not_disturb`, `set_do_not_disturb` | `xdg-open`; dunst, SwayNC or mako for do-not-disturb |
| `custom` | anything in the config file | — |

Leave groups out with `-disable`, e.g. `hyprland-mcp -disable network,bluetooth`.

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
