// The hub UI's texts. Russian is the default (the product is Russian first), English is the second language; both
// dictionaries have the same keys, which the compiler checks (locales/en.ts is typed by the Russian keys). A text
// holds `{name}` placeholders; a counted text holds one form per plural category of the language.
import { en } from '../locales/en';
import { type Entry, ru, type Key } from '../locales/ru';

export type { Entry, Key };
export type Lang = 'ru' | 'en';
type Params = Record<string, string | number>;

const STORE = 'hl-hub-lang';

/** `?lang=en` once, remembered; otherwise the remembered choice; otherwise Russian. */
export const detectLang = (search: string, read: () => string | null, write: (v: Lang) => void): Lang => {
  const asked = new URLSearchParams(search).get('lang');
  if (asked === 'ru' || asked === 'en') {
    write(asked);
    return asked;
  }
  const stored = read();
  return stored === 'en' ? 'en' : 'ru';
};

let lang: Lang = 'ru';

export const initLang = (): Lang => {
  lang = detectLang(
    typeof location === 'undefined' ? '' : location.search,
    () => {
      try {
        return localStorage.getItem(STORE);
      } catch {
        return null;
      }
    },
    (v) => {
      try {
        localStorage.setItem(STORE, v);
      } catch {
        // a blocked storage: the choice holds for this page only
      }
    },
  );
  if (typeof document !== 'undefined') document.documentElement.lang = lang;
  return lang;
};

export const setLang = (next: Lang) => {
  lang = next;
};

export const currentLang = (): Lang => lang;

const fill = (text: string, params?: Params): string =>
  params
    ? text.replace(/\{(\w+)\}/g, (all, name: string) =>
        params[name] === undefined ? all : String(params[name]),
      )
    : text;

const entryOf = (key: Key): Entry => (lang === 'en' ? en[key] : ru[key]);

export const t = (key: Key, params?: Params): string => {
  const entry = entryOf(key);
  if (typeof entry === 'string') return fill(entry, params);
  return fill(entry.other ?? Object.values(entry)[0] ?? key, params);
};

const rules = new Map<Lang, Intl.PluralRules>();

/** A counted text; `{n}` is the count. */
export const plural = (n: number, key: Key, params?: Params): string => {
  const entry = entryOf(key);
  if (typeof entry === 'string') return fill(entry, { n, ...params });
  let rule = rules.get(lang);
  if (!rule) {
    rule = new Intl.PluralRules(lang);
    rules.set(lang, rule);
  }
  const form = entry[rule.select(n)] ?? entry.other ?? '';
  return fill(form, { n, ...params });
};
