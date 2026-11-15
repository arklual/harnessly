// The digest on the page cut into the phrases the voice reads: the same parts the page draws, split at the
// ends of sentences, so the lit phrase is exactly the words on screen. A phrase's anchor is the first thing it links to
// (the voice talks about that thing when asked), else its place in the digest.
import type { TItemId, TRichPart } from '@/next/features/day/feed';
import type { IDigestPhrase } from '@/next/features/voice-live/digest-voice';

export interface IDayPhrase extends IDigestPhrase {
  /** The parts of the line in this phrase, drawn as they are. */
  parts: TRichPart[];
  /** The thing the anchor names, as the feed gives its id. */
  item: TItemId | null;
}

/** The end of a sentence and the spaces after it: the cut goes after the spaces. */
const END = /[.!?…]+["»)]*\s+/g;

const textOf = (part: TRichPart): string => (typeof part === 'string' ? part : part.t);

/** The line split into phrases; a link never splits, a string splits at the ends of its sentences. */
export const dayPhrases = (parts: readonly TRichPart[]): IDayPhrase[] => {
  const groups: TRichPart[][] = [[]];
  for (const part of parts) {
    if (typeof part !== 'string') {
      groups[groups.length - 1].push(part);
      continue;
    }
    let at = 0;
    for (const match of part.matchAll(END)) {
      const cut = match.index + match[0].length;
      groups[groups.length - 1].push(part.slice(at, cut));
      groups.push([]);
      at = cut;
    }
    if (at < part.length) groups[groups.length - 1].push(part.slice(at));
  }
  const out: IDayPhrase[] = [];
  for (const group of groups) {
    const text = group.map(textOf).join('').replace(/\s+/g, ' ').trim();
    if (!text) {
      // Spaces alone stay with the phrase before, so nothing on screen is lost.
      if (group.length > 0 && out.length > 0) out[out.length - 1].parts.push(...group);
      continue;
    }
    const link = group.find((p) => typeof p !== 'string' && p.item !== undefined);
    const item = link !== undefined && typeof link !== 'string' ? (link.item ?? null) : null;
    out.push({ text, parts: group, item, anchor: item === null ? `digest:${out.length}` : String(item) });
  }
  return out;
};
