// The quick settings of the voice mode (the wave): the microphone with its level, the voice that answers (a row that
// opens the voices) and whether speaking over it stops it. The machine's own settings (Settings → Accounts → ChatGPT),
// saved the way that page does.
import { useRef, useState } from 'react';
import { saveVoiceSettings, settingsOf, type IVoiceSettings } from '@/next/features/voice-live/api';
import { useVoiceState } from '@/next/features/voice-live/use-voice-state';
import { MicHead, useMic } from '@/next/features/voice-quick/mic';
import {
  QuickFoot,
  QuickItem,
  QuickList,
  QuickRows,
  QuickSwitch,
  useReturnFocus,
} from '@/next/features/voice-quick/parts';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { toast } from '@/next/ui/toast';

const voiceLabel = (v: string) => v.charAt(0).toUpperCase() + v.slice(1);

const SUB = { mic: 'mic', voice: 'voice' } as const;

interface ILiveQuickProps {
  machine: MachineId | null;
  /** A call is on: the microphone it listens to is the one it started with. */
  inCall?: boolean;
  sub: string | null;
  setSub: (sub: string | null) => void;
  onNavigate: () => void;
}

const LiveQuick = ({ machine, inCall = false, sub, setSub, onNavigate }: ILiveQuickProps) => {
  const root = useRef<HTMLDivElement>(null);
  const mic = useMic();
  const [state] = useVoiceState(machine);
  const [busy, setBusy] = useState(false);
  useReturnFocus(sub, root);

  const save = (change: Partial<IVoiceSettings>) => {
    if (!machine || !state) return;
    setBusy(true);
    saveVoiceSettings(machine, { ...settingsOf(state), ...change }).then(
      () => setBusy(false),
      () => {
        setBusy(false);
        toast(t('next.settings.failed'), { tone: 'error' });
      },
    );
  };
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
  if (sub === SUB.voice && state?.ready)
    return (
      <QuickList
        title={t('next.chatgpt.voice.voice')}
        options={state.voices.map((v) => ({ value: v, label: voiceLabel(v) }))}
        value={state.voice}
        onPick={(voice) => save({ voice })}
        onBack={back}
      />
    );

  return (
    <div ref={root}>
      <MicHead mic={mic} title={t('next.settings.voice.mic')} onOpen={() => setSub(SUB.mic)} />
      {inCall && <p className="hx-ctx__used">{t('next.voice.quick.nextCall')}</p>}
      <QuickRows>
        {state?.ready && (
          <>
            <QuickItem
              id={SUB.voice}
              label={t('next.chatgpt.voice.voice')}
              value={voiceLabel(state.voice)}
              onOpen={() => setSub(SUB.voice)}
            />
            <QuickSwitch
              label={t('next.chatgpt.voice.barge')}
              checked={state.barge}
              disabled={busy}
              onChange={(on) => save({ noBarge: !on })}
            />
          </>
        )}
      </QuickRows>
      {state && !state.ready && (
        <p className="hx-ctx__used hx-vq__note">
          {t(state.subscription ? 'next.chatgpt.voice.noLogin' : 'next.chatgpt.voice.offHint')}
        </p>
      )}
      <QuickFoot section="accounts" onNavigate={onNavigate} />
    </div>
  );
};

export default LiveQuick;
