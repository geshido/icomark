# ztabs — Zellij Tab Status Manager

A small tool that turns Zellij tabs into a lightweight Kanban board. Each tab
gets a **status** (`wip`, `review`, `test`, `done`, `blocked`, …) rendered as
an emoji prefix in the tab bar, plus an optional **comment** stored in a
sidecar file.

```
[wip]     🔨 auth-refactor            — waiting on review from @alice
[review]  👀 payments-migration       — needs second pair of eyes
[done]    ✅ docs-cleanup
[blocked] 🚫 flaky-e2e                — blocked on CI fix
```

## Why

Zellij tabs are ephemeral labels. When you juggle many tabs across branches or
tickets, you lose track of what's in progress, what's waiting on someone, what
you've already finished but haven't closed yet. Renaming tabs by hand to
encode status is tedious, and any note longer than a word doesn't fit in the
tab bar anyway.

`ztabs` gives you:

- **Status** — a one-keystroke cycle through configurable states, shown as an
  emoji prefix on the tab name.
- **Comments** — a sidecar "why is this tab blocked / what's the ticket /
  what was I doing" note, with a 💬 indicator on tabs that have one.
- **Persistence** — status and comments survive `zellij-session-manager` /
  `resurrect`, keyed per session.
- **Headless cycle** — `ztabs next` / `ztabs prev` to bump the current tab's
  status from a keybinding without opening the TUI.

## Requirements

- [Zellij](https://zellij.dev/) (tested on 0.42+)
- Go 1.22+ (to build the CLI)
- Rust with the `wasm32-wasip1` target (only if you want the background
  plugin — needed for invisible status cycling and rename tracking)

## Install

### CLI

```sh
go install github.com/geshido/ztabs@latest
```

Or from source:

```sh
git clone https://github.com/geshido/ztabs
cd ztabs
go build -o ztabs .
# put the binary somewhere on $PATH, e.g.
mv ztabs ~/.local/bin/
```

### Background plugin (optional but recommended)

The plugin does two things:

1. Relays `MessagePlugin` pipes from your keybindings into `ztabs next|prev`
   so status cycling happens without opening a visible pane.
2. Watches Zellij's `TabUpdate` events and migrates the sidecar state when
   a tab is renamed outside of `ztabs` (e.g. via Zellij's own rename mode).

Build it:

```sh
rustup target add wasm32-wasip1
cd plugin
make release
# produces plugin/target/wasm32-wasip1/release/ztabs-plugin.wasm
```

Point Zellij at the artifact (absolute path required — see keybinding
examples below).

## Usage

### Interactive TUI

```sh
ztabs
```

Keys:

| Key            | Action                                  |
| -------------- | --------------------------------------- |
| `↑` / `k`      | Move cursor up                          |
| `↓` / `j`      | Move cursor down                        |
| `←` / `h`      | Cycle status backward                   |
| `→` / `l`      | Cycle status forward (or `Tab` / space) |
| `⏎`            | Jump to the selected tab and quit       |
| `r`            | Rename the tab                          |
| `c`            | Edit the tab's comment                  |
| `J` / `K`      | Move the tab right / left in the bar    |
| `q` / `Esc`    | Quit                                    |

### Headless

```sh
ztabs next                    # cycle current tab status forward
ztabs prev                    # cycle current tab status backward
ztabs migrate-state OLD NEW   # rename the sidecar key after an external rename
ztabs --exit-mode normal      # TUI, then drop Zellij back into Normal mode
```

## Zellij integration

Add to your `~/.config/zellij/config.kdl`. Adjust paths to match where you
installed the plugin `.wasm`.

### Open the TUI

```kdl
keybinds {
    shared_except "locked" {
        bind "Alt s" {
            Run "ztabs" "--exit-mode" "normal" {
                floating true
                close_on_exit true
            }
        }
    }
}
```

### Cycle status without a pane flash

Requires the background plugin to be loaded at session start:

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

If you don't want the background plugin, you can use `Run "ztabs" "next"`
instead — but it flashes a short-lived pane.

## Configuration

`ztabs` reads its config from the first file that exists:

1. `$XDG_CONFIG_HOME/ztabs/config.yml`
2. `~/.config/ztabs/config.yml`

If neither exists the built-in defaults are used. Example:

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

Notes:

- The first status should be a "no status" entry with an empty prefix.
- `prefix` is matched literally against the start of the tab name, so include
  the trailing space if you want one between the emoji and the tab name.
- Status is persisted by its `name`, so reordering statuses in the config is
  safe; renaming them is not (the old name becomes orphaned in the sidecar).

## State file

Per-session state (comments + current status per tab) lives at:

```
$XDG_STATE_HOME/ztabs/<session-name>.json
# default: ~/.local/state/ztabs/<session-name>.json
```

The file is keyed by the tab's *base name* (the tab name stripped of status
and 💬 prefixes), so renaming a tab through `ztabs` (or through Zellij, with
the background plugin running) migrates the entry automatically.

Format:

```json
{
  "auth-refactor": {
    "comment": "waiting on review from @alice",
    "status": "wip"
  }
}
```

## How it works

- Status is encoded as an emoji **prefix** on the Zellij tab name, which
  `ztabs` drives via `zellij action rename-tab-by-id`. The tab name is the
  source of truth for display; the sidecar is the source of truth for
  comments and for restoring status after `resurrect`.
- Comments live only in the sidecar; the 💬 indicator in the tab name is
  cosmetic and shows that "this tab has a comment — open `ztabs` to read it."
- The Rust plugin is deliberately invisible (`hide_self()`) — its only job is
  to shell out to the `ztabs` CLI on pipe messages and on detected renames.

## License

MIT
