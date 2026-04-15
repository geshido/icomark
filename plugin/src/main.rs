use std::collections::{BTreeMap, HashMap};
use zellij_tile::prelude::*;

/// Background ztabs plugin. Two jobs:
///
/// 1. Relay `MessagePlugin` pipes from keybindings into `ztabs next|prev`
///    invocations via `run_command` (keeps the action invisible — no pane
///    flash from the `Run` action).
///
/// 2. Watch `TabUpdate` events and detect tab renames by diffing against the
///    previous snapshot (keyed by `tab_id`, which is stable within a session).
///    On rename, shell out to `ztabs migrate-state OLD NEW` so the sidecar
///    entry (comment + status) follows the renamed tab.
#[derive(Default)]
struct State {
    last_names: HashMap<usize, String>,
}

register_plugin!(State);

impl ZellijPlugin for State {
    fn load(&mut self, _configuration: BTreeMap<String, String>) {
        // RunCommands: to invoke the `ztabs` binary.
        // ReadApplicationState: required to receive TabUpdate events.
        request_permission(&[
            PermissionType::RunCommands,
            PermissionType::ReadApplicationState,
        ]);
        subscribe(&[EventType::TabUpdate]);
        // We have no UI — keep the plugin resident but invisible so that
        // MessagePlugin pipes don't spawn a blank pane.
        hide_self();
    }

    fn update(&mut self, event: Event) -> bool {
        if let Event::TabUpdate(tabs) = event {
            let mut next = HashMap::with_capacity(tabs.len());
            for t in &tabs {
                if let Some(old_name) = self.last_names.get(&t.tab_id) {
                    if old_name != &t.name {
                        // Rename detected: same tab_id, different name.
                        // ztabs strips status/💬 prefixes internally.
                        run_command(
                            &["ztabs", "migrate-state", old_name, &t.name],
                            BTreeMap::new(),
                        );
                    }
                }
                next.insert(t.tab_id, t.name.clone());
            }
            self.last_names = next;
        }
        false
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
