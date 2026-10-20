/**
 * @-mentions in doc comments. A comment body stores a mention as `@[Name](person:<email>)`;
 * the composer shows it as `@Name`, the comment as a chip, and the agent reads `@Name`.
 * Shared by the gateway (validation) and the browser (composer and rendering).
 */

export const MAX_MENTIONS = 5;
const MENTION_RE = /@\[([^\]\n]{1,80})\]\(person:([^)\s]{3,254})\)/g;
const MAX_QUERY = 30;

export interface IMentionedPerson {
  /** The person's email: the id of a mention. */
  id: string;
  name: string;
}

export interface IMentionMatch {
  /** Index of the `@` in the text. */
  start: number;
  query: string;
}

export type TMentionSegment = { kind: 'text'; text: string } | { kind: 'mention'; name: string; id: string };

/** Every mention in a stored body, in order, duplicates kept. */
export const parseMentions = (body: string): IMentionedPerson[] =>
  [...body.matchAll(MENTION_RE)].map((m) => ({ name: m[1], id: m[2].toLowerCase() }));

/** The distinct people a body mentions, minus the author (mentioning oneself is ignored). */
export const mentionedIds = (body: string, authorId: string): string[] => {
  const ids = new Set(parseMentions(body).map((m) => m.id));
  ids.delete(authorId.toLowerCase());
  return [...ids];
};

/** The body split into plain text and mentions, for rendering chips. */
export const splitMentions = (body: string): TMentionSegment[] => {
  const segments: TMentionSegment[] = [];
  let last = 0;
  for (const m of body.matchAll(MENTION_RE)) {
    if (m.index > last) segments.push({ kind: 'text', text: body.slice(last, m.index) });
    segments.push({ kind: 'mention', name: m[1], id: m[2].toLowerCase() });
    last = m.index + m[0].length;
  }
  if (last < body.length) segments.push({ kind: 'text', text: body.slice(last) });
  return segments;
};

/** `@Name` for the agent and for lists: the markup without the id. */
export const plainMentions = (body: string): string => body.replace(MENTION_RE, '@$1');

/** The mention being typed at the caret: an `@` that starts a word, followed by up to two words without a line break. */
export const activeMention = (text: string, caret: number): IMentionMatch | null => {
  const before = text.slice(0, caret);
  const at = before.lastIndexOf('@');
  if (at < 0 || (at > 0 && !/\s/.test(before[at - 1]))) return null;
  const query = before.slice(at + 1);
  if (query.length > MAX_QUERY || /[\n@]/.test(query) || (query.match(/ /g)?.length ?? 0) > 1) return null;
  return { start: at, query };
};

/** Puts a chosen person into the text in place of what was typed after the `@`. */
export const applyMention = (
  text: string,
  caret: number,
  match: IMentionMatch,
  person: IMentionedPerson,
): { text: string; caret: number } => {
  const inserted = `@${person.name} `;
  return {
    text: `${text.slice(0, match.start)}${inserted}${text.slice(caret)}`,
    caret: match.start + inserted.length,
  };
};

/** The text of the composer to the stored body: each `@Name` that was picked from the list becomes markup. */
export const encodeMentions = (text: string, picked: IMentionedPerson[]): string => {
  let body = text;
  // Longest names first, so «@Anna Petrova» is not eaten by «@Anna».
  for (const person of [...picked].sort((a, b) => b.name.length - a.name.length)) {
    const token = `@${person.name}`;
    const re = new RegExp(`(^|\\s)${token.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?=$|[\\s.,;:!?)])`, 'g');
    body = body.replace(re, (_all, lead: string) => `${lead}@[${person.name}](person:${person.id})`);
  }
  return body;
};

/** Which picked people still stand in the text (a deleted chip is not a mention any more). */
export const stillMentioned = (text: string, picked: IMentionedPerson[]): IMentionedPerson[] =>
  picked.filter((p) => text.includes(`@${p.name}`));

/** Ranges of the composer text that are picked mentions, to draw chips behind them. */
export const mentionRanges = (text: string, picked: IMentionedPerson[]): { start: number; end: number }[] => {
  const ranges: { start: number; end: number }[] = [];
  for (const person of picked) {
    const token = `@${person.name}`;
    for (let from = text.indexOf(token); from >= 0; from = text.indexOf(token, from + token.length)) {
      ranges.push({ start: from, end: from + token.length });
    }
  }
  return ranges.sort((a, b) => a.start - b.start);
};

/** Moves the highlighted row of the list by `delta`, wrapping at both ends. */
export const stepIndex = (active: number, delta: number, length: number): number =>
  length === 0 ? 0 : (((active + delta) % length) + length) % length;
