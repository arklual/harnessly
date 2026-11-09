// The texts of the page's language, loaded before anything that shows text runs: the module waits for its language's
// dictionary (top-level await), so a t() call at import time already has it. Only one language goes to the phone; the
// development galleries (`?dev=`) and the tests switch languages, so they get both.
import { appConfig } from '@/platform/config';

import { ALL_LANGUAGES, type TLanguage } from '@/platform/languages';

export type TLang = TLanguage;
type TEntry = string | Partial<Record<Intl.LDMLPluralRule, string>>;
export type TDictionary = Record<string, TEntry>;

const LOADERS = {
  en: () => import('virtual:dictionary/en'),
  ru: () => import('virtual:dictionary/ru'),
  ko: () => import('virtual:dictionary/ko'),
  ja: () => import('virtual:dictionary/ja'),
  'zh-CN': () => import('virtual:dictionary/zh-CN'),
  'zh-TW': () => import('virtual:dictionary/zh-TW'),
  es: () => import('virtual:dictionary/es'),
  de: () => import('virtual:dictionary/de'),
  fr: () => import('virtual:dictionary/fr'),
  'pt-BR': () => import('virtual:dictionary/pt-BR'),
  tr: () => import('virtual:dictionary/tr'),
} satisfies Record<TLang, () => Promise<{ default: TDictionary }>>;

const loaded: Partial<Record<TLang, TDictionary>> = {};

const load = async (lang: TLang) => {
  loaded[lang] ??= (await LOADERS[lang]()).default;
};

/** The dictionary of a language: the page's own one is always there, another one only where both are loaded. */
export const dictionary = (lang: TLang): TDictionary => loaded[lang] ?? loaded.en ?? loaded.ru ?? {};

const both =
  import.meta.env.MODE === 'test' ||
  (typeof location !== 'undefined' && new URLSearchParams(location.search).has('dev'));

await (both ? Promise.all(ALL_LANGUAGES.map(load)) : load(appConfig().locale));
