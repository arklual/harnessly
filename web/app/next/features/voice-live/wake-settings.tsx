// Settings → voice: «Привет, harnessly» starts the voice (wake.ts). Off by default; it saves its own field only, so the
// other voice settings stay as they are.
import { useEffect, useState } from 'react';
import { loadVoiceState, saveVoiceWake, type IVoiceWake } from '@/next/features/voice-live/api';
import { WAKE_SILENCE_DEFAULT } from '@/next/features/voice-live/wake';
import { SegRow, SwitchRow } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';

const SILENCE_CHOICES = [10, 20, 30, 60] as const;

const WakeSettings = ({ machine }: { machine: MachineId }) => {
  const [wake, setWake] = useState<IVoiceWake | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    void loadVoiceState(machine)
      .then((st) => live && setWake(st.wake ?? null))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [machine]);

  // A machine that does not know the phrase yet shows nothing.
  if (!wake) return null;
  const save = (change: Partial<IVoiceWake>) => {
    setBusy(true);
    saveVoiceWake(machine, { ...wake, ...change }).then(
      (st) => {
        setWake(st.wake ?? null);
        setBusy(false);
      },
      () => setBusy(false),
    );
  };

  return (
    <>
      <SwitchRow
        label={t('next.chatgpt.voice.wake')}
        hint={t('next.chatgpt.voice.wakeHint')}
        on={wake.on}
        busy={busy}
        onChange={(on) => save({ on })}
      />
      {wake.on && (
        <SegRow
          label={t('next.chatgpt.voice.wakeSilence')}
          value={wake.silence || WAKE_SILENCE_DEFAULT}
          choices={SILENCE_CHOICES.map((n) => ({
            value: n,
            label: t('next.chatgpt.voice.wakeSilenceSec', { n }),
          }))}
          busy={busy}
          onPick={(silence) => save({ silence })}
        />
      )}
    </>
  );
};

export default WakeSettings;
