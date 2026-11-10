// The move of a machine's data into the hub under its row in «Машины»: «переносим данные с машины X: 412 из
// 420» while it goes; when it stopped, the objects with their codes and two buttons: «Повторить» and «Продолжить с
// агентом» (a chat on that machine with the brief, docs/migrations/<id>.md).
import { SetButton } from '@/next/features/settings/rows';
import { t } from '@/next/i18n';
import type { IMoveFailure } from '@/next/features/settings/machine-move';

export interface IMoveCard {
  /** «Переносим данные с машины X: 412 из 420», or that it stopped. */
  line: string;
  stopped: boolean;
  /** «MIG-READ ×6, MIG-HASH ×2». */
  codes: string;
  failures: readonly IMoveFailure[];
  busy: boolean;
  onRetry: () => void;
  onAgent: () => void;
}

const SHOWN = 3;

const MoveCard = ({ card }: { card: IMoveCard }) => (
  <div className="hx-mmove" data-stopped={card.stopped}>
    <div className="hx-mmove__line">{card.line}</div>
    {card.stopped && (
      <>
        {card.codes && <div className="hx-mmove__codes">{card.codes}</div>}
        <ul className="hx-mmove__list">
          {card.failures.slice(0, SHOWN).map((f) => (
            <li key={`${f.kind}/${f.object}`}>
              <span className="hx-mmove__obj">
                {f.kind}/{f.object}
              </span>{' '}
              {f.code}: {f.error}
            </li>
          ))}
          {card.failures.length > SHOWN && (
            <li>{t('next.move.more', { n: card.failures.length - SHOWN })}</li>
          )}
        </ul>
        <div className="hx-mmove__acts">
          <SetButton tone="primary" busy={card.busy} onClick={card.onRetry}>
            {t('next.move.retry')}
          </SetButton>
          <SetButton tone="ghost" onClick={card.onAgent}>
            {t('next.move.agent')}
          </SetButton>
        </div>
      </>
    )}
  </div>
);

export default MoveCard;
