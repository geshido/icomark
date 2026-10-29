# Icomark

<p align="center">
  <img src="assets/icomark.png" alt="Icomark logo" width="320">
</p>

Icomark adds lightweight workflow marks to terminal tabs and sessions. It uses
emoji prefixes such as `🔨`, `👀`, `🧪`, `✅`, and `🚫`, plus an optional `💬`
marker for comments.

One shared core powers four backend-specific commands:

| Command | Target |
| --- | --- |
| `ztabs` | Zellij tabs |
| `atabs` | agterm sessions |
| `htabs` | Herdr tabs |
| `rtabs` | Rex windows (tabs) |

```text
🔨 auth-refactor
👀 💬 payments-migration
✅ docs-cleanup
🚫 flaky-e2e
```

Icomark is headless-only: commands are designed for terminal-manager
keybindings and scripts. Running any binary without a command prints help.

## Install

Install only the command for your terminal manager. Each command works independently:

```sh
go install github.com/geshido/icomark/cmd/ztabs@latest
go install github.com/geshido/icomark/cmd/atabs@latest
go install github.com/geshido/icomark/cmd/htabs@latest
go install github.com/geshido/icomark/cmd/rtabs@latest
```

Or build your chosen command from source (Rex example):

```sh
git clone https://github.com/geshido/icomark
cd icomark
go install ./cmd/rtabs
```

Replace `rtabs` with your chosen command. To install all commands, use
`go install ./cmd/...`.

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

Replace `htabs` with `ztabs`, `atabs`, or `rtabs` as appropriate.

Shared flags can appear before or after the command:

```text
--target ID       operate on an explicit tab/session
--socket PATH     use an explicit agterm/Herdr socket or Rex server address
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

## Rex

`rtabs` marks Rex windows (tabs), not individual blocks (panes). It uses window
labels because Rex does not currently expose custom window metadata or badges.
Native OSC 7501 program status is separate and is not changed by Icomark.

After each mutation, `rtabs` prefixes the owning session's name with its marked
tabs' status icons: `🔨👀✅ Project`. Each icon appears once, in first-tab order,
with no spaces between icons and one space before the name. Tabs with `none`
and comment-only markers are omitted. Leading configured status icons are
replaced on updates and removed when no tabs are marked, like marks on tabs.
The old ` [icomark: ...]` suffix is migrated automatically. Custom status prefixes
must not contain `[icomark:`. Use the session ID in scripts because the full
displayed name changes.

`rtabs sync` also rebuilds the current session's summary from visible tab labels.
Run it after closing or reordering tabs, or manually renaming a session. Other
sessions are updated when you run an Icomark mutation in them.

Inside a Rex terminal, `rtabs` uses the window containing `REX_BLOCK`, even if
another tab is active. Rex resolves `--target` as a window ID, label, position,
or ID suffix within `REX_SESSION`; use full IDs in scripts. Rex reports missing
or ambiguous targets without renaming anything.

```sh
rtabs next
rtabs set review --target 'window:<uuid>'
rtabs status --socket 'unix:///absolute/path/to/rex/server.sock'
```

`--socket` overrides `REX_SERVER` and accepts the same server addresses or host
labels as `rex -S`. State is scoped by the resolved Rex session ID, with window
IDs as entry keys. `--state-key` only overrides the state filename, not the Rex
session. To select another session, set `REX_SESSION` explicitly.

`rtabs` uses `$REX_BIN_DIR/rex` when set, otherwise `rex` from `PATH`. Ensure one
is available to commands launched by Rex, whose environment can differ from
an interactive shell. Rex limits window labels, including Icomark's marks, to
128 characters; a rejected window rename does not save new state. If the session
summary exceeds this limit, `rtabs` reports an error and leaves the session name
unchanged; the tab's status has already been saved.

Add this to `~/.config/rex/init.lua`, replacing the binary path:

```lua
local function icomark(command)
  return function(ctx)
    if not ctx.session_id or not ctx.block_id then
      error("Icomark requires a focused Rex terminal", 0)
    end
    local result, err = rex.exec({ "/absolute/path/to/rtabs", command }, {
      env = { REX_SESSION = ctx.session_id, REX_BLOCK = ctx.block_id },
      timeout = 5,
    })
    if not result then error(err, 0) end
    if not result.ok then error(result.stderr, 0) end
  end
end

rex.action{ name = "icomark_next", title = "Next Workflow Status", run = icomark("next") }
rex.action{ name = "icomark_prev", title = "Previous Workflow Status", run = icomark("prev") }
rex.action{ name = "icomark_sync", title = "Restore Workflow Marks", run = icomark("sync") }
rex.bind("alt+.", "icomark_next")
rex.bind("alt+,", "icomark_prev")
rex.bind("alt+s", "icomark_sync")
```

Run `rex config check` and `rex config reload`. Actions run without opening a
pane and also appear in the command palette. After a manual tab rename,
`rtabs sync` reapplies its saved status and comment marker.

## Development

```sh
go test ./...
go vet ./...
go build ./cmd/...
```

## License

MIT
