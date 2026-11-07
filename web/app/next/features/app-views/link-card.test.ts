import { describe, expect, it } from 'vitest';
import { linkMatches, withoutQuery } from './link-card';

describe('a link kind of an app', () => {
  it.each([
    ['https://*.bank.example/cashback/*', 'https://my.bank.example/cashback/42', true],
    ['https://*.bank.example/cashback/*', 'https://bank.example/cashback/', true],
    ['https://*.bank.example/cashback/*', 'https://evilbank.example/cashback/1', false],
    ['https://*.bank.example/cashback/*', 'https://my.bank.example/other', false],
    ['https://bank.example', 'https://bank.example/any/path', true],
    ['https://bank.example', 'https://x.bank.example/', false],
    ['https://bank.example/a', 'https://bank.example/a/b', false],
    ['https://bank.example/*', 'http://bank.example/a', false],
    ['https://bank.example/*', 'https://bank.example:8443/a', false],
    ['https://bank.example/*', 'not a url', false],
  ])('%s names %s: %s', (match, href, want) => {
    expect(linkMatches(match, href)).toBe(want);
  });
});

it('gives the card the address without its query and fragment', () => {
  expect(withoutQuery('https://bank.example/c/1?token=x#y')).toBe('https://bank.example/c/1');
});
