// What is left of a subscription in one line, for a list (the Settings' accounts and providers): the windows of the
// composer's chip, «5ч 94% · сброс 2 ч», or the balance of an account paid by the token. Nothing is drawn for an
// account the machine has no limits of: unknown is not zero.
import { useEffect, useState } from 'react';
import {
  leftOf,
  levelOf,
  money,
  spanShort,
  windowShort,
  windowsOf,
  type ProviderLimits,
} from '@/next/features/limits/model';
import { t } from '@/next/i18n';
import '@/next/styles/limits.css';

/** The spans are told to the minute at most. */
const TICK_MS = 60_000;

interface ILimitsLineProps {
  limits: ProviderLimits | null;
}

const LimitsLine = ({ limits }: ILimitsLineProps) => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), TICK_MS);
    return () => window.clearInterval(timer);
  }, []);

  const windows = windowsOf(limits, now / 1000);
  if (windows.length === 0 && !limits?.balance) return null;
  return (
    <span className="hx-lim-line">
      {windows.map((w) => {
        const left = leftOf(w.window);
        return (
          <span key={w.kind} className="hx-lim__win" data-level={levelOf(left)}>
            <small>{windowShort(w.kind)}</small>
            {left}%
            <small>{t('next.limits.resetShort', { span: spanShort(w.window.resetsAt - now / 1000) })}</small>
          </span>
        );
      })}
      {windows.length === 0 && limits?.balance && (
        <span className="hx-lim__win">
          {money(limits.balance)}
          <small>{t('next.limits.balanceShort')}</small>
        </span>
      )}
    </span>
  );
};

export default LimitsLine;
