import { describe, expect, it } from 'vitest';
import type { IDayCalendar } from '@/next/features/day/feed';
import {
  isFree,
  openingDay,
  OWN_MS,
  percentOf,
  slotKey,
  snapStart,
  stripDay,
} from '@/next/features/day/strip';

const at = (hm: string, day = 12) => `2026-03-${day}T${hm}:00Z`;
const ms = (hm: string, day = 12) => Date.parse(at(hm, day));

const calendar: IDayCalendar = {
  from: at('08:00'),
  to: at('20:00'),
  slots: [
    { id: 'late', kind: 'meet', title: 'Late', start: at('16:00'), end: at('16:30') },
    { id: 'planning', kind: 'meet', title: 'Planning', start: at('11:00'), end: at('12:00') },
    { id: 'sync', kind: 'meet', title: 'Sync', start: at('11:30'), end: at('12:30') },
    { id: 'focus', kind: 'focus', title: 'Focus', start: at('12:00'), end: at('14:00') },
    { id: 'offer', kind: 'meet', title: 'Offer', start: at('16:15'), end: at('16:45'), offer: true },
    { kind: 'meet', title: 'Yesterday', start: at('10:00', 11), end: at('11:00', 11) },
  ],
  flags: [
    { who: 'agent', kind: 'run', title: 'Deck', due: at('12:30') },
    { who: 'me', kind: 'reply', title: 'Dana' },
    { who: 'me', kind: 'reply', title: 'Sam' },
    { who: 'me', kind: 'promise', title: 'Contract' },
  ],
};

describe('the calendar strip as numbers', () => {
  it('puts the hours of the day along the strip', () => {
    const day = stripDay(calendar, 0);
    expect(day.hours).toHaveLength(12);
    expect(day.hours[0]).toEqual({ at: ms('08:00'), left: 0 });
    expect(day.hours[6]?.left).toBe(50);
  });

  it('stacks the meetings that share a time, and keeps a focus window and an offer apart', () => {
    const day = stripDay(calendar, 0);
    expect(day.events.map((e) => e.slots.map((s) => s.id))).toEqual([
      ['planning', 'sync'],
      ['focus'],
      ['late'],
      ['offer'],
    ]);
    const stack = day.events[0];
    expect(stack?.left).toBe(25);
    expect(stack?.end).toBe(ms('12:30'));
    expect(stack?.width).toBeCloseTo(12.5);
    expect(day.events[2]?.short).toBe(true);
  });

  it('leaves out what belongs to another day', () => {
    expect(stripDay(calendar, 0).events.flatMap((e) => e.slots.map((s) => s.title))).not.toContain(
      'Yesterday',
    );
  });

  it('hangs a promise at its deadline and piles those without one by kind', () => {
    const { flags } = stripDay(calendar, 0);
    expect(flags.map((f) => [f.who, f.reply, f.due, f.flags.length])).toEqual([
      ['agent', false, ms('12:30'), 1],
      ['me', true, null, 2],
      ['me', false, null, 1],
    ]);
  });

  it('draws nothing on a day off', () => {
    const day = stripDay({ from: at('08:00', 14), to: at('20:00', 14), off: true, slots: calendar.slots }, 2);
    expect(day).toMatchObject({ n: 2, off: true, hours: [], events: [], flags: [] });
  });

  it('names a slot by its id, or by what it is', () => {
    expect(slotKey({ id: 'a', kind: 'meet', title: 'A', start: at('09:00'), end: at('10:00') })).toBe('a');
    expect(slotKey({ kind: 'meet', title: 'A', start: at('09:00'), end: at('10:00') })).toBe(
      `meet/${at('09:00')}/A`,
    );
  });

  it('keeps a moment inside the strip', () => {
    const day = { from: ms('08:00'), to: ms('20:00') };
    expect(percentOf(day, ms('07:00'))).toBe(0);
    expect(percentOf(day, ms('14:00'))).toBe(50);
    expect(percentOf(day, ms('23:00'))).toBe(100);
  });

  it('snaps what the person puts to a quarter of an hour, inside the day', () => {
    const day = { from: ms('08:00'), to: ms('20:00') };
    expect(snapStart(day, ms('10:22'))).toBe(ms('10:00'));
    expect(snapStart(day, ms('10:24'))).toBe(ms('10:15'));
    expect(snapStart(day, ms('08:02'))).toBe(ms('08:00'));
    expect(snapStart(day, ms('19:59'))).toBe(ms('20:00') - OWN_MS);
  });

  it('knows a free place from a taken one', () => {
    const taken = [{ start: ms('11:00'), end: ms('12:00') }];
    expect(isFree(taken, ms('10:30'), ms('11:00'))).toBe(true);
    expect(isFree(taken, ms('10:45'), ms('11:15'))).toBe(false);
    expect(isFree(taken, ms('12:00'), ms('12:30'))).toBe(true);
  });

  it('opens on today while its hours last and on the next day after them', () => {
    const days = [stripDay(calendar, 0), stripDay({ from: at('08:00', 13), to: at('20:00', 13) }, 1)];
    expect(openingDay(days, ms('07:00'))).toBe(0);
    expect(openingDay(days, ms('19:59'))).toBe(0);
    expect(openingDay(days, ms('21:00'))).toBe(1);
    expect(openingDay(days.slice(0, 1), ms('21:00'))).toBe(0);
  });
});
