// «2 уведомления ⌄»: the column's notices (the folder's offer, «Ждут ответа») stand under the island, and with many
// widgets they scroll out of sight. While they are out, this small pill stays at the top of the column, says how many
// there are and takes the column back to them by a press. It draws nothing while the notices are in view.
import { useEffect, useState } from 'react';
import Icon from '@/next/ui/icon';
import { plural } from '@/next/i18n';

export interface INotesPillProps {
  /** The scrolling column and the block of the notices in it. */
  col: HTMLElement | null;
  notes: HTMLElement | null;
  count: number;
}

type TAway = 'above' | 'below' | null;

export const useNotesAway = (col: HTMLElement | null, notes: HTMLElement | null): TAway => {
  const [away, setAway] = useState<TAway>(null);
  useEffect(() => {
    if (!col || !notes || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver(
      ([entry]) => {
        if (!entry || entry.isIntersecting) return setAway(null);
        const box = entry.rootBounds;
        setAway(box && entry.boundingClientRect.top < box.top ? 'above' : 'below');
      },
      { root: col, threshold: 0.01 },
    );
    io.observe(notes);
    return () => {
      io.disconnect();
      setAway(null);
    };
  }, [col, notes]);
  return away;
};

const NotesPill = ({ col, notes, count }: INotesPillProps) => {
  const away = useNotesAway(col, notes);
  return (
    <button
      type="button"
      className="hx-notes-pill"
      data-shown={away !== null || undefined}
      tabIndex={away === null ? -1 : 0}
      aria-hidden={away === null}
      onClick={() =>
        notes?.scrollIntoView({
          block: 'start',
          behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth',
        })
      }
    >
      <Icon name="bell" size="sm" />
      {plural(count, 'next.notes.count')}
      <Icon name={away === 'below' ? 'chevron-down' : 'chevron-up'} size="sm" />
    </button>
  );
};

export default NotesPill;
