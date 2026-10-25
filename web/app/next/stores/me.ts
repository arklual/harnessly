// The person and their settings (spec §5): the cluster's person_settings on Acme, <data>/v2/me.json standalone,
// both behind GET /api/v2/me and PATCH /api/v2/me/settings{rev, patch}. Until the gateway (stage E) serves them the
// source is a stub behind the same interface: defaults, kept in memory.

import { LOCAL_PERSON } from '@/next/core/persist';
import { createStore, useStore } from '@/next/core/store';
import { clusterApi } from '@/next/core/net/machine';
import { getPage, sendJson } from '@/next/core/net/http';
import type { MachineId } from '@/next/protocol/v2';

export interface PersonSettings {
  uiVersion: 'legacy' | 'next';
  locale: 'ru' | 'en';
  density: 'compact' | 'full';
  textSize: 's' | 'm' | 'l';
  enterSends: boolean;
  defaultMachine: MachineId | null;
  notify: { types: string[]; sound: boolean; dnd: boolean };
  quickPrompts: string[];
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
  enterSends: true,
  defaultMachine: null,
  notify: { types: ['turn-finished', 'question', 'permission'], sound: false, dnd: false },
  quickPrompts: [],
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

/** GET /api/v2/me: 401 is the sign-in screen; a 404 (no stage E yet) falls back to the local stand-in, and so do
 *  the settings writes where PATCH /api/v2/me/settings is not served. The record is not checked: its contract is stage
 *  E's. */
export function httpMeSource(fallback: MeSource = localMeSource()): MeSource {
  return {
    async load() {
      const res = await getPage(clusterApi('/me'), (v) => v as Me, 10000);
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
        (v) => (v as { me: Me }).me,
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

let source: MeSource = localMeSource();

export function setMeSource(next: MeSource): void {
  source = next;
}

/** Finished turns fold; «full» was the old default, saved with every other setting, and no page
 *  offers the switch yet: a saved «full» is that default. Goes when the settings get the switch. */
const folded = (st: MeState): MeState =>
  st.k === 'ready' && st.me.settings.density === 'full'
    ? { ...st, me: { ...st.me, settings: { ...st.me.settings, density: 'compact' } } }
    : st;

export async function loadMe(): Promise<MeState> {
  const st = folded(await source.load());
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
