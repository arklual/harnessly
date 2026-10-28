// The state of leaving a comment, shared by the bar's «Комментировать» and the C key (the shell), the capture layer over
// the slide and the popover (./deck-comments.tsx): picking a place, and the comment being written there. Kept per deck,
// for the page only. The shell starts it with startDeckPick and reads useDeckPick for `data-pick` and the phone's
// `data-es`; Esc goes through escapeDeckComment, which closes what is open and says whether something was. A finger
// held on the board is the board's own gesture: the shell feeds its ring through feedDeckRing and opens the draft.
import { createKeyedStore, flush, useKey } from '@/next/core/store';

/** Where a comment will stand: percent of the slide, the block's label under the point, and where the popover opens. */
export interface IDeckDraft {
  x: number;
  y: number;
  label: string;
  /** The finger or click, in the window's pixels: the popover opens near it (a phone's sheet ignores it). */
  at: { left: number; top: number };
}

export interface IDeckPickState {
  /** The bar «Нажмите на место на слайде» is up and the next click picks a place. */
  picking: boolean;
  draft: IDeckDraft | null;
}

const IDLE: IDeckPickState = { picking: false, draft: null };

const store = createKeyedStore<string, IDeckPickState>();

const put = (artifact: string, change: (have: IDeckPickState) => IDeckPickState) => {
  store.patch(artifact, (have) => {
    const next = change(have ?? IDLE);
    return next.picking || next.draft ? next : undefined;
  });
  flush();
};

export const useDeckPick = (artifact: string | null): IDeckPickState => useKey(store, artifact) ?? IDLE;

export const deckPickOf = (artifact: string): IDeckPickState => store.get(artifact) ?? IDLE;

/** «Комментировать» / C: the next click on the slide picks a place. A comment being written is dropped. */
export const startDeckPick = (artifact: string): void =>
  put(artifact, () => ({ picking: true, draft: null }));

export const stopDeckPick = (artifact: string): void =>
  put(artifact, (have) => ({ ...have, picking: false }));

/** A place was picked (a click while picking, or a held finger): the popover opens there. */
export const openDeckDraft = (artifact: string, draft: IDeckDraft): void =>
  put(artifact, () => ({ picking: false, draft }));

export const closeDeckDraft = (artifact: string): void => put(artifact, (have) => ({ ...have, draft: null }));

/** Esc: picking or the popover closes (the popover first). True when something did, so the key is taken. */
export const escapeDeckComment = (artifact: string): boolean => {
  const have = deckPickOf(artifact);
  if (have.draft) closeDeckDraft(artifact);
  else if (have.picking) stopDeckPick(artifact);
  else return false;
  return true;
};

// ---- the finger held on the board

/** Where the board rings a held finger (percent of the slide); null when the finger moved or lifted. */
export interface IDeckRing {
  px: number;
  py: number;
}

type TRingListener = (at: IDeckRing | null) => void;
const ringListeners = new Map<string, Set<TRingListener>>();

/**
 * The shell hands the board's long press here (`tk:deck-press`: the ring at 250 ms, or its cancel); the pick at 730 ms
 * comes as `tk:picked` with its point and opens the draft. The capture layer draws the ring.
 */
export const feedDeckRing = (artifact: string, at: IDeckRing | null): void => {
  ringListeners.get(artifact)?.forEach((fn) => fn(at));
};

export const onDeckRing = (artifact: string, fn: TRingListener): (() => void) => {
  let set = ringListeners.get(artifact);
  if (!set) ringListeners.set(artifact, (set = new Set()));
  set.add(fn);
  return () => {
    set.delete(fn);
    if (set.size === 0) ringListeners.delete(artifact);
  };
};
