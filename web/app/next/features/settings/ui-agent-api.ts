// The agent of the window (cmd/runtime/routes_uiagent.go): a request in words answered at once by a quick
// model, with no chat. The page sends the window's map (features/settings/agent-map, «- <name>: /<path>» lines) and the
// settings as their pages name them; the machine adds its catalog of settings. The answer is a stream: at most one
// action first (go to a path of the map, or a change the person confirms), then the words.
import { deviceId } from '@/next/core/device';
import { platformPath, runtimePath } from '@/next/core/net/machine';
import { pageSettings } from '@/next/features/search/page-settings';
import { locale, t } from '@/next/i18n';
import { DEVICE_HEADER, type MachineId } from '@/next/protocol/v2';
import type { SettingsSection } from '@/next/routes/routes';

export interface IUIPlace {
  /** The place's path in the window (`/settings/general`): an action `go` names it, the page opens it. */
  id: string;
  title: string;
  words?: string;
}

/**
 * The settings as their pages name them, with the other words they are found by («Enter отправляет», «шрифт»): what the
 * window's map (agent-map) does not say. Sent beside the map as `places`.
 */
export const settingPlaces = (hidden: readonly SettingsSection[] = []): IUIPlace[] =>
  pageSettings()
    .filter((setting) => !hidden.includes(setting.section))
    .map((setting) => ({
      id: `/settings/${setting.section}`,
      title: `${t(`next.settings.section.${setting.section}`)} → ${setting.title}`,
      words: setting.words,
    }));

export const UI_EVENT = {
  go: 'go',
  confirm: 'confirm',
  delta: 'delta',
  done: 'done',
  error: 'error',
} as const;

export type TUIEvent =
  | { type: typeof UI_EVENT.go; place: string; label: string }
  /** A change the person confirms; made with PUT settings-catalog/{key}. */
  | { type: typeof UI_EVENT.confirm; key: string; value: string; label: string; title: string; was: string }
  | { type: typeof UI_EVENT.delta; text: string }
  | { type: typeof UI_EVENT.done; text: string }
  /** `code` is the machine's reason ('no-model' when no model can answer), `message` its words ('' for none). */
  | { type: typeof UI_EVENT.error; code: string; message: string };

export const UI_NO_MODEL = 'no-model';

const s = (v: unknown): string => (typeof v === 'string' ? v : '');

const eventOf = (name: string, data: string): TUIEvent | null => {
  let o: Record<string, unknown>;
  try {
    const doc: unknown = JSON.parse(data);
    if (typeof doc !== 'object' || doc === null) return null;
    o = doc as Record<string, unknown>;
  } catch {
    return null;
  }
  if (name === 'action' && o.kind === 'go')
    return { type: UI_EVENT.go, place: s(o.place), label: s(o.label) };
  if (name === 'action' && o.kind === 'confirm')
    return {
      type: UI_EVENT.confirm,
      key: s(o.key),
      value: s(o.value),
      label: s(o.label),
      title: s(o.title),
      was: s(o.was),
    };
  if (name === UI_EVENT.delta) return { type: UI_EVENT.delta, text: s(o.text) };
  if (name === UI_EVENT.done) return { type: UI_EVENT.done, text: s(o.text) };
  if (name === UI_EVENT.error)
    return { type: UI_EVENT.error, code: s(o.error) || 'failed', message: s(o.message) };
  return null;
};

/** Splits what came so far into whole events and what is left. */
export const readUIFrames = (buffer: string): { events: TUIEvent[]; rest: string } => {
  const frames = buffer.split(/\r?\n\r?\n/);
  const rest = frames.pop() ?? '';
  const events = frames.flatMap((frame) => {
    const lines = frame.split(/\r?\n/);
    const name =
      lines
        .find((line) => line.startsWith('event:'))
        ?.slice(6)
        .trim() ?? '';
    const data = lines
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice(5).trim())
      .join('');
    return eventOf(name, data) ?? [];
  });
  return { events, rest };
};

const failed = (code: string, message = ''): TUIEvent => ({ type: UI_EVENT.error, code, message });

/** Asks the agent; every outcome but the caller's own abort ends in done or error. `page` is the path the person is at;
 *  `go` comes with a path of the map, which the page opens (goTo(parseRoute(path))). */
export const askUIAgent = async (
  machine: MachineId,
  ask: { query: string; page: string; map: string; places: readonly IUIPlace[] },
  signal: AbortSignal,
  onEvent: (event: TUIEvent) => void,
): Promise<void> => {
  let res: Response;
  try {
    res = await fetch(runtimePath(platformPath('/ui-agent'), machine), {
      method: 'POST',
      credentials: 'same-origin',
      headers: {
        [DEVICE_HEADER]: deviceId(),
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
      },
      // The answer is in the interface's language, whatever the request's.
      body: JSON.stringify({ ...ask, lang: locale() }),
      signal,
    });
  } catch {
    if (!signal.aborted) onEvent(failed('network'));
    return;
  }
  if (!(res.headers.get('Content-Type') ?? '').includes('text/event-stream') || !res.body) {
    const doc: unknown = await res.json().catch(() => null);
    if (signal.aborted) return;
    const o = typeof doc === 'object' && doc !== null ? (doc as Record<string, unknown>) : {};
    onEvent(failed(s(o.error) || 'failed', s(o.message)));
    return;
  }
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let rest = '';
  let ended = false;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      const parsed = readUIFrames(rest + decoder.decode(value, { stream: true }));
      rest = parsed.rest;
      for (const event of parsed.events) {
        if (event.type === UI_EVENT.done || event.type === UI_EVENT.error) ended = true;
        onEvent(event);
      }
    }
  } catch {
    if (!signal.aborted) onEvent(failed('interrupted'));
    return;
  }
  if (!ended && !signal.aborted) onEvent(failed('interrupted'));
};
