import { useEffect, useMemo, useState } from 'react';
import { ChevronRight, SquareTerminal } from 'lucide-react';
import ClaudeSpark from '@/components/icons/claude-spark';
import ElapsedTime from '@/components/timeline/elapsed-time';
import { Fold } from '@/components/timeline/turn-row';
import { useHoldTimelinePosition } from '@/components/timeline/timeline-scroll-context';
import useBackgroundTasks from '@/hooks/use-background-tasks';
import {
  backgroundWorkLine,
  deriveBackgroundWork,
  holdBackgroundWork,
  shouldShowBackgroundWork,
} from '@/lib/background-work';
import { PHASE_HOLD_MS, type TTurnPhase } from '@/lib/turn-phase';
import { t } from '@/lib/i18n';

const currentTime = () => Date.now();

interface IBackgroundWorkLineProps {
  tabId?: string;
  phase: TTurnPhase;
}

/** Under a finished turn: what the agent still runs in the background, in the style of the turn's own live line. */
const BackgroundWorkLine = ({ tabId, phase }: IBackgroundWorkLineProps) => {
  const holdPosition = useHoldTimelinePosition();
  const tasks = useBackgroundTasks(tabId);
  const derived = useMemo(
    () => (shouldShowBackgroundWork(phase) ? deriveBackgroundWork(tasks) : null),
    [phase, tasks],
  );
  const [held, setHeld] = useState<{ work: ReturnType<typeof deriveBackgroundWork>; at: number }>({
    work: null,
    at: 0,
  });
  const [now, setNow] = useState(currentTime);
  const result = holdBackgroundWork(held.work, held.at, derived, now);
  if (result.work !== held.work || result.at !== held.at) setHeld(result);
  const pending = (derived === null) !== (result.work === null);

  useEffect(() => {
    if (!pending) return;
    const timer = setTimeout(() => setNow(currentTime()), PHASE_HOLD_MS + 10);
    return () => clearTimeout(timer);
  }, [pending, held.at]);

  const [open, setOpen] = useState(false);
  const work = result.work;
  if (!work) return null;
  return (
    <div className="tw-turn tw-bgwork" data-testid="background-work">
      <button
        type="button"
        className="tw-turn-summary tw-turn-summary-live tw-bgwork-head"
        aria-expanded={open}
        onClick={() => {
          holdPosition();
          setOpen((v) => !v);
        }}
      >
        <span className="tw-turn-icon tw-bgwork-icon" aria-hidden="true">
          <ClaudeSpark state="thinking" />
        </span>
        <span className="tw-turn-label">{backgroundWorkLine(work)}</span>
        <ChevronRight size={13} className="tw-turn-chevron" aria-hidden="true" />
        {work.since !== null && (
          <span className="tw-turn-time" aria-hidden="true">
            <ElapsedTime since={work.since} />
          </span>
        )}
      </button>
      <Fold open={open} enter className="tw-bgwork-body">
        <ul className="tw-bgwork-list">
          {work.items.map((item) => (
            <li key={item.id}>
              {item.kind === 'command' && <SquareTerminal size={13} aria-hidden="true" />}
              <span className="tw-bgwork-name">
                {item.label ||
                  (item.kind === 'command'
                    ? t('timeline.backgroundWorkLine.command')
                    : t('timeline.backgroundWorkLine.subagent'))}
              </span>
              {item.startedAt !== null && (
                <small>
                  <ElapsedTime since={item.startedAt} />
                </small>
              )}
            </li>
          ))}
        </ul>
      </Fold>
    </div>
  );
};

export default BackgroundWorkLine;
