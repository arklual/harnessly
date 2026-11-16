# Harnessly native app

A native desktop app for Harnessly written in Rust on [Tauri 2](https://v2.tauri.app). The window shows the same web
interface as the browser, served by the runtime or hub you connect to, so it looks and behaves exactly like the site.
The shell around it is small: it starts in under a second and uses the system's web view (WKWebView on macOS, WebView2
on Windows, WebKitGTK on Linux) instead of shipping a browser.

It replaces nothing yet: the Electron app in `desktop/` stays. The two share the config file
`~/.harnessly-desktop/config.json`, and this app gives the page the same `window.electronAPI` bridge.

## What it does

- **Which server**: `HARNESSLY_DESKTOP_URL` if set; else the saved remote server; else a local runtime that answers on
  `HARNESSLY_DESKTOP_PORT` (default 8022), started from `HARNESSLY_RUNTIME_BIN` when none runs; else a connect page
  with the recent servers. «Switch Server…» (⇧⌘K on macOS) brings the connect page back.
- **Links**: every http(s) page stays in the window (sign-in through an identity provider works); `mailto:` and the like
  open in the system; a page's `window.open` and `target=_blank` open in the default browser.
- **Bridge** (`src/bridge.js`): `openExternal`, `showNotification` (only while no window of the app is focused),
  `openNewWindow`, `closeWindow` (⌘W stays with the page), `setDockBadge`, `setLocale`, `setNativeTheme`. Only pages of
  the server the app opened may call it.
- **Windows**: size, position, maximized and full-screen state are kept; a position off every display is dropped; new
  windows cascade. On macOS closing the last window keeps the app in the Dock.
- **Deep links** `harnessly://open/<path>` when `deepLinks` is on in the config or `HARNESSLY_DESKTOP_DEEPLINKS=1`.

## Build

You need Rust (stable) and, on Linux, WebKitGTK:

```sh
sudo apt-get install libwebkit2gtk-4.1-dev libsoup-3.0-dev librsvg2-dev   # Debian, Ubuntu
cargo install tauri-cli --version "^2" --locked
cd native
cargo test                                   # the logic: config, links, deep links, windows, local runtime
cargo tauri dev                              # run it
cargo tauri build                            # installers in target/release/bundle/
cargo tauri build --target universal-apple-darwin --bundles dmg   # one .dmg for Apple silicon and Intel
```

`cargo run` works too (without the installers).

The GitHub workflow `.github/workflows/native.yml` builds the .dmg, .msi/.exe, .deb and .AppImage on every change of
`native/` and on demand (Actions → native → Run workflow); they are the run's artifacts.

### macOS without a Developer ID

The .dmg is signed ad hoc. The first time, macOS says the app «cannot be opened»: open System Settings → Privacy &
Security → «Open Anyway», or run

```sh
xattr -dr com.apple.quarantine /Applications/Harnessly.app
```

Signing with a Developer ID and notarization need `APPLE_CERTIFICATE`, `APPLE_SIGNING_IDENTITY`, `APPLE_ID`,
`APPLE_PASSWORD` and `APPLE_TEAM_ID` in the workflow; Tauri does the rest.

## Mobile

Tauri 2 builds the same crate for iOS and Android (`src/lib.rs` is the mobile entry point; the local runtime and the
single-instance lock are desktop only):

```sh
cargo tauri ios init && cargo tauri ios dev       # Xcode on a Mac
cargo tauri android init && cargo tauri android dev   # Android SDK and NDK
```

## Not yet ported from the Electron app

- automatic updates (Tauri's updater plugin needs a signed update feed);
- focusing the chat on a notification click;
- the embedded browser bridge of the right pane;
- Developer ID signing and notarization.
