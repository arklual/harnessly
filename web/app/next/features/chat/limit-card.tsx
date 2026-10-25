// A chat held on its agent's usage limit, at the end of its feed: a calm card that says when the agent goes on by
// itself and what the person can do now. The buttons wait for the runtime's answer (the card then changes or goes with
// the conversation's next upsert); the card leaves with an animation, not at once.
import { useEffect, useState } from 'react';
import { atPhrase, limitLine, limitPhase, LIMIT_PHASE } from '@/next/features/chat/limit';
import type { LimitAction, LimitHold, Ms } from '@/next/protocol/v2';
import { Button } from '@/next/ui/button';
import Icon from '@/next/ui/icon';
import { PRESENCE, usePresence } from '@/next/ui/presence';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import '@/next/styles/limit.css';

/** The card folds away as long as hx-limitfold in limit.css. */
const EXIT_MS = 320;
/** The times on the card are minutes: they move once in this long. */
const TICK_MS = 15_000;

export interface ILimitCardProps {
  /** The hold; null once the chat goes on (the card then folds away). */
  limited: LimitHold | null;
  /** The machine's now: the hold's times are its own. */
  clock: () => Ms;
  /** The runtime offers to decide (it announced the feature); without it the card only says. */
  controls: boolean;
  /** Settles when the runtime has done it, rejects with the reason when it has not. */
  onDecide: (action: LimitAction) => Promise<unknown>;
}

const LimitCard = ({ limited, clock, controls, onDecide }: ILimitCardProps) => {
  const { value, state } = usePresence(limited, EXIT_MS);
  // The minute moves by this tick; the card reads the clock when it draws, so it is never a tick behind a new hold.
  const [, setTick] = useState(0);
  const action = usePending<LimitAction>();
  useEffect(() => {
    if (!limited) return;
    const timer = window.setInterval(() => setTick((n) => n + 1), TICK_MS);
    return () => window.clearInterval(timer);
  }, [limited]);
  if (!value) return null;

  const now = clock();
  const busy = isBusy(action.phase);
  const phase = limitPhase(value);
  const press = (which: LimitAction) => action.run(which, () => onDecide(which));
  const at = value.resetsAt === null ? null : atPhrase(value.resetsAt, now);
  const button = (which: LimitAction, label: string, variant?: 'ghost') => (
    <Button
      size="sm"
      variant={variant}
      busy={busy && action.key === which}
      disabled={busy}
      onClick={() => press(which)}
    >
      {label}
    </Button>
  );
  return (
    <div className="hx-limitfold" data-state={state === PRESENCE.closing ? 'closing' : undefined}>
      <div className="hx-limitfold__in">
        <div className="hx-limit" role="status">
          <Icon name="clock" size="sm" className="hx-limit__ic" />
          <span className="hx-limit__text">{limitLine(value, now)}</span>
          {controls && (
            <span className="hx-limit__actions">
              {button('now', t('next.limit.now'))}
              {phase === LIMIT_PHASE.auto && button('cancel', t('next.limit.cancel'), 'ghost')}
              {phase === LIMIT_PHASE.held && at && button('auto', t('next.limit.autoAt', { at }), 'ghost')}
            </span>
          )}
          {action.phase === PENDING.failed && (
            <span className="hx-limit__note">{t('next.limit.failed')}</span>
          )}
        </div>
      </div>
    </div>
  );
};

export default LimitCard;
