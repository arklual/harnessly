// What did not load, said the one way everywhere: why, and a «Повторить» that asks for exactly this again.
// It stands where the data would have been; the data already on screen is never replaced by it (the caller shows it
// only where there is nothing to keep). The retry is pessimistic: pressed, it turns busy until the ask ends.
import { useState } from 'react';
import { t } from '@/next/i18n';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';

export const LOAD_FAILED_SIZE = { page: 'page', pane: 'pane', inline: 'inline' } as const;
export type TLoadFailedSize = (typeof LOAD_FAILED_SIZE)[keyof typeof LOAD_FAILED_SIZE];

export interface ILoadFailedProps {
  /** What did not load («Чаты не загрузились»). */
  title?: string;
  /** Why, in words (the page outcome's text); null says nothing more than the title. */
  reason?: string | null;
  /** Asks again. A promise keeps the button busy until it settles. */
  onRetry: () => void | Promise<unknown>;
  /** page: a whole screen; pane: a tool of the right pane; inline: a line inside a list or a card. */
  size?: TLoadFailedSize;
  className?: string;
}

const LoadFailed = ({
  title = t('next.load.failed'),
  reason = null,
  onRetry,
  size = LOAD_FAILED_SIZE.pane,
  className,
}: ILoadFailedProps) => {
  const [busy, setBusy] = useState(false);
  const press = () => {
    if (busy) return;
    const asked = onRetry();
    if (!asked) return;
    setBusy(true);
    void Promise.resolve(asked).finally(() => setBusy(false));
  };
  return (
    <div className={className ? `hx-lf ${className}` : 'hx-lf'} data-size={size} role="alert">
      <span className="hx-lf__icon">
        <Icon name="circle-alert" size={size === LOAD_FAILED_SIZE.inline ? 'sm' : 'md'} />
      </span>
      <span className="hx-lf__text">
        <span className="hx-lf__title">{title}</span>
        {reason && <span className="hx-lf__reason">{reason}</span>}
      </span>
      <Button size="sm" icon="rotate-ccw" busy={busy} onClick={press}>
        {t('next.load.retry')}
      </Button>
    </div>
  );
};

export default LoadFailed;
