// window.electronAPI for the Harnessly web page: the same surface the Electron shell's preload gives, so the page
// needs no second code path. Calls from a page outside the origin the app was pointed at are refused by the ACL.
(function () {
  if (window.electronAPI) return;
  var invoke = function (cmd, args) {
    var ipc = window.__TAURI_INTERNALS__;
    return ipc ? ipc.invoke(cmd, args || {}) : Promise.reject(new Error('desktop bridge unavailable'));
  };
  var ua = navigator.userAgent;
  var platform = /Mac/.test(ua) ? 'darwin' : /Win/.test(ua) ? 'win32' : 'linux';
  // The pane's browser on this computer (pane_browser.rs): a site of the browser tab opens in a webview of the window,
  // over the box the page keeps for it. Its moves come back as the 'harnessly:pane-browser' event of the window.
  var paneBrowser = window.__HARNESSLY_PANE_BROWSER__ === true ? Object.freeze({
    open: function (tab, url, script, bounds) {
      return invoke('pane_browser_open', { tab: String(tab), url: String(url), script: String(script), bounds: bounds });
    },
    bounds: function (tab, bounds, visible) {
      return invoke('pane_browser_bounds', { tab: String(tab), bounds: bounds, visible: visible === true });
    },
    go: function (tab, url) { return invoke('pane_browser_go', { tab: String(tab), url: String(url) }); },
    history: function (tab, step) { return invoke('pane_browser_history', { tab: String(tab), step: Number(step) | 0 }); },
    close: function (tab) { return invoke('pane_browser_close', { tab: String(tab) }); },
    act: function (tab, message, timeoutMs) {
      return invoke('pane_browser_act', { tab: String(tab), message: message, timeoutMs: Number(timeoutMs) || 0 });
    },
  }) : null;
  var api = {
    isElectron: false,
    isDesktop: true,
    shell: 'tauri',
    platform: platform,
    openExternal: function (url) { return invoke('open_external', { url: String(url) }); },
    showNotification: function (title, body) {
      return invoke('show_notification', { title: String(title), body: String(body) });
    },
    openNewWindow: function () { return invoke('open_new_window'); },
    closeWindow: function () { return invoke('close_window'); },
    setDockBadge: function (count) { return invoke('set_dock_badge', { count: Number(count) || 0 }); },
    setLocale: function (locale) { return invoke('set_locale', { locale: String(locale) }); },
    setNativeTheme: function (theme) { return invoke('set_native_theme', { theme: String(theme) }); },
    checkForUpdates: function () { return Promise.resolve(null); },
    // Desktop notifications carry no click callback here; the subscription is kept so callers need no branch.
    onNotificationClick: function () { return function () {}; },
    paneBrowser: paneBrowser,
  };
  Object.defineProperty(window, 'electronAPI', { value: Object.freeze(api), configurable: false, writable: false });
})();
