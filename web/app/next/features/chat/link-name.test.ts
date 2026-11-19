import { describe, expect, it } from 'vitest';
import { isBareAddress, slugWords } from '@/next/features/chat/link-name';

describe('slugWords (HRN-1652)', () => {
  it('drops the 6-character id at the tail and turns dashes into spaces', () => {
    expect(slugWords('eval-laboratoriya-proektnaya-doka-uuwhtz')).toBe('eval laboratoriya proektnaya doka');
  });

  it('keeps a slug that is only an id, and a slug without one', () => {
    expect(slugWords('uuwhtz')).toBe('uuwhtz');
    expect(slugWords('plan')).toBe('plan');
  });

  it('reads an encoded slug', () => {
    expect(slugWords('%D0%BF%D0%BB%D0%B0%D0%BD-abc123')).toBe('план');
    expect(slugWords('%E0%A4%A')).toBe('%E0%A4%A');
  });
});

describe('isBareAddress (HRN-1652)', () => {
  const href = 'https://a.example.org/a/plan-abc123';

  it('is an autolink or an empty text, not a text of the author’s', () => {
    expect(isBareAddress(href, href)).toBe(true);
    expect(isBareAddress(`${href}/`, href)).toBe(true);
    expect(isBareAddress('', href)).toBe(true);
    expect(isBareAddress('План', href)).toBe(false);
  });
});
