// The chat's goal on a slim bar over the field: «Цель: …» while the agent works toward it (Claude's /goal), and
// «Остановить» ends it. A goal on pause says so; a met goal leaves the composer and stays in the feed's history.
// It comes and goes with an animation, in the place of the other lines of the dock.
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

const STATUS_KEY = {
  active: 'next.goalBar.active',
  paused: 'next.goalBar.paused',
} as const;

const GoalBar = ({ goal, onStop }: IGoalBarProps) => {
  const stop = usePending<'stop'>();
  const shown = goal && goal.status !== 'met' ? { ...goal, status: goal.status } : null;
  const key = shown ? `${shown.status} ${shown.since} ${shown.text}` : '';
  const stable = useStable(shown, key);
  const { value, state } = usePresence(stable, GOAL_BAR_EXIT_MS);
  if (!value) return null;
  return (
    <div className="hx-dockstat hx-goalbar" data-state={state} data-status={value.status} role="status">
      <div className="hx-dockstat__clip">
        <p className="hx-dockstat__line">
          <Icon name="locate" size="sm" className="hx-goalbar__ic" />
          <span className="hx-goalbar__text" title={value.text}>
            <b>{t(STATUS_KEY[value.status])}</b> {value.text}
          </span>
          <button
            type="button"
            className="hx-goalbar__act"
            disabled={isBusy(stop.phase)}
            onClick={() => stop.run('stop', onStop)}
          >
            {t('next.goalBar.stop')}
          </button>
        </p>
      </div>
    </div>
  );
};

export default GoalBar;
