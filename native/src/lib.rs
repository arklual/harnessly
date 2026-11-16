//! Harnessly native shell: one window per view of a Harnessly runtime or hub, the same web page the browser shows.

mod config;
mod deeplink;
#[cfg(desktop)]
mod local_runtime;
mod pane_browser;
mod url_policy;
mod window_state;

use config::{AppConfig, ServerConfig, DEFAULT_WINDOW_STATE};
use std::collections::{HashMap, HashSet};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::Mutex;
use std::time::Duration;
use tauri::ipc::CapabilityBuilder;
use tauri::webview::NewWindowResponse;
use tauri::{AppHandle, Manager, RunEvent, Theme, Webview, WebviewUrl, WebviewWindow, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_deep_link::DeepLinkExt;
use tauri_plugin_notification::NotificationExt;
use tauri_plugin_opener::OpenerExt;
use url::Url;
use url_policy::Navigation;
use window_state::Rect;

const BRIDGE: &str = include_str!("bridge.js");
const MIN_SIZE: (f64, f64) = (800.0, 500.0);
const DARK_BACKGROUND: tauri::window::Color = tauri::window::Color(9, 9, 11, 255);
/// The commands a page of the configured origin may call (`window.electronAPI`).
const BRIDGE_PERMISSIONS: [&str; 13] = [
    "allow-open-external",
    "allow-show-notification",
    "allow-open-new-window",
    "allow-close-window",
    "allow-set-dock-badge",
    "allow-set-locale",
    "allow-set-native-theme",
    "allow-pane-browser-open",
    "allow-pane-browser-bounds",
    "allow-pane-browser-go",
    "allow-pane-browser-history",
    "allow-pane-browser-close",
    "allow-pane-browser-act",
];
#[cfg(any(windows, target_os = "android"))]
const APP_PAGE: &str = "http://tauri.localhost/index.html";
#[cfg(not(any(windows, target_os = "android")))]
const APP_PAGE: &str = "tauri://localhost/index.html";

struct Shell {
    config_path: PathBuf,
    config: Mutex<AppConfig>,
    env: HashMap<String, String>,
    current_url: Mutex<Option<String>>,
    pending_deep_link: Mutex<Option<String>>,
    #[cfg(desktop)]
    local_runtime: Mutex<Option<local_runtime::LocalRuntime>>,
    bridged: Mutex<HashSet<String>>,
    window_seq: AtomicUsize,
    quitting: AtomicBool,
    dirty: Mutex<HashSet<String>>,
}

impl Shell {
    fn config(&self) -> AppConfig {
        self.config.lock().unwrap().clone()
    }

    fn update_config(&self, change: impl FnOnce(&mut AppConfig)) {
        let mut cfg = self.config.lock().unwrap();
        change(&mut cfg);
        if let Err(err) = write_config(&self.config_path, &cfg) {
            eprintln!("[native] cannot save config: {err}");
        }
    }
}

fn write_config(path: &PathBuf, cfg: &AppConfig) -> std::io::Result<()> {
    if let Some(dir) = path.parent() {
        std::fs::create_dir_all(dir)?;
    }
    let tmp = path.with_extension("json.tmp");
    std::fs::write(&tmp, serde_json::to_vec_pretty(cfg).map_err(std::io::Error::other)?)?;
    std::fs::rename(tmp, path)
}

fn config_path(app: &AppHandle, env: &HashMap<String, String>) -> PathBuf {
    if let Some(home) = env.get("HARNESSLY_DESKTOP_HOME").filter(|v| !v.is_empty()) {
        return PathBuf::from(home).join("config.json");
    }
    #[cfg(desktop)]
    if let Ok(home) = app.path().home_dir() {
        return home.join(".harnessly-desktop").join("config.json");
    }
    app.path().app_config_dir().unwrap_or_else(|_| PathBuf::from(".")).join("config.json")
}

fn origin_of(raw: &str) -> Option<String> {
    let url = Url::parse(raw).ok()?;
    url.origin().is_tuple().then(|| url.origin().ascii_serialization())
}

/// Lets pages of this URL's origin call the bridge commands (once per origin).
fn ensure_bridge(app: &AppHandle, url: &str) {
    let Some(pattern) = url_policy::origin_pattern(url) else { return };
    let shell = app.state::<Shell>();
    let mut bridged = shell.bridged.lock().unwrap();
    if bridged.contains(&pattern) {
        return;
    }
    let mut cap = CapabilityBuilder::new(format!("bridge-{}", bridged.len())).local(false).remote(pattern.clone()).webview("main").webview("window-*");
    for permission in BRIDGE_PERMISSIONS {
        cap = cap.permission(permission);
    }
    match app.add_capability(cap) {
        Ok(()) => {
            bridged.insert(pattern);
        }
        Err(err) => eprintln!("[native] bridge for {pattern}: {err}"),
    }
}

fn open_in(app: &AppHandle, view: &Webview, target: &str) {
    ensure_bridge(app, target);
    match Url::parse(target) {
        Ok(url) => {
            if let Err(err) = view.navigate(url) {
                eprintln!("[native] navigate: {err}");
            }
        }
        Err(err) => eprintln!("[native] bad address {target}: {err}"),
    }
}

fn open_safe_external(app: &AppHandle, url: &str) {
    if url_policy::is_safe_external_url(url) {
        if let Err(err) = app.opener().open_url(url, None::<&str>) {
            eprintln!("[native] open {url}: {err}");
        }
    }
}

/// The page's webview of the focused window, else of the first one. A window that holds a pane's browser is no longer
/// a WebviewWindow to tauri (it has two webviews), so the shell works with windows and their page's webview.
fn primary_window(app: &AppHandle) -> Option<Webview> {
    let windows = app.windows();
    let window = windows
        .values()
        .find(|w| w.is_focused().unwrap_or(false))
        .or_else(|| windows.get("main"))
        .or_else(|| windows.values().next())?;
    app.get_webview(window.label())
}

fn work_areas(app: &AppHandle) -> Vec<Rect> {
    app.available_monitors()
        .unwrap_or_default()
        .iter()
        .map(|m| {
            let (area, scale) = (m.work_area(), m.scale_factor());
            Rect {
                x: area.position.x as f64 / scale,
                y: area.position.y as f64 / scale,
                width: area.size.width as f64 / scale,
                height: area.size.height as f64 / scale,
            }
        })
        .collect()
}

/// The bridge, told whether the pane's browser may open sites on this computer (HARNESSLY_DESKTOP_PANE_BROWSER=0 keeps
/// the page's own browser, the machine's).
fn bridge_script(env: &HashMap<String, String>) -> String {
    let on = pane_browser_enabled(env);
    format!("window.__HARNESSLY_PANE_BROWSER__={on};\n{BRIDGE}")
}

fn pane_browser_enabled(env: &HashMap<String, String>) -> bool {
    env.get("HARNESSLY_DESKTOP_PANE_BROWSER").is_none_or(|v| v != "0")
}

fn create_window(app: &AppHandle, url: Option<&str>) -> tauri::Result<WebviewWindow> {
    let shell = app.state::<Shell>();
    let seq = shell.window_seq.fetch_add(1, Ordering::SeqCst);
    let label = if seq == 0 { "main".to_string() } else { format!("window-{seq}") };
    let open_windows = app.windows().len();
    let saved = shell.config().window_state.unwrap_or(DEFAULT_WINDOW_STATE);
    let areas = work_areas(app);
    let largest = areas.iter().copied().max_by(|a, b| (a.width * a.height).total_cmp(&(b.width * b.height)));
    let (width, height) = window_state::clamp_size(&saved, MIN_SIZE, largest);

    let webview_url = match url {
        Some(u) => {
            ensure_bridge(app, u);
            WebviewUrl::External(Url::parse(u).map_err(|e| tauri::Error::Anyhow(e.into()))?)
        }
        None => WebviewUrl::App("index.html".into()),
    };
    let nav_app = app.clone();
    let popup_app = app.clone();
    let mut builder = WebviewWindowBuilder::new(app, &label, webview_url)
        .title("Harnessly")
        .inner_size(width, height)
        .min_inner_size(MIN_SIZE.0, MIN_SIZE.1)
        .initialization_script(bridge_script(&shell.env))
        // The page handles dropped files itself, as in a browser.
        .disable_drag_drop_handler()
        .on_navigation(move |target| match url_policy::navigation(target) {
            Navigation::Stay => true,
            Navigation::External => {
                open_safe_external(&nav_app, target.as_str());
                false
            }
            Navigation::Deny => false,
        })
        .on_new_window(move |target, _features| {
            open_safe_external(&popup_app, target.as_str());
            NewWindowResponse::Deny
        });
    if shell.config().app_theme.as_deref() == Some("dark") {
        builder = builder.background_color(DARK_BACKGROUND);
    }
    builder = match window_state::restore_position(&saved, &areas) {
        Some((x, y)) => {
            let offset = window_state::cascade_offset(open_windows);
            builder.position(x + offset, y + offset)
        }
        None => builder.center(),
    };
    let window = builder.build()?;
    if matches!(window.theme(), Ok(Theme::Dark)) {
        let _ = window.set_background_color(Some(DARK_BACKGROUND));
    }
    if open_windows == 0 {
        if saved.is_maximized {
            let _ = window.maximize();
        }
        if saved.is_full_screen {
            let _ = window.set_fullscreen(true);
        }
    }
    Ok(window)
}

fn save_window_state(app: &AppHandle, window: &tauri::Window) {
    let (Ok(scale), Ok(maximized), Ok(full)) = (window.scale_factor(), window.is_maximized(), window.is_fullscreen())
    else {
        return;
    };
    let shell = app.state::<Shell>();
    let mut state = shell.config().window_state.unwrap_or(DEFAULT_WINDOW_STATE);
    // Maximized and full-screen bounds are the screen's; the normal ones stay as they were.
    if !maximized && !full && !window.is_minimized().unwrap_or(false) {
        if let (Ok(pos), Ok(size)) = (window.outer_position(), window.inner_size()) {
            state.x = Some(pos.x as f64 / scale);
            state.y = Some(pos.y as f64 / scale);
            state.width = size.width as f64 / scale;
            state.height = size.height as f64 / scale;
        }
    }
    state.is_maximized = maximized;
    state.is_full_screen = full;
    shell.update_config(|cfg| cfg.window_state = Some(state));
}

/// Writes the moved or resized windows' bounds at most twice a second instead of on every pixel.
fn start_state_saver(app: AppHandle) {
    std::thread::spawn(move || loop {
        std::thread::sleep(Duration::from_millis(500));
        let labels: Vec<String> = app.state::<Shell>().dirty.lock().unwrap().drain().collect();
        for label in labels {
            if let Some(window) = app.get_window(&label) {
                save_window_state(&app, &window);
            }
        }
    });
}

/// The address to show, found once: forced by the environment, saved, or a local runtime that answers.
fn resolve_start(app: &AppHandle) -> Option<String> {
    let shell = app.state::<Shell>();
    if let Some(url) = shell.current_url.lock().unwrap().clone() {
        return Some(url);
    }
    let cfg = shell.config();
    #[allow(unused_mut)]
    let mut local_port = None;
    #[cfg(desktop)]
    if config::wants_local(&cfg, &shell.env) {
        let mut slot = shell.local_runtime.lock().unwrap();
        if slot.is_none() {
            *slot = local_runtime::ensure_local_runtime(&shell.env, Duration::from_secs(30));
        }
        local_port = slot.as_ref().map(|rt| rt.port);
    }
    let url = config::resolve_start_url(&cfg, &shell.env, local_port);
    *shell.current_url.lock().unwrap() = url.clone();
    url
}

fn handle_deep_link(app: &AppHandle, raw: &str) {
    let shell = app.state::<Shell>();
    if !config::deep_links_enabled(&shell.config(), &shell.env) {
        return;
    }
    let Some(current) = shell.current_url.lock().unwrap().clone() else {
        *shell.pending_deep_link.lock().unwrap() = Some(raw.to_string());
        return;
    };
    let Some(target) = origin_of(&current).and_then(|origin| deeplink::apply_deep_link(&origin, raw)) else { return };
    let view = primary_window(app).or_else(|| create_window(app, None).ok().map(|w| w.as_ref().clone()));
    if let Some(view) = view {
        open_in(app, &view, &target);
        let window = view.window();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

// ── commands of the app's own page ───────────────────────────────────────────────────────────────────────────────

/// Called by the start page: navigates the window when an address is known, else returns the recent servers.
#[tauri::command]
async fn startup(app: AppHandle, webview: Webview) -> Result<Option<Vec<String>>, String> {
    let resolver = app.clone();
    let url = tauri::async_runtime::spawn_blocking(move || resolve_start(&resolver)).await.map_err(|e| e.to_string())?;
    let shell = app.state::<Shell>();
    match url {
        Some(url) => {
            let pending = shell.pending_deep_link.lock().unwrap().take();
            let target = pending
                .and_then(|raw| origin_of(&url).and_then(|o| deeplink::apply_deep_link(&o, &raw)))
                .unwrap_or(url);
            open_in(&app, &webview, &target);
            Ok(None)
        }
        None => Ok(Some(shell.config().recent_remote_urls)),
    }
}

#[tauri::command]
fn connect(app: AppHandle, webview: Webview, url: String) -> bool {
    let Some(url) = config::normalize_remote_url(&url) else { return false };
    let shell = app.state::<Shell>();
    shell.update_config(|cfg| {
        cfg.server = Some(ServerConfig { mode: "remote".into(), remote_url: Some(url.clone()) });
        cfg.recent_remote_urls = config::add_recent_remote_url(&cfg.recent_remote_urls, &url);
    });
    *shell.current_url.lock().unwrap() = Some(url.clone());
    open_in(&app, &webview, &url);
    true
}

#[tauri::command]
fn recent_servers(app: AppHandle) -> Vec<String> {
    app.state::<Shell>().config().recent_remote_urls
}

#[tauri::command]
fn forget_server(app: AppHandle, url: String) -> Vec<String> {
    let shell = app.state::<Shell>();
    shell.update_config(|cfg| cfg.recent_remote_urls = config::remove_recent_remote_url(&cfg.recent_remote_urls, &url));
    shell.config().recent_remote_urls
}

/// Back to local mode: the saved server is dropped and the start page looks for a local runtime again.
#[tauri::command]
fn use_local(app: AppHandle) {
    let shell = app.state::<Shell>();
    shell.update_config(|cfg| cfg.server = Some(ServerConfig { mode: "local".into(), remote_url: None }));
    *shell.current_url.lock().unwrap() = None;
}

// ── window.electronAPI ───────────────────────────────────────────────────────────────────────────────────────────

#[tauri::command]
fn open_external(app: AppHandle, url: String) {
    open_safe_external(&app, &url);
}

#[tauri::command]
fn show_notification(app: AppHandle, title: String, body: String) -> bool {
    if app.windows().values().any(|w| w.is_focused().unwrap_or(false)) {
        return false;
    }
    app.notification().builder().title(title).body(body).show().is_ok()
}

#[tauri::command]
fn open_new_window(app: AppHandle) {
    let current = app.state::<Shell>().current_url.lock().unwrap().clone();
    if let Some(url) = current {
        if let Err(err) = create_window(&app, Some(&url)) {
            eprintln!("[native] new window: {err}");
        }
    }
}

/// ⌘W / Ctrl+W belongs to the page: it closes the chats one at a time and asks for this once none is left.
#[tauri::command]
fn close_window(webview: Webview) {
    let _ = webview.window().close();
}

#[tauri::command]
fn set_dock_badge(webview: Webview, count: i64) {
    let _ = webview.window().set_badge_count(if count > 0 { Some(count) } else { None });
}

#[tauri::command]
fn set_locale(app: AppHandle, locale: String) {
    app.state::<Shell>().update_config(|cfg| cfg.locale = Some(locale));
}

#[tauri::command]
fn set_native_theme(app: AppHandle, theme: String) {
    if !url_policy::is_theme_source(&theme) {
        return;
    }
    #[cfg(desktop)]
    app.set_theme(match theme.as_str() {
        "dark" => Some(Theme::Dark),
        "light" => Some(Theme::Light),
        _ => None,
    });
    app.state::<Shell>().update_config(|cfg| cfg.app_theme = Some(theme));
}

#[cfg(target_os = "macos")]
fn build_menu(app: &AppHandle) -> tauri::Result<tauri::menu::Menu<tauri::Wry>> {
    use tauri::menu::{Menu, MenuItem, PredefinedMenuItem as P, Submenu};
    let switch = MenuItem::with_id(app, "switch-server", "Switch Server…", true, Some("CmdOrCtrl+Shift+K"))?;
    let new_window = MenuItem::with_id(app, "new-window", "New Window", true, Some("CmdOrCtrl+Shift+N"))?;
    let reload = MenuItem::with_id(app, "reload", "Reload", true, Some("CmdOrCtrl+R"))?;
    Menu::with_items(
        app,
        &[
            &Submenu::with_items(
                app,
                "Harnessly",
                true,
                &[
                    &P::about(app, None, None)?,
                    &P::separator(app)?,
                    &switch,
                    &P::separator(app)?,
                    &P::services(app, None)?,
                    &P::separator(app)?,
                    &P::hide(app, None)?,
                    &P::hide_others(app, None)?,
                    &P::show_all(app, None)?,
                    &P::separator(app)?,
                    &P::quit(app, None)?,
                ],
            )?,
            &Submenu::with_items(
                app,
                "Edit",
                true,
                &[
                    &P::undo(app, None)?,
                    &P::redo(app, None)?,
                    &P::separator(app)?,
                    &P::cut(app, None)?,
                    &P::copy(app, None)?,
                    &P::paste(app, None)?,
                    &P::select_all(app, None)?,
                ],
            )?,
            &Submenu::with_items(app, "View", true, &[&reload, &P::separator(app)?, &P::fullscreen(app, None)?])?,
            // No «Close Window» item: its ⌘W would take the key from the page.
            &Submenu::with_items(
                app,
                "Window",
                true,
                &[&new_window, &P::separator(app)?, &P::minimize(app, None)?, &P::maximize(app, None)?],
            )?,
        ],
    )
}

fn on_menu(app: &AppHandle, id: &str) {
    let Some(view) = primary_window(app) else { return };
    match id {
        "switch-server" => {
            if let Ok(url) = Url::parse(&format!("{APP_PAGE}?connect=1")) {
                let _ = view.navigate(url);
            }
        }
        "new-window" => open_new_window(app.clone()),
        "reload" => {
            let _ = view.reload();
        }
        _ => {}
    }
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let mut builder = tauri::Builder::default();
    #[cfg(desktop)]
    {
        builder = builder.plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| {
            // A deep link in argv reaches on_open_url through the deep-link feature of this plugin.
            if let Some(window) = primary_window(app).map(|v| v.window()) {
                let _ = window.show();
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }));
    }
    let app = builder
        .plugin(tauri_plugin_deep_link::init())
        .plugin(tauri_plugin_opener::init())
        .plugin(tauri_plugin_notification::init())
        .invoke_handler(tauri::generate_handler![
            startup,
            connect,
            recent_servers,
            forget_server,
            use_local,
            open_external,
            show_notification,
            open_new_window,
            close_window,
            set_dock_badge,
            set_locale,
            set_native_theme,
            pane_browser::pane_browser_open,
            pane_browser::pane_browser_bounds,
            pane_browser::pane_browser_go,
            pane_browser::pane_browser_history,
            pane_browser::pane_browser_close,
            pane_browser::pane_browser_act,
        ])
        .setup(|app| {
            let handle = app.handle().clone();
            let env: HashMap<String, String> = std::env::vars().collect();
            let path = config_path(&handle, &env);
            let cfg = std::fs::read_to_string(&path).map(|raw| config::parse_app_config(&raw)).unwrap_or_default();
            let deep_links = config::deep_links_enabled(&cfg, &env);
            app.manage(pane_browser::Panes::default());
            app.manage(Shell {
                config_path: path,
                config: Mutex::new(cfg),
                env,
                current_url: Mutex::new(None),
                pending_deep_link: Mutex::new(None),
                #[cfg(desktop)]
                local_runtime: Mutex::new(None),
                bridged: Mutex::new(HashSet::new()),
                window_seq: AtomicUsize::new(0),
                quitting: AtomicBool::new(false),
                dirty: Mutex::new(HashSet::new()),
            });

            #[cfg(target_os = "macos")]
            {
                app.set_menu(build_menu(&handle)?)?;
                app.on_menu_event(|app, event| on_menu(app, event.id().as_ref()));
            }
            #[cfg(not(target_os = "macos"))]
            let _ = on_menu;

            #[cfg(any(windows, target_os = "linux"))]
            if deep_links {
                if let Err(err) = app.deep_link().register_all() {
                    eprintln!("[native] deep link registration: {err}");
                }
            }
            let _ = deep_links;
            let link_handle = handle.clone();
            app.deep_link().on_open_url(move |event| {
                for url in event.urls() {
                    handle_deep_link(&link_handle, url.as_str());
                }
            });
            if let Ok(Some(urls)) = app.deep_link().get_current() {
                if let Some(first) = urls.first() {
                    *app.state::<Shell>().pending_deep_link.lock().unwrap() = Some(first.to_string());
                }
            } else if let Some(link) = deeplink::find_deep_link(&std::env::args().collect::<Vec<_>>()) {
                *app.state::<Shell>().pending_deep_link.lock().unwrap() = Some(link);
            }

            create_window(&handle, None)?;
            start_state_saver(handle);
            Ok(())
        })
        .on_window_event(|window, event| {
            let app = window.app_handle();
            match event {
                WindowEvent::Moved(_) | WindowEvent::Resized(_) => {
                    app.state::<Shell>().dirty.lock().unwrap().insert(window.label().to_string());
                }
                WindowEvent::CloseRequested { api, .. } => {
                    save_window_state(app, window);
                    // macOS keeps the app and its last window alive behind the Dock icon.
                    let last = app.windows().len() == 1;
                    if cfg!(target_os = "macos") && last && !app.state::<Shell>().quitting.load(Ordering::SeqCst) {
                        api.prevent_close();
                        let _ = window.hide();
                    }
                }
                _ => {}
            }
        })
        .build(tauri::generate_context!())
        .expect("error while building the Harnessly app");

    app.run(|app, event| match event {
        RunEvent::ExitRequested { .. } => {
            app.state::<Shell>().quitting.store(true, Ordering::SeqCst);
        }
        RunEvent::Exit => {
            #[cfg(desktop)]
            if let Some(mut rt) = app.state::<Shell>().local_runtime.lock().unwrap().take() {
                rt.stop();
            }
        }
        #[cfg(target_os = "macos")]
        RunEvent::Reopen { .. } => match primary_window(app).map(|v| v.window()) {
            Some(window) => {
                let _ = window.show();
                let _ = window.set_focus();
            }
            None => {
                let current = app.state::<Shell>().current_url.lock().unwrap().clone();
                let _ = create_window(app, current.as_deref());
            }
        },
        _ => {}
    });
}
