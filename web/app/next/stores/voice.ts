// «Голосовой ввод» (Settings → Voice input): the microphone and the language of dictation. This browser's, as the theme
// is: a microphone is a device of this computer, and the language goes with the person at it.
import { useSyncExternalStore } from 'react';
import { locale } from '@/next/i18n';

const KEY = 'hl:voice';

export const VOICE_LANGS = ['ui', 'ru', 'en', 'auto'] as const;
/** `ui`: the language of the page; `auto`: the model finds out. */
export type TVoiceLang = (typeof VOICE_LANGS)[number];

export interface IVoicePrefs {
  /** The microphone's deviceId; null: the system's default. */
  deviceId: string | null;
  lang: TVoiceLang;
}

export const DEFAULT_VOICE_PREFS: IVoicePrefs = { deviceId: null, lang: 'ui' };

const listeners = new Set<() => void>();
let cached: IVoicePrefs | null = null;

const read = (): IVoicePrefs => {
  try {
    const raw = window.localStorage.getItem(KEY);
    if (!raw) return DEFAULT_VOICE_PREFS;
    const o = JSON.parse(raw) as Record<string, unknown>;
    return {
      deviceId: typeof o.deviceId === 'string' && o.deviceId !== '' ? o.deviceId : null,
      lang: VOICE_LANGS.includes(o.lang as TVoiceLang) ? (o.lang as TVoiceLang) : 'ui',
    };
  } catch {
    return DEFAULT_VOICE_PREFS;
  }
};

export const voicePrefs = (): IVoicePrefs => (cached ??= read());

export const setVoicePrefs = (patch: Partial<IVoicePrefs>): void => {
  cached = { ...voicePrefs(), ...patch };
  try {
    window.localStorage.setItem(KEY, JSON.stringify(cached));
  } catch {
    // Private mode or a full storage: the choice holds for this page.
  }
  for (const listener of [...listeners]) listener();
};

/** What a dictation asks the machine to hear. */
export type TDictationLang = 'ru' | 'en' | 'auto';

/** The language a dictation asks the machine for. */
export const dictationLang = (): TDictationLang => {
  const { lang } = voicePrefs();
  return lang === 'ui' ? locale() : lang;
};

export const useVoicePrefs = (): IVoicePrefs =>
  useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      const onStorage = (event: StorageEvent) => {
        if (event.key !== KEY) return;
        cached = null;
        listener();
      };
      window.addEventListener('storage', onStorage);
      return () => {
        listeners.delete(listener);
        window.removeEventListener('storage', onStorage);
      };
    },
    voicePrefs,
    () => DEFAULT_VOICE_PREFS,
  );
