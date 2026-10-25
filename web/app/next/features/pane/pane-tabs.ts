// The pane's own tabs of a chat, and where a request to show something (the agent's `open`, what the runtime saw it
// work on) puts it among them. Pure: the binding (right-pane.tsx) keeps the state and runs the effects.
import { parseAddress, type IAddress } from '@/next/features/pane/address';
import type { IPaneRequest, TLegacyKind } from '@/next/features/pane/intents';

export type TTab =
  | { id: string; kind: 'side' }
  | { id: string; kind: 'plan' }
  | { id: string; kind: 'machine' }
  | { id: string; kind: 'designly'; design: string | null }
  | { id: string; kind: 'subagent'; focus: string | null }
  | { id: string; kind: 'files'; path: string; file: boolean }
  | { id: string; kind: 'shell'; terminal: string }
  /** `reload` counts the reloads asked from outside the tab (a page the agent published again). */
  | { id: string; kind: 'browser'; hist: IAddress[]; hi: number; reload?: number };

export interface IPaneTabs {
  tabs: TTab[];
  /** The tab in front; '' or the tools' id: the tools. */
  active: string;
  /** Opened behind the tab the person was in and not looked at yet. */
  unread: string[];
}

/** What the binding does besides the tabs: a tab of Legacy's pane, the agent's terminal, the side chat. */
export type TPaneEffect =
  | { k: 'legacy'; kind: TLegacyKind; target: string | null; background: boolean; reload: boolean }
  | { k: 'terminal' }
  | { k: 'side' };

export interface IPlaced {
  next: IPaneTabs;
  effect: TPaneEffect | null;
  /** The pane comes out for it (it was asked to open something, not only to reload it). */
  show: boolean;
}

export interface IPlaceContext {
  /** The person is busy in a tab of the open pane: what comes goes behind it. */
  busy: boolean;
  /** The chat's folder: a file in it opens in the pane's own files tab. */
  cwd: string;
  nextId: () => string;
}

const TOOLS = 'tools' as const;

/** A path inside the chat's folder, relative to it; null for one outside. */
const inFolder = (cwd: string, path: string): string | null => {
  const root = cwd.replace(/\/+$/, '');
  if (path === root) return '';
  return path.startsWith(`${root}/`) ? path.slice(root.length + 1) : null;
};

/** A path names a file when its last part has an extension; a folder otherwise. */
const isFilePath = (path: string): boolean => /\.[^./]+$/.test(path.split('/').at(-1) ?? '');

const front = (state: IPaneTabs, id: string, busy: boolean): IPaneTabs =>
  busy && state.active !== id
    ? { ...state, unread: state.unread.includes(id) ? state.unread : [...state.unread, id] }
    : { ...state, active: id, unread: state.unread.filter((x) => x !== id) };

const added = (state: IPaneTabs, tab: TTab, busy: boolean): IPaneTabs =>
  front({ ...state, tabs: [...state.tabs, tab] }, tab.id, busy);

const replaced = (state: IPaneTabs, tab: TTab): IPaneTabs => ({
  ...state,
  tabs: state.tabs.map((x) => (x.id === tab.id ? tab : x)),
});

/** The tab is a browser that stands on this address now. */
export const browserAt = (tab: TTab, text: string): boolean =>
  tab.kind === 'browser' && tab.hist[tab.hi]?.text === text;

const nothing = (state: IPaneTabs): IPlaced => ({ next: state, effect: null, show: false });

const placeBrowser = (
  state: IPaneTabs,
  target: string | null,
  reload: boolean,
  ctx: IPlaceContext,
): IPlaced => {
  const address = target ? parseAddress(target) : null;
  const have = address ? state.tabs.find((x) => browserAt(x, address.text)) : undefined;
  if (have?.kind === 'browser') {
    const next = reload ? replaced(state, { ...have, reload: (have.reload ?? 0) + 1 }) : state;
    return { next: reload ? next : front(next, have.id, ctx.busy), effect: null, show: !reload };
  }
  if (reload) return nothing(state);
  // Asked for a browser with no page: the new tab that is there already is looked at, not stacked on.
  const blank = address ? undefined : state.tabs.find((x) => x.kind === 'browser' && x.hi < 0);
  if (blank) return { next: front(state, blank.id, ctx.busy), effect: null, show: true };
  const tab: TTab = {
    id: ctx.nextId(),
    kind: 'browser',
    hist: address ? [address] : [],
    hi: address ? 0 : -1,
  };
  return { next: added(state, tab, ctx.busy), effect: null, show: true };
};

const placeFiles = (state: IPaneTabs, target: string | null, ctx: IPlaceContext): IPlaced => {
  const rel = target === null ? '' : inFolder(ctx.cwd, target);
  // A file outside the chat's folder is Legacy's to show: the pane's own files tab reads only the folder.
  if (rel === null)
    return {
      next: state,
      effect: { k: 'legacy', kind: 'files', target, background: ctx.busy, reload: false },
      show: true,
    };
  const file = rel !== '' && isFilePath(rel);
  const have = state.tabs.find((x) => x.kind === 'files' && x.path === rel);
  if (have) return { next: front(state, have.id, ctx.busy), effect: null, show: true };
  return {
    next: added(state, { id: ctx.nextId(), kind: 'files', path: rel, file }, ctx.busy),
    effect: null,
    show: true,
  };
};

const singleTab = (kind: 'plan' | 'subagent' | 'designly', id: string, design: string | null): TTab => {
  if (kind === 'designly') return { id, kind, design };
  if (kind === 'subagent') return { id, kind, focus: null };
  return { id, kind };
};

const placeSingle = (
  state: IPaneTabs,
  kind: 'plan' | 'subagent' | 'designly',
  ctx: IPlaceContext,
  design: string | null,
): IPlaced => {
  const have = state.tabs.find((x) => x.kind === kind);
  if (have?.kind === 'designly') {
    // The design asked for comes in the tab of designs; without one, the tab stays where it is.
    const next = design !== null && have.design !== design ? replaced(state, { ...have, design }) : state;
    return { next: front(next, have.id, ctx.busy), effect: null, show: true };
  }
  if (have?.kind === 'subagent' && have.focus !== null) {
    // Asked for the list of subagents: a card an earlier «Открыть» put in focus does not stay open under it.
    return {
      next: front(replaced(state, { ...have, focus: null }), have.id, ctx.busy),
      effect: null,
      show: true,
    };
  }
  if (have) return { next: front(state, have.id, ctx.busy), effect: null, show: true };
  return { next: added(state, singleTab(kind, ctx.nextId(), design), ctx.busy), effect: null, show: true };
};

/**
 * Where a request goes: a tab of its own kind, focused when the person is not busy in another one, created when there
 * is none; a reload only reloads a tab that is there. Legacy's tools, the terminal and the side chat are effects.
 */
export const placeRequest = (state: IPaneTabs, request: IPaneRequest, ctx: IPlaceContext): IPlaced => {
  const { reload } = request;
  switch (request.k) {
    case 'show':
      return { next: state, effect: null, show: true };
    case 'tools':
      if (reload) return nothing(state);
      return { next: ctx.busy ? state : { ...state, active: TOOLS }, effect: null, show: true };
    case 'browser':
      return placeBrowser(state, request.target, reload, ctx);
    case 'design':
      if (reload) return nothing(state);
      return placeSingle(state, 'designly', ctx, request.target);
    case 'files':
      if (reload) return nothing(state);
      return placeFiles(state, request.target, ctx);
    case 'plan':
    case 'subagent':
      if (reload) return nothing(state);
      return placeSingle(state, request.k, ctx, null);
    case 'terminal':
    case 'side':
      if (reload) return nothing(state);
      // The agent's terminal is beside the chat, not in the pane; the side chat comes where it lives.
      return { next: state, effect: { k: request.k }, show: false };
    case 'legacy':
      return {
        next: state,
        effect: { k: 'legacy', kind: request.kind, target: request.target, background: ctx.busy, reload },
        show: !reload,
      };
  }
};
