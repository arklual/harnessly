// The library's kits: one folder «UI-киты», always the first of the library's folders, and inside it
// a card per theme of a kit (a kit of several brands shows each with its own cover), or per kit when it is one theme; a
// click opens the kit screen on that theme. The folder is the library's own: it is not moved, renamed or deleted, and
// it is there before the kits are read, so nothing on the home jumps when they come.
import type { IKitShelfCard } from '@/next/features/designly/kit/kit-api';
import Icon from '@/next/ui/icon';
import { plural, t } from '@/next/i18n';

export interface IKitShelf {
  /** null while the kits are read. */
  cards: readonly IKitShelfCard[] | null;
  /** The card's picture, '' when it has none. */
  cover: (card: IKitShelfCard) => string;
  onOpen: (card: IKitShelfCard) => void;
}

/** The folder's tile, first in the row of folders: no menu and no drop place, it is not the person's. */
export const KitsFolderTile = ({ onOpen }: { onOpen: () => void }) => (
  <div className="hx-dl-folder" data-pinned="true">
    <button type="button" className="hx-dl-folder__open" onClick={onOpen}>
      <Icon name="component" />
      <span className="hx-dl-folder__name">{t('next.kit.card.section')}</span>
    </button>
  </div>
);

/** How many cards the skeleton of the folder holds. */
const SKELETON_CARDS = 3;

const KitShelf = ({ cards, cover, onOpen }: IKitShelf) => {
  if (cards === null)
    return (
      <section className="hx-kit-shelf" aria-label={t('next.kit.card.section')} aria-busy="true">
        <ul className="hx-kit-shelf__list">
          {Array.from({ length: SKELETON_CARDS }, (_, i) => (
            <li key={i} className="hx-kit-shelf__ghost">
              <span className="hx-sk hx-kit-shelf__sk-shot" />
              <span className="hx-sk hx-kit-shelf__sk-title" />
            </li>
          ))}
        </ul>
      </section>
    );
  if (cards.length === 0) return <div className="hx-dl__empty">{t('next.kit.card.empty')}</div>;
  return (
    <section className="hx-kit-shelf" aria-label={t('next.kit.card.section')}>
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
