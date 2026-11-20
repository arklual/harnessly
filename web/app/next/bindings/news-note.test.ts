import { beforeAll, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { essenceOf } from '@/next/bindings/news-note';
import { STATUS_LOOK } from '@/next/features/sidebar/status';

beforeAll(() => pinLocale('en'));

describe('essenceOf', () => {
  it('is the start of what the agent said', () => {
    expect(essenceOf({ text: 'Done: the page is live', summary: 'Built the page\nDeployed it' })).toBe(
      'Done: the page is live',
    );
  });

  it('is the recap, not the status word, when the answer has no words of its own', () => {
    expect(essenceOf({ text: STATUS_LOOK.done.label, summary: '\nBuilt the page\nDeployed it' })).toBe(
      'Built the page',
    );
    expect(essenceOf({ text: '', summary: 'Built the page' })).toBe('Built the page');
  });

  it('falls back to the status word when there is nothing else to say', () => {
    expect(essenceOf({ text: STATUS_LOOK.done.label, summary: null })).toBe(STATUS_LOOK.done.label);
  });
});
