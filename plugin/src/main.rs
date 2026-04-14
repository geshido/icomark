use std::collections::BTreeMap;
use zellij_tile::prelude::*;

/// Background ztabs plugin: receives `MessagePlugin` pipes from keybindings
/// and shells out to the `ztabs` binary via zellij's `run_command` host function.
///
/// The plugin itself holds no state and never renders — it only exists so that
/// `run_command` can be invoked without opening a visible pane (which is the
/// only way to avoid the floating/tiled pane flash from the `Run` action).
#[derive(Default)]
struct State;

register_plugin!(State);

impl ZellijPlugin for State {
    fn load(&mut self, _configuration: BTreeMap<String, String>) {
        // Required so we can invoke `run_command` below. Zellij will prompt
        // the user to grant this permission on first load and remember it.
        request_permission(&[PermissionType::RunCommands]);
        // Suppress any pane zellij may have attached to this plugin instance.
        // We have no UI — if a `MessagePlugin` keybinding spawns a fresh
        // instance (when `load_plugins` hasn't pre-loaded us) it would otherwise
        // render a blank pane. Hiding ourselves keeps the plugin resident in
        // memory but invisible, so future pipe messages don't create panes.
        hide_self();
    }

    fn pipe(&mut self, pipe_message: PipeMessage) -> bool {
        // Ignore pipes that aren't addressed to us.
        if pipe_message.name != "ztabs" {
            return false;
        }
        let action = match pipe_message.payload.as_deref() {
            Some("next") => "next",
            Some("prev") => "prev",
            _ => return false,
        };
        run_command(&["ztabs", action], BTreeMap::new());
        // Never render — we have no UI.
        false
    }
}
