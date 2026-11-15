// The digest of the day read out by voice: the page's own phrases go to the voice by the machine's
// ChatGPT subscription (voice-live/use-digest-voice.ts). «Послушать» first asks whether the voice can start; when it
// cannot, a notice says how to turn it on and leads to Settings. The page draws the phrases and the item talked about.
import { useEffect, useRef, type ReactNode } from 'react';
import type { IDayPhrase } from '@/next/features/day/digest-phrases';
import { loadVoiceState } from '@/next/features/voice-live/api';
import DigestLine from '@/next/features/voice-live/digest-line';
import DigestListen, { DIGEST_VOICE, type TDigestVoice } from '@/next/features/voice-live/digest-listen';
import { useDigestVoice, type IDigestVoice } from '@/next/features/voice-live/use-digest-voice';
import { VOICE_FAIL, type TVoiceFail } from '@/next/features/voice-live/use-voice-call';
import { HEAD, type THead } from '@/next/features/voice-live/voice-head';
import { t } from '@/next/i18n';
import type { MachineId } from '@/next/protocol/v2';
import { goTo, ROUTE } from '@/next/routes/routes';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { toast } from '@/next/ui/toast';

/** The talk line leaves in this long (hx-day-talk-out). */
const TALK_EXIT_MS = 200;

const HEAD_OF: Record<TDigestVoice, THead> = {
  off: HEAD.idle,
  reading: HEAD.speak,
  paused: HEAD.idle,
  listening: HEAD.listen,
  thinking: HEAD.think,
  barged: HEAD.barged,
};

/** A notice that tells how to turn the voice on leads to the accounts of Settings. */
const toSettings = (fail: TVoiceFail) =>
  fail === VOICE_FAIL.off || fail === VOICE_FAIL.noLogin
    ? {
        label: t(fail === VOICE_FAIL.noLogin ? 'next.chatgpt.voice.signIn' : 'next.chatgpt.voice.settings'),
        onClick: () => goTo({ kind: ROUTE.settings, section: 'accounts' }),
      }
    : undefined;

const tell = (fail: TVoiceFail) =>
  toast(t(`next.chatgpt.voice.fail.${fail}`), { tone: 'warning', action: toSettings(fail) });

export interface IDayVoice {
  voice: IDigestVoice;
  /** «Послушать»: starts the reading, or tells how to turn the voice on. */
  listen: () => void;
}

export const useDayVoice = (phrases: readonly IDayPhrase[], machine: MachineId): IDayVoice => {
  const voice = useDigestVoice(phrases, machine);
  const told = useRef<TVoiceFail | null>(null);
  useEffect(() => {
    if (voice.fail === told.current) return;
    told.current = voice.fail;
    if (voice.fail !== null) tell(voice.fail);
  }, [voice.fail]);
  const { play } = voice;
  const listen = () => {
    loadVoiceState(machine).then(
      (st) => {
        if (st.ready) play();
        else tell(st.subscription ? VOICE_FAIL.noLogin : VOICE_FAIL.off);
      },
      () => tell(VOICE_FAIL.broken),
    );
  };
  return { voice, listen };
};

/** «Послушать», and pause / go on / end while the voice reads. */
export const DayListen = ({ dv }: { dv: IDayVoice }) => (
  <DigestListen
    state={dv.voice.state}
    onPlay={dv.listen}
    onPause={dv.voice.state === DIGEST_VOICE.paused ? dv.voice.resume : dv.voice.pause}
    onEnd={dv.voice.end}
  />
);

/** The talk about one item under the digest: the person's words and the voice's answer, with «Открыть пункт». */
export const DayTalk = ({ dv, onOpen }: { dv: IDayVoice; onOpen: (anchor: string) => void }): ReactNode => {
  const { voice } = dv;
  const live = usePresence(voice.item, TALK_EXIT_MS);
  if (live.value === null) return null;
  const anchor = live.value;
  return (
    <div className="hx-day-talk" data-closing={live.state === PRESENCE.closing || undefined}>
      <DigestLine
        head={HEAD_OF[voice.state]}
        me={voice.heard}
        said={voice.said}
        rest={voice.rest}
        level={voice.level}
        onOpen={() => onOpen(anchor)}
        openLabel={t('next.day.voice.open')}
      />
    </div>
  );
};
