// The apps turned on in chats, as the sidebar and the chat's own row list them. The list a chat keeps is the whole list
// (the `conversation.apps` command replaces it), so a change is computed here and sent whole.
import type { IInstalledApp } from '@/next/features/app-views/api';

/** The chat's apps with one turned on or off; the order of the rest is kept, a new one goes last. */
export const withApp = (apps: readonly string[], id: string, on: boolean): string[] => {
  const rest = apps.filter((a) => a !== id);
  return on ? [...rest, id] : rest;
};

export interface IActiveApp {
  app: IInstalledApp;
  /** The chat it was last on in: the most recent of the chats it is on in. */
  chat: string;
}

/**
 * The installed apps that are on in some chat, each with its most recent chat. `chats` come most recent first (as the
 * stores list them); an app that is on in a chat but no longer installed is not listed.
 */
export const activeApps = (
  chats: readonly { id: string; apps?: readonly string[] }[],
  installed: readonly IInstalledApp[],
): IActiveApp[] => {
  const byId = new Map(installed.map((a) => [a.id, a]));
  const seen = new Map<string, IActiveApp>();
  for (const chat of chats)
    for (const id of chat.apps ?? []) {
      const app = byId.get(id);
      if (app && !seen.has(id)) seen.set(id, { app, chat: chat.id });
    }
  return [...seen.values()];
};

/** The apps a chat can turn on or off: the installed ones that work in a chat. */
export const offeredApps = (installed: readonly IInstalledApp[]): IInstalledApp[] =>
  installed.filter((a) => a.targets.includes('chat'));
