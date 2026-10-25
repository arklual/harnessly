// Where a conversation came from, as a quiet line of the feed like a change of model: «Ответвление от «Контракт v2»»
// after the turn it branched at, or «⇄ Передача контекста · Opus 5.5 → GPT-5.5» above a handoff. The title opens the
// chat it came from while that is on this machine.
import { useContext } from 'react';
import { ChatLinks } from '@/next/features/chat/handoff';
import type { OriginItem } from '@/next/features/chat/types';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/handoff.css';

/** «Opus 5.5 → GPT-5.5», or the one model that is known; null when neither is. */
const modelsOf = (item: OriginItem): string | null => {
  if (item.from && item.to) return t('next.modelSwitch.fromTo', { from: item.from, to: item.to });
  return item.to ?? item.from;
};

const OriginRow = ({ item }: { item: OriginItem }) => {
  const links = useContext(ChatLinks);
  const title = item.title || t('next.shellData.untitled');
  const canOpen = links?.has(item.fromId) ?? false;
  const source = canOpen ? (
    <button type="button" className="hx-origin__link" onClick={() => links?.open(item.fromId)}>
      «{title}»
    </button>
  ) : (
    <span className="hx-origin__title">«{title}»</span>
  );
  const models = modelsOf(item);
  if (item.fork)
    return (
      <div className="hx-origin" data-kind="fork" role="note">
        <Icon name="git-fork" size="sm" className="hx-origin__ic" />
        <span className="hx-origin__text">
          {t('next.handoff.origin.fork')} {source}
        </span>
      </div>
    );
  return (
    <div className="hx-origin" data-kind="handoff" role="note">
      <Icon name="arrow-left-right" size="sm" className="hx-origin__ic" />
      <span className="hx-origin__text">
        <span className="hx-origin__label">{t('next.handoff.origin.handoff')}</span>
        {models && <span className="hx-origin__models">{models}</span>}
        <span className="hx-origin__from">
          {t('next.handoff.origin.from')} {source}
        </span>
      </span>
    </div>
  );
};

export default OriginRow;
