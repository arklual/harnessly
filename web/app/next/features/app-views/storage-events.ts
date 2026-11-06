// The changes of the app storage, as the runtime tells them (GET /apps/events): which key of which area of which app,
// never the value. One stream per machine serves every view open on the page; a view hears only what is its own
// (`reaches`), and answers by reading the key again.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

export interface IStorageChange {
  app: string;
  scope: string;
  /** The chat or document of an area per chat or document; empty for the other scopes. */
  owner: string;
  key: string;
}

export const parseStorageChange = (data: string): IStorageChange | null => {
  let v: unknown;
  try {
    v = JSON.parse(data);
  } catch {
    return null;
  }
  if (typeof v !== 'object' || v === null) return null;
  const r = v as Record<string, unknown>;
  const { app, scope, owner, key } = r;
  if (typeof app !== 'string' || typeof scope !== 'string' || typeof key !== 'string') return null;
  return { app, scope, owner: typeof owner === 'string' ? owner : '', key };
};

export interface IStorageViewer {
  app: { id: string; permissions: readonly string[] };
  /** The chat the view is in (null if it has none or the app is not on there). */
  chat: string | null;
  /** The document a Sinopia view is bound to. */
  document?: string | null;
}

/**
 * Whether a change is one the view may be told of: its own app's, from a view that holds the storage right, and for an
 * area per chat or document only that of the chat or document the view is in. Another chat's key is not told.
 */
export const reaches = (c: IStorageChange, v: IStorageViewer): boolean => {
  if (c.app !== v.app.id || !v.app.permissions.includes('storage')) return false;
  switch (c.scope) {
    case 'install':
    case 'user':
    case 'space':
      return true;
    case 'chat':
      return v.chat !== null && c.owner === v.chat;
    case 'doc':
      return v.document !== undefined && v.document !== null && c.owner === v.document;
    default:
      return false;
  }
};

type TListener = (change: IStorageChange) => void;

interface IStream {
  source: EventSource;
  listeners: Set<TListener>;
}

const streams = new Map<MachineId, IStream>();

/** Listens to the storage changes of a machine; the stream opens with the first listener and closes with the last. */
export const subscribeStorageChanges = (m: MachineId, fn: TListener): (() => void) => {
  let stream = streams.get(m);
  if (!stream) {
    const listeners = new Set<TListener>();
    const source = new EventSource(runtimePath(platformPath('/apps/events'), m), { withCredentials: true });
    source.onmessage = (event: MessageEvent<string>) => {
      const change = parseStorageChange(event.data);
      if (change) for (const l of [...listeners]) l(change);
    };
    stream = { source, listeners };
    streams.set(m, stream);
  }
  stream.listeners.add(fn);
  const held = stream;
  return () => {
    held.listeners.delete(fn);
    if (held.listeners.size === 0) {
      held.source.close();
      if (streams.get(m) === held) streams.delete(m);
    }
  };
};
