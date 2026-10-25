// The chat's goal on a slim bar over the field: «Цель: …» while the agent works toward it (Claude's /goal), and
// «Остановить» ends it. A goal on pause says so; a goal that is met stays as a quiet line until the person lets it go.
// It comes and goes with an animation, in the place of the other lines of the dock.
import { useState } from 'react';
import { useStable } from '@/next/features/composer/use-stable';
import type { GoalStatus } from '@/next/protocol/v2';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import usePending, { isBusy } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import '@/next/styles/composer.css';
import '@/next/styles/goal.css';

export const GOAL_BAR_EXIT_MS = 240;

export interface IGoalBarProps {
  /** The goal on the record; null when the chat has none. */
  goal: { text: string; status: GoalStatus; since: number } | null;
  /** Ends the goal (types `/goal clear` into the agent); resolves when the runtime has taken it. */
  onStop: () => Promise<unknown>;
}

const STATUS_KEY: Record<GoalStatus, string> = {
  active: 'next.goalBar.active',
  paused: 'next.goalBar.paused',
  met: 'next.goalBar.met',
};

const GoalBar = ({ goal, onStop }: IGoalBarProps) => {
  // A goal that was met is let go for good: the same goal does not come back with the next refresh of the record.
  const [letGo, setLetGo] = useState<string | null>(null);
  const stop = usePending<'stop'>();
  const hidden = !!goal && goal.status === 'met' && letGo === `${goal.since} ${goal.text}`;
  const shown = goal && !hidden ? goal : null;
  const key = shown ? `${shown.status} ${shown.since} ${shown.text}` : '';
  const stable = useStable(shown, key);
  const { value, state } = usePresence(stable, GOAL_BAR_EXIT_MS);
  if (!value) return null;
  const met = value.status === 'met';
  return (
    <div className="hx-dockstat hx-goalbar" data-state={state} data-status={value.status} role="status">
      <div className="hx-dockstat__clip">
        <p className="hx-dockstat__line">
          <Icon name={met ? 'circle-check' : 'locate'} size="sm" className="hx-goalbar__ic" />
          <span className="hx-goalbar__text" title={value.text}>
            <b>{t(STATUS_KEY[value.status])}</b> {value.text}
          </span>
          {met ? (
            <button
              type="button"
              className="hx-goalbar__act"
              onClick={() => setLetGo(`${value.since} ${value.text}`)}
            >
              {t('next.goalBar.hide')}
            </button>
          ) : (
            <button
              type="button"
              className="hx-goalbar__act"
              disabled={isBusy(stop.phase)}
              onClick={() => stop.run('stop', onStop)}
            >
              {t('next.goalBar.stop')}
            </button>
          )}
        </p>
      </div>
    </div>
  );
};

export default GoalBar;
