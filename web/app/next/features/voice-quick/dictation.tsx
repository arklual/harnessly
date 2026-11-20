// The quick settings of the dictation (the mic): the microphone with its level, the language and the model that
// recognizes. The same stores as Settings → Voice input, so a change here is a change there.
import { useEffect, useId, useState } from 'react';
import { loadSpeechKeys, type SpeechKeys } from '@/next/features/providers/speech-keys';
import { engineChoices } from '@/next/features/settings/voice-engines';
import AllSettings from '@/next/features/voice-quick/all-settings';
import MicPick from '@/next/features/voice-quick/mic-pick';
import { t } from '@/next/i18n';
import { useActiveMachine } from '@/next/stores/machines';
import { patchSettings, useVoiceSettings, type VoiceEngineId } from '@/next/stores/me';
import { setVoicePrefs, useVoicePrefs, VOICE_LANGS, type TVoiceLang } from '@/next/stores/voice';
import { toast } from '@/next/ui/toast';

const useSpeechKeysOf = (): SpeechKeys | null => {
  const machine = useActiveMachine();
  const [keys, setKeys] = useState<SpeechKeys | null>(null);
  useEffect(() => {
    if (!machine) return;
    let live = true;
    void loadSpeechKeys(machine).then((res) => {
      if (live && 'ok' in res) setKeys(res.value);
    });
    return () => {
      live = false;
    };
  }, [machine]);
  return keys;
};

const DictationQuick = ({ onNavigate }: { onNavigate: () => void }) => {
  const id = useId();
  const prefs = useVoicePrefs();
  const voice = useVoiceSettings();
  const keys = useSpeechKeysOf();
  const [busy, setBusy] = useState(false);

  // Pessimistic, as in the settings: the choice moves when the account has kept it.
  const pickEngine = (engine: VoiceEngineId) => {
    setBusy(true);
    void patchSettings({ voice: { ...voice, engine } }).then((res) => {
      setBusy(false);
      if (res.r !== 'accepted') toast(t('next.settings.failed'), { tone: 'error' });
    });
  };

  return (
    <>
      <MicPick />
      <div className="hx-vq__row">
        <label className="hx-vq__label" htmlFor={`${id}-lang`}>
          {t('next.settings.voice.lang')}
        </label>
        <select
          id={`${id}-lang`}
          className="hx-vq__select"
          value={prefs.lang}
          onChange={(e) => setVoicePrefs({ lang: e.target.value as TVoiceLang })}
        >
          {VOICE_LANGS.map((lang) => (
            <option key={lang} value={lang}>
              {t(`next.settings.voice.lang.${lang}`)}
            </option>
          ))}
        </select>
      </div>
      <div className="hx-vq__row">
        <label className="hx-vq__label" htmlFor={`${id}-engine`}>
          {t('next.settings.voice.recognizer.label')}
        </label>
        <select
          id={`${id}-engine`}
          className="hx-vq__select"
          value={voice.engine}
          disabled={busy}
          onChange={(e) => pickEngine(e.target.value as VoiceEngineId)}
        >
          {engineChoices(keys).map((c) => (
            <option key={String(c.value)} value={String(c.value)} disabled={c.disabled}>
              {c.disabled && c.sub ? `${c.label} · ${c.sub}` : c.label}
            </option>
          ))}
        </select>
      </div>
      <AllSettings section="voice" onNavigate={onNavigate} />
    </>
  );
};

export default DictationQuick;
