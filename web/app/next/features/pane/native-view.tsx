// An outside site in the desktop app's own browser: the app's shell lays a webview of the person's computer
// exactly over this box, so the site runs here, with the person's own sign-ins, and the agent acts on it through the
// shell. Only the box is drawn by the page; the webview above it follows the box as it moves, resizes and hides.
import { useEffect, useRef } from 'react';
import type { ILiveActed, ILivePage } from '@/next/core/net/live-browser';
import type { ILiveViewProps } from '@/next/features/pane/live-view';
// The act script is the page bridge's own code, built as a file of its own for the shell to put in the site.
// eslint-disable-next-line no-restricted-imports
import nativeActsUrl from '@/lib/hl-preview/native-entry.ts?worker&url';

/** A box of the window, CSS pixels of the app's page. */
export interface IPaneBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

/** The shell's pane browser (native/src/bridge.js), present only in the desktop app with the switch on. */
export interface IPaneBrowser {
  open: (tab: string, url: string, script: string, bounds: IPaneBounds) => Promise<unknown>;
  bounds: (tab: string, bounds: IPaneBounds, visible: boolean) => Promise<unknown>;
  go: (tab: string, url: string) => Promise<unknown>;
  history: (tab: string, step: number) => Promise<unknown>;
  close: (tab: string) => Promise<unknown>;
  act: (tab: string, message: unknown, timeoutMs: number) => Promise<unknown>;
}

export const PANE_BROWSER_EVENT = 'harnessly:pane-browser';
/** A tab not shown for this long gives its webview back; its sign-ins stay with the computer's browser store. */
const CLOSE_AFTER_MS = 10 * 60_000;

export const paneBrowser = (): IPaneBrowser | null => {
  if (typeof window === 'undefined') return null;
  const api = (window as unknown as { electronAPI?: { paneBrowser?: IPaneBrowser | null } }).electronAPI;
  return api?.paneBrowser ?? null;
};

const num = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);
const rec = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null;

/** The shell's word on a pane's page, for this tab only. */
export const readPaneState = (detail: unknown, tab: string): ILivePage | null => {
  if (!rec(detail) || detail.tab !== tab || typeof detail.url !== 'string') return null;
  return {
    url: detail.url,
    title: typeof detail.title === 'string' ? detail.title : '',
    canBack: detail.canBack === true,
    canForward: detail.canForward === true,
    loading: detail.loading === true,
  };
};

/** The act script's outcome, as the tab expects the machine's live browser to answer. */
export const readActed = (answer: unknown, id: string): ILiveActed | null => {
  if (!rec(answer) || answer.id !== id || typeof answer.ok !== 'boolean') return null;
  const r = answer.rect;
  const rect =
    rec(r) && num(r.x) && num(r.y) && num(r.width) && num(r.height)
      ? { x: r.x, y: r.y, width: r.width, height: r.height }
      : null;
  return { id, ok: answer.ok, text: typeof answer.text === 'string' ? answer.text : '', rect };
};

/** The box as the shell places it: whole pixels, and nothing when the box is not on screen. */
export const boundsOf = (el: HTMLElement | null): { bounds: IPaneBounds; visible: boolean } => {
  const r = el?.getBoundingClientRect();
  const hidden =
    !el || !el.isConnected || document.visibilityState === 'hidden' || !r || r.width < 2 || r.height < 2;
  const bounds = r
    ? {
        x: Math.round(r.left),
        y: Math.round(r.top),
        width: Math.round(r.width),
        height: Math.round(r.height),
      }
    : { x: 0, y: 0, width: 0, height: 0 };
  return { bounds, visible: !hidden };
};

let actsScript: Promise<string> | null = null;
/** The act script, fetched once from the app's own files. */
const loadActsScript = () => {
  actsScript ??= fetch(nativeActsUrl)
    .then((res) => (res.ok ? res.text() : ''))
    .catch(() => '');
  return actsScript;
};

const closing = new Map<string, number>();

const NativeView = ({ tab, url, onPage, onControl, onBox }: ILiveViewProps) => {
  const box = useRef<HTMLDivElement>(null);
  const page = useRef(onPage);
  const control = useRef(onControl);
  useEffect(() => {
    page.current = onPage;
    control.current = onControl;
  });

  useEffect(() => {
    const api = paneBrowser();
    if (!api) return;
    const wait = closing.get(tab);
    if (wait !== undefined) {
      window.clearTimeout(wait);
      closing.delete(tab);
    }
    let alive = true;
    let opened = false;
    let last = '';
    let frame = 0;
    const heard = (event: Event) => {
      const state = readPaneState((event as CustomEvent).detail, tab);
      if (state) page.current(state);
    };
    window.addEventListener(PANE_BROWSER_EVENT, heard);
    let script: string | null = null;
    let opening = false;
    // A shell that refused the page is asked again a little later, not every frame.
    let retryAt = 0;
    const open = (bounds: IPaneBounds) => {
      opening = true;
      api
        .open(tab, url, script ?? '', bounds)
        .then(() => {
          if (!alive) return;
          opened = true;
          last = '';
          control.current({
            go: (next) => void api.go(tab, next).catch(() => undefined),
            back: () => void api.history(tab, -1).catch(() => undefined),
            forward: () => void api.history(tab, 1).catch(() => undefined),
            reload: () => void api.history(tab, 0).catch(() => undefined),
            act: (message, timeoutMs) =>
              api.act(tab, message, timeoutMs).then(
                (answer) => readActed(answer, message.id),
                () => null,
              ),
          });
        })
        .catch(() => {
          retryAt = performance.now() + 2000;
        })
        .finally(() => {
          opening = false;
        });
    };
    // The box may move with no resize of its own (the sidebar, the split, the window): its place is read every frame
    // and sent only when it changed. The webview is made once the box is on screen and the act script is here.
    const follow = () => {
      frame = window.requestAnimationFrame(follow);
      if (opening || script === null) return;
      const { bounds, visible } = boundsOf(box.current);
      if (!opened) {
        if (visible && performance.now() >= retryAt) open(bounds);
        return;
      }
      const now = `${bounds.x},${bounds.y},${bounds.width},${bounds.height},${visible}`;
      if (now === last) return;
      last = now;
      void api.bounds(tab, bounds, visible).catch(() => undefined);
    };
    void loadActsScript().then((text) => {
      script = text;
    });
    frame = window.requestAnimationFrame(follow);
    return () => {
      alive = false;
      window.cancelAnimationFrame(frame);
      window.removeEventListener(PANE_BROWSER_EVENT, heard);
      control.current(null);
      const { bounds } = boundsOf(box.current);
      void api.bounds(tab, bounds, false).catch(() => undefined);
      closing.set(
        tab,
        window.setTimeout(() => {
          closing.delete(tab);
          void api.close(tab).catch(() => undefined);
        }, CLOSE_AFTER_MS),
      );
    };
    // The address is where the tab started; later ones go through the control, not a new webview.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tab]);

  return (
    <div
      ref={(el) => {
        box.current = el;
        onBox?.(el);
      }}
      className="hx-tbrowser__frame hx-tbrowser__live hx-tbrowser__native"
    />
  );
};

export default NativeView;
