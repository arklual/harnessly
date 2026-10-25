// «Нет связи» as the design draws it (.hx-net): a pill at the top of the window, over everything and in the way of
// nothing. The conversation, the draft and the queue stay where they are; the pill says what happens — no connection
// and a try again in N s with «Сейчас», then «Подключаемся», then «Снова на связи», which leaves by itself.
import { useEffect, useMemo, useState } from 'react';
import { usePillWidth } from '@/next/ui/use-pill-width';
import { createPortal } from 'react-dom';
import { secondsToRetry } from '@/next/stores/connection';
import type { Connection } from '@/next/protocol/v2';
import Icon, { type TIconName } from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import usePending, { isBusy } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import '@/next/styles/connection.css';

const EXIT_MS = 260;
/** How long «Снова на связи» stays before it leaves. */
const BACK_MS = 1600;

const NET = { lost: 'lost', retry: 'retry', back: 'back' } as const;
type TNet = (typeof NET)[keyof typeof NET];

const ICON: Record<TNet, TIconName> = { lost: 'wifi-off', retry: 'loader-circle', back: 'check' };

const WHY = {
  network: 'next.connection.network',
  'machine-unreachable': 'next.connection.machineAway',
  unauthorized: 'next.connection.unauthorized',
  proto: 'next.connection.proto',
} as const;

interface IPill {
  net: TNet;
  text: string;
  sub: string | null;
  canRetry: boolean;
}

/** Whole seconds tick while there is a countdown to show. */
const useSecond = (on: boolean): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [on]);
  return now;
};

interface IBannerProps {
  connection: Connection | undefined;
  machineName: string;
  /** Tries now; resolves once the attempt is made. */
  onRetryNow: () => void;
}

const ConnectionBanner = ({ connection, machineName, onRetryNow }: IBannerProps) => {
  const container = usePortalContainer();
  const retry = usePending<'retry'>();
  const trouble = connection && (connection.k === 'stale' || connection.k === 'offline') ? connection : null;
  // Once the connection was lost, the way back is shown too: «Подключаемся», then «Снова на связи» for a moment.
  const [lost, setLost] = useState(false);
  const [back, setBack] = useState(false);
  if (trouble && !lost) setLost(true);
  if (trouble && back) setBack(false);
  if (!trouble && lost && connection?.k === 'live') {
    setLost(false);
    setBack(true);
  }
  useEffect(() => {
    if (!back) return;
    const timer = setTimeout(() => setBack(false), BACK_MS);
    return () => clearTimeout(timer);
  }, [back]);

  const now = useSecond(trouble?.k === 'offline');
  const seconds = trouble ? secondsToRetry(trouble, now) : null;

  let net: TNet | null = null;
  if (trouble) net = NET.lost;
  else if (lost) net = NET.retry;
  else if (back) net = NET.back;

  let text = '';
  if (trouble?.k === 'stale') text = t('next.connection.stale', { machine: machineName });
  else if (trouble?.k === 'offline') text = t(WHY[trouble.cause], { machine: machineName });
  else if (net === NET.retry) text = t('next.connection.retrying');
  else if (net === NET.back) text = t('next.connection.back');

  const canRetry =
    trouble?.k === 'stale' || trouble?.cause === 'network' || trouble?.cause === 'machine-unreachable';
  const sub = seconds !== null ? t('next.connection.retryIn', { s: seconds }) : null;

  const pill = useMemo<IPill | null>(
    () => (net ? { net, text, sub, canRetry: !!canRetry } : null),
    [net, text, sub, canRetry],
  );
  const { value, state } = usePresence(pill, EXIT_MS);
  const { outer, inner } = usePillWidth();
  const host = container?.current;
  if (!value || !host) return null;

  return createPortal(
    <div
      ref={outer}
      className="hx-net"
      role="status"
      aria-live="polite"
      data-state={state === 'closing' ? 'closing' : value.net}
    >
      {/* Keyed by the state: a new state fades in over the old one while the pill eases to its new width. */}
      <span ref={inner} key={value.net} className="hx-net__in">
        <Icon name={ICON[value.net]} size="sm" className="hx-net__icon" />
        <span className="hx-net__text">{value.text}</span>
        {value.sub && <span className="hx-net__sub">{value.sub}</span>}
        {value.canRetry && (
          <button
            type="button"
            className="hx-net__now"
            disabled={isBusy(retry.phase)}
            onClick={() => retry.run('retry', async () => onRetryNow())}
          >
            {t('next.connection.now')}
          </button>
        )}
      </span>
    </div>,
    host,
  );
};

export default ConnectionBanner;
