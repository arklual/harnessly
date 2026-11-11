// The person and their settings (spec §5): the cluster's person_settings on Acme, <data>/v2/me.json standalone,
// both behind GET /api/v2/me and PATCH /api/v2/me/settings{rev, patch}. Until the gateway (stage E) serves them the
// source is a stub behind the same interface: defaults, kept in memory.

import { LOCAL_PERSON } from '@/next/core/persist';
import { createStore, useStore } from '@/next/core/store';
import { clusterApi } from '@/next/core/net/machine';
import { getPage, sendJson } from '@/next/core/net/http';
import type { MachineId } from '@/next/protocol/v2';

/** The recognition models of dictation: the machine's own, OpenAI's, Yandex's. */
export const VOICE_ENGINE_IDS = ['local', 'chatgpt', 'openai', 'yandex'] as const;
export type VoiceEngineId = (typeof VOICE_ENGINE_IDS)[number];

/** A word dictation gets wrong: how it is written and, optionally, how it is heard. */
export interface VoiceWord {
  word: string;
  heard: string[];
}

/** The dictionary's limits, as the runtime keeps them (internal/wire/me.go). */
export const VOICE_LIMITS = { words: 200, wordLen: 64, heard: 10 } as const;

export interface VoiceSettings {
  engine: VoiceEngineId;
  words: VoiceWord[];
}

export const NOTICE_COLORS = ['contrast', 'theme'] as const;
export type TNoticeColor = (typeof NOTICE_COLORS)[number];

export interface PersonSettings {
  uiVersion: 'legacy' | 'next';
  locale: 'ru' | 'en';
  density: 'compact' | 'full';
  textSize: 's' | 'm' | 'l';
  /** What pops up (notices, toasts): `contrast` — the other of the theme (dark on light, light on dark); `theme` — the
   *  theme's own. */
  noticeColor: TNoticeColor;
  enterSends: boolean;
  defaultMachine: MachineId | null;
  notify: { types: string[]; sound: boolean; dnd: boolean };
  quickPrompts: string[];
  /** The shortcuts the person changed: an action's id to its shortcuts (features/keys); an empty list turns it off. */
  keys: Record<string, string[]>;
  voice: VoiceSettings;
  /** The Moscow hour the person's day starts at (0–23): «today» of the agent's spend counts from it. */
  dayStartHour: number;
}

export interface Me {
  email: string | null;
  name: string | null;
  settings: PersonSettings;
  rev: number;
}

/** Whose browser data this is: the account's e-mail, or the one local person of a standalone runtime. */
export const personKey = (me: Me): string => me.email ?? LOCAL_PERSON;

export type MeState =
  | { k: 'loading' }
  | { k: 'ready'; me: Me }
  | { k: 'signed-out' }
  | { k: 'failed'; code: 'timeout' | 'network' | 'http' };

export const DEFAULT_SETTINGS: PersonSettings = {
  uiVersion: 'next',
  locale: 'ru',
  density: 'compact',
  textSize: 'm',
  noticeColor: 'contrast',
  enterSends: true,
  defaultMachine: null,
  notify: { types: ['turn-finished', 'question', 'permission'], sound: false, dnd: false },
  quickPrompts: [],
  keys: {},
  voice: { engine: 'local', words: [] },
  dayStartHour: 3,
};

export type PatchResult =
  { r: 'accepted'; me: Me } | { r: 'refused'; code: 'conflict' | 'bad-request' } | { r: 'failed' };

export interface MeSource {
  load(): Promise<MeState>;
  patch(rev: number, patch: Partial<PersonSettings>): Promise<PatchResult>;
}

/** The stand-in until /api/v2/me exists: defaults, revisions in memory. */
export function localMeSource(initial: Partial<Me> = {}): MeSource {
  let me: Me = { email: null, name: null, settings: DEFAULT_SETTINGS, rev: 1, ...initial };
  return {
    load: async () => ({ k: 'ready', me }),
    patch: async (rev, patch) => {
      if (rev !== me.rev) return { r: 'refused', code: 'conflict' };
      me = { ...me, settings: { ...me.settings, ...patch }, rev: me.rev + 1 };
      return { r: 'accepted', me };
    },
  };
}

const SETTINGS_TIMEOUT_MS = 10000;

const isObj = (v: unknown): v is Record<string, unknown> =>
  typeof v === 'object' && v !== null && !Array.isArray(v);
const strOrNull = (v: unknown): v is string | null => v === null || typeof v === 'string';

/**
 * The strict reader of GET /api/v2/me and of the record a settings write answers. A record without its settings, its
 * revision or a person is not one: it throws, and the request reads as failed{http} (a runtime that is restarting
 * mid-upgrade, a proxy's page). A setting the runtime does not send yet (an older runtime of a rolling upgrade) is
 * the default; one of the wrong type is too, so one bad field never takes the page down.
 */
export function readMe(v: unknown): Me {
  if (!isObj(v) || !isObj(v.settings)) throw new Error('me: no settings');
  if (typeof v.rev !== 'number' || !Number.isFinite(v.rev)) throw new Error('me: no rev');
  const email = v.email ?? null;
  const name = v.name ?? null;
  if (!strOrNull(email) || !strOrNull(name)) throw new Error('me: bad person');
  const got = v.settings;
  const settings = { ...DEFAULT_SETTINGS } as Record<string, unknown>;
  for (const [key, def] of Object.entries(DEFAULT_SETTINGS)) {
    const val = got[key];
    if (val === undefined) continue;
    const same =
      def === null
        ? val === null || typeof val === 'string'
        : Array.isArray(def)
          ? Array.isArray(val)
          : typeof val === typeof def && (typeof def !== 'object' || isObj(val));
    if (same) settings[key] = isObj(def) && isObj(val) ? { ...def, ...val } : val;
  }
  return { email, name, settings: settings as unknown as PersonSettings, rev: v.rev };
}

/** GET /api/v2/me: 401 is the sign-in screen; a 404 (no stage E yet) falls back to the local stand-in, and so do
 *  the settings writes where PATCH /api/v2/me/settings is not served. The record goes through readMe: a record that is
 *  not one is a failure, which the page retries, never an exception. */
export function httpMeSource(fallback: MeSource = localMeSource()): MeSource {
  return {
    async load() {
      const res = await getPage(clusterApi('/me'), readMe, 10000);
      if ('ok' in res) return { k: 'ready', me: res.value };
      if (res.r === 'refused')
        return res.code === 'not-found' ? fallback.load() : { k: 'failed', code: 'http' };
      if (res.code !== 'http') return { k: 'failed', code: res.code };
      if (res.status === 401 || res.status === 403) return { k: 'signed-out' };
      return res.status === 404 ? fallback.load() : { k: 'failed', code: 'http' };
    },
    async patch(rev, patch) {
      const res = await sendJson(
        clusterApi('/me/settings'),
        { rev, patch },
        (v) => readMe(isObj(v) ? v.me : undefined),
        SETTINGS_TIMEOUT_MS,
      );
      if ('ok' in res) return { r: 'accepted', me: res.value };
      if (res.r === 'refused') {
        if (res.code === 'not-found') return fallback.patch(rev, patch);
        return res.code === 'conflict' || res.code === 'bad-request'
          ? { r: 'refused', code: res.code }
          : { r: 'failed' };
      }
      return { r: 'failed' };
    },
  };
}

export const meStore = createStore<MeState>({ k: 'loading' });
export const useMe = (): MeState => useStore(meStore);

/** Whether Enter sends in the message field (Settings); until the settings are known it does, as it always has. */
export const useEnterSends = (): boolean => {
  const me = useMe();
  return me.k === 'ready' ? me.me.settings.enterSends : true;
};

/** The Moscow hour the person's day starts at (Settings); until the settings are known, the default one. */
export const useDayStartHour = (): number => {
  const me = useMe();
  return me.k === 'ready' ? me.me.settings.dayStartHour : DEFAULT_SETTINGS.dayStartHour;
};

/** The dictation's settings: the model and the person's words; until the settings are known, the machine's own model. */
export const useVoiceSettings = (): VoiceSettings => {
  const me = useMe();
  return me.k === 'ready' ? me.me.settings.voice : DEFAULT_SETTINGS.voice;
};

/** The same, read once (a dictation that begins). */
export const voiceSettings = (): VoiceSettings => {
  const st = meStore.get();
  return st.k === 'ready' ? st.me.settings.voice : DEFAULT_SETTINGS.voice;
};

let source: MeSource = localMeSource();

export function setMeSource(next: MeSource): void {
  source = next;
}

/** Finished turns fold; «full» was the old default, saved with every other setting, and no page
 *  offers the switch yet: a saved «full» is that default. Goes when the settings get the switch. */
const folded = (st: MeState): MeState =>
  st.k === 'ready' && st.me.settings?.density === 'full'
    ? { ...st, me: { ...st.me, settings: { ...st.me.settings, density: 'compact' } } }
    : st;

export async function loadMe(): Promise<MeState> {
  let st: MeState;
  try {
    st = folded(await source.load());
  } catch {
    // A source that throws (a stand-in, a reader's bug) is a failure like any other, never a page stuck on it.
    st = { k: 'failed', code: 'http' };
  }
  meStore.set(st);
  return st;
}

/** Pessimistic: the settings change on the source's answer; a conflict reloads and reports it. */
export async function patchSettings(patch: Partial<PersonSettings>): Promise<PatchResult> {
  const st = meStore.get();
  if (st.k !== 'ready') return { r: 'failed' };
  const res = await source.patch(st.me.rev, patch);
  if (res.r === 'accepted') meStore.set({ k: 'ready', me: res.me });
  else if (res.r === 'refused' && res.code === 'conflict') await loadMe();
  return res;
}
