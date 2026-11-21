// The texts of «Что нового» for the dictionaries: one file per entry in app/next/features/whats-new/entries, both
// languages side by side (see the README there). A broken entry stops the build with its file name, so a branch that
// forgot a language or a day never reaches main.
import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';

import { isText, whatsNewProblems, type IText } from './app/next/features/whats-new/check';

export const WHATS_NEW_ENTRIES = fileURLToPath(
  new URL('./app/next/features/whats-new/entries/', import.meta.url),
);

/** Every entry file: its id and its parsed body. Throws on the first broken one. */
export const readWhatsNew = (
  onFile?: (path: string) => void,
): { id: string; entry: Record<string, unknown> }[] =>
  readdirSync(WHATS_NEW_ENTRIES)
    .filter((file) => file.endsWith('.json'))
    .sort()
    .map((file) => {
      onFile?.(WHATS_NEW_ENTRIES + file);
      const raw = readFileSync(WHATS_NEW_ENTRIES + file, 'utf8');
      const problems = whatsNewProblems(file, raw);
      if (problems.length > 0) throw new Error(`«Что нового» entries/${file}: ${problems.join('; ')}`);
      return { id: file.replace(/\.json$/, ''), entry: JSON.parse(raw) as Record<string, unknown> };
    });

/** The dictionary keys of one language: next.whatsNew.<id>.title / .text, English where the language has none. */
export const whatsNewTexts = (lang: string, onFile?: (path: string) => void): Record<string, string> => {
  const texts: Record<string, string> = {};
  for (const { id, entry } of readWhatsNew(onFile)) {
    const own = entry[lang];
    const text = isText(own) ? own : (entry.en as IText);
    texts[`next.whatsNew.${id}.title`] = text.title;
    texts[`next.whatsNew.${id}.text`] = text.text;
  }
  return texts;
};
