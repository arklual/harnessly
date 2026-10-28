// The montage's model: which parts of the transcript the automatic cut drops, which of those the person brought back,
// how long the cut is, which slide and which phrase are on screen at a moment of the video. Pure. Times are ms of the
// recording, as the runtime keeps them.
import type { IDeckEdit, IDeckPart, IDeckSegment, IDeckTake } from '@/next/features/designly/deck/types';

/** The index a kept part is stored under in `edit.kept`: `<segment>.<part>`. */
export const partKey = (segment: number, part: number): string => `${segment}.${part}`;

export interface ICutRange {
  t0: number;
  t1: number;
}

/** The part is dropped from the video: the cut is on, the part is one the cut takes, and the person has not kept it. */
export const isDropped = (edit: IDeckEdit, part: IDeckPart, key: string): boolean =>
  edit.cut && part.cut && !edit.kept.includes(key);

/** Time ranges the video skips, in order, touching ranges joined. */
export const cutRanges = (segments: readonly IDeckSegment[], edit: IDeckEdit): ICutRange[] => {
  const out: ICutRange[] = [];
  segments.forEach((segment, si) => {
    segment.parts.forEach((part, pi) => {
      if (!isDropped(edit, part, partKey(si, pi)) || part.t1 <= part.t0) return;
      const last = out[out.length - 1];
      if (last && part.t0 <= last.t1) last.t1 = Math.max(last.t1, part.t1);
      else out.push({ t0: part.t0, t1: part.t1 });
    });
  });
  return out;
};

export const skippedMs = (ranges: readonly ICutRange[]): number =>
  ranges.reduce((sum, r) => sum + (r.t1 - r.t0), 0);

/** Where playback at `ms` must jump to if `ms` falls inside a skipped range; null when it plays. */
export const skipTarget = (ranges: readonly ICutRange[], ms: number): number | null => {
  const hit = ranges.find((r) => ms >= r.t0 && ms < r.t1);
  return hit ? hit.t1 : null;
};

/** Clicking a part the cut took: bring it back, or take it again. Returns the new `kept` list. */
export const toggleKept = (edit: IDeckEdit, key: string): string[] =>
  edit.kept.includes(key) ? edit.kept.filter((k) => k !== key) : [...edit.kept, key];

/** The index of the segment that holds `ms`; the last one before it in a gap; -1 before the first. */
export const segmentAt = (segments: readonly IDeckSegment[], ms: number): number => {
  let found = -1;
  segments.forEach((segment, i) => {
    if (segment.t0 <= ms) found = i;
  });
  return found;
};

/** The slide on screen at `ms`: the newest take that has begun (a retake replaces the old one); null before any. */
export const slideAt = (takes: readonly IDeckTake[], ms: number): string | null => {
  let found: IDeckTake | null = null;
  for (const take of takes) {
    if (take.startMs <= ms && (!found || take.startMs >= found.startMs)) found = take;
  }
  return found ? found.page : null;
};

const SUBTITLE_WORDS = 9;

/** The line under the picture: the first words of what is said in the segment that holds `ms`, without the dropped parts. */
export const subtitleAt = (
  segments: readonly IDeckSegment[],
  edit: IDeckEdit,
  ms: number,
  words = SUBTITLE_WORDS,
): string => {
  const si = segmentAt(segments, ms);
  const segment = segments[si];
  if (!segment) return '';
  const spoken = segment.parts
    .map((part, pi) => (part.kind === 'pause' || isDropped(edit, part, partKey(si, pi)) ? '' : part.text))
    .join(' ')
    .split(/\s+/)
    .filter(Boolean);
  return spoken.length > words ? `${spoken.slice(0, words).join(' ')}…` : spoken.join(' ');
};

/** The segment's duration for the track: never zero, so a tiny one can still be clicked. */
export const trackWeight = (segment: IDeckSegment): number => Math.max(1, segment.t1 - segment.t0);
