// The new interface's texts without the current interface's lib/i18n: the dictionaries app/locales/<lang>/next*.json, loaded by ./dictionary
// (flat `{ "next.<component>.<name>": "text" }`), a missing Russian text is the English one, a missing English text is
// the key, `{name}` placeholders are filled, a counted text keeps one form per plural category and takes {n}. `{design}`
// is the installation's name of the design tool (RUNTIME_DESIGN_NAME) unless the call gives one.
import { dictionary, type TLang } from '@/next/i18n/dictionary';
import { appConfig } from '@/platform/config';

type TParams = Record<string, string | number>;
export type { TLang };

let override: TLang | null = null;

/** The page's language; the development galleries may pin another one for their screenshots. */
export const locale = (): TLang => override ?? (appConfig().locale === 'ru' ? 'ru' : 'en');

/** Only the development galleries call this, before they render. */
export const pinLocale = (lang: TLang | null) => {
  override = lang;
};

const fill = (text: string, given?: TParams): string => {
  if (!text.includes('{')) return text;
  const params: TParams = { design: appConfig().designName, ...given };
  return text.replace(/\{(\w+)\}/g, (all, name: string) =>
    params[name] === undefined ? all : String(params[name]),
  );
};

/** The text of a key with `{name}` placeholders filled; a missing key shows itself, which is a bug to fix. */
export const t = (key: string, params?: TParams): string => {
  const entry = dictionary(locale())[key];
  if (typeof entry === 'string') return fill(entry, params);
  if (entry && typeof entry === 'object') return fill(entry.other ?? Object.values(entry)[0] ?? key, params);
  return key;
};

const rules = new Map<TLang, Intl.PluralRules>();

/** A counted text: the form the language's plural rules pick for `n`. */
export const plural = (n: number, key: string, params?: TParams): string => {
  const lang = locale();
  const entry = dictionary(lang)[key];
  if (typeof entry !== 'object' || !entry) return t(key, { n, ...params });
  let rule = rules.get(lang);
  if (!rule) {
    rule = new Intl.PluralRules(lang);
    rules.set(lang, rule);
  }
  return fill(entry[rule.select(n)] ?? entry.other ?? '', { n, ...params });
};
