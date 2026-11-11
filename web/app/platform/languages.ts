// The interface languages. Russian and English are always there; the nine others (ko, ja, zh-CN, zh-TW, es, de, fr,
// pt-BR, tr) are offered only where the installation turns on the `languages` page feature
// (RUNTIME_WEB_FEATURES=languages). A text a language does not have yet falls back to the English one.
export const BASE_LANGUAGES = ['ru', 'en'] as const;
export const EXTRA_LANGUAGES = ['ko', 'ja', 'zh-CN', 'zh-TW', 'es', 'de', 'fr', 'pt-BR', 'tr'] as const;
export const ALL_LANGUAGES = [...BASE_LANGUAGES, ...EXTRA_LANGUAGES] as const;

export type TLanguage = (typeof ALL_LANGUAGES)[number];

/** Each language's own name, as the choice shows it. */
export const LANGUAGE_NAMES: Record<TLanguage, string> = {
  ru: 'Русский',
  en: 'English',
  ko: '한국어',
  ja: '日本語',
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  es: 'Español',
  de: 'Deutsch',
  fr: 'Français',
  'pt-BR': 'Português (Brasil)',
  tr: 'Türkçe',
};

/** The language a stored or configured value names, or null; the extra ones only when `extra` allows them. */
export const parseLanguage = (value: unknown, extra: boolean): TLanguage | null => {
  if (typeof value !== 'string') return null;
  const list: readonly string[] = extra ? ALL_LANGUAGES : BASE_LANGUAGES;
  return list.includes(value) ? (value as TLanguage) : null;
};

/** The languages the choice offers. */
export const offeredLanguages = (extra: boolean): readonly TLanguage[] =>
  extra ? ALL_LANGUAGES : BASE_LANGUAGES;

/**
 * The first language of the browser's preference list that the page has, or null. `pt` and `zh` alone name the variant
 * the page has (`pt-BR`, `zh-CN`); a regional tag the page has no text for (`ru-RU`, `en-GB`) falls to its language.
 */
export const browserLanguage = (preferred: readonly string[], extra: boolean): TLanguage | null => {
  const offered: readonly string[] = extra ? ALL_LANGUAGES : BASE_LANGUAGES;
  const variants: Record<string, string> = { pt: 'pt-BR', zh: 'zh-CN' };
  for (const tag of preferred) {
    const lower = tag.toLowerCase();
    const exact = offered.find((l) => l.toLowerCase() === lower);
    if (exact) return exact as TLanguage;
    const base = lower.split('-')[0] ?? '';
    const near = offered.find((l) => l.toLowerCase() === base) ?? offered.find((l) => l === variants[base]);
    if (near) return near as TLanguage;
  }
  return null;
};
