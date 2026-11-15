// The end of a conversation the wake phrase began (wake.ts): N seconds of nobody speaking, or «пока» / «спасибо, всё»
// in a line of the person. Silence is read from the call's loudness and its lines, never from the audio element: a
// barged-in answer is muted there while the conversation goes on. The person's line is read without what the
// microphone heard of the voice itself (echo.ts), or the voice's own «спасибо» would end it.
import { useEffect, useRef } from 'react';
import { dropEcho } from '@/next/features/voice-live/echo';
import { isBye } from '@/next/features/voice-live/wake';

/** Loudness over this is someone speaking (the call's level is of whoever has the floor, 0…1). */
export const WAKE_SOUND_LEVEL = 0.08;
/** After «пока» the voice may still say goodbye: the call ends this long after the line. */
export const BYE_GRACE_MS = 1500;

interface ILineLike {
  role: string;
  text: string;
  done: boolean;
}

/** The person said goodbye in a line finished since `from`: its index, else -1. */
export const byeLine = (lines: readonly ILineLike[], from: number): number => {
  const voiceSaid = lines.filter((l) => l.role === 'assistant').map((l) => l.text);
  for (let i = from; i < lines.length; i++) {
    const l = lines[i];
    if (l.role === 'user' && l.done && isBye(dropEcho(l.text, voiceSaid.slice(-6)))) return i;
  }
  return -1;
};

interface IWakeEnd {
  /** The call is live and the phrase started it. */
  active: boolean;
  /** Something goes on that is not silence: the agent works, the voice thinks or speaks. */
  busy: boolean;
  lines: readonly ILineLike[];
  level: () => number;
  silenceSec: number;
  end: () => void;
}

export const useWakeEnd = ({ active, busy, lines, level, silenceSec, end }: IWakeEnd) => {
  const heardAt = useRef(0);
  const checked = useRef(0);
  const endRef = useRef(end);
  useEffect(() => {
    endRef.current = end;
  }, [end]);

  // Every new or growing line is someone speaking.
  useEffect(() => {
    heardAt.current = performance.now();
  }, [lines, busy]);

  useEffect(() => {
    if (!active) {
      checked.current = 0;
      return;
    }
    const at = byeLine(lines, checked.current);
    checked.current = lines.length;
    if (at < 0) return;
    const timer = window.setTimeout(() => endRef.current(), BYE_GRACE_MS);
    return () => window.clearTimeout(timer);
  }, [active, lines]);

  useEffect(() => {
    if (!active) return;
    heardAt.current = performance.now();
    const timer = window.setInterval(() => {
      const now = performance.now();
      if (busy || level() > WAKE_SOUND_LEVEL) heardAt.current = now;
      else if (now - heardAt.current > silenceSec * 1000) endRef.current();
    }, 500);
    return () => window.clearInterval(timer);
  }, [active, busy, level, silenceSec]);
};
