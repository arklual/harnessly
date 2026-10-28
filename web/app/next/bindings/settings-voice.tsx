// Settings → «Голосовой ввод»: the machine's dictation (state, model) and this browser's microphone and language.
import { useEffect, useState } from 'react';
import VoiceSection, { type IMicrophone } from '@/next/features/settings/voice-section';
import {
  loadVoiceEngine,
  saveVoiceModel,
  type IVoiceEngine,
  type TVoiceModel,
} from '@/next/features/settings/voice-api';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { setVoicePrefs, useVoicePrefs } from '@/next/stores/voice';
import { toast } from '@/next/ui/toast';

/** While the model is being downloaded the state is asked again this often. */
const PREPARING_POLL_MS = 2_000;

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
  const microphones = useMicrophones();
  const [read, setRead] = useState<{ machine: MachineId; value: IVoiceEngine } | null>(null);
  const [busy, setBusy] = useState(false);
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
      onDevice={(deviceId) => setVoicePrefs({ deviceId })}
      onLang={(lang) => setVoicePrefs({ lang })}
      onModel={onModel}
      onCopyInstall={onCopyInstall}
    />
  );
};
