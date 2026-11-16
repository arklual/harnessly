//! Which addresses stay in the window, which go to the system browser and which are dropped.

use url::Url;

const BLOCKED_SCHEMES: [&str; 7] = ["javascript", "data", "vbscript", "blob", "file", "about", "view-source"];

/// An address the system may open: any scheme except the ones that run code or read local files.
pub fn is_safe_external_url(raw: &str) -> bool {
    match Url::parse(raw) {
        Ok(url) => !BLOCKED_SCHEMES.contains(&url.scheme()),
        Err(_) => false,
    }
}

pub fn is_theme_source(value: &str) -> bool {
    matches!(value, "dark" | "light" | "system")
}

/// The app's own pages: `tauri://localhost` on macOS and Linux, `http(s)://tauri.localhost` on Windows and Android.
pub fn is_app_asset(url: &Url) -> bool {
    url.scheme() == "tauri" || (matches!(url.scheme(), "http" | "https") && url.host_str() == Some("tauri.localhost"))
}

#[derive(Debug, PartialEq, Eq)]
pub enum Navigation {
    /// Load in the window.
    Stay,
    /// Hand to the system (mail, other apps' schemes).
    External,
    /// Drop.
    Deny,
}

/// The window behaves like a browser tab of the site: web addresses load in it, so a sign-in through another origin
/// (an identity provider) works. The desktop bridge is scoped to the configured origin by the ACL, so a foreign page
/// in the window gets no native calls.
pub fn navigation(target: &Url) -> Navigation {
    if is_app_asset(target) || matches!(target.scheme(), "http" | "https") {
        Navigation::Stay
    } else if is_safe_external_url(target.as_str()) {
        Navigation::External
    } else {
        Navigation::Deny
    }
}

/// The ACL pattern for the bridge capability: every page of the origin the window was pointed at.
pub fn origin_pattern(raw: &str) -> Option<String> {
    let url = Url::parse(raw).ok()?;
    url.origin().is_tuple().then(|| format!("{}/*", url.origin().ascii_serialization()))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn external_urls() {
        assert!(is_safe_external_url("https://example.com"));
        assert!(is_safe_external_url("mailto:a@example.com"));
        assert!(!is_safe_external_url("javascript:alert(1)"));
        assert!(!is_safe_external_url("file:///etc/passwd"));
        assert!(!is_safe_external_url("data:text/html,x"));
        assert!(!is_safe_external_url("not a url"));
    }

    #[test]
    fn origins() {
        assert_eq!(origin_pattern("https://hub.example.com/agents").as_deref(), Some("https://hub.example.com/*"));
        assert_eq!(origin_pattern("http://127.0.0.1:8022").as_deref(), Some("http://127.0.0.1:8022/*"));
        assert_eq!(origin_pattern("data:x"), None);
    }

    #[test]
    fn navigation_policy() {
        let nav = |s: &str| navigation(&Url::parse(s).unwrap());
        assert_eq!(nav("https://id.example.com/login"), Navigation::Stay);
        assert_eq!(nav("tauri://localhost/index.html"), Navigation::Stay);
        assert_eq!(nav("http://tauri.localhost/index.html"), Navigation::Stay);
        assert_eq!(nav("mailto:a@example.com"), Navigation::External);
        assert_eq!(nav("javascript:alert(1)"), Navigation::Deny);
        assert_eq!(nav("file:///etc/passwd"), Navigation::Deny);
    }

    #[test]
    fn themes() {
        assert!(is_theme_source("dark") && is_theme_source("light") && is_theme_source("system"));
        assert!(!is_theme_source("blue"));
    }
}
