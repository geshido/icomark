use std::collections::BTreeMap;
use zellij_tile::prelude::*;

/// Invisible relay for `MessagePlugin` pipes from Zellij keybindings.
///
/// A payload of `next` or `prev` invokes the corresponding `ztabs` command
/// without opening a visible pane.
#[derive(Default)]
struct State;

register_plugin!(State);

impl ZellijPlugin for State {
    fn load(&mut self, _configuration: BTreeMap<String, String>) {
        request_permission(&[PermissionType::RunCommands]);
        hide_self();
    }

    fn pipe(&mut self, pipe_message: PipeMessage) -> bool {
        if pipe_message.name != "ztabs" {
            return false;
        }
        let action = match pipe_message.payload.as_deref() {
            Some("next") => "next",
            Some("prev") => "prev",
            _ => return false,
        };
        run_command(&["ztabs", action], BTreeMap::new());
        false
    }
}
