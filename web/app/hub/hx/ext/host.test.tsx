// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ViewBoundary, ViewFrame } from '../screens/extensions';
import { accentOf, forgetHostUI, fromFrame, loadHostUI, safeViews } from './host';
import { hxSectionsOf, navOf } from '../nav';

const view = {
  ext: 'corp',
  id: 'main',
  label: 'Корп',
  url: 'https://example.com/v',
  origin: 'https://example.com',
};

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  forgetHostUI();
});

describe('the frame check', () => {
  it('believes only the frame itself and the origin "null"', () => {
    const win = {} as Window;
    expect(fromFrame({ source: win, origin: 'null' }, win)).toBe(true);
    expect(fromFrame({ source: {}, origin: 'null' }, win)).toBe(false);
    expect(fromFrame({ source: win, origin: 'https://evil.example.com' }, win)).toBe(false);
    expect(fromFrame({ source: win, origin: 'null' }, null)).toBe(false);
  });
  it('takes an accent only as #rrggbb and views only on https', () => {
    expect(accentOf({ accent: '#12abEF' })).toBe('#12abEF');
    expect(accentOf({ accent: 'red; background:url(x)' })).toBeNull();
    expect(
      safeViews([view, { ...view, url: 'http://example.com' }, { ...view, url: 'javascript:1' }, 5, null]),
    ).toEqual([view]);
  });
});

describe('the answer of the hub', () => {
  it('is nothing while the flag is off or the hub fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response('', { status: 404 })),
    );
    expect(await loadHostUI()).toEqual({ views: [] });
    forgetHostUI();
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => Promise.reject(new Error('down'))),
    );
    expect(await loadHostUI()).toEqual({ views: [] });
  });
  it('adds the section only when there are views, and not for a member', () => {
    expect(hxSectionsOf('admin', [])).not.toContain('extensions');
    expect(hxSectionsOf('admin', [], true)).toContain('extensions');
    expect(hxSectionsOf('member', [], true)).not.toContain('extensions');
    const ids = (v: boolean) =>
      navOf('admin', null, [], 'tenant', v).flatMap((g) => g.items.map((i) => i.id));
    expect(ids(false)).not.toContain('extensions');
    expect(ids(true)).toContain('extensions');
  });
});

describe('a view', () => {
  beforeEach(() => vi.useFakeTimers());
  it('is sandboxed without allow-same-origin', () => {
    render(<ViewFrame view={view} theme="light" />);
    const f = screen.getByTitle('Корп');
    expect(f.getAttribute('sandbox')).toBe('allow-scripts');
  });
  it('that stays silent becomes a row with a retry, not a broken page', () => {
    render(<ViewFrame view={view} theme="dark" />);
    act(() => void vi.advanceTimersByTime(9000));
    expect(screen.getByRole('alert').textContent).toContain('не отвечает');
    fireEvent.click(screen.getByText('Повторить'));
    expect(screen.getByTitle('Корп')).toBeTruthy();
  });
  it('ignores a message from another window', () => {
    render(<ViewFrame view={view} theme="dark" />);
    act(
      () =>
        void window.dispatchEvent(
          new MessageEvent('message', { data: { type: 'harnessly.ext.error' }, origin: 'null' }),
        ),
    );
    expect(screen.queryByRole('alert')).toBeNull();
  });
  it('that throws while drawing stays in its own block', () => {
    const Bad = () => {
      throw new Error('boom');
    };
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    render(
      <>
        <ViewBoundary label="Плохое">
          <Bad />
        </ViewBoundary>
        <p>страница жива</p>
      </>,
    );
    expect(screen.getByText('страница жива')).toBeTruthy();
    expect(screen.getByRole('alert').textContent).toContain('Плохое');
  });
});
