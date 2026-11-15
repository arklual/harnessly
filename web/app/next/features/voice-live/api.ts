// The voice mode's routes on a machine (cmd/runtime/routes_voice_live.go). The machine makes the call with its
// Codex sign-in and keeps the control socket; the page holds only the WebRTC media. No token comes here.
import { flashQuickEdit } from '@/next/features/app-views/quick-edit-flash';
import { platformPath, runtimePath, runtimeSocket } from '@/next/core/net/machine';
import { postJson, runtimeJson } from '@/next/core/net/runtime-json';
import type { MachineId } from '@/next/protocol/v2';

export const VOICE_ROUTE = { subscription: 'subscription', key: 'key' } as const;
export type TVoiceRoute = (typeof VOICE_ROUTE)[keyof typeof VOICE_ROUTE];

export interface IVoiceState {
  subscription: boolean;
  signedIn: boolean;
  /** The machine keeps an OpenAI key: the key route works without the subscription. */
  hasKey: boolean;
  /** The route a call takes now; '' when there is none. */
  route: TVoiceRoute | '';
  ready: boolean;
  voice: string;
  voices: string[];
  model: string;
  models: string[];
  /** How a call shows in the field: its words written on the line, or only heard (VOICE_TALK). */
  talk: TVoiceTalk;
  /** The voice agent's look in the field. */
  head: THeadKind;
  /** Speaking over the voice stops it. */
  barge: boolean;
  /** What the person tells the voice about themselves and how to answer them (only into their own calls). */
  about: string;
  /** The voice also knows their agents' notes about them (memories of type user or feedback). */
  notes: boolean;
  /** How the work is told while it goes by default (style.ts DELIVERY). */
  delivery: string;
  /** The default step of how hard the voice thinks (style.ts EFFORT_STEPS). */
  effort: string;
  efforts: string[];
  /** The machine can carry a call's sound itself (machine-sound.ts); absent from a machine that can not yet. */
  relay?: boolean;
  /** «Привет, harnessly» starts the voice (wake.ts); absent from a machine that does not know it yet. */
  wake?: IVoiceWake;
}

/** The wake phrase's setting: on, and the seconds of silence that end a call it started. */
export interface IVoiceWake {
  on: boolean;
  silence: number;
}

export const HEAD_KIND = { h: 'h', face: 'face' } as const;
export type THeadKind = (typeof HEAD_KIND)[keyof typeof HEAD_KIND];

export const VOICE_TALK = { text: 'text', voice: 'voice' } as const;
export type TVoiceTalk = (typeof VOICE_TALK)[keyof typeof VOICE_TALK];

export interface IVoiceSettings {
  subscription: boolean;
  voice: string;
  model: string;
  talk: TVoiceTalk;
  head: THeadKind;
  noBarge: boolean;
  about: string;
  notes: boolean;
  delivery: string;
  effort: string;
}

export const VOICE_MODE = { agent: 'agent', chat: 'chat', digest: 'digest' } as const;
export type TVoiceMode = (typeof VOICE_MODE)[keyof typeof VOICE_MODE];

export interface IVoiceEvent {
  seq: number;
  type: 'ready' | 'user' | 'assistant' | 'turn' | 'task' | 'error' | 'ended';
  role?: 'user' | 'assistant';
  text?: string;
  id?: string;
  message?: string;
}

const path = (tail = ''): `/${string}` => platformPath(`/voice/live${tail}` as `/${string}`);

export const loadVoiceState = (m: MachineId) => runtimeJson<IVoiceState>(m, path());

// A setting saved in Settings reaches the voice buttons already open in chats, which read the state only once.
const voiceChanges = new EventTarget();
const VOICE_CHANGED = 'voice-state';

export const saveVoiceSettings = async (m: MachineId, body: IVoiceSettings) => {
  const st = await runtimeJson<IVoiceState>(m, path('/settings'), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  voiceChanges.dispatchEvent(new CustomEvent(VOICE_CHANGED, { detail: { m, st } }));
  return st;
};

/** Calls fn with each state of machine m saved from now on; the answer stops it. */
export const onVoiceState = (m: MachineId, fn: (st: IVoiceState) => void) => {
  const take = (e: Event) => {
    const d = (e as CustomEvent<{ m: MachineId; st: IVoiceState }>).detail;
    if (d.m === m) fn(d.st);
  };
  voiceChanges.addEventListener(VOICE_CHANGED, take);
  return () => voiceChanges.removeEventListener(VOICE_CHANGED, take);
};

/** How long the voice button waits before it asks the machine again after a failed read. */
export const voiceStateRetryDelay = (tries: number) => Math.min(2000 * 2 ** tries, 30000);

/** Saves the wake phrase's setting alone: the machine keeps every other field as it is. */
export const saveVoiceWake = async (m: MachineId, body: IVoiceWake) => {
  const st = await runtimeJson<IVoiceState>(m, path('/settings/wake'), {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  voiceChanges.dispatchEvent(new CustomEvent(VOICE_CHANGED, { detail: { m, st } }));
  return st;
};

export const startCall = (
  m: MachineId,
  body: {
    sdp: string;
    mode: TVoiceMode;
    voice: string;
    model: string;
    context: string;
    conv?: string;
    screen?: string;
    delivery?: string;
    effort?: string;
    /** The sound goes over the machine: no offer, the page joins callAudioSocket. */
    relay?: boolean;
  },
) =>
  runtimeJson<{ id: string; sdp: string; route: TVoiceRoute; model: string; relay?: boolean }>(
    m,
    path('/calls'),
    postJson(body),
    40_000,
  );

export const sayInCall = (
  m: MachineId,
  id: string,
  body: { text: string; task?: string; speak?: boolean; typed?: boolean },
) => runtimeJson<{ ok: boolean }>(m, path(`/calls/${encodeURIComponent(id)}/say`), postJson(body));

/** The socket of a call's sound over the machine (machine-sound.ts). */
export const callAudioSocket = (m: MachineId, id: string) =>
  runtimeSocket(path(`/calls/${encodeURIComponent(id)}/audio`), m);

export const hangCall = (m: MachineId, id: string) =>
  runtimeJson<{ ok: boolean }>(m, path(`/calls/${encodeURIComponent(id)}`), { method: 'DELETE' });

/** Follows a call's events; the returned function stops following. */
export const followCall = (m: MachineId, id: string, on: (e: IVoiceEvent) => void): (() => void) => {
  const source = new EventSource(runtimePath(path(`/calls/${encodeURIComponent(id)}/events`), m));
  const types: IVoiceEvent['type'][] = ['ready', 'user', 'assistant', 'turn', 'task', 'error', 'ended'];
  for (const type of types)
    source.addEventListener(type, (e) => {
      on(JSON.parse((e as MessageEvent<string>).data) as IVoiceEvent);
      if (type === 'ended') source.close();
    });
  return () => source.close();
};

/** A quick edit's outcome: the model's line on what changed, and how long it took. */
export interface IQuickEdit {
  said: string;
  changed: boolean;
  ms: number;
  /** The changed lines as they are now: the board flashes the block they make. */
  hint?: string;
}

const editPath = (tail = ''): `/${string}` => platformPath(`/voice/edit${tail}` as `/${string}`);

/** A small change said over an open file, made in seconds by a fast model on that file only. */
export const quickEdit = (
  m: MachineId,
  body: { file: string; selection?: string; ask: string; chat?: string },
) => runtimeJson<IQuickEdit>(m, editPath(), postJson(body));

/** The voice began over a file it may edit: the fast model starts ahead (the first edit then takes seconds too). */
export const warmQuickEdit = (m: MachineId) =>
  runtimeJson<{ ok: boolean }>(m, editPath('/warm'), postJson({}));

/** «Верни»: the file's last quick edit put back. */
export const undoQuickEdit = (m: MachineId, file: string) =>
  runtimeJson<IQuickEdit>(m, editPath('/undo'), postJson({ file }));

/** A quick edit of an app's object (a board, a document): what changed and the elements to light up. */
export interface IQuickEditApp extends IQuickEdit {
  ids?: string[];
}

/** The object of an app a quick edit goes to: the app's own quickEdit tools read and change it. */
export interface IAppObject {
  app: string;
  object: string;
}

/**
 * A small change said over an app's object, planned by the fast model and applied by the app (manifest quickEdit). A
 * removal is refused with `edit_needs_yes` and the planned words: after «да» the same call with confirm applies it.
 */
export const quickEditApp = (
  m: MachineId,
  body: IAppObject & { selection?: string; ask: string; confirm?: boolean },
) =>
  runtimeJson<IQuickEditApp>(m, editPath('/app'), postJson(body)).then((res) => {
    // The app's open views light up what changed.
    if (res.changed)
      flashQuickEdit(body.app, { object: body.object, ids: res.ids ?? [], hint: res.hint ?? '' });
    return res;
  });

/** «Верни»: the app object's last quick edit put back. */
export const undoQuickEditApp = (m: MachineId, body: IAppObject) =>
  runtimeJson<IQuickEditApp>(m, editPath('/app/undo'), postJson(body));
