// Settings → «Голосовой ввод»: the machine's dictation (state, model) and this browser's microphone and language.
import { useEffect, useState } from 'react';
import VoiceSection, { type IMicrophone, type IRemoteForm } from '@/next/features/settings/voice-section';
import {
  loadVoiceEngine,
  saveVoiceEngine,
  saveVoiceModel,
  type IVoiceEngine,
  type TVoiceModel,
} from '@/next/features/settings/voice-api';
import { useSpeechKeys } from '@/next/bindings/settings-speech';
import { t } from '@/next/i18n';
import {
  patchSettings,
  useVoiceSettings,
  type PatchResult,
  type VoiceEngineId,
  type VoiceWord,
} from '@/next/stores/me';
import type { MachineId } from '@/next/protocol/v2';
import { setVoicePrefs, useVoicePrefs } from '@/next/stores/voice';
import { toast } from '@/next/ui/toast';

/** While the model is being downloaded the state is asked again this often. */
const PREPARING_POLL_MS = 2_000;

/** The words for a change of the dictation's settings that did not go through. */
const refusalText = (res: Exclude<PatchResult, { r: 'accepted' }>): string =>
  res.r === 'refused' ? t(`next.refuse.${res.code}`) : t('next.failure.network');

/** The browser's microphones; their names come only after the page was allowed a microphone once. */
const useMicrophones = (): IMicrophone[] | null => {
  const [list, setList] = useState<IMicrophone[] | null>(null);
  useEffect(() => {
    const media = navigator.mediaDevices;
    if (!media?.enumerateDevices) return;
    let live = true;
    const read = () =>
      void media.enumerateDevices().then(
        (devices) => {
          if (!live) return;
          const inputs = devices.filter((d) => d.kind === 'audioinput' && d.deviceId && d.label);
          setList(inputs.length ? inputs.map((d) => ({ id: d.deviceId, label: d.label })) : null);
        },
        () => undefined,
      );
    read();
    media.addEventListener?.('devicechange', read);
    return () => {
      live = false;
      media.removeEventListener?.('devicechange', read);
    };
  }, []);
  return list;
};

export const VoicePage = ({ machine }: { machine: MachineId }) => {
  const prefs = useVoicePrefs();
  const voice = useVoiceSettings();
  const speech = useSpeechKeys(machine);
  const [voiceBusy, setVoiceBusy] = useState(false);
  const microphones = useMicrophones();
  const [read, setRead] = useState<{ machine: MachineId; value: IVoiceEngine } | null>(null);
  const [busy, setBusy] = useState(false);
  const [engineBusy, setEngineBusy] = useState(false);
  const engine = read?.machine === machine ? read.value : null;
  const preparing = engine?.state === 'preparing';

  useEffect(() => {
    let live = true;
    const load = () =>
      void loadVoiceEngine(machine).then((value) => {
        if (live && value) setRead({ machine, value });
      });
    load();
    const timer = preparing ? setInterval(load, PREPARING_POLL_MS) : null;
    return () => {
      live = false;
      if (timer) clearInterval(timer);
    };
  }, [machine, preparing]);

  // Pessimistic: the choice moves when the machine has kept it.
  const onModel = (model: TVoiceModel) => {
    setBusy(true);
    void saveVoiceModel(machine, model).then((value) => {
      setBusy(false);
      if (value) setRead({ machine, value });
      else toast(t('next.settings.failed'), { tone: 'error' });
    });
  };

  // Pessimistic as well: the engine is the machine's once it has kept it. A refusal comes back in the machine's words.
  const onRemote = async (form: IRemoteForm): Promise<string | null> => {
    setEngineBusy(true);
    const res = await saveVoiceEngine(machine, { engine: 'remote', ...form });
    setEngineBusy(false);
    if (res === null) return t('next.settings.failed');
    if ('error' in res) return res.error;
    setRead({ machine, value: res });
    return null;
  };

  const onBuiltin = () => {
    setEngineBusy(true);
    void saveVoiceEngine(machine, { engine: 'builtin' }).then((res) => {
      setEngineBusy(false);
      if (res && 'decoder' in res) setRead({ machine, value: res });
      else toast(res?.error ?? t('next.settings.failed'), { tone: 'error' });
    });
  };

  // Pessimistic: the choice moves when the account has kept it.
  const onVoiceEngine = (next: VoiceEngineId) => {
    setVoiceBusy(true);
    void patchSettings({ voice: { ...voice, engine: next } }).then((res) => {
      setVoiceBusy(false);
      if (res.r !== 'accepted') toast(refusalText(res), { tone: 'error' });
    });
  };

  const onWords = async (words: VoiceWord[]): Promise<string | null> => {
    const res = await patchSettings({ voice: { ...voice, words } });
    return res.r === 'accepted' ? null : refusalText(res);
  };

  const onCopyInstall = () => {
    if (!engine) return;
    void navigator.clipboard?.writeText(engine.install).then(
      () => toast(t('next.settings.voice.copied')),
      () => toast(t('next.shellData.noClipboard'), { tone: 'error' }),
    );
  };

  return (
    <VoiceSection
      engine={engine}
      prefs={prefs}
      microphones={microphones}
      modelBusy={busy}
      engineBusy={engineBusy}
      voice={voice}
      speechKeys={speech.state.k === 'ready' ? speech.state.keys : null}
      voiceBusy={voiceBusy}
      onVoiceEngine={onVoiceEngine}
      onWords={onWords}
      onDevice={(deviceId) => setVoicePrefs({ deviceId })}
      onLang={(lang) => setVoicePrefs({ lang })}
      onModel={onModel}
      onBuiltin={onBuiltin}
      onRemote={onRemote}
      onCopyInstall={onCopyInstall}
    />
  );
};
