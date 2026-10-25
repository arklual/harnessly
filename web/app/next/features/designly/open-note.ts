// A sent note's chip opens the page it was made on: the chip is in the feed or the queue, far from the right pane that
// shows designs, so it asks here. The pane hears and puts the design in its Designly tab; the design's viewer, once it
// is there (now or after it mounts), takes the board it was asked for and shows that page.
export interface INotePlace {
  design: string;
  board: string;
}

type TListener = (place: INotePlace) => void;

const listeners = new Set<TListener>();
/** The board each design was last asked to show, until its viewer takes it. */
const asked = new Map<string, string>();

export const openNotePage = (place: INotePlace): void => {
  asked.set(place.design, place.board);
  listeners.forEach((listen) => listen(place));
};

/** The pane and the viewer listen while they are mounted; the returned function stops it. */
export const onOpenNotePage = (listen: TListener): (() => void) => {
  listeners.add(listen);
  return () => listeners.delete(listen);
};

/** The board the design was asked to show, once: null when nothing waits. */
export const takeNoteBoard = (design: string): string | null => {
  const board = asked.get(design) ?? null;
  asked.delete(design);
  return board;
};
