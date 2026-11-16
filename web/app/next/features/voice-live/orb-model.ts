// The full-screen talk's orb (voice-full.tsx): what it does in each state, as plain numbers, so the drawing
// (orb.tsx) stays a loop and the behaviour is testable.
import { FIELD_FLOOR, type TFieldFloor } from '@/next/features/voice-live/field';

/** What the orb shows: the voice listens, thinks, speaks; waits (starting, the microphone off); failed. */
export const ORB = {
  listening: 'listening',
  thinking: 'thinking',
  speaking: 'speaking',
  waiting: 'waiting',
  failed: 'failed',
} as const;
export type TOrb = (typeof ORB)[keyof typeof ORB];

export const orbOf = (floor: TFieldFloor): TOrb => {
  if (floor === FIELD_FLOOR.listening) return ORB.listening;
  if (floor === FIELD_FLOOR.thinking) return ORB.thinking;
  if (floor === FIELD_FLOOR.speaking) return ORB.speaking;
  if (floor === FIELD_FLOOR.failed) return ORB.failed;
  return ORB.waiting;
};

/** The look of the orb at one moment. All fields are targets: the drawing eases to them. */
export interface IOrbLook {
  /** How far the outline leaves the circle, in the share of the radius. */
  amp: number;
  /** The radius, 1 = the resting size. */
  scale: number;
  /** Turns per second of the inner light. */
  spin: number;
  /** How fast the outline moves. */
  speed: number;
  /** 0…1: the orb's opacity (dimmed when nothing happens or it failed). */
  alpha: number;
}

/** `level` is the loudness 0…1 of whoever speaks; `t` is the time in seconds (a slow breath that never stops). */
export const orbLook = (mode: TOrb, level: number, t: number): IOrbLook => {
  const loud = Math.min(1, Math.max(0, level));
  const breath = Math.sin(t * 1.3);
  switch (mode) {
    case ORB.listening:
      // The microphone's loudness pulses the whole orb, a little roughness with the voice.
      return {
        amp: 0.025 + loud * 0.2,
        scale: 0.96 + loud * 0.12 + breath * 0.008,
        spin: 0.04,
        speed: 0.9,
        alpha: 1,
      };
    case ORB.speaking:
      // The answer's loudness makes the wave: a bigger and quicker outline.
      return {
        amp: 0.04 + loud * 0.3,
        scale: 1 + loud * 0.1 + breath * 0.01,
        spin: 0.08,
        speed: 1.6,
        alpha: 1,
      };
    case ORB.thinking:
      // Still size, light turning inside and a soft shimmer.
      return {
        amp: 0.045 + Math.sin(t * 2.1) * 0.012,
        scale: 0.94 + breath * 0.015,
        spin: 0.45,
        speed: 0.7,
        alpha: 0.95,
      };
    case ORB.failed:
      return { amp: 0.01, scale: 0.86, spin: 0, speed: 0.2, alpha: 0.45 };
    default:
      return { amp: 0.02, scale: 0.9 + breath * 0.02, spin: 0.03, speed: 0.4, alpha: 0.7 };
  }
};

/** Eases `from` to `to`: the same speed whatever the frame rate. */
export const ease = (from: number, to: number, dt: number, rate = 9): number =>
  from + (to - from) * (1 - Math.exp(-rate * Math.max(0, dt)));

/** Radii (share of the resting radius) of the outline's points: a circle bent by a few slow waves. */
export const outline = (points: number, amp: number, phase: number, detail = 3): number[] => {
  const out: number[] = [];
  for (let i = 0; i < points; i++) {
    const a = (i / points) * Math.PI * 2;
    let bend = 0;
    for (let k = 0; k < detail; k++) {
      const n = 2 + k; // 2, 3, 4 lobes
      bend += Math.sin(a * n + phase * (1 + k * 0.45) + k * 1.7) / (1 + k * 0.8);
    }
    out.push(1 + (amp * bend) / 1.8);
  }
  return out;
};

/** The label under the orb: one short word. */
export const ORB_WORD: Record<TOrb, string> = {
  listening: 'listening',
  thinking: 'thinking',
  speaking: 'speaking',
  waiting: 'waiting',
  failed: 'failed',
};

/** The caption's tail: the last words that fit a line or two. */
export const tailOf = (text: string, max = 90): string => {
  const flat = text.replace(/\s+/g, ' ').trim();
  if (flat.length <= max) return flat;
  const cut = flat.slice(flat.length - max);
  const space = cut.indexOf(' ');
  return `…${space > 0 && space < 20 ? cut.slice(space + 1) : cut}`;
};
