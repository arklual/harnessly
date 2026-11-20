// The quick settings of the dictation (the mic): the microphone with its level, then the language and the model that
// recognizes, each a row that opens its choices. The same stores as Settings → Voice input, so a change here is a change
// there.
import { useEffect, useRef, useState } from 'react';
import { loadSpeechKeys, type SpeechKeys } from '@/next/features/providers/speech-keys';
import { engineChoices } from '@/next/features/settings/voice-engines';
import { MicHead, useMic } from '@/next/features/voice-quick/mic';
import {
  QuickFoot,
  QuickItem,
  QuickList,
  QuickRows,
  useReturnFocus,
  type IOption,
} from '@/next/features/voice-quick/parts';
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

const SUB = { mic: 'mic', lang: 'lang', engine: 'engine' } as const;

interface IBodyProps {
  sub: string | null;
  setSub: (sub: string | null) => void;
  onNavigate: () => void;
}

const DictationQuick = ({ sub, setSub, onNavigate }: IBodyProps) => {
  const root = useRef<HTMLDivElement>(null);
  const mic = useMic();
  const prefs = useVoicePrefs();
  const voice = useVoiceSettings();
  const keys = useSpeechKeysOf();
  useReturnFocus(sub, root);

  // Pessimistic, as in the settings: the choice moves when the account has kept it.
  const pickEngine = (engine: string) => {
    void patchSettings({ voice: { ...voice, engine: engine as VoiceEngineId } }).then((res) => {
      if (res.r !== 'accepted') toast(t('next.settings.failed'), { tone: 'error' });
    });
  };

  const langs: IOption[] = VOICE_LANGS.map((value) => ({
    value,
    label: t(`next.settings.voice.lang.${value}`),
  }));
  const engines: IOption[] = engineChoices(keys).map((c) => ({
    value: String(c.value),
    label: c.label,
    sub: c.sub,
    disabled: c.disabled,
  }));
  const back = () => setSub(null);

  if (sub === SUB.mic)
    return (
      <QuickList
        title={t('next.settings.voice.mic')}
        options={mic.options}
        value={mic.value}
        onPick={mic.pick}
        onBack={back}
      />
    );
  if (sub === SUB.lang)
    return (
      <QuickList
        title={t('next.voice.quick.lang')}
        options={langs}
        value={prefs.lang}
        onPick={(v) => setVoicePrefs({ lang: v as TVoiceLang })}
        onBack={back}
      />
    );
  if (sub === SUB.engine)
    return (
      <QuickList
        title={t('next.settings.voice.recognizer.label')}
        options={engines}
        value={voice.engine}
        onPick={pickEngine}
        onBack={back}
      />
    );

  return (
    <div ref={root}>
      <MicHead mic={mic} title={t('next.settings.voice.mic')} onOpen={() => setSub(SUB.mic)} />
      <QuickRows>
        <QuickItem
          id={SUB.lang}
          label={t('next.voice.quick.lang')}
          value={t(`next.settings.voice.lang.${prefs.lang}`)}
          onOpen={() => setSub(SUB.lang)}
        />
        <QuickItem
          id={SUB.engine}
          label={t('next.settings.voice.recognizer.label')}
          value={t(`next.voice.quick.engine.${voice.engine}`)}
          onOpen={() => setSub(SUB.engine)}
        />
      </QuickRows>
      <QuickFoot section="voice" onNavigate={onNavigate} />
    </div>
  );
};

export default DictationQuick;
