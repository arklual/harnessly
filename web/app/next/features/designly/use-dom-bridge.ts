// The page's DOM for the «Elements» tab, over the board's own script (the library's client in the page): the tree is
// asked for in pieces by paths, a node is lit on the page while the pointer is on its row, the picked node's box and
// styles are read, and one click in the page can pick a node. Messages go only to the frame's window at its own origin
// and are read only from them; a page whose script does not answer is told so, not guessed at.
import { useEffect, useRef, useState, type RefObject } from 'react';
import { readDomMessage, type IDomDetail, type TDomMessage } from '@/next/features/designly/dom-bridge';
import {
  domRows,
  nodeAt,
  pathKey,
  pathsTo,
  withNode,
  type IDomNode,
  type IDomRow,
  type TDomPath,
} from '@/next/features/designly/dom-tree';

/** How long the page has to answer a request. */
const ASK_MS = 2500;
/** The first piece of the tree: the document, its head and body, and a little under them. */
const FIRST_DEPTH = 3;
/** The page's markup changes in bursts (a board draws itself); the tree is asked again once after one. */
const CHANGE_MS = 300;

export const DOM_STATUS = { idle: 'idle', loading: 'loading', ready: 'ready', silent: 'silent' } as const;
export type TDomStatus = (typeof DOM_STATUS)[keyof typeof DOM_STATUS];

interface IDomState {
  /** The frame this is of; a state of another frame (or load) is not shown. */
  frame: string;
  status: TDomStatus;
  root: IDomNode | null;
  open: ReadonlySet<string>;
  selected: TDomPath | null;
  detail: IDomDetail | null;
  inspect: boolean;
}

const fresh = (frame: string): IDomState => ({
  frame,
  status: DOM_STATUS.idle,
  root: null,
  open: new Set(),
  selected: null,
  detail: null,
  inspect: false,
});

export interface IDomBridge {
  status: TDomStatus;
  rows: IDomRow[];
  selected: TDomPath | null;
  detail: IDomDetail | null;
  inspect: boolean;
  /** Opens or closes a node; one that was never asked for is asked for first. */
  toggle: (path: TDomPath) => void;
  /** Takes a node: it is lit on the page and brought into view, its box and styles are read. */
  select: (path: TDomPath) => void;
  /** Lights a node on the page for as long as the pointer is on it; null gives the light back to the taken one. */
  hover: (path: TDomPath | null) => void;
  /** Asks the page to pick a node by a click, or stops that. */
  setInspect: (on: boolean) => void;
  /** Asks for the tree again. */
  reload: () => void;
}

/**
 * Before its page has loaded the frame holds a blank page of this window's own origin: a message to the slot's origin
 * would be refused there (WebKit logs it). The page says it is ready once it has come, and is asked then.
 */
const ownBlank = (win: Window): boolean => {
  try {
    return win.location.href === 'about:blank';
  } catch {
    return false;
  }
};

/** The frame is the page's slot: its window, and the origin its messages must come from. */
export const useDomBridge = (
  frameRef: RefObject<HTMLIFrameElement | null>,
  src: string | null,
  reloadKey: number,
  active: boolean,
): IDomBridge => {
  const frame = `${src}#${reloadKey}`;
  const [stored, setStored] = useState<IDomState>(() => fresh(frame));
  const state = stored.frame === frame ? stored : fresh(frame);
  // What the answers that arrive later need to read: the state as it is now, not as it was when they were asked for.
  const now = useRef(state);
  const seq = useRef(0);
  const waiting = useRef(new Map<number, (message: TDomMessage | null) => void>());
  const activeNow = useRef(active);
  useEffect(() => {
    activeNow.current = active;
  });

  const frameNow = useRef(frame);
  useEffect(() => {
    frameNow.current = frame;
  });
  const commit = (patch: Partial<IDomState>, of: string = frameNow.current) => {
    const base = now.current.frame === of ? now.current : fresh(of);
    now.current = { ...base, ...patch };
    setStored(now.current);
  };

  const send = (message: Record<string, unknown>) => {
    const target = frameRef.current;
    if (!target?.contentWindow || src === null || ownBlank(target.contentWindow)) return;
    target.contentWindow.postMessage(message, new URL(src, window.location.href).origin);
  };

  /** A request the page answers by its number; no answer in time is null. */
  const ask = (message: Record<string, unknown>) =>
    new Promise<TDomMessage | null>((resolve) => {
      seq.current += 1;
      const req = seq.current;
      const timer = window.setTimeout(() => {
        waiting.current.delete(req);
        resolve(null);
      }, ASK_MS);
      waiting.current.set(req, (answer) => {
        window.clearTimeout(timer);
        resolve(answer);
      });
      send({ ...message, req });
    });

  /** Reads the branch at `path` from the page into the tree; false when the page did not answer or the node is gone. */
  const load = async (path: TDomPath, depth: number) => {
    const of = frameNow.current;
    const answer = await ask({ type: 'tk:dom-tree', path, depth });
    if (frameNow.current !== of) return false;
    if (answer?.k !== 'tree' || answer.node === null) return false;
    const base = now.current.frame === of ? now.current : fresh(of);
    commit({ root: withNode(base.root, path, answer.node) }, of);
    return true;
  };

  const readDetail = async (path: TDomPath) => {
    const of = frameNow.current;
    const answer = await ask({ type: 'tk:dom-node', path });
    if (frameNow.current !== of || now.current.selected === null) return;
    if (pathKey(now.current.selected) !== pathKey(path)) return;
    commit({ detail: answer?.k === 'node' ? answer.detail : null }, of);
  };

  const start = async () => {
    const of = frameNow.current;
    commit({ ...fresh(of), status: DOM_STATUS.loading }, of);
    const ok = await load([], FIRST_DEPTH);
    if (frameNow.current !== of) return;
    if (!ok) {
      commit({ status: DOM_STATUS.silent }, of);
      return;
    }
    const open = new Set<string>(['']);
    const body = now.current.root?.kids?.findIndex((kid) => kid.tag === 'body') ?? -1;
    if (body >= 0) open.add(String(body));
    commit({ status: DOM_STATUS.ready, open }, of);
  };

  /** The page changed: the tree is read again, and so is every opened node whose children went missing with it. */
  const refresh = async () => {
    const of = frameNow.current;
    const opened = [...now.current.open].sort((a, b) => a.length - b.length);
    if (!(await load([], FIRST_DEPTH))) return;
    for (const key of opened) {
      if (frameNow.current !== of) return;
      const path = key === '' ? [] : key.split('.').map(Number);
      const node = nodeAt(now.current.root, path);
      if (node && node.n > 0 && node.kids === null) await load(path, 1);
    }
    const selected = now.current.selected;
    if (selected) void readDetail(selected);
  };

  const select = (path: TDomPath) => {
    commit({ selected: path, detail: null });
    send({ type: 'tk:dom-mark', path, reveal: true });
    void readDetail(path);
  };

  /** A node the page picked: the way down to it is read and opened, then it is taken. */
  const adopt = async (path: TDomPath) => {
    const of = frameNow.current;
    for (const prefix of pathsTo(path).slice(0, -1)) {
      const node = nodeAt(now.current.root, prefix);
      if (node && node.n > 0 && node.kids === null && !(await load(prefix, 1))) return;
      if (frameNow.current !== of) return;
    }
    const open = new Set(now.current.open);
    for (const prefix of pathsTo(path).slice(0, -1)) open.add(pathKey(prefix));
    commit({ open, inspect: false, status: DOM_STATUS.ready }, of);
    select(path);
  };

  const toggle = (path: TDomPath) => {
    const key = pathKey(path);
    const open = new Set(now.current.open);
    if (open.has(key)) open.delete(key);
    else open.add(key);
    commit({ open });
    const node = nodeAt(now.current.root, path);
    if (open.has(key) && node && node.n > 0 && node.kids === null) void load(path, 2);
  };

  const hover = (path: TDomPath | null) => {
    send({ type: 'tk:dom-mark', path: path ?? now.current.selected, reveal: false });
  };

  const setInspect = (on: boolean) => {
    commit({ inspect: on });
    send({ type: 'tk:dom-inspect', on });
  };

  // The page speaks: answers to what was asked, a pick, a change; only the frame's own window and origin are heard.
  useEffect(() => {
    const target = frameRef.current;
    if (!target || src === null) return;
    const origin = new URL(src, window.location.href).origin;
    let changeTimer = 0;
    const onMessage = (event: MessageEvent) => {
      if (event.source !== target.contentWindow || event.origin !== origin) return;
      const message = readDomMessage(event.data);
      if (!message) return;
      if (message.k === 'tree' || message.k === 'node') {
        waiting.current.get(message.req)?.(message);
        waiting.current.delete(message.req);
      } else if (message.k === 'picked') void adopt(message.path);
      else if (message.k === 'inspect-off') commit({ inspect: false });
      else if (message.k === 'changed' && activeNow.current) {
        window.clearTimeout(changeTimer);
        changeTimer = window.setTimeout(() => void refresh(), CHANGE_MS);
      } else if (message.k === 'ready') {
        commit(fresh(frameNow.current));
        if (activeNow.current) {
          send({ type: 'tk:dom-watch', on: true });
          void start();
        }
      }
    };
    window.addEventListener('message', onMessage);
    return () => {
      window.removeEventListener('message', onMessage);
      window.clearTimeout(changeTimer);
    };
    // The handlers read the latest state through refs; a new function each render must not listen again.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [src, reloadKey]);

  // The tab is in view: the tree is read once and the page is watched; out of view, nothing of the page is lit or watched.
  useEffect(() => {
    if (active) {
      send({ type: 'tk:dom-watch', on: true });
      if (now.current.frame !== frame || now.current.status === DOM_STATUS.idle) void start();
      return;
    }
    send({ type: 'tk:dom-mark', path: null });
    send({ type: 'tk:dom-watch', on: false });
    if (now.current.inspect) setInspect(false);
    // start and send read the frame of this src.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, frame]);

  return {
    status: state.status,
    rows: domRows(state.root, state.open),
    selected: state.selected,
    detail: state.detail,
    inspect: state.inspect,
    toggle,
    select,
    hover,
    setInspect,
    reload: () => void start(),
  };
};
