# Harnessly desktop

Electron shell that opens a Harnessly runtime or hub in a native window. Ported from the private platform's desktop app.

- **Local mode** attaches to a runtime already listening on `127.0.0.1:8022` (`HARNESSLY_DESKTOP_PORT` overrides). Set `HARNESSLY_RUNTIME_BIN` to let the app start and stop its own runtime.
- **Remote mode**: `HARNESSLY_DESKTOP_URL=https://hub.example.com`, or pick an address on the connect page (saved in `~/.harnessly-desktop/config.json`, directory overridable with `HARNESSLY_DESKTOP_HOME`).
- **Deep links** `harnessly://open/<path>` open a path on the origin the window already shows. Off by default: set `"deepLinks": true` in the config or `HARNESSLY_DESKTOP_DEEPLINKS=1`.
- **Auto-update** runs only in a packaged app with `HARNESSLY_UPDATE_URL` (a generic update feed). Nothing is contacted otherwise.
- **Preload API** `window.electronAPI`: openExternal, notifications, dock badge, new window, theme, locale, system resources, update check.

```bash
ELECTRON_SKIP_BINARY_DOWNLOAD=1 pnpm install --ignore-workspace
pnpm typecheck && pnpm test
pnpm build            # tsc to dist/
pnpm start            # needs the Electron binary (plain `pnpm install` without the skip variable)
HARNESSLY_DESKTOP_APP_ID=com.example.harnessly HARNESSLY_UPDATE_URL=https://updates.example.com pnpm dist
```

Not yet ported: the embedded browser tab bridge (webview + CDP), the macOS launch agent for the server, installer icons and notarization.
