// The drops in the chat's right column (stores/smart-drops.ts): what waits and was put away from the smart notice, one
// small black line each, as a live activity on a phone: the kind's icon (a code's ring runs down), what it is and a few
// words. Pressed, the line flows back into the smart notice, open; its chevron opens it in place to read what is asked
// without leaving the chat. They stand one under another; a new one comes in out of a dot, an answered one goes.
import { useRef, useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresentFlag, PRESENCE } from '@/next/ui/presence';
import type { IDrop } from '@/next/stores/smart-drops';
import { snap } from './fly';
import { Chip } from './smart-notice';

const OPEN_MS = 420;

/** A drop pressed: its key, where it stands and a copy of what it shows (for the flight back into the notice). */
type TBack = (key: string, from: DOMRect | null, view: HTMLElement | null) => void;

const Drop = ({ drop, now, onBack }: { drop: IDrop; now: number; onBack: TBack }) => {
  const [open, setOpen] = useState(false);
  const more = usePresentFlag(open, OPEN_MS);
  const el = useRef<HTMLLIElement | null>(null);
  const { item, what } = drop;
  const back = () => onBack(item.key, el.current?.getBoundingClientRect() ?? null, snap(el.current));
  return (
    <li ref={el} className="hx-sn-drop" data-drop-key={item.key} data-open={open || undefined}>
      <div className="hx-sn-drop__line">
        <button type="button" className="hx-sn-drop__main" onClick={back}>
          <Chip item={item} now={now} sent={false} />
          <span className="hx-sn-drop__lines">
            <b className="hx-sn-drop__title">{item.title}</b>
            <span className="hx-sn-drop__text">{item.text}</span>
          </span>
        </button>
        {what !== null && (
          <button
            type="button"
            className="hx-sn-drop__more"
            aria-expanded={open}
            aria-label={t(open ? 'next.smart.drops.less' : 'next.smart.drops.more')}
            onClick={() => setOpen((was) => !was)}
          >
            <Icon name="chevron-down" size="sm" />
          </button>
        )}
      </div>
      <div
        className="hx-sn-drop__fold"
        data-state={more.mounted && more.state === PRESENCE.closing ? 'closing' : undefined}
      >
        <div className="hx-sn-drop__inner">
          {more.mounted && what !== null && (
            <>
              <p className="hx-sn-drop__what">{what}</p>
              <button type="button" className="hx-sn-drop__answer" onClick={back}>
                {t('next.smart.answer')}
              </button>
            </>
          )}
        </div>
      </div>
    </li>
  );
};

const Drops = ({ drops, now, onBack }: { drops: readonly IDrop[]; now: number; onBack: TBack }) => {
  if (drops.length === 0) return null;
  return (
    <section className="hx-sn-drops" aria-label={t('next.smart.drops.title')}>
      <ul className="hx-sn-drops__list">
        {drops.map((drop) => (
          <Drop key={drop.item.key} drop={drop} now={now} onBack={onBack} />
        ))}
      </ul>
    </section>
  );
};

export default Drops;
