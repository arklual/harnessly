/** Asked of every timeline when a jump target is not on screen: one that folds it opens it and names what to land on. */
export const REVEAL_ENTRY_EVENT = 'hl:reveal-entry';

export interface IRevealEntryRequest {
  id: string;
  /** Set by the timeline that holds the entry: the rendered row to land on (a call lands on its tool run). */
  target: string | null;
}

const findEntry = (id: string) =>
  document.querySelector<HTMLElement>(`.tw-pane:not(.tw-pane-off) [data-entry-id="${CSS.escape(id)}"]`);

/** A revealed row renders on React's schedule; give up on it after about half a second of frames. */
const REVEAL_FRAMES = 30;

const landWhenRendered = (target: string, frames: number) => {
  const el = findEntry(target);
  if (el) land(el);
  else if (frames > 0) requestAnimationFrame(() => landWhenRendered(target, frames - 1));
};

const land = (el: HTMLElement) => {
  el.scrollIntoView({ behavior: 'smooth', block: 'center' });
  el.classList.remove('tw-flash');
  void el.offsetWidth;
  el.classList.add('tw-flash');
};

/**
 * Scrolls to and flashes one rendered timeline entry, wherever ⌘J and search-result jumps both land on it. An entry
 * folded inside an agent turn is revealed first and landed on once the timeline has rendered it.
 */
export const jumpToEntry = (id: string) => {
  const el = findEntry(id);
  if (el) {
    land(el);
    return true;
  }
  const request: IRevealEntryRequest = { id, target: null };
  window.dispatchEvent(new CustomEvent<IRevealEntryRequest>(REVEAL_ENTRY_EVENT, { detail: request }));
  const { target } = request;
  if (!target) return false;
  requestAnimationFrame(() => landWhenRendered(target, REVEAL_FRAMES));
  return true;
};
