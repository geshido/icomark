# Icomark

Icomark adds lightweight workflow marks to terminal tabs and sessions. It uses
emoji prefixes such as `🔨`, `👀`, `🧪`, `✅`, and `🚫`, plus an optional `💬`
marker for comments.

One shared core powers three backend-specific commands:

| Command | Target |
| --- | --- |
| `ztabs` | Zellij tabs |
| `atabs` | agterm sessions |
| `htabs` | Herdr tabs |

```text
🔨 auth-refactor
👀 💬 payments-migration
✅ docs-cleanup
🚫 flaky-e2e
```

Icomark is headless-only: commands are designed for terminal-manager
keybindings and scripts. Running any binary without a command prints help.

## Install

Install all three commands:

```sh
go install github.com/geshido/icomark/cmd/ztabs@latest
go install github.com/geshido/icomark/cmd/atabs@latest
go install github.com/geshido/icomark/cmd/htabs@latest
```

Or build from source:

```sh
git clone https://github.com/geshido/icomark
cd icomark
go install ./cmd/...
```

Go 1.22 or newer is required.

## Commands

The command set is identical for all backends:

```sh
htabs next                     # cycle status forward
htabs prev                     # cycle status backward
htabs set review               # assign a status by name
htabs comment "waiting on CI"  # store a comment and show 💬
htabs comment                  # clear the comment
htabs sync                     # restore marks after a manual rename
htabs status                   # print status, base label, and comment
```

Replace `htabs` with `ztabs` or `atabs` as appropriate.

Shared flags can appear before or after the command:

```text
--target ID       operate on an explicit tab/session
--socket PATH     use an explicit agterm or Herdr socket
--state-key KEY   override the state scope
--config PATH     use an explicit config file
--                treat remaining arguments as literal values
```

Use `--` when comment text or a custom status name starts with a dash:

```sh
htabs comment -- "-- waiting for deploy"
```

## Configuration

The canonical configuration is:

```text
$XDG_CONFIG_HOME/icomark/config.yml
# default: ~/.config/icomark/config.yml
```

Example:

```yaml
statuses:
  - name: none
    prefix: ""
  - name: wip
    prefix: "🔨 "
  - name: review
    prefix: "👀 "
  - name: test
    prefix: "🧪 "
  - name: done
    prefix: "✅ "
  - name: blocked
    prefix: "🚫 "
```

The first status must be `none` with an empty prefix. Prefixes are matched
longest-first. Status is persisted by name, so reordering entries is safe;
renaming a persisted status is not.

For migration, `ztabs` also reads the old `ztabs/config.yml`, and `atabs` reads
`atabs/config.yml`, when the canonical Icomark config does not exist.

## State and `sync`

Canonical state lives under:

```text
$XDG_STATE_HOME/icomark/<backend>/<scope>.json
# default: ~/.local/state/icomark/...
```

Entries are keyed by runtime tab/session ID and store the base label, status,
and comment. A unique base-label fallback helps Zellij reconnect state after
`resurrect`, where tab IDs can change.

When a manual rename removes `🔨`/`💬` from a label, the runtime ID remains the
same. Run `sync` to treat the visible text as the new base label and reapply the
stored marks:

```sh
htabs sync
```

Old `ztabs` and `atabs` state files are read only when canonical state is
absent. The next mutating command writes canonical Icomark state. Very unusual
legacy scope names containing spaces or Unicode may require a manual state-file
move because canonical filenames are sanitized.

Zellij limitation: after `resurrect`, run an Icomark command before manually
renaming a restored tab if its runtime ID changed. Otherwise neither the old ID
nor old base label may match.

## Zellij

### Direct keybindings

```kdl
keybinds {
    shared_except "locked" {
        bind "Alt ." { Run "ztabs" "next"; }
        bind "Alt ," { Run "ztabs" "prev"; }
        bind "Alt s" { Run "ztabs" "sync"; }
    }
}
```

A direct `Run` can briefly open a pane. The optional invisible plugin avoids
that for `next`/`prev`.

### Optional invisible plugin

Build it with Rust and the WASI target:

```sh
rustup target add wasm32-wasip1
make -C integrations/zellij-plugin release
```

The artifact is:

```text
integrations/zellij-plugin/target/wasm32-wasip1/release/ztabs-plugin.wasm
```

Load it and send pipe messages:

```kdl
load_plugins {
    "file:/absolute/path/to/ztabs-plugin.wasm"
}

keybinds {
    shared_except "locked" {
        bind "Alt ." {
            MessagePlugin "file:/absolute/path/to/ztabs-plugin.wasm" {
                name "ztabs"
                payload "next"
                launch_new false
            }
        }
        bind "Alt ," {
            MessagePlugin "file:/absolute/path/to/ztabs-plugin.wasm" {
                name "ztabs"
                payload "prev"
                launch_new false
            }
        }
    }
}
```

The plugin only relays `next` and `prev`; `sync` can use a direct `Run` binding.

## agterm

Add commands to `~/.config/agterm/keymap.conf`. An absolute binary path is
recommended because the GUI environment may have a restricted `PATH`:

```conf
command "atabs next" opt+. /absolute/path/to/atabs next --target "$AGT_SESSION_ID" --socket "$AGT_SOCKET" --state-key "$AGT_WINDOW_ID"
command "atabs prev" opt+, /absolute/path/to/atabs prev --target "$AGT_SESSION_ID" --socket "$AGT_SOCKET" --state-key "$AGT_WINDOW_ID"
command "atabs sync" opt+s /absolute/path/to/atabs sync --target "$AGT_SESSION_ID" --socket "$AGT_SOCKET" --state-key "$AGT_WINDOW_ID"
```

Reload with:

```sh
agtermctl keymap reload
```

## Herdr

Herdr status remains tab-scoped: `htabs` keeps the status marker in each tab
label. After every mutation it also publishes the marked tabs' status icons, in
tab order, to the owning workspace's `$status` metadata token. Tabs with the
`none` status are omitted.

Render the aggregate in the expanded Space sidebar:

```toml
[ui]
sidebar.spaces.rows = [["state_icon", "workspace", "$status"]]
```

Add detached shell commands to `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = "prefix+a"
type = "shell"
command = "htabs next --target \"$HERDR_ACTIVE_TAB_ID\" --socket \"$HERDR_SOCKET_PATH\""
description = "cycle tab status forward"

[[keys.command]]
key = "prefix+shift+a"
type = "shell"
command = "htabs prev --target \"$HERDR_ACTIVE_TAB_ID\" --socket \"$HERDR_SOCKET_PATH\""
description = "cycle tab status backward"

[[keys.command]]
key = "prefix+shift+s"
type = "shell"
command = "htabs sync --target \"$HERDR_ACTIVE_TAB_ID\" --socket \"$HERDR_SOCKET_PATH\""
description = "restore tab status marks"
```

Use an absolute path to `htabs` if it is not visible in Herdr's `PATH`, then
reload Herdr configuration with `herdr server reload-config`.

Herdr custom commands expose the focused tab as `HERDR_ACTIVE_TAB_ID`.
Commands launched from a managed pane can also omit `--target`; `htabs` falls
back to `HERDR_TAB_ID` and then `HERDR_ACTIVE_TAB_ID`.

Workspace metadata is display-only and is not restored after a Herdr server
restart. Run `htabs sync` from any tab to republish the aggregates for every
live workspace from the visible tab labels.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/ztabs ./cmd/atabs ./cmd/htabs
```

## License

MIT
