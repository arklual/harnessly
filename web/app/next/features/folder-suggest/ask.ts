// The folder the app offers for a chat that has none: the page sends the person's prompts of the chat and the
// folders with some of their chats' titles to the machine (POST <platform>/folder-suggest), which asks the model picked
// for «Папка для чата» and answers with one of the folders, or none.
import { deviceId } from '@/next/core/device';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { DEVICE_HEADER, type MachineId } from '@/next/protocol/v2';

export interface IFolderInfo {
  id: string;
  name: string;
  titles: string[];
}

/** The prompts a chat needs before the app offers a folder, and the second try when the first found none. */
export const ASK_AT = [3, 6] as const;

/** The folder id the model picked, null when none fits or nobody could answer. */
export const askFolder = async (
  machine: MachineId,
  body: { chat: string; prompts: string[]; folders: IFolderInfo[] },
): Promise<string | null> => {
  try {
    const res = await fetch(runtimePath(platformPath('/folder-suggest'), machine), {
      method: 'POST',
      credentials: 'same-origin',
      headers: { [DEVICE_HEADER]: deviceId(), 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!res.ok) return null;
    const doc: unknown = await res.json().catch(() => null);
    const id = typeof doc === 'object' && doc !== null ? (doc as Record<string, unknown>).folderId : null;
    return typeof id === 'string' && body.folders.some((f) => f.id === id) ? id : null;
  } catch {
    return null;
  }
};

const KEY = 'hx-folder-suggest:';

/** What was done for a chat: how many prompts it had when asked last, and whether the person said no. */
export interface IAsked {
  at: number;
  no: boolean;
}

export const readAsked = (chat: string): IAsked | null => {
  try {
    const raw = window.localStorage.getItem(KEY + chat);
    if (!raw) return null;
    const v: unknown = JSON.parse(raw);
    if (typeof v !== 'object' || v === null) return null;
    const r = v as Record<string, unknown>;
    return { at: typeof r.at === 'number' ? r.at : 0, no: r.no === true };
  } catch {
    return null;
  }
};

export const keepAsked = (chat: string, asked: IAsked): void => {
  try {
    window.localStorage.setItem(KEY + chat, JSON.stringify(asked));
  } catch {
    // Held for this page only.
  }
};

/** The next number of prompts the chat waits for before it is asked about, or null when it was asked at every step. */
export const nextAsk = (asked: IAsked | null): number | null => {
  if (asked?.no) return null;
  return ASK_AT.find((n) => n > (asked?.at ?? 0)) ?? null;
};
