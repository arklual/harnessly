// The line in the feed where the chat's goal was reached: a divider with the goal's words and a way to resume it.
import { useState } from 'react';
import Icon from '@/next/ui/icon';
import usePending, { isBusy, PENDING } from '@/next/ui/use-pending';
import { t } from '@/next/i18n';
import '@/next/styles/goal.css';

interface IGoalNoteProps {
  text: string;
  onContinue: () => Promise<unknown>;
}

const GoalNote = ({ text, onContinue }: IGoalNoteProps) => {
  const action = usePending<'continue'>();
  const [error, setError] = useState<string | null>(null);
  const failed = action.phase === PENDING.failed;
  const continueGoal = async () => {
    setError(null);
    try {
      await onContinue();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : t('next.goalBar.continueFailed'));
      throw cause;
    }
  };
  return (
    <div className="hx-goalline" role="status">
      <Icon name="circle-check" size="sm" className="hx-goalline__ic" />
      <span className="hx-goalline__text" title={text}>
        {t('next.goalBar.reached', { goal: text })}
      </span>
      {failed && (
        <span className="hx-goalline__error" role="alert">
          {error ?? t('next.goalBar.continueFailed')}
        </span>
      )}
      <button
        type="button"
        className="hx-goalline__act"
        disabled={isBusy(action.phase)}
        onClick={() => (failed ? action.retry() : action.run('continue', continueGoal))}
      >
        {t(failed ? 'next.goalBar.retry' : 'next.goalBar.continue')}
      </button>
    </div>
  );
};

export default GoalNote;
