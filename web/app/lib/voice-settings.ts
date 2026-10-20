/** Voice input preferences: Settings → «Voice input» and the composer's mic dropdown share these. */

import { isAssignableVoiceKey } from '@/lib/voice-key';

export type TVoiceEngine = 'auto' | 'local' | 'browser' | 'server';
export type TVoiceLang = 'ru' | 'en' | 'auto';
export type TVoiceMode = 'hold' | 'toggle';

export interface IVoiceSettings {
  /** `auto` picks in-tab Whisper (`local`) where the browser can run it, then Web Speech, then server ASR. */
  engine: TVoiceEngine;
  lang: TVoiceLang;
  mode: TVoiceMode;
  autoSend: boolean;
  /** Server engine only: Whisper in the tab and the browser's own recognizer already punctuate. */
  punctuation: boolean;
  deviceId: string | null;
  /** `KeyboardEvent.code` of the dictation key: a tap starts dictation, the next ends it. Null: no such key. */
  key: string | null;
}

export const DEFAULT_VOICE_SETTINGS: IVoiceSettings = {
  engine: 'auto',
  lang: 'ru',
  mode: 'hold',
  autoSend: false,
  punctuation: true,
  deviceId: null,
  key: null,
};

const VOICE_ENGINES: readonly TVoiceEngine[] = ['auto', 'local', 'browser', 'server'];
const VOICE_LANGS: readonly TVoiceLang[] = ['ru', 'en', 'auto'];
const VOICE_MODES: readonly TVoiceMode[] = ['hold', 'toggle'];

export const isValidVoiceSettingsPatch = (value: unknown): value is Partial<IVoiceSettings> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return false;
  const v = value as Record<string, unknown>;
  if ('engine' in v && !VOICE_ENGINES.includes(v.engine as TVoiceEngine)) return false;
  if ('lang' in v && !VOICE_LANGS.includes(v.lang as TVoiceLang)) return false;
  if ('mode' in v && !VOICE_MODES.includes(v.mode as TVoiceMode)) return false;
  if ('autoSend' in v && typeof v.autoSend !== 'boolean') return false;
  if ('punctuation' in v && typeof v.punctuation !== 'boolean') return false;
  if ('deviceId' in v && v.deviceId !== null && typeof v.deviceId !== 'string') return false;
  if ('key' in v && v.key !== null && !isAssignableVoiceKey(v.key)) return false;
  return true;
};

export const mergeVoiceSettings = (base: IVoiceSettings, patch: Partial<IVoiceSettings>): IVoiceSettings => ({
  ...base,
  ...patch,
});
