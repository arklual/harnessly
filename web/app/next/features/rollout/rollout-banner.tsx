// «Идёт обновление — возможны перебои с подключением к машинам»: the same pill as «Нет связи» (.hx-net), at the top of
// the window, while the hub says a rollout is under way; «Обновление завершено» for a moment, then it leaves by itself.
// No versions, no states of the machines: the person needs to know why things blink, and that it ends.
import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import Icon from '@/next/ui/icon';
import { usePortalContainer } from '@/next/ui/portal';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import { ROLLOUT, useRollout, watchRollout } from '@/next/features/rollout/rollout';
import '@/next/styles/connection.css';

const EXIT_MS = 320;

const RolloutBanner = () => {
  useEffect(() => watchRollout(), []);
  const container = usePortalContainer();
  const phase = useRollout();
  const { value, state } = usePresence(phase === ROLLOUT.idle ? null : phase, EXIT_MS);
  const host = container?.current;
  if (!value || !host) return null;
  const done = value === ROLLOUT.done;
  let shown = done ? 'back' : 'retry';
  if (state === 'closing') shown = 'closing';

  return createPortal(
    <div className="hx-net" role="status" aria-live="polite" data-kind="rollout" data-state={shown}>
      <span key={value} className="hx-net__in">
        <Icon name={done ? 'check' : 'loader-circle'} size="sm" className="hx-net__icon" />
        <span className="hx-net__text">{t(done ? 'next.rollout.done' : 'next.rollout.active')}</span>
      </span>
    </div>,
    host,
  );
};

export default RolloutBanner;
