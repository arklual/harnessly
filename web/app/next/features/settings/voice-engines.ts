// The recognition models a person may pick for dictation, as the «Голосовой ввод» menu lists them. A cloud model without
// its key on the machine is shown but cannot be picked; its line says where the key goes. The ChatGPT plan's model needs
// no key but the machine's Codex signed in by a plan.
import type { SpeechKeys } from '@/next/features/providers/speech-keys';
import type { IChoice } from '@/next/features/settings/options';
import { t } from '@/next/i18n';
import { VOICE_ENGINE_IDS, type VoiceEngineId } from '@/next/stores/me';

const isReady = (engine: VoiceEngineId, keys: SpeechKeys | null): boolean => {
  if (engine === 'local') return true;
  if (keys === null) return false;
  if (engine === 'chatgpt') return keys.chatgpt === true;
  return keys[engine].configured;
};

export const engineChoices = (keys: SpeechKeys | null): IChoice[] =>
  VOICE_ENGINE_IDS.map((engine) => {
    const ready = isReady(engine, keys);
    return {
      value: engine,
      label: t(`next.settings.voice.recognizer.${engine}`),
      sub: ready
        ? t(`next.settings.voice.recognizer.${engine}.sub`)
        : engine === 'chatgpt'
          ? t('next.settings.voice.recognizer.chatgpt.noLogin')
          : t('next.settings.voice.recognizer.noKey'),
      disabled: !ready,
    };
  });
