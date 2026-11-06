// The head row of what a pane tab shows under its tab: one row, where it is on the left (the crumbs, the
// folders shrink first), what is done with it on the right. A view inside puts its own controls into the same row
// through `HeadTools` (the switch of the view of a text file, the save state of a Word file), so a tab never stacks a
// row of its own for one or two buttons.
import { createContext, Fragment, use, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';

/** The place in the head row for the controls of the view under it; null until the row is on screen. */
const HeadSlot = createContext<HTMLElement | null | undefined>(undefined);

export interface ICrumb {
  name: string;
  path: string;
}

/** Which step a crumb is, for which gives way first on a narrow row: the folders in the middle, then the first and
 *  the parent, the last (the thing itself) only when nothing else is left. */
const stepOf = (i: number, n: number) => {
  if (i === n - 1) return 'last';
  if (i === n - 2) return 'parent';
  return i === 0 ? 'first' : 'middle';
};

/** The way to where the shown thing lies: each folder leads to itself, the last step is the thing itself. */
export const Crumbs = ({ items, onPick }: { items: readonly ICrumb[]; onPick: (path: string) => void }) => (
  <div className="hx-disk__crumbs">
    {items.map((c, i) =>
      i === items.length - 1 ? (
        <span key={c.path} className="hx-disk__crumb" data-step="last" aria-current="true" title={c.name}>
          {c.name}
        </span>
      ) : (
        <Fragment key={c.path}>
          <button
            type="button"
            className="hx-disk__crumb"
            data-step={stepOf(i, items.length)}
            title={c.name}
            onClick={() => onPick(c.path)}
          >
            {c.name}
          </button>
          <Icon name="chevron-right" size="sm" />
        </Fragment>
      ),
    )}
  </div>
);

export interface IPaneHeadProps {
  /** Where it is: the crumbs. */
  lead: ReactNode;
  /** What is done with it, after what the view under it puts into the row. */
  tools?: ReactNode;
  /** What the head row and the view under it are: the view finds the row through it. */
  children?: ReactNode;
}

/** The head row and, under it, the view whose controls go into the row. */
const PaneHead = ({ lead, tools, children }: IPaneHeadProps) => {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  return (
    <HeadSlot value={slot}>
      <div className="hx-phead">
        <div className="hx-phead__lead">{lead}</div>
        <div className="hx-phead__tools">
          <span className="hx-phead__slot" ref={setSlot} />
          {tools}
        </div>
      </div>
      {children}
    </HeadSlot>
  );
};

/** The view's own controls, in the head row over it; a view shown without one keeps them in a bar of its own
 *  (`bar` is its class). */
export const HeadTools = ({ bar, children }: { bar: string; children: ReactNode }) => {
  const slot = use(HeadSlot);
  if (slot === undefined) return <div className={bar}>{children}</div>;
  return slot && createPortal(children, slot);
};

export default PaneHead;
