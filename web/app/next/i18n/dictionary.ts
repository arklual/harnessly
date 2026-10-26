// The texts of the page's language, loaded before anything that shows text runs: the module waits for its language's
// dictionary (top-level await), so a t() call at import time already has it. Only one language goes to the phone; the
// development galleries (`?dev=`) and the tests switch languages, so they get both.
import { appConfig } from '@/platform/config';

export type TLang = 'en' | 'ru';
type TEntry = string | Partial<Record<Intl.LDMLPluralRule, string>>;
export type TDictionary = Record<string, TEntry>;

const LOADERS: Record<TLang, () => Promise<{ default: TDictionary }>> = {
  en: () => import('virtual:dictionary/en'),
  ru: () => import('virtual:dictionary/ru'),
};

const loaded: Partial<Record<TLang, TDictionary>> = {};

const load = async (lang: TLang) => {
  loaded[lang] ??= (await LOADERS[lang]()).default;
};

/** The dictionary of a language: the page's own one is always there, another one only where both are loaded. */
export const dictionary = (lang: TLang): TDictionary => loaded[lang] ?? loaded.en ?? loaded.ru ?? {};

const both =
  import.meta.env.MODE === 'test' ||
  (typeof location !== 'undefined' && new URLSearchParams(location.search).has('dev'));

await (both ? Promise.all([load('en'), load('ru')]) : load(appConfig().locale === 'ru' ? 'ru' : 'en'));
