import { describe, expect, it } from 'vitest';
import { fieldFloat, MENTION_RE, withoutMention } from '@/next/bindings/site-stage';
import { isSpaceApp } from '@/next/features/app-views/service-health';

describe('a space calls its chat (HRN-1391)', () => {
  it('hears @harnessly at the start or after a space, not inside a word', () => {
    expect(MENTION_RE.test('@harnessly нарисуй схему')).toBe(true);
    expect(MENTION_RE.test('посмотри, @Harnessly, сюда')).toBe(true);
    expect(MENTION_RE.test('mail@harnessly.dev')).toBe(false);
    expect(MENTION_RE.test('просто текст')).toBe(false);
  });

  it('sends the request without the call', () => {
    expect(withoutMention('@harnessly, нарисуй схему')).toBe('нарисуй схему');
    expect(withoutMention('нарисуй @harnessly')).toBe('нарисуй @harnessly');
  });

  it('puts the window over the field, inside the screen, on a desktop and a phone', () => {
    for (const screen of [
      { width: 1440, height: 900 },
      { width: 390, height: 844 },
    ]) {
      const f = fieldFloat(screen);
      expect(f.x).toBeGreaterThanOrEqual(16);
      expect(f.x + f.w).toBeLessThanOrEqual(screen.width - 16 + 1);
      expect(f.y).toBeGreaterThanOrEqual(16);
      expect(f.y + f.h).toBeLessThan(screen.height);
    }
  });

  it('knows the boards and the documents as spaces, and no other app', () => {
    expect(isSpaceApp('harnessly.boards')).toBe(true);
    expect(isSpaceApp('harnessly.docs')).toBe(true);
    expect(isSpaceApp('harnessly.sinopia')).toBe(false);
  });
});
