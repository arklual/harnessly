// Pulling a script by its dots to change the order: the row follows the pointer with a lift shadow, the neighbours slide
// aside, and after the hub has kept the new order the row settles into its place. Escape and a cancelled pointer put it back.
// The motion is written on the elements while the pointer is down (it changes at every move; React does not own it).
import type { KeyboardEvent as ReactKeyboardEvent, PointerEvent as ReactPointerEvent } from 'react';

const EASE = 'cubic-bezier(.2,.8,.2,1)';
const LIFT = 'scale(1.015)';

const afterPaint = (): Promise<void> =>
  new Promise((resolve) => {
    requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
  });

/** Moves `id` to index `to`; the others keep their order. */
export const moved = (ids: readonly string[], id: string, to: number): string[] => {
  const rest = ids.filter((x) => x !== id);
  rest.splice(to, 0, id);
  return rest;
};

interface DragOptions {
  /** The group's rows, top to bottom: the elements and their entry ids. */
  ids: readonly string[];
  /** Asks the hub for the new order; true: it was kept and the screen shows it now. */
  commit: (next: string[]) => Promise<boolean>;
  group: string;
}

const rowsOf = (body: Element, group: string): HTMLElement[] => [
  ...body.querySelectorAll<HTMLElement>(`.ev-row[data-dg="${group}"]`),
];

// The rows are drawn loose first and then group by group, while the scripts run in the order of o.ids: a row is found
// by its entry, never by its place on the screen.
const rowOf = (rows: HTMLElement[], id: string | undefined) => rows.find((r) => r.dataset.entry === id);

/**
 * The scripts of a layer in the order they run: by saved position, then name, as the hub sorts them. Reordering starts
 * from here, so every move goes on from the last one (GitHub issue 40). The hub takes the order of all the layer's
 * scripts, switched off ones too.
 */
export const scriptOrder = (
  items: { entryId: string; kind: string; name: string; position: number }[],
): string[] =>
  items
    .filter((r) => r.kind === 'script')
    .sort(
      (x, y) =>
        x.position - y.position ||
        (x.name < y.name ? -1 : x.name > y.name ? 1 : 0) ||
        (x.entryId < y.entryId ? -1 : x.entryId > y.entryId ? 1 : 0),
    )
    .map((r) => r.entryId);

export interface DragLock {
  busy: boolean;
}

export const startDrag = (ev: ReactPointerEvent<HTMLElement>, id: string, o: DragOptions, lock: DragLock) => {
  if (ev.button !== 0 || lock.busy) return;
  ev.preventDefault();
  ev.stopPropagation();
  const row = ev.currentTarget.closest<HTMLElement>('.ev-row');
  const body = row?.closest('.ev-layer__body');
  if (!row || !body) return;
  const rows = rowsOf(body, o.group);
  const from = rows.indexOf(row);
  if (from === -1 || rows.length < 2) return;
  const rects = rows.map((r) => r.getBoundingClientRect());
  const first = rects[0];
  const last = rects[rows.length - 1];
  const me = rects[from];
  if (!first || !last || !me) return;
  const h = me.height;
  const min = first.top - me.top - 6;
  const max = last.bottom - me.bottom + 6;
  const y0 = ev.clientY;
  let dy = 0;
  let to = from;
  lock.busy = true;
  document.documentElement.setAttribute('data-ev-drag', '');
  row.setAttribute('data-lift', '');
  row.style.transition = 'box-shadow 180ms ease, border-radius 180ms ease, background 180ms ease';
  row.style.transform = LIFT;
  for (const r of rows) if (r !== row) r.style.transition = `transform 220ms ${EASE}`;

  const move = (e: PointerEvent) => {
    dy = Math.max(min, Math.min(max, e.clientY - y0));
    row.style.transform = `translateY(${dy}px) ${LIFT}`;
    const mid = me.top + dy + h / 2;
    to = from;
    for (let i = from + 1; i < rows.length; i++) {
      const r = rects[i];
      if (r && mid > r.top + r.height / 2) to = i;
    }
    if (to === from)
      for (let i = from - 1; i >= 0; i--) {
        const r = rects[i];
        if (r && mid < r.top + r.height / 2) to = i;
      }
    rows.forEach((r, i) => {
      if (i === from) return;
      let sh = 0;
      if (from < to && i > from && i <= to) sh = -h;
      if (to < from && i >= to && i < from) sh = h;
      r.style.transform = sh ? `translateY(${sh}px)` : '';
    });
  };

  const clean = (list: HTMLElement[]) =>
    window.setTimeout(() => {
      for (const r of list) r.style.transition = '';
    }, 320);

  const putBack = () => {
    row.style.transition = `transform 240ms ${EASE}, box-shadow 240ms ease, border-radius 240ms ease`;
    for (const r of rows) r.style.transform = '';
    row.removeAttribute('data-lift');
    clean(rows);
  };

  const stop = async (cancel: boolean) => {
    window.removeEventListener('pointermove', move);
    window.removeEventListener('pointerup', onUp);
    window.removeEventListener('pointercancel', onCancel);
    window.removeEventListener('keydown', onKey, true);
    document.documentElement.removeAttribute('data-ev-drag');
    if (cancel || to === from) {
      lock.busy = false;
      putBack();
      return;
    }
    // Where the row is now and where its new place is: the difference is played after the order has changed.
    const target = rects[to];
    if (!target) {
      lock.busy = false;
      putBack();
      return;
    }
    const slot = to > from ? target.bottom - h : target.top;
    const off = me.top + dy - slot;
    const at = o.ids.indexOf(rows[to]?.dataset.entry ?? '');
    if (at === -1) {
      lock.busy = false;
      putBack();
      return;
    }
    const ok = await o.commit(moved(o.ids, id, at));
    if (!ok) {
      lock.busy = false;
      putBack();
      return;
    }
    await afterPaint();
    const now = rowsOf(body, o.group);
    for (const r of now) {
      r.style.transition = 'none';
      r.style.transform = '';
      r.removeAttribute('data-lift');
    }
    const t = rowOf(now, id);
    lock.busy = false;
    if (!t) return;
    t.setAttribute('data-lift', '');
    t.style.transform = `translateY(${off}px) ${LIFT}`;
    t.getBoundingClientRect();
    requestAnimationFrame(() => {
      t.style.transition = `transform 260ms ${EASE}, box-shadow 260ms ease, border-radius 260ms ease, background 260ms ease`;
      t.style.transform = '';
      t.removeAttribute('data-lift');
    });
    clean(now);
  };
  const onUp = () => void stop(false);
  const onCancel = () => void stop(true);
  const onKey = (e: KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.preventDefault();
      e.stopPropagation();
      void stop(true);
    }
  };
  window.addEventListener('pointermove', move);
  window.addEventListener('pointerup', onUp);
  window.addEventListener('pointercancel', onCancel);
  window.addEventListener('keydown', onKey, true);
};

/** The arrow keys on a grip: one place up or down; the grip keeps the focus. */
export const keyMove = async (
  ev: ReactKeyboardEvent<HTMLElement>,
  id: string,
  o: DragOptions,
  lock: DragLock,
) => {
  if (ev.key !== 'ArrowUp' && ev.key !== 'ArrowDown') return;
  ev.preventDefault();
  ev.stopPropagation();
  if (lock.busy) return;
  const i = o.ids.indexOf(id);
  const j = ev.key === 'ArrowUp' ? i - 1 : i + 1;
  if (i === -1 || j < 0 || j >= o.ids.length) return;
  const body = ev.currentTarget.closest('.ev-layer__body');
  lock.busy = true;
  const ok = await o.commit(moved(o.ids, id, j));
  lock.busy = false;
  if (!ok || !body) return;
  await afterPaint();
  rowOf(rowsOf(body, o.group), id)?.querySelector<HTMLElement>('.ev-grip')?.focus();
};
