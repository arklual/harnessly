// The library's row of kits: every installed kit as a card with its picture; a click opens the kit screen.
import type { IKitCard } from '@/next/features/designly/kit/kit-api';
import { plural, t } from '@/next/i18n';

export interface IKitShelf {
  cards: readonly IKitCard[];
  /** The card's picture, '' when the kit has none. */
  cover: (card: IKitCard) => string;
  onOpen: (id: string) => void;
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
            <li key={card.id}>
              <button
                type="button"
                className="hx-kit-shelf__card"
                aria-label={`${t('next.kit.card.open')}: ${card.title}`}
                onClick={() => onOpen(card.id)}
              >
                <span className="hx-kit-shelf__shot">{src && <img src={src} alt="" loading="lazy" />}</span>
                <span className="hx-kit-shelf__title">{card.title}</span>
                <span className="hx-kit-shelf__meta">
                  {card.layouts > 0
                    ? plural(card.layouts, 'next.kit.panel.layouts', { n: card.layouts })
                    : `v${card.version}`}
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
