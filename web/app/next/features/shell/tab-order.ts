// Where a dragged tab goes. Pure: the pointer and the DOM are use-drag's, the order is the tabs store's (moveTab).
import { EDGE, type IDragSource, type IDropTarget, type TEdge } from '@/next/ui/use-drag';

/** The kind of what is carried and of where it goes: the strip's own. */
export const TAB = 'tab';
export const TAB_KINDS: readonly (typeof TAB)[] = [TAB];

/** The words of a drag: the tab `id` goes to `index` of the order it is in once it is out of its old place. */
export interface ITabMove {
  id: string;
  index: number;
}

/**
 * What dropping `source` on the `edge` of the tab `target` asks for; null when it would stay where it is (a tab dropped
 * on itself or on the edge that touches it), so such a place is not shown as one.
 */
export const planTabMove = (
  ids: readonly string[],
  source: IDragSource<typeof TAB>,
  target: IDropTarget<typeof TAB>,
): ITabMove | null => {
  const from = ids.indexOf(source.id);
  const over = target.id === undefined ? -1 : ids.indexOf(target.id);
  if (from < 0 || over < 0) return null;
  const edge: TEdge = target.edge ?? EDGE.before;
  const slot = over + (edge === EDGE.after ? 1 : 0);
  const index = from < slot ? slot - 1 : slot;
  return index === from ? null : { id: source.id, index };
};
