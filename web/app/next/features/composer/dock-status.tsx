// The quiet line over the field for what the server holds back and does by itself (the design's pending chip and
// launch steps): «Модель сменится на Sonnet 5.5 после хода» while a model change waits for the agent, and
// «Запускаю агента → Применяю модель → Отправляю сообщение» while an agent starts for a message. Both come and go with
// an animation and share one place, so the dock does not jump as one gives way to the other.
import { useStable } from '@/next/features/composer/use-stable';
import {
  STEP_STATE,
  stepsKey,
  type ILaunchStep,
  type TStep,
  type TStepState,
} from '@/next/features/composer/launch-chain';
import Icon from '@/next/ui/icon';
import { usePresence } from '@/next/ui/presence';
import { t } from '@/next/i18n';
import '@/next/styles/composer.css';

export const DOCK_STATUS_EXIT_MS = 240;

export interface IDockStatusProps {
  /** The sentence for a change that waits, already worded; null: none waits. */
  pending: string | null;
  /** The steps of a start; null: none is under way. They take the place of the sentence while they last. */
  steps: ILaunchStep[] | null;
}

type TContent = { kind: 'steps'; steps: ILaunchStep[] } | { kind: 'pending'; text: string };

const STEP_ICON: Record<TStepState, string> = {
  [STEP_STATE.todo]: 'circle',
  [STEP_STATE.active]: 'loader-circle',
  [STEP_STATE.done]: 'circle-check',
};

const stepLabel = (id: TStep): string => t(`next.launch.step.${id}`);

const DockStatus = ({ pending, steps }: IDockStatusProps) => {
  let content: TContent | null = null;
  let key = '';
  if (steps) {
    content = { kind: 'steps', steps };
    key = `steps ${stepsKey(steps)}`;
  } else if (pending) {
    content = { kind: 'pending', text: pending };
    key = `pending ${pending}`;
  }
  const stable = useStable(content, key);
  const { value, state } = usePresence(stable, DOCK_STATUS_EXIT_MS);
  if (!value) return null;
  return (
    <div className="hx-dockstat" data-state={state} data-kind={value.kind} role="status">
      <div className="hx-dockstat__clip">
        {value.kind === 'pending' ? (
          <p className="hx-dockstat__line">
            <Icon name="clock" size="sm" className="hx-dockstat__ic" />
            <span>{value.text}</span>
          </p>
        ) : (
          <ol className="hx-dockstat__steps" aria-label={t('next.launch.title')}>
            {value.steps.map((step) => (
              <li key={step.id} className="hx-dockstat__step" data-state={step.state}>
                <Icon name={STEP_ICON[step.state]} size="sm" className="hx-dockstat__mark" />
                <span>{stepLabel(step.id)}</span>
              </li>
            ))}
          </ol>
        )}
      </div>
    </div>
  );
};

export default DockStatus;
