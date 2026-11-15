// «Послушать» of the day's digest (harnessly design project → «Главная — дайджест голосом»): the voice's own
// controls; the page of the day owns what is read and when. Before: a soft accent pill «▶ Послушать». While the agent
// tells it: a pill with the voice's head, the small wave, pause/go on and «Закончить». The phrase being said is lit on
// the page by `DIGEST_PHRASE` (a class for the page's spans, `data-cur` on the one said now).
import VoiceHead, { HEAD, type THead } from '@/next/features/voice-live/voice-head';
import MiniWave from '@/next/features/voice-live/mini-wave';
import { VOICE_LOOK, type TVoiceLook } from '@/next/features/voice-live/field';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import { useVoiceMascot } from '@/next/stores/me';
import '@/next/styles/voice-layer.css';

/** The span of one phrase of the digest: lit while the voice says it (`data-cur="true"`), the rest dimmed meanwhile. */
export const DIGEST_PHRASE = 'hx-digest-phrase';
/** On the digest's block while it is read: the phrases not being said dim. */
export const DIGEST_READING = 'data-digest-voice';

export const DIGEST_VOICE = {
  off: 'off',
  reading: 'reading',
  paused: 'paused',
  listening: 'listening',
  thinking: 'thinking',
  barged: 'barged',
} as const;
export type TDigestVoice = (typeof DIGEST_VOICE)[keyof typeof DIGEST_VOICE];

const HEAD_OF: Record<Exclude<TDigestVoice, 'off'>, THead> = {
  reading: HEAD.speak,
  paused: HEAD.idle,
  listening: HEAD.listen,
  thinking: HEAD.think,
  barged: HEAD.barged,
};
const LOOK_OF: Record<Exclude<TDigestVoice, 'off'>, TVoiceLook | null> = {
  reading: VOICE_LOOK.speak,
  paused: null,
  listening: VOICE_LOOK.listen,
  thinking: VOICE_LOOK.think,
  barged: VOICE_LOOK.listen,
};

/** The controls leave in this long (hx-voice-pop-out). */
const EXIT_MS = 200;

interface IDigestListenProps {
  state: TDigestVoice;
  onPlay: () => void;
  /** Pause while reading, go on while paused. */
  onPause: () => void;
  onEnd: () => void;
}

const DigestListen = ({ state, onPlay, onPause, onEnd }: IDigestListenProps) => {
  const kind = useVoiceMascot();
  const live = usePresence(state === DIGEST_VOICE.off ? null : state, EXIT_MS);
  if (live.value === null) {
    return (
      <button
        type="button"
        className="hx-digest-play"
        title={t('next.chatgpt.voice.digest.hint')}
        onClick={onPlay}
      >
        <Icon name="play" size="sm" />
        {t('next.chatgpt.voice.digest.play')}
      </button>
    );
  }
  const now = live.value;
  const look = LOOK_OF[now];
  const paused = now === DIGEST_VOICE.paused;
  const pauseLabel = t(paused ? 'next.chatgpt.voice.digest.resume' : 'next.chatgpt.voice.digest.pause');
  return (
    <span className="hx-digest-ctl" data-closing={live.state === PRESENCE.closing || undefined}>
      <VoiceHead state={HEAD_OF[now]} kind={kind} />
      <MiniWave look={look ?? 'idle'} />
      <button
        type="button"
        className="hx-digest-ctl__b"
        aria-label={pauseLabel}
        title={pauseLabel}
        onClick={onPause}
      >
        <Icon name={paused ? 'play' : 'pause'} size="sm" />
      </button>
      <button
        type="button"
        className="hx-digest-ctl__b"
        aria-label={t('next.chatgpt.voice.digest.end')}
        title={t('next.chatgpt.voice.digest.end')}
        onClick={onEnd}
      >
        <Icon name="x" size="sm" />
      </button>
    </span>
  );
};

export default DigestListen;
