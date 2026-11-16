// The app's commands go through the ACL, so a remote page reaches only the ones its capability names.
const COMMANDS: &[&str] = &[
    "startup",
    "connect",
    "recent_servers",
    "forget_server",
    "use_local",
    "open_external",
    "show_notification",
    "open_new_window",
    "close_window",
    "set_dock_badge",
    "set_locale",
    "set_native_theme",
    "pane_browser_open",
    "pane_browser_bounds",
    "pane_browser_go",
    "pane_browser_history",
    "pane_browser_close",
    "pane_browser_act",
];

fn main() {
    tauri_build::try_build(
        tauri_build::Attributes::new().app_manifest(tauri_build::AppManifest::new().commands(COMMANDS)),
    )
    .expect("tauri build");
}
