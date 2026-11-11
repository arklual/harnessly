import { describe, expect, it } from 'vitest';
import { issueKeysOf, issueLink } from '@/next/features/review/issue-keys';

const PATTERN = '(?:HRN|ABC)-\\d{1,6}';

describe('tracker tasks of a change', () => {
  it('takes the keys from the title and the commits, each once, in order', () => {
    expect(
      issueKeysOf(
        ['feat(web): review screen (HRN-1148)', 'fix: ABC-12 and HRN-1148', null, 'UTF-8 and SHA-256'],
        PATTERN,
      ),
    ).toEqual(['HRN-1148', 'ABC-12']);
  });

  it('ignores keys inside paths and words', () => {
    expect(issueKeysOf(['see docs/HRN-1.md', 'XHRN-2', 'HRN-3x'], PATTERN)).toEqual([]);
  });

  it('links a key to the tracker when there is one', () => {
    expect(issueLink('HRN-1', 'https://tracker.example.com/issue/{key}')).toBe(
      'https://tracker.example.com/issue/HRN-1',
    );
    expect(issueLink('HRN-1', null)).toBeNull();
  });
});
