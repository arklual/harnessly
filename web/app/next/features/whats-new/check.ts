// What makes a «Что нового» entry file whole: the build (vite.whats-new.ts) refuses a broken one and the test of
// entries/ names every problem. Plain code without the file system, so both can use it.

/** The languages every entry must have; the others fall back to English like any text. */
export const WHATS_NEW_LANGUAGES = ['en', 'ru'] as const;

const ID = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
const DAY = /^\d{4}-\d\d-\d\d$/;

export interface IText {
  title: string;
  text: string;
}

export const isText = (value: unknown): value is IText =>
  typeof value === 'object' &&
  value !== null &&
  typeof (value as IText).title === 'string' &&
  (value as IText).title.trim() !== '' &&
  typeof (value as IText).text === 'string' &&
  (value as IText).text.trim() !== '';

/** What is wrong with one entry file, or nothing. */
export const whatsNewProblems = (file: string, raw: string): string[] => {
  const problems: string[] = [];
  const id = file.replace(/\.json$/, '');
  if (!ID.test(id)) problems.push('the file name is lower-case words with dashes (the branch slug)');
  let entry: Record<string, unknown>;
  try {
    entry = JSON.parse(raw) as Record<string, unknown>;
  } catch (error) {
    return [...problems, `not JSON: ${(error as Error).message}`];
  }
  const date = entry.date;
  if (typeof date !== 'string' || !DAY.test(date) || Number.isNaN(Date.parse(`${date}T00:00:00Z`))) {
    problems.push('"date" is the day it goes out, YYYY-MM-DD');
  }
  if (typeof entry.icon !== 'string' || entry.icon === '') problems.push('"icon" is a Lucide icon name');
  for (const lang of WHATS_NEW_LANGUAGES) {
    if (!isText(entry[lang])) problems.push(`"${lang}" needs a non-empty "title" and "text"`);
  }
  return problems;
};
