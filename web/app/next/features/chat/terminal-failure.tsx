// A terminal provider failure at the end of a chat. It never starts work by itself: the person retries the current
// model or opens the existing manual handoff picker.
import { useState } from 'react';
import type { FeedItem } from '@/next/features/chat/types';
import type { TerminalFailure } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import usePending, { isBusy } from '@/next/ui/use-pending';
import '@/next/styles/limit.css';

interface ITerminalFailureCardProps {
  failure: TerminalFailure | null;
  onChooseTarget: (() => void) | null;
  onRetry: () => Promise<unknown>;
}

/** What «Попробовать снова» sends again: the stopped turn's own message, unless messages wait in the queue — resuming
 *  the queue sends those. */
export const retryText = (items: readonly FeedItem[] | null, queued: number): string | null => {
  if (queued > 0) return null;
  for (let i = (items?.length ?? 0) - 1; i >= 0; i -= 1) {
    const item = items?.[i];
    if (item?.kind === 'turn') return item.prompt?.text || null;
  }
  return null;
};

const TerminalFailureCard = ({ failure, onChooseTarget, onRetry }: ITerminalFailureCardProps) => {
  const action = usePending();
  const [open, setOpen] = useState(false);
  if (!failure || !failure.terminal || !failure.needsTarget) return null;
  const busy = isBusy(action.phase);
  return (
    <div className="hx-terminalfailure" role="alert">
      <Icon name="circle-alert" size="sm" className="hx-terminalfailure__ic" />
      <span className="hx-terminalfailure__body">
        <strong className="hx-terminalfailure__title">{t('next.terminalFailure.quota.title')}</strong>
        <span className="hx-terminalfailure__text">{t('next.terminalFailure.quota.lead')}</span>
        {failure.detail && (
          <button
            type="button"
            className="hx-terminalfailure__detail"
            data-open={open}
            aria-expanded={open}
            title={open ? undefined : failure.detail}
            onClick={() => setOpen((was) => !was)}
          >
            {failure.detail}
          </button>
        )}
        <span className="hx-terminalfailure__actions">
          <Button size="sm" busy={busy} disabled={busy} onClick={() => action.run('retry', onRetry)}>
            {t('next.terminalFailure.retry')}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={busy || onChooseTarget === null}
            onClick={onChooseTarget ?? undefined}
          >
            {t('next.terminalFailure.choose')}
          </Button>
        </span>
      </span>
    </div>
  );
};

export default TerminalFailureCard;
