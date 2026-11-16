//! `harnessly://open/<path>?<query>` becomes an in-app path on the origin the window already shows.
//! The link never names a host, so a crafted link cannot send the window to another site.

use url::Url;

pub const DEEP_LINK_PROTOCOL: &str = "harnessly";

pub fn parse_deep_link(raw: &str) -> Option<String> {
    let url = Url::parse(raw).ok()?;
    if url.scheme() != DEEP_LINK_PROTOCOL || url.host_str() != Some("open") {
        return None;
    }
    let path = percent_decode(if url.path().is_empty() { "/" } else { url.path() })?;
    if path.contains("..") || path.contains('\\') || path.starts_with("//") {
        return None;
    }
    let mut out = if path.starts_with('/') { path } else { format!("/{path}") };
    if let Some(q) = url.query() {
        out.push('?');
        out.push_str(q);
    }
    if let Some(f) = url.fragment() {
        out.push('#');
        out.push_str(f);
    }
    Some(out)
}

pub fn find_deep_link<S: AsRef<str>>(argv: &[S]) -> Option<String> {
    let prefix = format!("{DEEP_LINK_PROTOCOL}://");
    argv.iter().map(AsRef::as_ref).find(|a| a.starts_with(&prefix)).map(str::to_string)
}

pub fn apply_deep_link(origin: &str, raw: &str) -> Option<String> {
    parse_deep_link(raw).map(|path| format!("{}{path}", origin.trim_end_matches('/')))
}

fn percent_decode(s: &str) -> Option<String> {
    let bytes = s.as_bytes();
    let mut out = Vec::with_capacity(bytes.len());
    let mut i = 0;
    while i < bytes.len() {
        if bytes[i] == b'%' {
            let hex = s.get(i + 1..i + 3)?;
            out.push(u8::from_str_radix(hex, 16).ok()?);
            i += 3;
        } else {
            out.push(bytes[i]);
            i += 1;
        }
    }
    String::from_utf8(out).ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_paths() {
        assert_eq!(parse_deep_link("harnessly://open/agents/c/42?x=1#t").as_deref(), Some("/agents/c/42?x=1#t"));
        assert_eq!(parse_deep_link("harnessly://open").as_deref(), Some("/"));
        assert_eq!(parse_deep_link("harnessly://open/a%20b").as_deref(), Some("/a b"));
    }

    #[test]
    fn refuses_escapes() {
        assert_eq!(parse_deep_link("harnessly://evil.example.com/x"), None);
        assert_eq!(parse_deep_link("https://open/x"), None);
        // An encoded dot segment is resolved by the URL parser itself and stays on the same origin.
        assert_eq!(parse_deep_link("harnessly://open/%2e%2e/x").as_deref(), Some("/x"));
        assert_eq!(parse_deep_link("harnessly://open/a/..%2fb"), None);
        assert_eq!(parse_deep_link("harnessly://open/%2F%2Fevil.example.com"), None);
        assert_eq!(parse_deep_link("harnessly://open/a%5Cb"), None);
    }

    #[test]
    fn finds_and_applies() {
        assert_eq!(find_deep_link(&["app", "--flag", "harnessly://open/x"]).as_deref(), Some("harnessly://open/x"));
        assert_eq!(find_deep_link(&["app"]), None);
        assert_eq!(
            apply_deep_link("https://hub.example.com/", "harnessly://open/agents").as_deref(),
            Some("https://hub.example.com/agents")
        );
    }
}
