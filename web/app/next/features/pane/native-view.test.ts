// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { boundsOf, paneBrowser, readActed, readPaneState } from '@/next/features/pane/native-view';

describe('native pane browser view (HRN-1287)', () => {
  it('hears only its own tab', () => {
    const detail = {
      tab: 't1',
      url: 'https://example.com/',
      title: 'Ex',
      loading: false,
      canBack: true,
      canForward: false,
    };
    expect(readPaneState(detail, 't1')).toEqual({
      url: 'https://example.com/',
      title: 'Ex',
      loading: false,
      canBack: true,
      canForward: false,
    });
    expect(readPaneState(detail, 't2')).toBeNull();
    expect(readPaneState({ tab: 't1' }, 't1')).toBeNull();
    expect(readPaneState(null, 't1')).toBeNull();
  });

  it('reads the outcome of its own action', () => {
    expect(
      readActed({ id: 'a', ok: true, text: 'done', rect: { x: 1, y: 2, width: 3, height: 4 } }, 'a'),
    ).toEqual({
      id: 'a',
      ok: true,
      text: 'done',
      rect: { x: 1, y: 2, width: 3, height: 4 },
    });
    expect(readActed({ id: 'a', ok: false, text: 'no', rect: null }, 'a')?.rect).toBeNull();
    expect(readActed({ id: 'b', ok: true, text: '' }, 'a')).toBeNull();
    expect(readActed(null, 'a')).toBeNull();
  });

  it('hides the webview when the box is not on screen', () => {
    expect(boundsOf(null).visible).toBe(false);
    const el = document.createElement('div');
    document.body.appendChild(el);
    // jsdom lays nothing out: an empty box is a hidden one.
    expect(boundsOf(el).visible).toBe(false);
    el.getBoundingClientRect = () => ({ left: 10.4, top: 20.6, width: 300, height: 200 }) as DOMRect;
    expect(boundsOf(el)).toEqual({ bounds: { x: 10, y: 21, width: 300, height: 200 }, visible: true });
  });

  it('is off outside the desktop app', () => {
    expect(paneBrowser()).toBeNull();
  });
});
