// What a file is by its name: the language of the editor (parsing, highlighting, indentation, the language's own
// completions) and the name the status row shows. Languages load on demand, each in a chunk of its own.
import { markdown, markdownLanguage } from '@codemirror/lang-markdown';
import { LanguageDescription, type LanguageSupport } from '@codemirror/language';
import { languages } from '@codemirror/language-data';
import { t } from '@/next/i18n';

const MARKDOWN = 'Markdown';

const describe = (path: string) =>
  LanguageDescription.matchFilename(languages, path.split('/').pop() ?? path);

/** The language's name as an IDE shows it («TypeScript», «Markdown»); a file of no known language is plain text. */
export const languageLabel = (path: string): string => describe(path)?.name ?? t('next.editor.plain');

/** No known language: the text wraps as prose does. */
export const isPlain = (path: string): boolean => describe(path) === null;

export const isMarkdown = (path: string): boolean => describe(path)?.name === MARKDOWN;

/** The language's support, or null for plain text. Fenced code in markdown is highlighted by its own language. */
export const loadLanguage = async (path: string): Promise<LanguageSupport | null> => {
  const found = describe(path);
  if (!found) return null;
  if (found.name === MARKDOWN) return markdown({ base: markdownLanguage, codeLanguages: languages });
  return found.load();
};
