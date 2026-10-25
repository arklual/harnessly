// The steps of a start, kept on screen as a whole: a chain begins when the agent starts or the runtime types a waiting
// change into it, keeps its steps while they are done one by one, and stays a moment after the last so the eye sees it
// finished. An agent that did not start ends it at once: the line over the field says what waits instead.
import { useEffect, useState } from 'react';
import {
  chainOver,
  chainPlan,
  chainSteps,
  stepsKey,
  type IChainInput,
  type IChainPlan,
  type ILaunchStep,
} from '@/next/features/composer/launch-chain';

/** How long a finished chain stays, all steps done, before it leaves. */
export const CHAIN_LINGER_MS = 900;

const FAILED = new Set(['stopped', 'gone', 'launch-failed']);

export const useLaunchChain = (input: IChainInput): ILaunchStep[] | null => {
  const [plan, setPlan] = useState<IChainPlan | null>(null);
  const [view, setView] = useState<{ key: string; steps: ILaunchStep[] | null }>({ key: '', steps: null });
  const aborted = plan !== null && input.agent !== undefined && FAILED.has(input.agent);

  // State adjusted during render, not in effects: a chain begins and ends with the stores it reads.
  let current = plan;
  if (aborted) current = null;
  else if (!current) current = chainPlan(input);
  if (current !== plan) setPlan(current);

  const steps = current ? chainSteps(current, input) : null;
  const key = steps ? stepsKey(steps) : '';
  if (key !== view.key) setView({ key, steps });

  const over = current !== null && chainOver(input);
  useEffect(() => {
    if (!over) return;
    const timer = setTimeout(() => setPlan(null), CHAIN_LINGER_MS);
    return () => clearTimeout(timer);
  }, [over]);

  return view.steps;
};
