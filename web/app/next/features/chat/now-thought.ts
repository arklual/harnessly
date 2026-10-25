// The thought the live tail («Now») shows. The machine's "now" carries the turn's latest thought (clipped to 200
// characters, its whitespace collapsed), and the same thought already stands among the turn's steps as its own line:
// shown under them again it read twice («Очистил кэши… — ищу, что ещё удалить» before the running command and after).
// The tail repeats only a thought the steps do not show (not loaded yet).
import type { Entry } from '@/next/features/chat/types';

/** As the machine collapses it (internal/feed collapse): runs of spaces, tabs and line breaks become one space. */
const collapse = (text: string): string => text.replace(/[\n\t\r ]+/g, ' ').trim();

export const tailThought = (
  thought: string | null | undefined,
  steps: readonly Entry[] | null,
): string | null => {
  if (!thought) return null;
  if (steps === null) return thought;
  const said = collapse(thought);
  const shown = steps.some((entry) => entry.t === 'think' && collapse(entry.text).startsWith(said));
  return shown ? null : thought;
};
