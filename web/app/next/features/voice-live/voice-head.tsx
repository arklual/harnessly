// The voice agent's head (the designer's «Человечек harnessly», harnessly design project → mascot/): a small living
// sign at the start of the field's line while the voice is on. One replaceable component: the look may change, the
// states stay (idle, listen, think, speak, barged, error); a mood (neutral, joy, sad, thinking, surprised) rides on them.
// KIND picks the «h» (the default) or «Головка» (face); Settings → voice keeps the choice.
import { useEffect, useRef } from 'react';
import { FIELD_FLOOR, type TFieldFloor } from '@/next/features/voice-live/field';
import VoiceHeadParts from '@/next/features/voice-live/voice-head-svg';
import { HEAD_KIND, type THeadKind } from '@/next/features/voice-live/api';
import { useVoiceMood, type VoiceMood } from '@/next/features/voice-live/voice-mood';
import '@/next/styles/voice-head.css';
import '@/next/styles/voice-mood.css';

export const HEAD = {
  idle: 'idle',
  listen: 'listen',
  think: 'think',
  speak: 'speak',
  barged: 'barged',
  error: 'error',
} as const;
export type THead = (typeof HEAD)[keyof typeof HEAD];

const OF_FLOOR: Record<TFieldFloor, THead> = {
  [FIELD_FLOOR.listening]: HEAD.listen,
  [FIELD_FLOOR.starting]: HEAD.idle,
  [FIELD_FLOOR.thinking]: HEAD.think,
  [FIELD_FLOOR.speaking]: HEAD.speak,
  [FIELD_FLOOR.muted]: HEAD.idle,
  [FIELD_FLOOR.failed]: HEAD.error,
};

/** The head's state for the field's floor. */
export const headOf = (floor: TFieldFloor): THead => OF_FLOOR[floor];

interface IVoiceHeadProps {
  state: THead;
  kind?: THeadKind;
  /** The voice's loudness, 0…1: while it speaks the mouth follows it instead of the talking loop. */
  level?: (() => number) | null;
  /** The face on top of the state (voice-mood.ts); without it the head takes the shared one the agent set. */
  mood?: VoiceMood;
}

const VoiceHead = ({ state, kind = HEAD_KIND.h, level = null, mood }: IVoiceHeadProps) => {
  const shared = useVoiceMood();
  const ref = useRef<SVGSVGElement | null>(null);
  const led = state === HEAD.speak && level !== null;
  useEffect(() => {
    const svg = ref.current;
    if (!svg || !led || !level) return;
    let frame = 0;
    const tick = () => {
      svg.style.setProperty('--m-tv', (Math.min(1, level()) * 1.3).toFixed(3));
      frame = requestAnimationFrame(tick);
    };
    frame = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(frame);
      svg.style.removeProperty('--m-tv');
    };
  }, [led, level]);
  return (
    <svg
      ref={ref}
      className="hx-head"
      viewBox="0 0 96 96"
      data-k={kind}
      data-s={state}
      data-m={mood ?? shared}
      data-vol={led || undefined}
      aria-hidden="true"
    >
      <VoiceHeadParts />
    </svg>
  );
};

export default VoiceHead;
