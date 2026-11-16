//! The app's saved choices (`~/.harnessly-desktop/config.json`, shared with the Electron shell) and the start address.

use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use url::Url;

pub const DEFAULT_LOCAL_PORT: u16 = 8022;
pub const MAX_RECENT_REMOTE_URLS: usize = 10;

#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ServerConfig {
    #[serde(default)]
    pub mode: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub remote_url: Option<String>,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct WindowState {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub x: Option<f64>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub y: Option<f64>,
    pub width: f64,
    pub height: f64,
    #[serde(default)]
    pub is_maximized: bool,
    #[serde(default)]
    pub is_full_screen: bool,
}

pub const DEFAULT_WINDOW_STATE: WindowState =
    WindowState { x: None, y: None, width: 1280.0, height: 800.0, is_maximized: false, is_full_screen: false };

/// Unknown keys (the Electron shell's `displayId`, future fields) are kept as they are when the file is written back.
#[derive(Debug, Clone, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppConfig {
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub server: Option<ServerConfig>,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub recent_remote_urls: Vec<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub window_state: Option<WindowState>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub app_theme: Option<String>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub locale: Option<String>,
    #[serde(default)]
    pub deep_links: bool,
    #[serde(flatten)]
    pub rest: HashMap<String, serde_json::Value>,
}

/// A broken or foreign file reads as an empty config instead of stopping the app.
pub fn parse_app_config(raw: &str) -> AppConfig {
    serde_json::from_str::<AppConfig>(raw).unwrap_or_default()
}

/// The saved remote address, when the saved mode is remote.
pub fn saved_remote(cfg: &AppConfig) -> Option<&str> {
    match &cfg.server {
        Some(ServerConfig { mode, remote_url: Some(url) }) if mode == "remote" && !url.is_empty() => Some(url),
        _ => None,
    }
}

/// Accepts "host", "host:port" and full http(s) URLs; returns the origin (plus a path prefix, if any) or None.
pub fn normalize_remote_url(input: &str) -> Option<String> {
    let text = input.trim();
    if text.is_empty() {
        return None;
    }
    let has_scheme = text.find("://").is_some_and(|i| {
        let scheme = &text[..i];
        scheme.chars().next().is_some_and(|c| c.is_ascii_alphabetic())
            && scheme.chars().all(|c| c.is_ascii_alphanumeric() || "+.-".contains(c))
    });
    let with_scheme = if has_scheme { text.to_string() } else { format!("https://{text}") };
    let url = Url::parse(&with_scheme).ok()?;
    if url.scheme() != "http" && url.scheme() != "https" || url.host_str().is_none_or(str::is_empty) {
        return None;
    }
    let origin = url.origin().ascii_serialization();
    let path = url.path();
    Some(if path == "/" { origin } else { format!("{origin}{}", path.trim_end_matches('/')) })
}

pub fn add_recent_remote_url(list: &[String], url: &str) -> Vec<String> {
    std::iter::once(url.to_string())
        .chain(list.iter().filter(|item| *item != url).cloned())
        .take(MAX_RECENT_REMOTE_URLS)
        .collect()
}

pub fn remove_recent_remote_url(list: &[String], url: &str) -> Vec<String> {
    list.iter().filter(|item| *item != url).cloned().collect()
}

pub fn local_url(port: u16) -> String {
    format!("http://127.0.0.1:{port}")
}

/// Whether the app should look for a local runtime at all: no forced address and no saved remote one.
pub fn wants_local(cfg: &AppConfig, env: &HashMap<String, String>) -> bool {
    env.get("HARNESSLY_DESKTOP_URL").is_none_or(|v| v.is_empty()) && saved_remote(cfg).is_none()
}

/// HARNESSLY_DESKTOP_URL (e.g. a staging hub) wins over the saved choice; None means "ask".
pub fn resolve_start_url(cfg: &AppConfig, env: &HashMap<String, String>, local_port: Option<u16>) -> Option<String> {
    if let Some(forced) = env.get("HARNESSLY_DESKTOP_URL").and_then(|v| normalize_remote_url(v)) {
        return Some(forced);
    }
    if let Some(remote) = saved_remote(cfg) {
        return normalize_remote_url(remote);
    }
    local_port.map(local_url)
}

pub fn deep_links_enabled(cfg: &AppConfig, env: &HashMap<String, String>) -> bool {
    cfg.deep_links || env.get("HARNESSLY_DESKTOP_DEEPLINKS").is_some_and(|v| v == "1")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn env(pairs: &[(&str, &str)]) -> HashMap<String, String> {
        pairs.iter().map(|(k, v)| (k.to_string(), v.to_string())).collect()
    }

    #[test]
    fn parses_garbage_as_empty() {
        assert_eq!(parse_app_config("not json"), AppConfig::default());
        assert_eq!(parse_app_config("[1,2]"), AppConfig::default());
    }

    #[test]
    fn keeps_unknown_keys_and_reads_electron_files() {
        let raw = r#"{"server":{"mode":"remote","remoteUrl":"https://hub.example.com"},
            "windowState":{"x":10,"y":20,"width":900,"height":700,"isMaximized":true,"displayId":3},"custom":1}"#;
        let cfg = parse_app_config(raw);
        assert_eq!(saved_remote(&cfg), Some("https://hub.example.com"));
        assert!(cfg.window_state.unwrap().is_maximized);
        let back = serde_json::to_value(&cfg).unwrap();
        assert_eq!(back["custom"], 1);
    }

    #[test]
    fn normalizes_addresses() {
        assert_eq!(normalize_remote_url("hub.example.com").as_deref(), Some("https://hub.example.com"));
        assert_eq!(normalize_remote_url(" hub.example.com:8443 ").as_deref(), Some("https://hub.example.com:8443"));
        assert_eq!(normalize_remote_url("http://127.0.0.1:8022/").as_deref(), Some("http://127.0.0.1:8022"));
        assert_eq!(normalize_remote_url("https://Example.com:443/agents/").as_deref(), Some("https://example.com/agents"));
        assert_eq!(normalize_remote_url("ftp://example.com"), None);
        assert_eq!(normalize_remote_url("javascript://x"), None);
        assert_eq!(normalize_remote_url("   "), None);
    }

    #[test]
    fn keeps_recent_list_unique_and_short() {
        let list: Vec<String> = (0..12).map(|i| format!("https://h{i}.example.com")).collect();
        let next = add_recent_remote_url(&list, "https://h5.example.com");
        assert_eq!(next[0], "https://h5.example.com");
        assert_eq!(next.len(), MAX_RECENT_REMOTE_URLS);
        assert_eq!(next.iter().filter(|u| *u == "https://h5.example.com").count(), 1);
        assert!(!remove_recent_remote_url(&next, "https://h0.example.com").contains(&"https://h0.example.com".into()));
    }

    #[test]
    fn resolves_start_url() {
        let remote = AppConfig {
            server: Some(ServerConfig { mode: "remote".into(), remote_url: Some("hub.example.com".into()) }),
            ..Default::default()
        };
        let none = env(&[]);
        assert_eq!(resolve_start_url(&AppConfig::default(), &none, Some(8022)).as_deref(), Some("http://127.0.0.1:8022"));
        assert_eq!(resolve_start_url(&AppConfig::default(), &none, None), None);
        assert_eq!(resolve_start_url(&remote, &none, Some(8022)).as_deref(), Some("https://hub.example.com"));
        let forced = env(&[("HARNESSLY_DESKTOP_URL", "staging.example.com")]);
        assert_eq!(resolve_start_url(&remote, &forced, Some(8022)).as_deref(), Some("https://staging.example.com"));
        assert!(wants_local(&AppConfig::default(), &none));
        assert!(!wants_local(&remote, &none));
        assert!(!wants_local(&AppConfig::default(), &forced));
    }

    #[test]
    fn deep_links_are_opt_in() {
        assert!(!deep_links_enabled(&AppConfig::default(), &env(&[])));
        assert!(deep_links_enabled(&AppConfig::default(), &env(&[("HARNESSLY_DESKTOP_DEEPLINKS", "1")])));
        assert!(deep_links_enabled(&AppConfig { deep_links: true, ..Default::default() }, &env(&[])));
    }
}
