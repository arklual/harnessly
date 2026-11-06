import { describe, expect, it } from 'vitest';
import { parseGuardDenial } from '@/next/features/app-views/guard-denial';

describe('parseGuardDenial', () => {
  it('reads the app and the rule off the head of the reason', () => {
    const text = '[app-guard "Ревью" "Bash(git push*)"] The app Ревью does not allow this tool.';
    expect(parseGuardDenial(text)).toEqual({
      app: 'Ревью',
      rule: 'Bash(git push*)',
      reason: 'The app Ревью does not allow this tool.',
    });
  });

  it('keeps quotes and brackets of the rule', () => {
    const text = `[app-guard "A \\"b\\"" "Bash(echo ])"] x`;
    expect(parseGuardDenial(text)).toMatchObject({ app: 'A "b"', rule: 'Bash(echo ])' });
  });

  it('leaves any other output alone', () => {
    expect(parseGuardDenial(undefined)).toBeNull();
    expect(parseGuardDenial('permission denied')).toBeNull();
    expect(parseGuardDenial('x [app-guard "a" "b"] y')).toBeNull();
  });
});
