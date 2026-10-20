import { plural, t } from '@/lib/i18n';
import { PHASE_HOLD_MS, type TTurnPhase } from '@/lib/turn-phase';
import type { IBackgroundTask } from '@/types/server/hl-background';

export interface IBackgroundWorkItem {
  id: string;
  kind: 'subagent' | 'command';
  label: string;
  startedAt: number | null;
}

export interface IBackgroundWork {
  subagents: number;
  commands: number;
  /** Named subagents first, then commands. */
  labels: string[];
  since: number | null;
  items: IBackgroundWorkItem[];
}

/** What keeps running after the agent ended its turn, from the same tasks as the composer's chip; alarms are not work. */
export const deriveBackgroundWork = (tasks: IBackgroundTask[]): IBackgroundWork | null => {
  const running = tasks.filter((task) => task.kind !== 'wakeup');
  if (running.length === 0) return null;
  const items = running.map<IBackgroundWorkItem>((task) => ({
    id: task.id,
    kind: task.kind === 'agent' ? 'subagent' : 'command',
    label: task.label,
    startedAt: task.startedAt > 0 ? task.startedAt : null,
  }));
  const started = items.flatMap((item) => (item.startedAt === null ? [] : [item.startedAt]));
  return {
    subagents: items.filter((item) => item.kind === 'subagent').length,
    commands: items.filter((item) => item.kind === 'command').length,
    labels: items.filter((item) => item.label).map((item) => item.label),
    since: started.length ? Math.min(...started) : null,
    items,
  };
};

const capitalize = (text: string) => text.charAt(0).toUpperCase() + text.slice(1);

export const backgroundWorkLine = ({ subagents, commands, items }: IBackgroundWork): string => {
  const commandLabels = items
    .filter((item) => item.kind === 'command' && item.label)
    .map((item) => item.label);
  const single = commands === 1 && commandLabels.length === 1;
  const commandsPart = single
    ? t('lib.backgroundWork.runningCommand', { command: commandLabels[0] })
    : commands > 0
      ? plural(commands, 'lib.backgroundWork.commandsInBackground')
      : null;
  const subagentsPart = subagents > 0 ? plural(subagents, 'lib.backgroundWork.subagentsWorking') : null;
  if (subagentsPart && commandsPart) return `${subagentsPart} · ${commandsPart}`;
  if (subagentsPart) return subagentsPart;
  if (!commandsPart) return '';
  if (single) return capitalize(commandsPart);
  return plural(commands, 'lib.backgroundWork.commandsWorking');
};

/** While the main turn runs its own live line says so; the background line only fills the gap after it. */
export const shouldShowBackgroundWork = (phase: TTurnPhase): boolean => phase === 'settled';

/** The line appears and goes away only after the work stayed that way for PHASE_HOLD_MS; counts of a shown line update at once. */
export const holdBackgroundWork = (
  shown: IBackgroundWork | null,
  shownAt: number,
  next: IBackgroundWork | null,
  now: number,
): { work: IBackgroundWork | null; at: number } => {
  if ((shown === null) === (next === null)) return { work: next, at: shownAt };
  if (now - shownAt < PHASE_HOLD_MS) return { work: shown, at: shownAt };
  return { work: next, at: now };
};
