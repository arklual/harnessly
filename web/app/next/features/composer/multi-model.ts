// A new chat on several models at once (a beta function): the set of models one prompt starts a chat on each. The
// first of the set is the pill's own choice; the rest are its companions.
import type { IModelChoice } from '@/next/features/composer/agents';

/** At most this many chats start from one prompt: each is an agent process and a git worktree. */
export const MULTI_MAX = 4;

export const choiceKey = (
  c: Pick<IModelChoice, 'provider' | 'model'> & { profile?: string | null },
): string => `${c.profile ?? c.provider}:${c.model ?? ''}`;

/** The set with the choice in it, or without when it was: the last one cannot be taken out, and the set is capped. */
export function toggleChoice(set: readonly IModelChoice[], choice: IModelChoice): IModelChoice[] {
  const key = choiceKey(choice);
  if (set.some((c) => choiceKey(c) === key))
    return set.length > 1 ? set.filter((c) => choiceKey(c) !== key) : [...set];
  return set.length < MULTI_MAX ? [...set, choice] : [...set];
}

/** The prompt as a launch's name: its first line, cut. */
export const launchPrompt = (text: string, max = 200): string => {
  const line = text.trim().split('\n')[0];
  return line.length > max ? `${line.slice(0, max - 1)}…` : line;
};
