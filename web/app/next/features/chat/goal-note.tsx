// The line in the feed where the chat's goal was reached: a divider with the goal's words, after the last turn.
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import '@/next/styles/goal.css';

const GoalNote = ({ text }: { text: string }) => (
  <div className="hx-goalline" role="status">
    <Icon name="circle-check" size="sm" className="hx-goalline__ic" />
    <span className="hx-goalline__text" title={text}>
      {t('next.goalBar.reached', { goal: text })}
    </span>
  </div>
);

export default GoalNote;
