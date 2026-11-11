// «Пройти ревью»: the agent beside the review goes through the change and answers with its suggestions in a fenced
// block the page reads back from the chat — each a remark on lines of a file, maybe with the code it would put there.
// The screen shows them as a layer of their own («предложено агентом»): taken into the draft, or thrown away.
import { hashText } from '@/next/features/review/patch';
import { LINE_SIDE, type IReviewFile, type TLineSide } from '@/next/features/review/model';

export const SUGGESTIONS_FENCE = 'review-suggestions';

export interface IAgentSuggestion {
  /** Stable for the same suggestion, so a choice about it holds when the chat is read again. */
  id: string;
  path: string;
  side: TLineSide;
  from: number;
  to: number;
  text: string;
  /** The code the agent would put in place of the lines, when it proposes an edit. */
  replacement?: string;
}

const MAX_SUGGESTIONS = 200;
const MAX_TEXT = 8000;

const toInt = (v: unknown): number | null =>
  typeof v === 'number' && Number.isInteger(v) && v > 0 ? v : null;

const readOne = (v: unknown): IAgentSuggestion | null => {
  if (!v || typeof v !== 'object') return null;
  const o = v as Record<string, unknown>;
  const from = toInt(o.from ?? o.line);
  const to = toInt(o.to ?? o.from ?? o.line);
  if (typeof o.path !== 'string' || !o.path || from === null || to === null || to < from) return null;
  if (typeof o.text !== 'string' || !o.text.trim() || o.text.length > MAX_TEXT) return null;
  const side = o.side === LINE_SIDE.before ? LINE_SIDE.before : LINE_SIDE.after;
  const replacement =
    typeof o.replacement === 'string' && o.replacement.length <= MAX_TEXT ? o.replacement : undefined;
  const id = hashText(JSON.stringify([o.path, side, from, to, o.text, replacement ?? '']));
  return {
    id,
    path: o.path,
    side,
    from,
    to,
    text: o.text.trim(),
    ...(replacement !== undefined ? { replacement } : {}),
  };
};

const BLOCK = new RegExp('```' + SUGGESTIONS_FENCE + '[^\\n]*\\n([\\s\\S]*?)\\n```', 'g');

/** The suggestions of the last block of the kind in a text; null when it has none. */
export const parseSuggestions = (text: string): IAgentSuggestion[] | null => {
  let last: string | null = null;
  for (const m of text.matchAll(BLOCK)) last = m[1] ?? null;
  if (last === null) return null;
  let raw: unknown;
  try {
    raw = JSON.parse(last);
  } catch {
    return [];
  }
  const list = Array.isArray(raw) ? raw : [];
  return list
    .slice(0, MAX_SUGGESTIONS)
    .map(readOne)
    .filter((s): s is IAgentSuggestion => s !== null);
};

/** The newest answer of the agent that has a block of suggestions, read from the newest text back. */
export const latestSuggestions = (texts: readonly string[]): IAgentSuggestion[] | null => {
  for (let i = texts.length - 1; i >= 0; i--) {
    const found = parseSuggestions(texts[i] ?? '');
    if (found !== null) return found;
  }
  return null;
};

/** What the agent is asked: go through the change and answer in the block the page reads. */
export const reviewRequest = (
  files: readonly Pick<IReviewFile, 'path' | 'additions' | 'deletions'>[],
  where: string,
  words: { ask: string; format: string },
): string => {
  const list = files.map((f) => `- ${f.path} (+${f.additions} −${f.deletions})`).join('\n');
  const sample = JSON.stringify(
    [{ path: 'src/a.ts', side: 'after', from: 12, to: 14, text: '…', replacement: '…' }],
    null,
    0,
  );
  return `${words.ask}\n${where}\n\n${list}\n\n${words.format}\n\`\`\`${SUGGESTIONS_FENCE}\n${sample}\n\`\`\``;
};

/** The lines of a text with the suggestion's replacement in place of lines from–to (1-based, inclusive). */
export const applyReplacement = (text: string, from: number, to: number, replacement: string): string => {
  const lines = text.split('\n');
  const body = replacement.endsWith('\n') ? replacement.slice(0, -1) : replacement;
  lines.splice(from - 1, to - from + 1, ...body.split('\n'));
  return lines.join('\n');
};

/** A GitLab suggestion of a note: the replacement of the lines above the note's line (on its last line). */
export const gitlabSuggestion = (
  s: Pick<IAgentSuggestion, 'from' | 'to' | 'text' | 'replacement'>,
): string => {
  if (s.replacement === undefined) return s.text;
  const above = s.to - s.from;
  let fence = '```';
  while (s.replacement.includes(fence)) fence += '`';
  return `${s.text}\n\n${fence}suggestion:-${above}+0\n${s.replacement.replace(/\n$/, '')}\n${fence}`;
};
