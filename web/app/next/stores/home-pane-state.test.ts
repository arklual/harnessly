// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import {
  EMPTY_HOME_PANE,
  HOME_TABS_MAX,
  homePaneOf,
  parseHomePane,
  resetHomePane,
  serializeHomePane,
  updateHomePane,
  type IHomePane,
} from '@/next/stores/home-pane-state';

afterEach(() => resetHomePane());

const PANE: IHomePane = {
  open: true,
  maxed: false,
  width: 520,
  tabs: [
    { id: 'r', kind: 'review' },
    {
      id: 'c',
      kind: 'change',
      url: 'https://gitlab.example.com/team/app/-/merge_requests/5',
      title: 'app !5',
      name: 'Queue',
      source: 'feat',
      target: 'main',
    },
  ],
  active: 'c',
  machine: null,
};

describe('home pane state', () => {
  it('reads back what it wrote', () => {
    expect(parseHomePane(serializeHomePane(PANE))).toEqual(PANE);
  });

  it('reads anything else as the empty pane', () => {
    expect(parseHomePane(null)).toBe(EMPTY_HOME_PANE);
    expect(parseHomePane('nope')).toBe(EMPTY_HOME_PANE);
    expect(parseHomePane('{"v":99,"pane":{}}')).toBe(EMPTY_HOME_PANE);
  });

  it('drops tabs it does not know and an active tab that is gone', () => {
    const raw = JSON.stringify({
      v: 1,
      pane: {
        open: true,
        tabs: [
          { id: 'x', kind: 'shell' },
          { id: 'c', kind: 'change', url: 'javascript:alert(1)' },
          { id: 'r', kind: 'review' },
        ],
        active: 'c',
      },
    });
    expect(parseHomePane(raw)).toEqual({
      ...EMPTY_HOME_PANE,
      open: true,
      tabs: [{ id: 'r', kind: 'review' }],
    });
  });

  it('keeps the newest tabs only', () => {
    const tabs = Array.from({ length: HOME_TABS_MAX + 4 }, (_, i) => ({
      id: `r${i}`,
      kind: 'review' as const,
    }));
    expect(parseHomePane(serializeHomePane({ ...PANE, tabs, active: '' })).tabs).toHaveLength(HOME_TABS_MAX);
  });

  it('writes a change a moment later, and once for the same state', () => {
    vi.useFakeTimers();
    const write = vi.spyOn(Storage.prototype, 'setItem');
    updateHomePane((s) => ({ ...s, open: true }));
    updateHomePane((s) => s);
    expect(homePaneOf().open).toBe(true);
    vi.runAllTimers();
    expect(write).toHaveBeenCalledTimes(1);
    write.mockRestore();
    vi.useRealTimers();
  });
});
