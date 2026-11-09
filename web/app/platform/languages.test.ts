import { readdirSync, readFileSync } from 'node:fs';
import { expect, it } from 'vitest';
import { ALL_LANGUAGES, EXTRA_LANGUAGES, LANGUAGE_NAMES, offeredLanguages, parseLanguage } from './languages';

it('offers Russian and English only until the languages feature is on', () => {
  expect(offeredLanguages(false)).toEqual(['ru', 'en']);
  expect(offeredLanguages(true)).toHaveLength(11);
  expect(parseLanguage('ko', false)).toBeNull();
  expect(parseLanguage('ko', true)).toBe('ko');
  expect(parseLanguage('pt-BR', true)).toBe('pt-BR');
  expect(parseLanguage('xx', true)).toBeNull();
  expect(parseLanguage(null, true)).toBeNull();
});

it('names every language and has a dictionary folder for each extra one', () => {
  const dirs = readdirSync(new URL('../locales/', import.meta.url));
  for (const lang of ALL_LANGUAGES) expect(LANGUAGE_NAMES[lang]).toBeTruthy();
  for (const lang of EXTRA_LANGUAGES) {
    expect(dirs).toContain(lang);
    const file = new URL(`../locales/${lang}/next-settings.json`, import.meta.url);
    const words = JSON.parse(readFileSync(file, 'utf8')) as Record<string, string>;
    expect(words['next.settings.general.language']).toBeTruthy();
  }
});

it('serves a PWA manifest with raster icons, a maskable one among them', () => {
  const manifest = JSON.parse(
    readFileSync(new URL('../../public/manifest.json', import.meta.url), 'utf8'),
  ) as { icons: { type: string; purpose: string }[] };
  expect(manifest.icons.some((i) => i.type === 'image/png' && i.purpose === 'maskable')).toBe(true);
  expect(manifest.icons.some((i) => i.type === 'image/png' && i.purpose === 'any')).toBe(true);
});
