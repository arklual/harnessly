// The quick settings of the voice mode (the wave): the microphone with its level, the voice that answers and whether
// speaking over it stops it. The machine's own settings (Settings → Accounts → ChatGPT), saved the way that page does.
import { useId, useState } from 'react';
import { saveVoiceSettings, settingsOf, type IVoiceSettings } from '@/next/features/voice-live/api';
import { useVoiceState } from '@/next/features/voice-live/use-voice-state';
import AllSettings from '@/next/features/voice-quick/all-settings';
import MicPick from '@/next/features/voice-quick/mic-pick';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import Switch from '@/next/ui/switch';
import { toast } from '@/next/ui/toast';

const voiceLabel = (v: string) => v.charAt(0).toUpperCase() + v.slice(1);

interface ILiveQuickProps {
  machine: MachineId | null;
  /** A call is on: the microphone it listens to is the one it started with. */
  inCall?: boolean;
  onNavigate: () => void;
}

const LiveQuick = ({ machine, inCall = false, onNavigate }: ILiveQuickProps) => {
  const id = useId();
  const [state] = useVoiceState(machine);
  const [busy, setBusy] = useState(false);

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

  return (
    <>
      <MicPick />
      {inCall && <p className="hx-vq__note">{t('next.voice.quick.nextCall')}</p>}
      {state?.ready && (
        <>
          <div className="hx-vq__row">
            <label className="hx-vq__label" htmlFor={`${id}-voice`}>
              {t('next.chatgpt.voice.voice')}
            </label>
            <select
              id={`${id}-voice`}
              className="hx-vq__select"
              value={state.voice}
              disabled={busy}
              onChange={(e) => save({ voice: e.target.value })}
            >
              {state.voices.map((v) => (
                <option key={v} value={v}>
                  {voiceLabel(v)}
                </option>
              ))}
            </select>
          </div>
          <div className="hx-vq__switch">
            <span>{t('next.chatgpt.voice.barge')}</span>
            <Switch
              label={t('next.chatgpt.voice.barge')}
              checked={state.barge}
              disabled={busy}
              onChange={(on) => save({ noBarge: !on })}
            />
          </div>
        </>
      )}
      {state && !state.ready && (
        <p className="hx-vq__note">
          {t(state.subscription ? 'next.chatgpt.voice.noLogin' : 'next.chatgpt.voice.offHint')}
        </p>
      )}
      <AllSettings section="accounts" onNavigate={onNavigate} />
    </>
  );
};

export default LiveQuick;
