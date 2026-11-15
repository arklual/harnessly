// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { outOfSight, pageVisible } from '@/next/bindings/notices';

const notices = [
  { key: 'a:1', chatId: 'tab-open' },
  { key: 'b:1', chatId: 'tab-other' },
];

afterEach(() => vi.restoreAllMocks());

describe('a notice of the chat in sight (HRN-1487)', () => {
  it('is kept away while the page is on screen, also one that came from another device', () => {
    expect(outOfSight(notices, 'tab-open', true).map((n) => n.key)).toEqual(['b:1']);
  });

  it('shows while the page is hidden, and with no chat open', () => {
    expect(outOfSight(notices, 'tab-open', false)).toHaveLength(2);
    expect(outOfSight(notices, null, true)).toHaveLength(2);
  });

  it('takes the page as on screen without the focus (a phone, a frame of the right pane)', () => {
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    vi.spyOn(document, 'hasFocus').mockReturnValue(false);
    expect(pageVisible()).toBe(true);
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden');
    expect(pageVisible()).toBe(false);
  });
});
