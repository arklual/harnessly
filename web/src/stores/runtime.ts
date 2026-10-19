import { socketUrl } from '../api/base';
import { bootstrap, layout as layoutApi, workspaces as workspacesApi } from '../api/endpoints';
import { HttpError } from '../api/http';
import { ReconnectingSocket, type SocketState } from '../api/socket';
import { emptyStatus, reduceStatus, type StatusState } from '../api/status';
import type { Layout, WorkspaceList } from '../api/types';
import { createStore } from './store';

// The page's live view of the runtime: the workspace list and the layouts it has opened, refetched when the sync
// socket says they changed, and the tabs' states from the status socket.

export const workspacesStore = createStore<WorkspaceList | null>(null);
export const layoutsStore = createStore<Record<string, Layout>>({});
export const statusStore = createStore<StatusState>(emptyStatus);
export const connectionStore = createStore<{ sync: SocketState; status: SocketState }>({
  sync: 'connecting',
  status: 'connecting',
});
export const errorStore = createStore<string | null>(null);

const watched = new Set<string>();
const pending = new Map<string, ReturnType<typeof setTimeout>>();
let sockets: ReconnectingSocket[] = [];
let heartbeat: ReturnType<typeof setInterval> | undefined;

export function reportError(err: unknown) {
  const message = err instanceof Error ? err.message : String(err);
  errorStore.set(message);
}

export async function refreshWorkspaces() {
  try {
    workspacesStore.set(await workspacesApi.list());
  } catch (err) {
    if (!(err instanceof HttpError && err.status === 401)) reportError(err);
  }
}

/** Keeps a workspace's layout loaded and fresh. */
export function watchLayout(workspaceId: string) {
  watched.add(workspaceId);
  void refreshLayout(workspaceId);
}

export function unwatchLayout(workspaceId: string) {
  watched.delete(workspaceId);
}

export function applyLayout(workspaceId: string, next: Layout) {
  layoutsStore.set((all) => ({ ...all, [workspaceId]: next }));
}

export async function refreshLayout(workspaceId: string) {
  try {
    applyLayout(workspaceId, await layoutApi.get(workspaceId));
  } catch (err) {
    if (!(err instanceof HttpError && err.status === 401)) reportError(err);
  }
}

// Tab states are written to the layout often (every hook): refetches of one workspace are coalesced.
function layoutChanged(workspaceId: string) {
  if (!watched.has(workspaceId) || pending.has(workspaceId)) return;
  pending.set(
    workspaceId,
    setTimeout(() => {
      pending.delete(workspaceId);
      void refreshLayout(workspaceId);
    }, 80),
  );
}

function parse(data: string | ArrayBuffer): { type?: unknown; [k: string]: unknown } | null {
  if (typeof data !== 'string') return null;
  try {
    return JSON.parse(data) as { type?: unknown };
  } catch {
    return null;
  }
}

export function startRuntime() {
  stopRuntime();
  const sync = new ReconnectingSocket({
    url: () => socketUrl('/api/sync'),
    onState: (s) => connectionStore.set((c) => ({ ...c, sync: s })),
    // Nothing is replayed on the sync socket: what changed while it was away is fetched again.
    onOpen: () => {
      void refreshWorkspaces();
      watched.forEach((id) => void refreshLayout(id));
    },
    onMessage: (data) => {
      const msg = parse(data);
      if (msg?.type === 'layout' && typeof msg.workspaceId === 'string') layoutChanged(msg.workspaceId);
      else if (msg?.type === 'workspace') void refreshWorkspaces();
    },
  });
  const status = new ReconnectingSocket({
    url: () => socketUrl('/api/status'),
    onState: (s) => connectionStore.set((c) => ({ ...c, status: s })),
    onMessage: (data) => {
      const msg = parse(data);
      if (msg) statusStore.set((s) => reduceStatus(s, msg));
    },
  });
  sockets = [sync, status];
  heartbeat = setInterval(() => sockets.forEach((s) => s.send('{"type":"ping"}')), 20_000);
  void firstLoad();
}

// The first load goes through the bootstrap route, which makes the machine's first workspace when it has none; it
// answers 503 while the runtime's core is still starting.
async function firstLoad(tries = 30) {
  try {
    const b = await bootstrap();
    workspacesStore.set((cur) => cur ?? b.initialWorkspace);
    void refreshWorkspaces();
  } catch (err) {
    if (err instanceof HttpError && err.status === 503 && tries > 0) {
      setTimeout(() => void firstLoad(tries - 1), 1000);
      return;
    }
    void refreshWorkspaces();
  }
}

export function stopRuntime() {
  sockets.forEach((s) => s.close());
  sockets = [];
  clearInterval(heartbeat);
  pending.forEach((t) => clearTimeout(t));
  pending.clear();
}

/** Tells the status socket that a finished agent tab has been looked at. */
export function dismissTab(tabId: string) {
  sockets[1]?.send(JSON.stringify({ type: 'status:tab-dismissed', tabId }));
}
