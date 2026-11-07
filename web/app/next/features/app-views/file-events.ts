// The files that changed for the views that watch (hl.files.watch), as the runtime tells them
// (GET /apps/file-events): the session of the view and the names of the files, never what is in them. One stream per
// machine serves every watching view on the page; each hears only its own session.
import { platformPath, runtimePath } from '@/next/core/net/machine';
import type { MachineId } from '@/next/protocol/v2';

export interface IFileChange {
  session: string;
  paths: string[];
}

export const parseFileChange = (data: string): IFileChange | null => {
  let v: unknown;
  try {
    v = JSON.parse(data);
  } catch {
    return null;
  }
  if (typeof v !== 'object' || v === null) return null;
  const { session, paths } = v as Record<string, unknown>;
  if (typeof session !== 'string' || !Array.isArray(paths)) return null;
  return { session, paths: paths.filter((p): p is string => typeof p === 'string') };
};

type TListener = (paths: string[]) => void;

interface IStream {
  source: EventSource;
  bySession: Map<string, Set<TListener>>;
}

const streams = new Map<MachineId, IStream>();

/** Hears the changed files of one view's session; the machine's stream opens with the first and closes with the last. */
export const subscribeFileChanges = (m: MachineId, session: string, fn: TListener): (() => void) => {
  let stream = streams.get(m);
  if (!stream) {
    const bySession = new Map<string, Set<TListener>>();
    const source = new EventSource(runtimePath(platformPath('/apps/file-events'), m), {
      withCredentials: true,
    });
    source.onmessage = (event: MessageEvent<string>) => {
      const change = parseFileChange(event.data);
      if (change) for (const l of [...(bySession.get(change.session) ?? [])]) l(change.paths);
    };
    stream = { source, bySession };
    streams.set(m, stream);
  }
  const held = stream;
  const set = held.bySession.get(session) ?? new Set<TListener>();
  set.add(fn);
  held.bySession.set(session, set);
  return () => {
    set.delete(fn);
    if (set.size === 0) held.bySession.delete(session);
    if (held.bySession.size === 0) {
      held.source.close();
      if (streams.get(m) === held) streams.delete(m);
    }
  };
};
