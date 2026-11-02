// The recognition models a person may pick for dictation, as the «Голосовой ввод» menu lists them. A cloud model without
// its key on the machine is shown but cannot be picked; its line says where the key goes.
import type { SpeechKeys } from '@/next/features/providers/speech-keys';
import type { IChoice } from '@/next/features/settings/options';
import { t } from '@/next/i18n';
import { VOICE_ENGINE_IDS, type VoiceEngineId } from '@/next/stores/me';

/** The key each cloud model needs; the machine's own model needs none. */
const KEY_OF = { openai: 'openai', yandex: 'yandex' } as const;

const hasKey = (engine: VoiceEngineId, keys: SpeechKeys | null): boolean => {
  if (engine === 'local') return true;
  return keys !== null && keys[KEY_OF[engine]].configured;
};

export const engineChoices = (keys: SpeechKeys | null): IChoice[] =>
  VOICE_ENGINE_IDS.map((engine) => {
    const ready = hasKey(engine, keys);
    return {
      value: engine,
      label: t(`next.settings.voice.recognizer.${engine}`),
      sub: ready
        ? t(`next.settings.voice.recognizer.${engine}.sub`)
        : t('next.settings.voice.recognizer.noKey'),
      disabled: !ready,
    };
  });
