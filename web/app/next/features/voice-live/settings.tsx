// Settings → Accounts, under ChatGPT: the voice mode on the subscription. Off by default; the line under the
// switch says honestly what the route is.
import { useEffect, useState } from 'react';
import {
  loadVoiceState,
  saveVoiceSettings,
  VOICE_ROUTE,
  VOICE_TALK,
  type IVoiceSettings,
  type IVoiceState,
} from '@/next/features/voice-live/api';
import { Group, MenuRow, Note, SwitchRow } from '@/next/features/settings/rows';
import MascotPick from '@/next/features/voice-live/mascot-pick';
import WakeSettings from '@/next/features/voice-live/wake-settings';
import { t } from '@/next/i18n';
import VoiceSessionSettings from '@/next/features/voice-live/settings-session';
import type { MachineId } from '@/next/protocol/v2';

const voiceLabel = (v: string) => v.charAt(0).toUpperCase() + v.slice(1);

const VoiceLiveSettings = ({ machine }: { machine: MachineId }) => {
  const [state, setState] = useState<IVoiceState | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let live = true;
    void loadVoiceState(machine)
      .then((st) => live && setState(st))
      .catch(() => undefined);
    return () => {
      live = false;
    };
  }, [machine]);

  if (!state) return null;
  const kept: IVoiceSettings = {
    subscription: state.subscription,
    voice: state.voice,
    model: state.model,
    talk: state.talk,
    head: state.head,
    noBarge: !state.barge,
    about: state.about,
    notes: state.notes,
    delivery: state.delivery,
    effort: state.effort,
  };
  const save = (change: Partial<IVoiceSettings>) => {
    setBusy(true);
    saveVoiceSettings(machine, { ...kept, ...change }).then(
      (st) => {
        setState(st);
        setBusy(false);
      },
      () => setBusy(false),
    );
  };

  return (
    <Group title={t('next.chatgpt.voice.group')}>
      <SwitchRow
        label={t('next.chatgpt.voice.switch')}
        hint={t('next.chatgpt.voice.risk')}
        on={state.subscription}
        busy={busy}
        onChange={(on) => save({ subscription: on })}
      />
      {state.ready && (
        <MenuRow
          label={t('next.chatgpt.voice.voice')}
          value={state.voice}
          choices={state.voices.map((v) => ({ value: v, label: voiceLabel(v), sub: null }))}
          busy={busy}
          onPick={(v) => save({ voice: String(v) })}
        />
      )}
      {state.ready && (
        <MenuRow
          label={t('next.chatgpt.voice.talk')}
          value={state.talk}
          choices={[
            {
              value: VOICE_TALK.text,
              label: t('next.chatgpt.voice.talkText'),
              sub: t('next.chatgpt.voice.talkTextSub'),
            },
            {
              value: VOICE_TALK.voice,
              label: t('next.chatgpt.voice.talkVoice'),
              sub: t('next.chatgpt.voice.talkVoiceSub'),
            },
          ]}
          busy={busy}
          onPick={(v) => save({ talk: v === VOICE_TALK.voice ? VOICE_TALK.voice : VOICE_TALK.text })}
        />
      )}
      {state.ready && <MascotPick />}
      {state.ready && (
        <SwitchRow
          label={t('next.chatgpt.voice.barge')}
          hint={t('next.chatgpt.voice.bargeHint')}
          on={state.barge}
          busy={busy}
          onChange={(on) => save({ noBarge: !on })}
        />
      )}
      {state.ready && <VoiceSessionSettings state={state} busy={busy} save={save} />}
      {state.ready && <WakeSettings machine={machine} />}
      {state.route === VOICE_ROUTE.key && <Note>{t('next.chatgpt.voice.byKey')}</Note>}
      {state.subscription && !state.signedIn && !state.hasKey && (
        <Note>{t('next.chatgpt.voice.noLogin')}</Note>
      )}
    </Group>
  );
};

export default VoiceLiveSettings;
