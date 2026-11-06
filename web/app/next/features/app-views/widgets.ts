// The widgets of a chat (internal/apps/widgets.go): what an agent shows in the feed with the widget_show and
// widget_render tools or `harnessly widget`, kept on the machine. The step that first showed a widget names it by id in
// the tool's answer («widget <id> shown»); the widget itself — its data and revision — is read from the machine, asked
// again while the chat is on screen, so the same id shown again («widget <id> changed», a plain step) changes the widget
// where it first stood.
import { useEffect, useState, useSyncExternalStore } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import { pageClock } from '@/next/core/clock';
import type { ConvId, MachineId } from '@/next/protocol/v2';
import type { IInstalledApp } from '@/next/features/app-views/api';

/** The app id the agent's own widgets are served under; no package has it. */
export const WIDGETS_APP = 'harnessly.widgets';

export interface IChatWidget {
  id: string;
  kind: 'view' | 'html';
  app: string;
  view?: string;
  title?: string;
  data: unknown;
  rev: number;
  createdAt: number;
  updatedAt: number;
}

/** While a chat with widgets is on screen, its widgets are asked this often. */
export const WIDGETS_POLL_MS = 2000;

const WIDGET_SHOWN = /^widget ([a-z0-9][a-z0-9_-]{0,39}) shown\b/m;
const CLI_WIDGET = /(?:^|[\s;&|(])harnessly\s+widgets?\s+(?:show|render)\b/;

/**
 * The widget a step shows: the host's widget_show / widget_render tool, or `harnessly widget show|render` in a shell
 * step; null for any other step or before the step has its answer.
 */
export const parseWidgetRef = (tool: string, summary: string, output: string | null): string | null => {
  if (output === null) return null;
  const host = /^mcp__harnessly-app__widget_(show|render)$/.test(tool);
  const shell = /^(bash|shell|exec)/i.test(tool) && CLI_WIDGET.test(summary.split('\n')[0] ?? '');
  if (!host && !shell) return null;
  return WIDGET_SHOWN.exec(output)?.[1] ?? null;
};

type TState = { list: IChatWidget[] | null; error: boolean };

interface IEntry {
  state: TState;
  subs: Set<() => void>;
  timer: unknown;
  looking: boolean;
}

const entries = new Map<string, IEntry>();
const keyOf = (m: MachineId, conv: ConvId) => `${m}|${conv}`;
const EMPTY: TState = { list: null, error: false };

const sameList = (a: IChatWidget[] | null, b: IChatWidget[]) =>
  a !== null && a.length === b.length && a.every((w, i) => w.id === b[i].id && w.rev === b[i].rev);

const look = async (m: MachineId, conv: ConvId, e: IEntry) => {
  if (e.looking || document.visibilityState !== 'visible') return;
  e.looking = true;
  try {
    const r = await runtimeJson<{ widgets: IChatWidget[] }>(
      m,
      platformPath(`/apps/widgets?conversation=${encodeURIComponent(conv)}`),
      { cache: 'no-store' },
    );
    if (!sameList(e.state.list, r.widgets) || e.state.error) {
      e.state = { list: r.widgets, error: false };
      e.subs.forEach((f) => f());
    }
  } catch {
    if (e.state.list === null && !e.state.error) {
      e.state = { list: null, error: true };
      e.subs.forEach((f) => f());
    }
  } finally {
    e.looking = false;
  }
};

const subscribe = (m: MachineId, conv: ConvId, fn: () => void): (() => void) => {
  const k = keyOf(m, conv);
  let e = entries.get(k);
  if (!e) {
    e = { state: EMPTY, subs: new Set(), timer: null, looking: false };
    entries.set(k, e);
  }
  const entry = e;
  entry.subs.add(fn);
  if (entry.subs.size === 1) {
    const clock = pageClock();
    const again = () => {
      entry.timer = clock.setTimeout(again, WIDGETS_POLL_MS);
      void look(m, conv, entry);
    };
    again();
  }
  return () => {
    entry.subs.delete(fn);
    if (entry.subs.size === 0) {
      pageClock().clearTimeout(entry.timer);
      entry.timer = null;
    }
  };
};

/** One widget of the chat: undefined while the list loads, null when the machine has no such widget. */
export const useChatWidget = (
  m: MachineId | null,
  conv: ConvId | null,
  id: string,
): { widget: IChatWidget | null | undefined; error: boolean } => {
  const state = useSyncExternalStore(
    (fn) => (m && conv ? subscribe(m, conv, fn) : () => undefined),
    () => (m && conv ? (entries.get(keyOf(m, conv))?.state ?? EMPTY) : EMPTY),
  );
  if (state.list === null) return { widget: undefined, error: state.error };
  return { widget: state.list.find((w) => w.id === id) ?? null, error: false };
};

/**
 * The app the page hands its frame for an agent's own widget: no package, the widget is its one view (served by the
 * runtime under the widget's session), and the one right it has is to send the person's choice to the chat.
 */
export const widgetApp = (
  id: string,
  title: string | undefined,
  rev: number,
  name: string,
): IInstalledApp => ({
  id: WIDGETS_APP,
  name: title || name,
  description: '',
  version: String(rev),
  trust: 'declarative',
  targets: ['chat'],
  permissions: ['chat.prompt'],
  views: [{ id, title, targets: ['chat'], path: `/${WIDGETS_APP}/${id}/index.html` }],
  mcpServers: [],
});

/** A revision number that changes only after the widget settled for a moment, so a burst of updates mounts once. */
export const useSettled = (value: number, ms = 300): number => {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const clock = pageClock();
    const timer = clock.setTimeout(() => setSettled(value), ms);
    return () => clock.clearTimeout(timer);
  }, [value, ms]);
  return settled;
};
