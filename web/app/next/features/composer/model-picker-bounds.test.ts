// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { visibleBounds } from '@/next/features/composer/model-picker';

afterEach(() => {
  document.body.innerHTML = '';
  vi.restoreAllMocks();
});

const rect = (top: number, bottom: number) =>
  ({ top, bottom, left: 0, right: 0, width: 0, height: bottom - top }) as DOMRect;

// a screen body with its own overflow clips the popover at its top edge; the room is counted from there.
describe('visibleBounds', () => {
  it('is the window when nothing clips', () => {
    document.body.innerHTML = '<div><button id="a"></button></div>';
    const bounds = visibleBounds(document.getElementById('a'));
    expect(bounds).toEqual({ top: 0, bottom: window.innerHeight });
  });

  it('stops at an ancestor that clips', () => {
    document.body.innerHTML =
      '<div id="body" style="overflow:hidden"><div><button id="a"></button></div></div>';
    vi.spyOn(document.getElementById('body')!, 'getBoundingClientRect').mockReturnValue(rect(139, 400));
    const bounds = visibleBounds(document.getElementById('a'));
    expect(bounds).toEqual({ top: 139, bottom: 400 });
  });
});
