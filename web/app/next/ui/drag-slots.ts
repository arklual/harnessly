// The arithmetic of a carried row among its neighbours (ui/use-drag). Pure: places are spans along the list's axis in
// the list's own content (so a list that scrolls moves nothing), measured once when the row is picked up.

/** Where a row runs along the list's axis: its first and its last pixel. */
export interface ISpan {
  start: number;
  end: number;
}

const middle = (span: ISpan) => (span.start + span.end) / 2;

/**
 * The place the row that stood at `from` takes when its middle is at `center`: past a neighbour once its middle reaches
 * the neighbour's middle, as far as it goes on that side.
 */
export const slotOf = (spans: readonly ISpan[], from: number, center: number): number => {
  let to = from;
  for (let j = from + 1; j < spans.length; j += 1) {
    const span = spans[j];
    if (!span || center < middle(span)) break;
    to = j;
  }
  if (to !== from) return to;
  for (let j = from - 1; j >= 0; j -= 1) {
    const span = spans[j];
    if (!span || center > middle(span)) break;
    to = j;
  }
  return to;
};

/**
 * How far each row stands from its own place while the row of `from` is held over the place `to`: the rows between
 * step aside by its room (its size and the gap after it), and its own entry is where it settles once let go.
 */
export const shiftsOf = (spans: readonly ISpan[], from: number, to: number): number[] => {
  const out = spans.map(() => 0);
  const self = spans[from];
  const target = spans[to];
  if (!self || !target || to === from) return out;
  if (to > from) {
    const room = (spans[from + 1]?.start ?? self.end) - self.start;
    for (let j = from + 1; j <= to; j += 1) out[j] = -room;
    out[from] = target.end - self.end;
  } else {
    const room = self.end - (spans[from - 1]?.end ?? self.start);
    for (let j = to; j < from; j += 1) out[j] = room;
    out[from] = target.start - self.start;
  }
  return out;
};

/** `value` kept between `lo` and `hi`; a range that cannot hold it (a row longer than the list) keeps nothing. */
export const clampTo = (value: number, lo: number, hi: number): number =>
  hi < lo ? value : Math.min(hi, Math.max(lo, value));
