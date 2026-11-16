import { describe, expect, it } from 'vitest';
import { FIELD_FLOOR } from '@/next/features/voice-live/field';
import { ease, ORB, orbLook, orbOf, outline, tailOf } from '@/next/features/voice-live/orb-model';

describe('the orb of the full-screen talk', () => {
  it('maps every floor to a state', () => {
    expect(orbOf(FIELD_FLOOR.listening)).toBe(ORB.listening);
    expect(orbOf(FIELD_FLOOR.thinking)).toBe(ORB.thinking);
    expect(orbOf(FIELD_FLOOR.speaking)).toBe(ORB.speaking);
    expect(orbOf(FIELD_FLOOR.failed)).toBe(ORB.failed);
    expect(orbOf(FIELD_FLOOR.starting)).toBe(ORB.waiting);
    expect(orbOf(FIELD_FLOOR.muted)).toBe(ORB.waiting);
  });

  it('pulses with the loudness while it listens and speaks, and not while it thinks', () => {
    for (const mode of [ORB.listening, ORB.speaking]) {
      expect(orbLook(mode, 0.9, 0).amp).toBeGreaterThan(orbLook(mode, 0, 0).amp * 3);
      expect(orbLook(mode, 0.9, 0).scale).toBeGreaterThan(orbLook(mode, 0, 0).scale);
    }
    expect(orbLook(ORB.thinking, 0.9, 0).amp).toBe(orbLook(ORB.thinking, 0, 0).amp);
    expect(orbLook(ORB.thinking, 0, 0).spin).toBeGreaterThan(orbLook(ORB.speaking, 0, 0).spin);
  });

  it('is dimmed when waiting or failed, and keeps the loudness in range', () => {
    expect(orbLook(ORB.failed, 1, 0).alpha).toBeLessThan(0.5);
    expect(orbLook(ORB.waiting, 1, 0).alpha).toBeLessThan(orbLook(ORB.listening, 1, 0).alpha);
    expect(orbLook(ORB.speaking, 5, 0).amp).toBe(orbLook(ORB.speaking, 1, 0).amp);
    expect(orbLook(ORB.speaking, -1, 0).amp).toBe(orbLook(ORB.speaking, 0, 0).amp);
  });

  it('eases to a target at the same speed whatever the frame rate', () => {
    const once = ease(0, 1, 0.1);
    const twice = ease(ease(0, 1, 0.05), 1, 0.05);
    expect(once).toBeCloseTo(twice, 6);
    expect(ease(0, 1, 0)).toBe(0);
  });

  it('bends the outline by the amplitude, a circle at zero', () => {
    expect(outline(8, 0, 1).every((r) => r === 1)).toBe(true);
    const bent = outline(48, 0.2, 1);
    expect(Math.max(...bent)).toBeGreaterThan(1.02);
    expect(Math.min(...bent)).toBeLessThan(0.98);
  });

  it('shows the tail of a long phrase', () => {
    expect(tailOf('  коротко   и ясно ')).toBe('коротко и ясно');
    const long = `${'слово '.repeat(40)}последнее`;
    const tail = tailOf(long, 40);
    expect(tail.startsWith('…')).toBe(true);
    expect(tail.endsWith('последнее')).toBe(true);
    expect(tail.length).toBeLessThanOrEqual(41);
  });
});
