// The page's texts in English (the default) and Russian, picked by the runtime's locale (RUNTIME_LOCALE in the page
// config). Dictionaries live in app/locales/<lang>/<namespace>.json, one namespace per part of the page, so the parts
// can be worked on separately. Keys are `<namespace>.<component>.<name>`; a missing Russian text falls back to the
// English one, a missing English text to the key itself.
//
//   t('timeline.toolGroup.oneError')                       → "1 with an error"
//   t('timeline.toolGroup.errors', { n: 3 })                → "{n} with an error" with {n} filled in
//   plural(3, 'timeline.toolGroup.actions')                 → picks one/few/many/other by the language's plural rules
import { appConfig } from '@/platform/config';

type TEntry = string | Partial<Record<Intl.LDMLPluralRule, string>>;
type TDictionary = Record<string, TEntry>;

const load = (lang: 'en' | 'ru'): TDictionary => {
  const files =
    lang === 'en'
      ? import.meta.glob('../locales/en/*.json', { eager: true, import: 'default' })
      : import.meta.glob('../locales/ru/*.json', { eager: true, import: 'default' });
  return Object.assign({}, ...Object.values(files)) as TDictionary;
};

const english = load('en');
export const locale = (): 'en' | 'ru' => appConfig().locale;
const current: TDictionary = locale() === 'ru' ? { ...english, ...load('ru') } : english;

const fill = (text: string, params?: Record<string, string | number>): string =>
  params
    ? text.replace(/\{(\w+)\}/g, (all, name: string) =>
        params[name] === undefined ? all : String(params[name]),
      )
    : text;

/** The text of a key in the page's language, with `{name}` placeholders filled from `params`. */
export const t = (key: string, params?: Record<string, string | number>): string => {
  const entry = current[key];
  if (typeof entry === 'string') return fill(entry, params);
  if (entry && typeof entry === 'object')
    return fill(entry.other ?? entry.many ?? Object.values(entry)[0] ?? key, params);
  return key;
};

const rules = new Map<string, Intl.PluralRules>();

/** A counted text: the key holds one form per plural category of the language (`{n}` is the count). */
export const plural = (n: number, key: string, params?: Record<string, string | number>): string => {
  const entry = current[key];
  if (typeof entry !== 'object' || !entry) return t(key, { n, ...params });
  const lang = locale();
  let rule = rules.get(lang);
  if (!rule) {
    rule = new Intl.PluralRules(lang);
    rules.set(lang, rule);
  }
  const form = entry[rule.select(n)] ?? entry.other ?? entry.many ?? '';
  return fill(form, { n, ...params });
};
