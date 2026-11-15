// The calendar strip as numbers: where each hour, meeting and promise of a day stands along it (in percent of the
// day's span), which meetings share a time and go in one stack, which promises without a deadline go in one pile. No
// DOM here: the component draws what this says.
import {
  FLAG_WHO,
  SLOT,
  type IDayCalendar,
  type IDayFlag,
  type IDaySlot,
  type TFlagWho,
} from '@/next/features/day/feed';
import { HOUR_MS, MINUTE_MS } from '@/next/features/day/time';

/** A meeting this short is drawn narrow, with its name only. */
const SHORT_MS = 30 * MINUTE_MS;
/** What the person puts into a free place takes half an hour, snapped to a quarter. */
export const OWN_MS = 30 * MINUTE_MS;
const SNAP_MS = 15 * MINUTE_MS;

export interface IStripEvent {
  key: string;
  /** One slot, or the meetings that overlap: a stack. */
  slots: IDaySlot[];
  start: number;
  end: number;
  left: number;
  width: number;
  short: boolean;
}

export interface IStripFlag {
  key: string;
  /** One promise with a deadline, or every promise of a kind without one. */
  flags: IDayFlag[];
  who: TFlagWho;
  reply: boolean;
  /** The deadline; null for a pile that stands beside «now». */
  due: number | null;
}

export interface IStripDay {
  n: number;
  from: number;
  to: number;
  off: boolean;
  hours: { at: number; left: number }[];
  events: IStripEvent[];
  flags: IStripFlag[];
}

/** The key the feed's text links name a slot by. */
export const slotKey = (slot: IDaySlot): string => slot.id ?? `${slot.kind}/${slot.start}/${slot.title}`;

export const percentOf = (day: { from: number; to: number }, at: number): number => {
  const span = Math.max(day.to - day.from, 1);
  return Math.max(0, Math.min(100, ((at - day.from) / span) * 100));
};

const overlaps = (a: number, z: number, b: number, y: number) => a < y && z > b;

export const stripDay = (calendar: IDayCalendar, n: number): IStripDay => {
  const from = Date.parse(calendar.from);
  const to = Date.parse(calendar.to);
  const day = { n, from, to, off: calendar.off === true };
  if (day.off) return { ...day, hours: [], events: [], flags: [] };

  const hours: IStripDay['hours'] = [];
  for (let at = Math.ceil(from / HOUR_MS) * HOUR_MS; at < to; at += HOUR_MS)
    hours.push({ at, left: percentOf(day, at) });

  const slots = (calendar.slots ?? [])
    .map((slot) => ({ slot, start: Date.parse(slot.start), end: Date.parse(slot.end) }))
    .filter((s) => overlaps(s.start, s.end, from, to))
    .sort((a, b) => a.start - b.start);
  const groups: (typeof slots)[] = [];
  for (const s of slots) {
    const last = groups[groups.length - 1];
    const lastEnd = last ? Math.max(...last.map((x) => x.end)) : 0;
    // Meetings at one time stand in a stack, the earlier one on top; a focus window and an offer never join one.
    const stacks = (slot: IDaySlot) => slot.kind === SLOT.meet && slot.offer !== true;
    const head = last?.[0];
    if (last && head && stacks(s.slot) && stacks(head.slot) && s.start < lastEnd) last.push(s);
    else groups.push([s]);
  }
  const events = groups.flatMap((group): IStripEvent[] => {
    const first = group[0];
    if (!first) return [];
    const end = Math.max(...group.map((x) => x.end));
    return [
      {
        key: slotKey(first.slot),
        slots: group.map((x) => x.slot),
        start: first.start,
        end,
        left: percentOf(day, first.start),
        width: percentOf(day, end) - percentOf(day, first.start),
        short: end - first.start <= SHORT_MS,
      },
    ];
  });

  const flags: IStripFlag[] = [];
  const piles = new Map<string, IStripFlag>();
  (calendar.flags ?? []).forEach((flag, i) => {
    const who = flag.who === FLAG_WHO.agent ? FLAG_WHO.agent : FLAG_WHO.me;
    const reply = flag.kind === 'reply';
    if (flag.due !== undefined) {
      flags.push({ key: `due/${i}`, flags: [flag], who, reply, due: Date.parse(flag.due) });
      return;
    }
    const key = `${who}/${flag.kind}`;
    const pile = piles.get(key);
    if (pile) pile.flags.push(flag);
    else {
      const made: IStripFlag = { key: `pile/${key}`, flags: [flag], who, reply, due: null };
      piles.set(key, made);
      flags.push(made);
    }
  });

  return { ...day, hours, events, flags };
};

/** The start of what the person puts where they point: a quarter of an hour, inside the day. */
export const snapStart = (day: { from: number; to: number }, at: number): number => {
  const snapped = Math.round((at - OWN_MS / 2) / SNAP_MS) * SNAP_MS;
  return Math.min(Math.max(snapped, day.from), day.to - OWN_MS);
};

/** Whether nothing stands in the span: the feed's meetings and what the person has put themselves. */
export const isFree = (
  taken: readonly { start: number; end: number }[],
  start: number,
  end: number,
): boolean => !taken.some((x) => overlaps(start, end, x.start, x.end));

/** The day the strip opens on: today while its hours last, the next one in the evening and at night. */
export const openingDay = (days: readonly IStripDay[], now: number): number => {
  const today = days[0];
  if (!today || now < today.to || days.length < 2) return 0;
  return 1;
};
