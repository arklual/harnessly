// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import WhatsNew from './whats-new';
import { RELEASES, unseenRelease } from './releases';

beforeAll(() => pinLocale('ru'));
beforeEach(() => {
  vi.useFakeTimers();
  window.localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const settle = () => act(() => void vi.advanceTimersByTime(1000));
const latest = RELEASES[0].id;

describe('unseenRelease', () => {
  const releases = [
    { id: '2026-11-02', items: [] },
    { id: '2026-10-27', items: [] },
  ];
  it('tells about the newest release to someone who has not seen it', () => {
    expect(unseenRelease(releases, null)?.id).toBe('2026-11-02');
    expect(unseenRelease(releases, '2026-10-27')?.id).toBe('2026-11-02');
  });
  it('stays quiet once the newest one is seen', () => {
    expect(unseenRelease(releases, '2026-11-02')).toBeNull();
    expect(unseenRelease([], null)).toBeNull();
  });
});

describe('WhatsNew', () => {
  it('comes once the page is up, and never again after «Понятно»', () => {
    const { rerender } = inRoot(<WhatsNew ready={false} updating={false} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();

    rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <WhatsNew ready updating={false} />
      </PortalContext.Provider>,
    );
    settle();
    expect(screen.getByRole('dialog', { name: 'Что нового' })).toBeTruthy();
    expect(screen.getByText('Продолжить здесь')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Понятно' }));
    expect(screen.getByRole('dialog').getAttribute('data-state')).toBe('closing');
    act(() => void vi.advanceTimersByTime(400));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(window.localStorage.getItem('hx:whats-new')).toBe(latest);

    cleanup();
    inRoot(<WhatsNew ready updating={false} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('waits while «Вышла новая версия» is up', () => {
    inRoot(<WhatsNew ready updating />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('goes away on Escape', () => {
    inRoot(<WhatsNew ready updating={false} />);
    settle();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    act(() => void vi.advanceTimersByTime(400));
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
