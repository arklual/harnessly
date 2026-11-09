// The library's row of kits: a card per theme of a kit (a kit of several brands shows each with its own
// cover), or per kit when it is one theme; a click opens the kit screen on that theme.
import type { IKitShelfCard } from '@/next/features/designly/kit/kit-api';
import { plural, t } from '@/next/i18n';

export interface IKitShelf {
  cards: readonly IKitShelfCard[];
  /** The card's picture, '' when it has none. */
  cover: (card: IKitShelfCard) => string;
  onOpen: (card: IKitShelfCard) => void;
}

const KitShelf = ({ cards, cover, onOpen }: IKitShelf) => {
  if (cards.length === 0) return null;
  return (
    <section className="hx-kit-shelf" aria-label={t('next.kit.card.section')}>
      <h3 className="hx-kit-shelf__h">{t('next.kit.card.section')}</h3>
      <ul className="hx-kit-shelf__list">
        {cards.map((card) => {
          const src = cover(card);
          return (
            <li key={card.key}>
              <button
                type="button"
                className="hx-kit-shelf__card"
                aria-label={`${t('next.kit.card.open')}: ${card.kitTitle ? `${card.kitTitle} · ` : ''}${card.title}`}
                onClick={() => onOpen(card)}
              >
                <span className="hx-kit-shelf__shot">{src && <img src={src} alt="" loading="lazy" />}</span>
                <span className="hx-kit-shelf__title">{card.title}</span>
                <span className="hx-kit-shelf__meta">
                  {card.kitTitle && <span>{card.kitTitle}</span>}
                  {card.layouts > 0 && (
                    <span>{plural(card.layouts, 'next.kit.card.templates', { n: card.layouts })}</span>
                  )}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </section>
  );
};

export default KitShelf;
