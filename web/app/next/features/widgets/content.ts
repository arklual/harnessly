// What a widget made by an agent shows: its markup and style, read from the machine when the agent changed
// them, handed to the live card as data so it changes where it stands (no new frame, no blink), and kept in this
// browser so the next time a chat opens the card is drawn at once from the last one while the frame comes up.
import { useEffect, useState } from 'react';
import { platformPath } from '@/next/core/net/machine';
import { runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

export interface IWidgetContent {
  rev: string;
  html: string;
  css: string;
}

const KEY = 'hx-wg-content:';
/** The largest card kept; a bigger one is drawn from the machine as before. */
const MAX_KEPT = 60_000;

const keyOf = (machine: MachineId, appId: string) => `${KEY}${machine}:${appId}`;

const isContent = (v: unknown): v is IWidgetContent => {
  if (typeof v !== 'object' || v === null) return false;
  const c = v as Record<string, unknown>;
  return typeof c.rev === 'string' && typeof c.html === 'string' && typeof c.css === 'string';
};

export const readKeptContent = (machine: MachineId, appId: string): IWidgetContent | null => {
  try {
    const v: unknown = JSON.parse(window.localStorage.getItem(keyOf(machine, appId)) ?? 'null');
    return isContent(v) ? v : null;
  } catch {
    return null;
  }
};

export const keepContent = (machine: MachineId, appId: string, c: IWidgetContent): void => {
  try {
    if (c.html.length + c.css.length > MAX_KEPT) return;
    window.localStorage.setItem(keyOf(machine, appId), JSON.stringify(c));
  } catch {
    // The card is drawn from the machine, as before.
  }
};

/** Forgets what was kept of a widget that is gone. */
export const forgetContent = (machine: MachineId, appId: string): void => {
  try {
    window.localStorage.removeItem(keyOf(machine, appId));
  } catch {
    // Nothing kept to forget.
  }
};

export const loadContent = (machine: MachineId, appId: string): Promise<IWidgetContent> =>
  runtimeJson<unknown>(machine, platformPath(`/side-widgets/content?id=${encodeURIComponent(appId)}`)).then(
    (v) => {
      if (!isContent(v)) throw new Error('bad widget content');
      return v;
    },
  );

/**
 * The widget's content: what was kept now, the machine's as it comes, and again when the app was installed again (the
 * agent changed the widget: `stamp`). Null for an app that is no widget of an agent's.
 */
export const useWidgetContent = (
  machine: MachineId,
  appId: string,
  stamp: string,
  enabled: boolean,
): { kept: IWidgetContent | null; live: IWidgetContent | null } => {
  const [kept] = useState(() => (enabled ? readKeptContent(machine, appId) : null));
  const [live, setLive] = useState<IWidgetContent | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let on = true;
    loadContent(machine, appId).then(
      (c) => {
        if (!on) return;
        keepContent(machine, appId, c);
        setLive(c);
      },
      () => undefined,
    );
    return () => {
      on = false;
    };
  }, [machine, appId, stamp, enabled]);
  return { kept, live };
};
