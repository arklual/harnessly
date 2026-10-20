// The person's settings the agents page reads: terminal font and line height, voice input. Stored by the runtime
// (GET/PATCH /api/config); the page keeps a copy and writes changes back.
import { create } from 'zustand';
import { appFetch } from '@/lib/app-path';
import { DEFAULT_VOICE_SETTINGS, mergeVoiceSettings, type IVoiceSettings } from '@/lib/voice-settings';

export interface IConfigInitialData {
  fontSize?: string;
  lineHeight?: string;
  lineHeightCustom?: number;
  voiceSettings?: Partial<IVoiceSettings>;
  terminalTheme?: string;
  [k: string]: unknown;
}

interface IConfigState {
  fontSize: string;
  lineHeight: string;
  lineHeightCustom: number;
  voiceSettings: IVoiceSettings;
  hydrate: (data: IConfigInitialData | null | undefined) => void;
  setVoiceSettings: (patch: Partial<IVoiceSettings>) => void;
}

const save = (body: Record<string, unknown>) =>
  void appFetch('/api/config', {
    method: 'PATCH',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  }).catch(() => {});

const useConfigStore = create<IConfigState>((set, get) => ({
  fontSize: 'normal',
  lineHeight: 'normal',
  lineHeightCustom: 1.1,
  voiceSettings: DEFAULT_VOICE_SETTINGS,
  hydrate: (data) => {
    if (!data) return;
    set({
      fontSize: typeof data.fontSize === 'string' ? data.fontSize : get().fontSize,
      lineHeight: typeof data.lineHeight === 'string' ? data.lineHeight : get().lineHeight,
      lineHeightCustom:
        typeof data.lineHeightCustom === 'number' ? data.lineHeightCustom : get().lineHeightCustom,
      voiceSettings: mergeVoiceSettings(DEFAULT_VOICE_SETTINGS, data.voiceSettings ?? {}),
    });
  },
  setVoiceSettings: (patch) => {
    const next = mergeVoiceSettings(get().voiceSettings, patch);
    set({ voiceSettings: next });
    save({ voiceSettings: next });
  },
}));

export default useConfigStore;
