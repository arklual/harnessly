import { app, BrowserWindow, ipcMain, Menu, nativeTheme, Notification, screen, shell, type IpcMainInvokeEvent } from 'electron';
import { autoUpdater } from 'electron-updater';
import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import {
  addRecentRemoteUrl,
  DEFAULT_WINDOW_STATE,
  normalizeRemoteUrl,
  parseAppConfig,
  resolveStartUrl,
  type AppConfig,
} from './config';
import { buildConnectPage } from './connect-page';
import { applyDeepLink, DEEP_LINK_PROTOCOL, findDeepLink } from './deeplink';
import { ensureLocalRuntime, type LocalRuntime } from './local-runtime';
import { isSafeExternalUrl, isSameOrigin, isThemeSource } from './url-policy';
import { cascadeOffset, restorePosition } from './window-state';

const CONFIG_DIR = process.env.HARNESSLY_DESKTOP_HOME || path.join(os.homedir(), '.harnessly-desktop');
const CONFIG_FILE = path.join(CONFIG_DIR, 'config.json');
const UPDATE_CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000;

const readConfig = (): AppConfig => {
  try {
    return parseAppConfig(fs.readFileSync(CONFIG_FILE, 'utf-8'));
  } catch {
    return {};
  }
};

const writeConfig = (cfg: AppConfig) => {
  try {
    fs.mkdirSync(CONFIG_DIR, { recursive: true });
    const tmp = `${CONFIG_FILE}.tmp`;
    fs.writeFileSync(tmp, JSON.stringify(cfg, null, 2));
    fs.renameSync(tmp, CONFIG_FILE);
  } catch (err) {
    console.error('[desktop] cannot save config:', err);
  }
};

const windows = new Set<BrowserWindow>();
let isQuitting = false;
let localRuntime: LocalRuntime | null = null;
let currentUrl: string | null = null;
let pendingDeepLink: string | null = null;

const isDark = (): boolean => nativeTheme.shouldUseDarkColors;

const saveBounds = (win: BrowserWindow) => {
  if (win.isDestroyed()) return;
  const b = win.getNormalBounds();
  const cfg = readConfig();
  cfg.windowState = {
    ...b,
    isMaximized: win.isMaximized(),
    isFullScreen: win.isFullScreen(),
    displayId: screen.getDisplayMatching(win.getBounds()).id,
  };
  writeConfig(cfg);
};

const createWindow = (url: string | null): BrowserWindow => {
  const saved = readConfig().windowState ?? DEFAULT_WINDOW_STATE;
  const pos = restorePosition(saved, screen.getAllDisplays());
  const offset = cascadeOffset(windows.size);
  const first = windows.size === 0;

  const win = new BrowserWindow({
    width: saved.width,
    height: saved.height,
    x: pos.x != null ? pos.x + offset : undefined,
    y: pos.y != null ? pos.y + offset : undefined,
    minWidth: 800,
    minHeight: 500,
    titleBarStyle: process.platform === 'darwin' ? 'hiddenInset' : 'default',
    backgroundColor: isDark() ? '#09090b' : '#ffffff',
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      nodeIntegration: false,
      contextIsolation: true,
      sandbox: true,
    },
  });
  windows.add(win);
  if (first && saved.isMaximized) win.maximize();
  if (first && saved.isFullScreen) win.setFullScreen(true);

  if (url) void win.loadURL(url);
  else void win.loadURL(`data:text/html;charset=utf-8,${encodeURIComponent(buildConnectPage(readConfig().recentRemoteUrls ?? [], isDark()))}`);

  win.webContents.setWindowOpenHandler(({ url: target }) => {
    if (isSafeExternalUrl(target)) void shell.openExternal(target);
    return { action: 'deny' };
  });
  win.webContents.on('will-navigate', (event, target) => {
    const here = win.webContents.getURL();
    if (here.startsWith('data:') || isSameOrigin(here, target)) return;
    event.preventDefault();
    if (isSafeExternalUrl(target)) void shell.openExternal(target);
  });

  let timer: ReturnType<typeof setTimeout> | null = null;
  const schedule = () => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => saveBounds(win), 500);
  };
  for (const ev of ['resize', 'move', 'maximize', 'unmaximize', 'enter-full-screen', 'leave-full-screen'] as const) {
    win.on(ev as 'resize', schedule);
  }
  win.on('close', (e) => {
    if (timer) clearTimeout(timer);
    saveBounds(win);
    if (process.platform === 'darwin' && !isQuitting && windows.size === 1) {
      e.preventDefault();
      win.hide();
    }
  });
  win.on('closed', () => windows.delete(win));
  return win;
};

const primaryWindow = (): BrowserWindow | null =>
  BrowserWindow.getFocusedWindow() ?? [...windows].find((w) => !w.isDestroyed()) ?? null;

const handleDeepLink = (raw: string) => {
  if (!currentUrl) {
    pendingDeepLink = raw;
    return;
  }
  const target = applyDeepLink(new URL(currentUrl).origin, raw);
  if (!target) return;
  const win = primaryWindow() ?? createWindow(null);
  void win.loadURL(target);
  win.show();
  win.focus();
};

const connect = (rawUrl: string, win: BrowserWindow | null): boolean => {
  const url = normalizeRemoteUrl(rawUrl);
  if (!url) return false;
  const cfg = readConfig();
  cfg.server = { mode: 'remote', remoteUrl: url };
  cfg.recentRemoteUrls = addRecentRemoteUrl(cfg.recentRemoteUrls, url);
  writeConfig(cfg);
  currentUrl = url;
  void win?.loadURL(url);
  return true;
};

const setupAutoUpdater = () => {
  const feed = process.env.HARNESSLY_UPDATE_URL;
  if (!feed || !app.isPackaged) return;
  autoUpdater.setFeedURL({ provider: 'generic', url: feed });
  autoUpdater.autoDownload = true;
  autoUpdater.autoInstallOnAppQuit = true;
  autoUpdater.on('error', (err) => console.error('[desktop] update error:', err.message));
  const check = () => autoUpdater.checkForUpdates().catch((err: Error) => console.error('[desktop] update check:', err.message));
  setTimeout(check, 3000);
  setInterval(check, UPDATE_CHECK_INTERVAL_MS);
  ipcMain.handle('check-for-updates', async () => {
    const result = await autoUpdater.checkForUpdates();
    return result?.updateInfo.version ?? null;
  });
};

const fromWindow = (event: IpcMainInvokeEvent) => BrowserWindow.fromWebContents(event.sender);

ipcMain.handle('open-external', (_e, url: string) => {
  if (isSafeExternalUrl(url)) void shell.openExternal(url);
});
ipcMain.handle('set-native-theme', (_e, theme: string) => {
  if (isThemeSource(theme)) nativeTheme.themeSource = theme;
});
ipcMain.handle('set-locale', (_e, locale: string) => {
  if (typeof locale === 'string') writeConfig({ ...readConfig(), locale });
});
ipcMain.handle('show-notification', (event, title: string, body: string) => {
  if (BrowserWindow.getAllWindows().some((w) => !w.isDestroyed() && w.isFocused())) return false;
  const sender = fromWindow(event);
  const n = new Notification({ title: String(title), body: String(body) });
  n.on('click', () => {
    const target = sender && !sender.isDestroyed() ? sender : primaryWindow();
    target?.show();
    target?.focus();
    target?.webContents.send('notification-click');
  });
  n.show();
  return true;
});
ipcMain.handle('open-new-window', () => {
  if (currentUrl) createWindow(currentUrl);
});
// ⌘W / Ctrl+W belongs to the page: it closes the chats one at a time and asks for this once none is left.
ipcMain.handle('close-window', (event) => {
  fromWindow(event)?.close();
});
ipcMain.handle('set-dock-badge', (_e, count: number) => {
  if (process.platform === 'darwin') app.dock?.setBadge(count > 0 ? String(count) : '');
});
ipcMain.handle('get-system-resources', () => {
  let cpu = 0;
  let memory = 0;
  for (const m of app.getAppMetrics()) {
    cpu += m.cpu.percentCPUUsage;
    memory += m.memory.workingSetSize * 1024;
  }
  return { cpu, memory: { used: memory } };
});
ipcMain.handle('desktop:connect', (event, url: string) => connect(String(url), fromWindow(event)));

const bootstrap = async () => {
  const cfg = readConfig();
  const wantsLocal = !process.env.HARNESSLY_DESKTOP_URL && (cfg.server?.mode ?? 'local') === 'local';
  localRuntime = wantsLocal ? await ensureLocalRuntime(process.env) : null;
  currentUrl = resolveStartUrl(cfg, process.env, localRuntime?.port ?? null);

  if (cfg.deepLinks || process.env.HARNESSLY_DESKTOP_DEEPLINKS === '1') {
    app.setAsDefaultProtocolClient(DEEP_LINK_PROTOCOL);
  }
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    { role: 'appMenu' }, { role: 'editMenu' }, { role: 'viewMenu' },
    // The stock Window menu binds Ctrl+W to «Close» on Windows and Linux, which would take the key from the page.
    {
      label: 'Window',
      role: 'window',
      submenu: process.platform === 'darwin'
        ? [{ role: 'minimize' }, { role: 'zoom' }, { type: 'separator' }, { role: 'front' }]
        : [{ role: 'minimize' }],
    },
  ]));
  createWindow(currentUrl);
  const initial = pendingDeepLink ?? findDeepLink(process.argv);
  pendingDeepLink = null;
  if (initial) handleDeepLink(initial);
  setupAutoUpdater();
};

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on('second-instance', (_e, argv) => {
    const link = findDeepLink(argv);
    if (link) handleDeepLink(link);
    const win = primaryWindow();
    win?.show();
    win?.focus();
  });
  app.on('open-url', (event, url) => {
    event.preventDefault();
    handleDeepLink(url);
  });
  app.on('ready', () => void bootstrap());
  app.on('activate', () => {
    const win = primaryWindow();
    if (win) win.show();
    else if (currentUrl) createWindow(currentUrl);
  });
  app.on('window-all-closed', () => {
    if (process.platform !== 'darwin') app.quit();
  });
  app.on('before-quit', () => {
    isQuitting = true;
    localRuntime?.stop();
  });
}
