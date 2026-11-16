// The person's choices that pages keep in the browser (the theme, Beta, the feed's cards, the model of a new chat,
// the quick prompts…) are the person's, not the device's: they live in the person's settings (`prefs`) and every browser keeps
// a copy. The stores stay as they are — they read and write their own storage key — and this module carries the copy:
//
// - the first page the person opens after this came moves its browser's values in (`synced` was false);
// - every page then takes the settings' values over its own (a key the settings lack is the default: it is removed);
// - a store's write (next/core/pref-write.ts) goes to the settings; refused, the browser gets the settings' value back.
//
// What a device has of its own stays out: the microphone, the sizes of the panels, what is folded, the open tabs. The
// notices and their switches have their own database (stores/notices-sync.ts).
import { meStore, patchSettings, type PrefsSettings } from '@/next/stores/me';
import { PREF_WRITE_EVENT } from '@/next/core/pref-write';
import { toast } from '@/next/ui/toast';
import { t } from '@/next/i18n';

/** The dictation's language, out of the voice's record (stores/voice.ts) whose microphone is the device's. */
export const VOICE_LANG = 'voice.lang';
const VOICE_KEY = 'hl:voice';

/** Every choice kept in the settings, by the browser's storage key; the same list as wire.PrefKeys. */
export const SYNCED_PREFS = [
  'hl-theme',
  'hl-palette',
  'hl:beta',
  'hl:feed-kinds',
  'hl:pane-auto-open',
  'hl:browser-agent',
  'hx-show-test-chats',
  VOICE_LANG,
  'hx-next-new-chat-choice',
  'hx.next.quickPrompts',
  'hx-top-tabs',
  'hx-looks',
  'hx-widgets',
] as const;
export type TSyncedPref = (typeof SYNCED_PREFS)[number];

const isSynced = (key: string): key is TSyncedPref => (SYNCED_PREFS as readonly string[]).includes(key);

export type TLocalPrefs = Readonly<Partial<Record<TSyncedPref, string | null>>>;

export type TSyncPlan =
  /** The settings have nothing yet: the browser's values go there. */
  | { k: 'migrate'; values: Record<string, string> }
  /** The settings say: the browser's keys to write (null removes). */
  | { k: 'apply'; writes: Partial<Record<TSyncedPref, string | null>> };

/** What a page does with the settings' choices and its browser's: pure, for the tests. */
export const planSync = (prefs: PrefsSettings, local: TLocalPrefs): TSyncPlan => {
  if (!prefs.synced) {
    const values: Record<string, string> = {};
    for (const key of SYNCED_PREFS) {
      const value = local[key];
      if (typeof value === 'string') values[key] = value;
    }
    return { k: 'migrate', values };
  }
  const writes: Partial<Record<TSyncedPref, string | null>> = {};
  for (const key of SYNCED_PREFS) {
    const want = prefs.values[key] ?? null;
    if ((local[key] ?? null) !== want) writes[key] = want;
  }
  return { k: 'apply', writes };
};

const voiceRecord = (): Record<string, unknown> => {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(VOICE_KEY) ?? '{}');
    return typeof parsed === 'object' && parsed !== null ? (parsed as Record<string, unknown>) : {};
  } catch {
    return {};
  }
};

const readLocal = (key: TSyncedPref): string | null => {
  try {
    if (key !== VOICE_LANG) return window.localStorage.getItem(key);
    const lang = voiceRecord().lang;
    return typeof lang === 'string' && lang !== 'ui' ? lang : null;
  } catch {
    return null;
  }
};

/** The browser's value set without its store's setter (no echo back), the store told by a storage event. */
const writeLocal = (key: TSyncedPref, value: string | null): void => {
  try {
    const storageKey = key === VOICE_LANG ? VOICE_KEY : key;
    if (key === VOICE_LANG) {
      window.localStorage.setItem(VOICE_KEY, JSON.stringify({ ...voiceRecord(), lang: value ?? 'ui' }));
    } else if (value === null) window.localStorage.removeItem(key);
    else window.localStorage.setItem(key, value);
    window.dispatchEvent(new StorageEvent('storage', { key: storageKey }));
  } catch {
    // Storage closed: the page keeps what it had until a reload.
  }
};

const localPrefs = (): TLocalPrefs => {
  const out: Partial<Record<TSyncedPref, string | null>> = {};
  for (const key of SYNCED_PREFS) out[key] = readLocal(key);
  return out;
};

const applyFromSettings = (prefs: PrefsSettings): void => {
  const plan = planSync(prefs, localPrefs());
  if (plan.k === 'apply')
    for (const [key, value] of Object.entries(plan.writes)) writeLocal(key as TSyncedPref, value ?? null);
};

/** One write of the choices, once more after a conflict (another device wrote between): the newest settings win. */
const patchPrefs = async (make: (prefs: PrefsSettings) => PrefsSettings): Promise<boolean> => {
  for (let attempt = 0; attempt < 2; attempt++) {
    const st = meStore.get();
    if (st.k !== 'ready') return false;
    const res = await patchSettings({ prefs: make(st.me.settings.prefs) });
    if (res.r === 'accepted') return true;
    if (res.r !== 'refused' || res.code !== 'conflict') return false;
  }
  return false;
};

const migrating = { on: false };

const onSettings = (): void => {
  const st = meStore.get();
  if (st.k !== 'ready') return;
  const prefs = st.me.settings.prefs;
  if (prefs.synced) {
    applyFromSettings(prefs);
    return;
  }
  if (migrating.on) return;
  const plan = planSync(prefs, localPrefs());
  if (plan.k !== 'migrate') return;
  migrating.on = true;
  // The first device moves its values in; another that got there first wins (the conflict reloads, and its values apply).
  void patchPrefs((now) => (now.synced ? now : { synced: true, values: plan.values })).finally(() => {
    migrating.on = false;
  });
};

const onWrite = (event: Event): void => {
  const key = (event as CustomEvent<string>).detail;
  if (!isSynced(key)) return;
  const st = meStore.get();
  if (st.k !== 'ready' || !st.me.settings.prefs.synced) return;
  const value = readLocal(key);
  if ((st.me.settings.prefs.values[key] ?? null) === value) return;
  void patchPrefs((now) => {
    const values = { ...now.values };
    if (value === null) delete values[key];
    else values[key] = value;
    return { synced: true, values };
  }).then((ok) => {
    if (ok) return;
    toast(t('next.prefs.saveFailed'), { tone: 'error' });
    const now = meStore.get();
    if (now.k === 'ready') writeLocal(key, now.me.settings.prefs.values[key] ?? null);
  });
};

let started = false;

/** Starts carrying the choices, once per page: after the settings load (boot.ts). */
export const startPrefsSync = (): void => {
  if (started || typeof window === 'undefined') return;
  started = true;
  meStore.subscribe(onSettings);
  window.addEventListener(PREF_WRITE_EVENT, onWrite);
  onSettings();
};
