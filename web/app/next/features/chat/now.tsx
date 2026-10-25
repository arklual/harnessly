// The live tail of a turn (block «Now»): what the agent last thought, what it does this moment with its command, and
// «✻ Working · 2 min 14 s» that ticks by itself once a second. While the machine does not answer the turn waits for it
// instead of failing, and says so.
import { useEffect, useState } from 'react';
import type { Entry, Ms, Now, TurnWait } from '@/next/features/chat/types';
import { tailThought } from '@/next/features/chat/now-thought';
import Icon from '@/next/ui/icon';
import { t } from '@/next/i18n';
import { elapsed } from '@/next/i18n/format';

/** The clock, once a second, for this component only: the rest of the feed does not render on the tick. */
const useTick = (on: boolean): Ms => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!on) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [on]);
  return now;
};

/** The live lines on screen and when one last left: the line under the feed and the turn's own are one line to the eye. */
let linesShown = 0;
let lineGoneAt = 0;
const SAME_LINE_MS = 900;

interface INowBlockProps {
  now: Now | null;
  startedAt: Ms;
  wait: TurnWait;
  machineName: string;
  /** The turn's steps over the tail: a thought they show is not repeated (now-thought.ts); null not loaded. */
  steps?: readonly Entry[] | null;
}

const NowBlock = ({ now, startedAt, wait, machineName, steps = null }: INowBlockProps) => {
  const clockNow = useTick(wait === null);
  // It comes in softly; but when it only changes owner (the line under the feed becomes the turn's) it is already there.
  const [continued] = useState(() => linesShown > 0 || Date.now() - lineGoneAt < SAME_LINE_MS);
  useEffect(() => {
    linesShown += 1;
    return () => {
      linesShown -= 1;
      lineGoneAt = Date.now();
    };
  }, []);
  const machineWait = wait === 'machine';
  let label = now?.action?.summary ?? t('next.now.starting');
  if (machineWait) label = t('next.now.waitMachineLabel');
  let word = now?.word ?? t('next.now.working');
  if (machineWait) word = t('next.now.waitMachine');
  const command = machineWait ? null : (now?.action?.command ?? null);
  const thought = tailThought(now?.thought, steps);
  return (
    <div
      className="hx-now"
      data-wait={machineWait ? 'machine' : undefined}
      data-continued={continued || undefined}
      role="status"
    >
      <div className="hx-now__rail">
        {thought && (
          <div className="hx-now__think">
            <Icon name="lightbulb" size="sm" />
            <span>{thought}</span>
          </div>
        )}
        <div className="hx-now__step">
          <span className="hx-now__spin" />
          <span className="hx-now__label">{label}</span>
          {command && <code className="hx-now__detail">{command}</code>}
        </div>
      </div>
      <div className="hx-now__status">
        <span className="hx-now__star">✻</span>
        <span className="hx-now__word">{word}</span>
        <span className="hx-now__time">
          {machineWait
            ? t('next.now.machineSilent', { machine: machineName })
            : elapsed(clockNow - startedAt)}
        </span>
        <span className="hx-now__hint">{t('next.now.stopHint')}</span>
      </div>
    </div>
  );
};

export default NowBlock;
