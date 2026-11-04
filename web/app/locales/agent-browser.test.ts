import { expect, it } from 'vitest';
import enText from './en/next-shell.json?raw';
import ruText from './ru/next-shell.json?raw';

it('keeps shell and browser translation keys identical in English and Russian', () => {
  const en = JSON.parse(enText) as Record<string, unknown>;
  const ru = JSON.parse(ruText) as Record<string, unknown>;
  expect(Object.keys(en).sort()).toEqual(Object.keys(ru).sort());
  const keys = Object.keys(en).filter(
    (key) => key.startsWith('next.agentBrowser.') || key.startsWith('next.browserProfiles.'),
  );
  expect(keys.length).toBeGreaterThan(30);
  for (const key of keys) {
    expect(typeof en[key]).toBe('string');
    expect(typeof ru[key]).toBe('string');
    expect(String(en[key]).trim()).not.toBe('');
    expect(String(ru[key]).trim()).not.toBe('');
  }
});

it('tells the person to open a new tab after agent JavaScript in both languages', () => {
  const en = JSON.parse(enText) as Record<string, string>;
  const ru = JSON.parse(ruText) as Record<string, string>;
  expect(en['next.agentBrowser.scriptedWarning']).toBe(
    'The agent ran JavaScript on this page. Do not enter a password here: open a new tab.',
  );
  expect(ru['next.agentBrowser.scriptedWarning']).toBe(
    'Агент запускал JavaScript на этой странице. Не вводите здесь пароль: откройте новую вкладку.',
  );
});
