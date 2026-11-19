import { contextBridge, ipcRenderer, webUtils } from 'electron';

// The page's bridge to the desktop shell. Each call is one IPC request that the main process answers (main.ts);
// the table keeps a call's name, its channel and its arguments on one line.
const ask =
  <A extends unknown[]>(channel: string) =>
  (...args: A): Promise<unknown> =>
    ipcRenderer.invoke(channel, ...args);

const bridge = {
  isElectron: true,
  platform: process.platform,
  getPathForFile: (file: File) => webUtils.getPathForFile(file),
  openExternal: ask<[url: string]>('open-external'),
  getSystemResources: ask<[]>('get-system-resources'),
  showNotification: ask<[title: string, body: string]>('show-notification'),
  openNewWindow: ask<[]>('open-new-window'),
  closeWindow: ask<[]>('close-window'),
  setDockBadge: ask<[count: number]>('set-dock-badge'),
  setLocale: ask<[locale: string]>('set-locale'),
  setNativeTheme: ask<[theme: string]>('set-native-theme'),
  connect: ask<[url: string]>('desktop:connect'),
  checkForUpdates: ask<[]>('check-for-updates'),
  /** Calls back on a click on the shell's notification; the returned function stops it. */
  onNotificationClick(callback: () => void): () => void {
    const listener = () => callback();
    ipcRenderer.on('notification-click', listener);
    return () => {
      ipcRenderer.removeListener('notification-click', listener);
    };
  },
};

contextBridge.exposeInMainWorld('electronAPI', bridge);
