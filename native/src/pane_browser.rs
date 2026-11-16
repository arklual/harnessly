//! The pane's browser on the computer itself: a site the page would show in its browser tab opens in a webview of this
//! window (WKWebView, WebView2, WebKitGTK), laid over the box the page keeps for it. The page sends the box, the
//! address and the agent's actions; the webview's moves (address, title, loading, history) go back to the page as a
//! `harnessly:pane-browser` DOM event. The agent's actions run inside the site by the page's own act script (the same
//! code as the preview bridge), handed over with the first open and injected before any script of the site.

use std::collections::HashMap;
use std::sync::mpsc;
use std::sync::Mutex;
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use serde_json::Value;
use tauri::webview::{NewWindowResponse, PageLoadEvent, WebviewBuilder};
use tauri::{AppHandle, LogicalPosition, LogicalSize, Manager, Webview, WebviewUrl, Window};
use url::Url;

use crate::url_policy;

/// The act script is the page's whole act code bundled; a bigger one is not what the page sends.
const MAX_SCRIPT: usize = 4 << 20;
/// The longest an action may take: a wait for a video's event is 25 s, and the page gives up before this.
const MAX_ACT: Duration = Duration::from_secs(45);
const POLL_EVERY: Duration = Duration::from_millis(80);
const EVAL_ANSWER: Duration = Duration::from_millis(1500);

/// The page reads it, and the act script checks that it lives in the app's pane before it installs.
const PANE_MARK: &str = "Object.defineProperty(window,'__HL_NATIVE_PANE__',{value:true});";

/// What the page last heard of each pane, by the pane's webview label.
#[derive(Default)]
pub struct Panes(Mutex<HashMap<String, PaneState>>);

#[derive(Clone, Serialize)]
#[serde(rename_all = "camelCase")]
struct PaneState {
    #[serde(skip)]
    owner: String,
    tab: String,
    url: String,
    title: String,
    loading: bool,
    can_back: bool,
    can_forward: bool,
}

#[derive(Deserialize, Clone, Copy)]
pub struct Bounds {
    x: f64,
    y: f64,
    width: f64,
    height: f64,
}

impl Bounds {
    fn sane(self) -> Option<Self> {
        let ok = [self.x, self.y, self.width, self.height].iter().all(|v| v.is_finite() && v.abs() < 100_000.0);
        (ok && self.width >= 1.0 && self.height >= 1.0).then_some(self)
    }
}

/// The webview's label: one per window and tab of the page, in the characters a label may hold.
pub fn pane_label(owner: &str, tab: &str) -> Option<String> {
    let clean: String = tab.chars().filter(|c| c.is_ascii_alphanumeric() || *c == '-' || *c == '_').take(64).collect();
    (!clean.is_empty() && clean.len() == tab.len()).then(|| format!("pane-{owner}-{clean}"))
}

/// Only web pages open in the pane; anything else is the person's own system's business.
pub fn pane_url(raw: &str) -> Option<Url> {
    let url = Url::parse(raw).ok()?;
    matches!(url.scheme(), "http" | "https").then_some(url)
}

/// The value an eval came back with: the act script answers a JSON text, which the webview may hand over as a JSON
/// string of it (WebKit, WebView2) or as the value itself.
pub fn read_eval(out: &str) -> Option<Value> {
    match serde_json::from_str::<Value>(out).ok()? {
        Value::Null => None,
        Value::String(inner) => match serde_json::from_str::<Value>(&inner).ok()? {
            Value::Null => None,
            v => Some(v),
        },
        v => Some(v),
    }
}

/// An action as the page sent it: an object with a short id; the act script checks the rest.
pub fn act_id(message: &Value) -> Option<String> {
    let id = message.get("id")?.as_str()?;
    (!id.is_empty() && id.len() <= 64).then(|| id.to_string())
}

/// The window of the page that asked: only a window's own page webview drives its panes, never a pane's site.
fn page_window(webview: &Webview) -> Result<Window, String> {
    let window = webview.window();
    if webview.label() != window.label() {
        return Err("only the app's page drives the pane".into());
    }
    Ok(window)
}

fn find(webview: &Webview, tab: &str) -> Result<Webview, String> {
    let window = page_window(webview)?;
    let label = pane_label(window.label(), tab).ok_or("bad tab")?;
    window.app_handle().get_webview(&label).ok_or_else(|| "no page in this tab".to_string())
}

#[cfg(not(target_os = "linux"))]
fn place(view: &Webview, b: Bounds) -> tauri::Result<()> {
    view.set_position(LogicalPosition::new(b.x, b.y))?;
    view.set_size(LogicalSize::new(b.width, b.height))
}

#[cfg(not(target_os = "linux"))]
fn set_shown(view: &Webview, shown: bool) -> tauri::Result<()> {
    if shown {
        view.show()
    } else {
        view.hide()
    }
}

#[cfg(not(target_os = "linux"))]
fn adopt(_window: &Window, _view: &Webview) {}

/// WebKitGTK: tauri packs a window's webviews one under another in its box, so the pane's webview is moved into a GTK
/// overlay over the page's own and placed there by its margins.
#[cfg(target_os = "linux")]
mod gtk_pane {
    use std::cell::RefCell;
    use std::collections::HashMap;

    use gtk::prelude::*;
    use tauri::Webview;

    use super::Bounds;

    thread_local! {
        /// Each window's overlay, by the window's label; GTK lives on the main thread only.
        static OVERLAYS: RefCell<HashMap<String, gtk::Overlay>> = RefCell::new(HashMap::new());
        /// Where each pane's webview goes, by the widget's name (the pane's label).
        static PLACES: RefCell<HashMap<String, gtk::Rectangle>> = RefCell::new(HashMap::new());
    }

    /// A size request is only a minimum, so the overlay is told each pane's exact box instead.
    fn place_of(_: &gtk::Overlay, child: &gtk::Widget) -> Option<gtk::Rectangle> {
        PLACES.with(|p| p.borrow().get(child.widget_name().as_str()).copied())
    }

    /// Puts the page's webview into an overlay (once per window) and the pane's on top of it.
    pub fn adopt(page: &Webview, pane: &Webview) {
        let owner = page.label().to_string();
        let owner_pane = owner.clone();
        let _ = page.with_webview(move |pw| {
            let page: gtk::Widget = pw.inner().upcast();
            if OVERLAYS.with(|o| o.borrow().contains_key(&owner)) {
                return;
            }
            let Some(parent) = page.parent().and_then(|p| p.downcast::<gtk::Box>().ok()) else { return };
            let overlay = gtk::Overlay::new();
            overlay.connect_get_child_position(place_of);
            parent.remove(&page);
            overlay.add(&page);
            parent.pack_start(&overlay, true, true, 0);
            overlay.show_all();
            OVERLAYS.with(|o| o.borrow_mut().insert(owner, overlay));
        });
        let name = pane.label().to_string();
        let _ = pane.with_webview(move |pw| {
            let pane: gtk::Widget = pw.inner().upcast();
            let Some(overlay) = OVERLAYS.with(|o| o.borrow().get(&owner_pane).cloned()) else { return };
            pane.set_widget_name(&name);
            if let Some(parent) = pane.parent().and_then(|p| p.downcast::<gtk::Container>().ok()) {
                parent.remove(&pane);
            }
            // Filled: an aligned child is shrunk to its natural size inside the box it is given.
            pane.set_halign(gtk::Align::Fill);
            pane.set_valign(gtk::Align::Fill);
            overlay.add_overlay(&pane);
            pane.hide();
        });
    }

    pub fn place(pane: &Webview, b: Bounds) {
        let name = pane.label().to_string();
        let _ = pane.with_webview(move |pw| {
            let pane: gtk::Widget = pw.inner().upcast();
            let rect = gtk::Rectangle::new(
                b.x.max(0.0).round() as i32,
                b.y.max(0.0).round() as i32,
                b.width.round() as i32,
                b.height.round() as i32,
            );
            PLACES.with(|p| p.borrow_mut().insert(name, rect));
            pane.queue_resize();
        });
    }

    /// A closed pane's box is dropped with it.
    pub fn forget(pane: &Webview) {
        let name = pane.label().to_string();
        let _ = pane.with_webview(move |_| {
            PLACES.with(|p| p.borrow_mut().remove(&name));
        });
    }

    pub fn set_shown(pane: &Webview, shown: bool) {
        let _ = pane.with_webview(move |pw| {
            let pane: gtk::Widget = pw.inner().upcast();
            if shown {
                pane.show();
            } else {
                pane.hide();
            }
        });
    }
}

#[cfg(target_os = "linux")]
fn place(view: &Webview, b: Bounds) -> tauri::Result<()> {
    gtk_pane::place(view, b);
    Ok(())
}

#[cfg(target_os = "linux")]
fn set_shown(view: &Webview, shown: bool) -> tauri::Result<()> {
    gtk_pane::set_shown(view, shown);
    Ok(())
}

#[cfg(target_os = "linux")]
fn adopt(window: &Window, view: &Webview) {
    if let Some(page) = window.app_handle().get_webview(window.label()) {
        gtk_pane::adopt(&page, view);
    }
}

/// Tells the page what its pane shows now.
fn tell(app: &AppHandle, label: &str) {
    let Some(state) = app.state::<Panes>().0.lock().unwrap().get(label).cloned() else { return };
    let Some(owner) = app.get_webview(&state.owner) else { return };
    let Ok(detail) = serde_json::to_string(&state) else { return };
    let _ = owner.eval(format!(
        "window.dispatchEvent(new CustomEvent('harnessly:pane-browser',{{detail:{detail}}}))"
    ));
}

fn update(app: &AppHandle, label: &str, change: impl FnOnce(&mut PaneState)) {
    if let Some(state) = app.state::<Panes>().0.lock().unwrap().get_mut(label) {
        change(state);
    }
    tell(app, label);
}

const PAGE_STATE_JS: &str = "(function(){var n=window.navigation,h=n&&typeof n.canGoBack==='boolean';\
return JSON.stringify({t:String(document.title||''),b:h?n.canGoBack:history.length>1,f:h?n.canGoForward:false})})()";

/// After a load: the title and the history buttons, read in the page.
fn read_page_state(app: AppHandle, view: Webview) {
    let label = view.label().to_string();
    let _ = view.eval_with_callback(PAGE_STATE_JS, move |out| {
        let read = read_eval(&out);
        update(&app, &label, |s| {
            if let Some(v) = read {
                if let Some(t) = v.get("t").and_then(Value::as_str) {
                    s.title = t.chars().take(300).collect();
                }
                s.can_back = v.get("b").and_then(Value::as_bool).unwrap_or(s.can_back);
                s.can_forward = v.get("f").and_then(Value::as_bool).unwrap_or(false);
            }
        });
    });
}

#[cfg(target_os = "macos")]
const USER_AGENT: Option<&str> = Some(
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
);
#[cfg(not(target_os = "macos"))]
const USER_AGENT: Option<&str> = None;

fn build(app: &AppHandle, window: &Window, label: &str, url: Url, script: &str) -> tauri::Result<Webview> {
    let (nav_app, popup_app, load_app, title_app) = (app.clone(), app.clone(), app.clone(), app.clone());
    let popup_label = label.to_string();
    let mut builder = WebviewBuilder::new(label, WebviewUrl::External(url))
        .initialization_script(format!("{PANE_MARK}{script}"))
        .on_navigation(move |target| {
            if pane_url(target.as_str()).is_some() || target.scheme() == "about" || target.scheme() == "blob" {
                return true;
            }
            if url_policy::is_safe_external_url(target.as_str()) {
                crate::open_safe_external(&nav_app, target.as_str());
            }
            false
        })
        // A link that opens a new window opens in the pane itself: the pane is one tab.
        .on_new_window(move |target, _features| {
            if pane_url(target.as_str()).is_some() {
                if let Some(view) = popup_app.get_webview(&popup_label) {
                    let _ = view.navigate(target);
                }
            }
            NewWindowResponse::Deny
        })
        .on_page_load(move |view, payload| {
            let label = view.label().to_string();
            let url = payload.url().to_string();
            match payload.event() {
                PageLoadEvent::Started => update(&load_app, &label, |s| {
                    s.url = url;
                    s.loading = true;
                }),
                PageLoadEvent::Finished => {
                    if let Some(s) = load_app.state::<Panes>().0.lock().unwrap().get_mut(&label) {
                        s.url = url;
                        s.loading = false;
                    }
                    read_page_state(load_app.clone(), view);
                }
            }
        })
        .on_document_title_changed(move |view, title| {
            update(&title_app, view.label(), |s| s.title = title.chars().take(300).collect());
        });
    if let Some(agent) = USER_AGENT {
        builder = builder.user_agent(agent);
    }
    let view = window.add_child(builder, LogicalPosition::new(0.0, 0.0), LogicalSize::new(1.0, 1.0))?;
    adopt(window, &view);
    Ok(view)
}

/// Opens the tab's page over the box, or shows the one the tab already has (it keeps its page and history).
#[tauri::command]
pub fn pane_browser_open(
    app: AppHandle,
    webview: Webview,
    tab: String,
    url: String,
    script: String,
    bounds: Bounds,
) -> Result<(), String> {
    let window = page_window(&webview)?;
    let label = pane_label(window.label(), &tab).ok_or("bad tab")?;
    let bounds = bounds.sane().ok_or("bad box")?;
    if let Some(view) = app.get_webview(&label) {
        place(&view, bounds).map_err(|e| e.to_string())?;
        set_shown(&view, true).map_err(|e| e.to_string())?;
        tell(&app, &label);
        return Ok(());
    }
    let url = pane_url(&url).ok_or("give an http(s) address")?;
    if script.len() > MAX_SCRIPT {
        return Err("act script too big".into());
    }
    app.state::<Panes>().0.lock().unwrap().insert(
        label.clone(),
        PaneState {
            owner: window.label().to_string(),
            tab,
            url: url.to_string(),
            title: String::new(),
            loading: true,
            can_back: false,
            can_forward: false,
        },
    );
    let view = build(&app, &window, &label, url, &script).map_err(|e| e.to_string())?;
    place(&view, bounds).map_err(|e| e.to_string())?;
    set_shown(&view, true).map_err(|e| e.to_string())
}

/// The box moved or the page hid it (another tab, a closed pane, a device frame).
#[tauri::command]
pub fn pane_browser_bounds(webview: Webview, tab: String, bounds: Bounds, visible: bool) -> Result<(), String> {
    let view = find(&webview, &tab)?;
    match bounds.sane() {
        Some(b) if visible => {
            place(&view, b).map_err(|e| e.to_string())?;
            set_shown(&view, true).map_err(|e| e.to_string())
        }
        _ => set_shown(&view, false).map_err(|e| e.to_string()),
    }
}

#[tauri::command]
pub fn pane_browser_go(webview: Webview, tab: String, url: String) -> Result<(), String> {
    let url = pane_url(&url).ok_or("give an http(s) address")?;
    find(&webview, &tab)?.navigate(url).map_err(|e| e.to_string())
}

/// -1 back, 1 forward, 0 reload.
#[tauri::command]
pub fn pane_browser_history(webview: Webview, tab: String, step: i32) -> Result<(), String> {
    let view = find(&webview, &tab)?;
    match step {
        0 => view.reload(),
        s if s < 0 => view.eval("history.back()"),
        _ => view.eval("history.forward()"),
    }
    .map_err(|e| e.to_string())
}

#[tauri::command]
pub fn pane_browser_close(app: AppHandle, webview: Webview, tab: String) -> Result<(), String> {
    let Ok(view) = find(&webview, &tab) else { return Ok(()) };
    app.state::<Panes>().0.lock().unwrap().remove(view.label());
    #[cfg(target_os = "linux")]
    gtk_pane::forget(&view);
    view.close().map_err(|e| e.to_string())
}

/// One action of the agent, run by the act script inside the site; None when it did not answer in time (the page
/// went elsewhere, or it is still loading).
#[tauri::command]
pub async fn pane_browser_act(
    webview: Webview,
    tab: String,
    message: Value,
    timeout_ms: u64,
) -> Result<Option<Value>, String> {
    let view = find(&webview, &tab)?;
    let id = act_id(&message).ok_or("bad action")?;
    let start = serde_json::to_string(&message).map_err(|e| e.to_string())?;
    let id_js = serde_json::to_string(&id).map_err(|e| e.to_string())?;
    view.eval(format!("window.__hlNativeAct&&window.__hlNativeAct.start({start})")).map_err(|e| e.to_string())?;
    let deadline = Instant::now() + Duration::from_millis(timeout_ms).min(MAX_ACT);
    let take = format!("window.__hlNativeAct?window.__hlNativeAct.take({id_js}):null");
    let answer = tauri::async_runtime::spawn_blocking(move || loop {
        let (tx, rx) = mpsc::channel();
        if view.eval_with_callback(take.clone(), move |out| { let _ = tx.send(out); }).is_ok() {
            if let Ok(out) = rx.recv_timeout(EVAL_ANSWER) {
                if let Some(v) = read_eval(&out) {
                    return Some(v);
                }
            }
        }
        if Instant::now() >= deadline {
            return None;
        }
        std::thread::sleep(POLL_EVERY);
    })
    .await
    .map_err(|e| e.to_string())?;
    Ok(answer)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn labels_take_only_plain_tab_ids() {
        assert_eq!(pane_label("main", "t-12_a").as_deref(), Some("pane-main-t-12_a"));
        assert_eq!(pane_label("main", ""), None);
        assert_eq!(pane_label("main", "a/b"), None);
        assert_eq!(pane_label("main", &"x".repeat(65)), None);
    }

    #[test]
    fn only_web_pages_open() {
        assert!(pane_url("https://example.com/a").is_some());
        assert!(pane_url("http://localhost:3000").is_some());
        assert!(pane_url("file:///etc/passwd").is_none());
        assert!(pane_url("javascript:alert(1)").is_none());
        assert!(pane_url("harnessly://x").is_none());
    }

    #[test]
    fn eval_answers_come_either_way() {
        assert_eq!(read_eval("null"), None);
        assert_eq!(read_eval(r#""null""#), None);
        assert_eq!(read_eval(r#""{\"ok\":true}""#), Some(serde_json::json!({"ok": true})));
        assert_eq!(read_eval(r#"{"ok":false}"#), Some(serde_json::json!({"ok": false})));
        assert_eq!(read_eval("garbage"), None);
    }

    #[test]
    fn actions_need_a_short_id() {
        assert_eq!(act_id(&serde_json::json!({"id": "a1", "op": "snapshot"})).as_deref(), Some("a1"));
        assert_eq!(act_id(&serde_json::json!({"op": "snapshot"})), None);
        assert_eq!(act_id(&serde_json::json!({"id": ""})), None);
        assert_eq!(act_id(&serde_json::json!({"id": "x".repeat(65)})), None);
    }

    #[test]
    fn boxes_must_be_real() {
        let b = |x: f64, y: f64, width: f64, height: f64| Bounds { x, y, width, height }.sane().is_some();
        assert!(b(10.0, 20.0, 300.0, 400.0));
        assert!(!b(0.0, 0.0, 0.0, 400.0));
        assert!(!b(f64::NAN, 0.0, 10.0, 10.0));
        assert!(!b(0.0, 0.0, 1e9, 10.0));
    }
}
