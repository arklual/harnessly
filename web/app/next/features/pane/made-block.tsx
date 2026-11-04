// «Сделано в этом чате» (.hx-made): above the tools, what the chat made, opened or changed: documents, boards and designs,
// newest first, one quiet line each. The first few show, the rest sit behind «ещё N» so the tools stay on the screen.
// The pictures the agent looked at are the Gallery's. Until the chat has said what there is nothing shows (no skeleton:
// it would push the tools down and then pull them back); with nothing made the block is not there at all, and it
// leaves with an exit when what it shows goes. It comes in as one piece, never row by row, growing from zero height
// so the sections below slide instead of jumping.
import { useEffect, useMemo, useState } from 'react';
import { t } from '@/next/i18n';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { Skeleton } from '@/next/ui/skeleton';

const EXIT_MS = 300;
/** How many show before «ещё»: the block and the first tools fit one screen of a usual pane. */
export const MADE_VISIBLE = 4;

export interface IMadeCard {
  id: string;
  /** The icon of its kind. */
  icon: string;
  title: string;
  /** Its name is still being read: a bar stands in for it. */
  pending: boolean;
  /** Where it lives, short: the host of a link, the kind of a design. */
  about: string;
  /** An epic's tasks: the card opens into them, one line each. */
  kids?: readonly IMadeKid[];
}

/** A task of an epic's card: the line it takes when the epic is opened. */
export interface IMadeKid {
  /** What a press on it names to the pane. */
  id: string;
  key: string;
  title: string;
  /** The tracker's state in words; empty when it does not say. */
  state: string;
  done: boolean;
}

export interface IMadeBlockProps {
  /** Newest first; null until the chat has said. */
  cards: readonly IMadeCard[] | null;
  onOpen: (card: IMadeCard) => void;
}

const Kids = ({ kids, onOpen }: { kids: readonly IMadeKid[]; onOpen: (id: string) => void }) => (
  <ul className="hx-made__kids">
    {kids.map((x) => (
      <li key={x.id}>
        <button
          type="button"
          className="hx-made__kid"
          data-done={x.done ? 'true' : undefined}
          onClick={() => onOpen(x.id)}
        >
          <Icon name={x.done ? 'circle-check' : 'circle'} size="sm" className="hx-made__dot" />
          <span className="hx-made__key">{x.key}</span>
          <span className="hx-made__kid-name">{x.title}</span>
          {x.state && <span className="hx-made__about">{x.state}</span>}
        </button>
      </li>
    ))}
  </ul>
);

/** A card; one with tasks opens into them (the grid animates its height, nothing is drawn only while it is open). */
const Card = ({ card, onOpen }: { card: IMadeCard; onOpen: IMadeBlockProps['onOpen'] }) => {
  const [open, setOpen] = useState(false);
  const kids = card.kids;
  return (
    <li className="hx-made__item" data-open={open ? 'true' : undefined}>
      <div className="hx-made__row">
        <button type="button" className="hx-made__card" title={card.title} onClick={() => onOpen(card)}>
          <span className="hx-tl__ico">
            <Icon name={card.icon} />
          </span>
          {card.pending ? (
            <Skeleton className="hx-made__name-sk" />
          ) : (
            <span className="hx-made__name">{card.title}</span>
          )}
          {card.about && <span className="hx-made__about">{card.about}</span>}
        </button>
        {kids && kids.length > 0 && (
          <button
            type="button"
            className="hx-made__toggle"
            aria-expanded={open}
            aria-label={card.title}
            onClick={() => setOpen((v) => !v)}
          >
            <Icon name="chevron-down" size="sm" className="hx-made__chev" />
          </button>
        )}
      </div>
      {kids && kids.length > 0 && (
        <div className="hx-made__fold" inert={!open}>
          <div className="hx-made__fold-in">
            <Kids kids={kids} onOpen={(id) => onOpen({ ...card, id })} />
          </div>
        </div>
      )}
    </li>
  );
};

const Content = ({ cards, onOpen }: { cards: readonly IMadeCard[]; onOpen: IMadeBlockProps['onOpen'] }) => {
  const [all, setAll] = useState(false);
  // One more than the limit is never hidden behind a button that would only reveal one line.
  const fold = cards.length > MADE_VISIBLE + 1;
  const shown = fold && !all ? cards.slice(0, MADE_VISIBLE) : cards;
  return (
    <ul className="hx-made__list">
      {shown.map((x) => (
        <Card key={x.id} card={x} onOpen={onOpen} />
      ))}
      {fold && (
        <li>
          <button
            type="button"
            className="hx-made__more"
            aria-expanded={all}
            onClick={() => setAll((v) => !v)}
          >
            {all ? t('next.made.less') : t('next.made.more', { n: cards.length - MADE_VISIBLE })}
            <Icon name="chevron-down" size="sm" className="hx-made__chev" />
          </button>
        </li>
      )}
    </ul>
  );
};

/** Mounted collapsed and opened two frames later, so the height has something to grow from. */
const Section = ({
  cards,
  state,
  onOpen,
}: {
  cards: readonly IMadeCard[];
  state: string;
  onOpen: IMadeBlockProps['onOpen'];
}) => {
  const [entered, setEntered] = useState(false);
  useEffect(() => {
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => setEntered(true));
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, []);
  return (
    <section className="hx-made" data-state={entered ? state : 'opening'} aria-label={t('next.made.title')}>
      <div className="hx-made__clip">
        <div className="hx-made__body">
          <h3 className="hx-made__ttl">{t('next.made.title')}</h3>
          <Content cards={cards} onOpen={onOpen} />
        </div>
      </div>
    </section>
  );
};

const MadeBlock = ({ cards, onOpen }: IMadeBlockProps) => {
  const has = cards !== null && cards.length > 0;
  const content = useMemo<readonly IMadeCard[] | null>(() => (has ? cards : null), [has, cards]);
  // What is shown stays while the block leaves.
  const { value, state } = usePresence(content, EXIT_MS);
  if (!value) return null;
  return <Section cards={value} state={state} onOpen={onOpen} />;
};

export default MadeBlock;
