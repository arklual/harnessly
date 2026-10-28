// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import ReleaseNotice from './release-notice';
import { readDismissed, readRelease, UPDATE_GUIDE_URL, writeDismissed } from './release';

beforeAll(() => pinLocale('ru'));
beforeEach(() => {
  vi.useFakeTimers();
  window.localStorage.clear();
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

const host = createRef<HTMLDivElement>();
const inRoot = (ui: ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const RELEASE = { version: '0.16.0', url: 'https://github.com/o/r/releases/tag/v0.16.0' };

describe('readRelease', () => {
  it('reads a newer release', () => {
    expect(
      readRelease({ current: '0.15.0', latest: '0.16.0', url: RELEASE.url, updateAvailable: true }),
    ).toEqual(RELEASE);
  });
  it('says nothing when there is no update or the answer is odd', () => {
    expect(readRelease({ current: '0.16.0', latest: '0.16.0', updateAvailable: false })).toBeNull();
    expect(readRelease({ updateAvailable: true })).toBeNull();
    expect(readRelease(null)).toBeNull();
    expect(readRelease('x')).toBeNull();
  });
  it('drops a link that is not https', () => {
    expect(readRelease({ latest: '0.16.0', url: 'javascript:alert(1)', updateAvailable: true })?.url).toBe(
      '',
    );
  });
});

describe('dismissed version', () => {
  it('is remembered, and storage that throws is not fatal', () => {
    expect(readDismissed()).toBeNull();
    writeDismissed('0.16.0');
    expect(readDismissed()).toBe('0.16.0');
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    expect(readDismissed()).toBeNull();
    expect(() => writeDismissed('0.17.0')).not.toThrow();
  });
});

describe('ReleaseNotice', () => {
  it('shows the version with the links, and is silent without a release', () => {
    const { rerender } = inRoot(<ReleaseNotice release={null} onDismiss={() => undefined} />);
    expect(screen.queryByRole('status')).toBeNull();
    rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <ReleaseNotice release={RELEASE} onDismiss={() => undefined} />
      </PortalContext.Provider>,
    );
    expect(screen.getByRole('status').textContent).toContain('Доступна версия 0.16.0');
    expect(screen.getByText('Что нового').getAttribute('href')).toBe(RELEASE.url);
    expect(screen.getByText('Как обновить').getAttribute('href')).toBe(UPDATE_GUIDE_URL);
  });

  it('puts itself away through the close button', () => {
    const onDismiss = vi.fn();
    // The host is set on the first commit; the notice shows on the next render, as in the app.
    const { rerender } = inRoot(<ReleaseNotice release={RELEASE} onDismiss={onDismiss} />);
    rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <ReleaseNotice release={RELEASE} onDismiss={onDismiss} />
      </PortalContext.Provider>,
    );
    act(() => void vi.advanceTimersByTime(10));
    fireEvent.click(screen.getByRole('button', { name: 'Скрыть' }));
    expect(onDismiss).toHaveBeenCalledOnce();
  });
});
