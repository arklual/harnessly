// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { createRef, type ReactNode } from 'react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale, t } from '@/next/i18n';
import { PortalContext } from '@/next/ui/portal';
import WhatsNew from './whats-new';
import { ENTRIES } from './entries';
import { SEEN_ALL, SEEN_KEY, type IEntry } from './seen';

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
const away = () => act(() => void vi.advanceTimersByTime(400));
// Real entries, so the texts come from the dictionary built out of entries/: the newest one and one of a day before it.
const newest = ENTRIES[0] as IEntry;
const older = ENTRIES.find((entry) => entry.date < newest.date) as IEntry;
const SOME = [newest, older];
const title = (entry: IEntry) => t(`next.whatsNew.${entry.id}.title`);

describe('WhatsNew', () => {
  it('reads every entry text from the dictionary in both languages', () => {
    for (const entry of ENTRIES) {
      expect(t(`next.whatsNew.${entry.id}.title`)).not.toBe(`next.whatsNew.${entry.id}.title`);
      expect(t(`next.whatsNew.${entry.id}.text`)).not.toBe(`next.whatsNew.${entry.id}.text`);
    }
  });

  it('comes once the page is up, and never again after «Понятно»', () => {
    const { rerender } = inRoot(<WhatsNew ready={false} updating={false} entries={SOME} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();

    rerender(
      <PortalContext.Provider value={host}>
        <div className="hx-root" ref={host} />
        <WhatsNew ready updating={false} entries={SOME} />
      </PortalContext.Provider>,
    );
    settle();
    expect(screen.getByRole('dialog', { name: 'Что нового' })).toBeTruthy();
    expect(screen.getByText(title(newest))).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Понятно' }));
    expect(screen.getByRole('dialog').getAttribute('data-state')).toBe('closing');
    away();
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(JSON.parse(window.localStorage.getItem(SEEN_KEY) ?? '{}').ids).toContain(newest.id);

    cleanup();
    inRoot(<WhatsNew ready updating={false} entries={SOME} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('shows a slide per entry, the untold first with «Новое», and leafs to the right', () => {
    window.localStorage.setItem(SEEN_KEY, JSON.stringify({ upTo: older.date, ids: [] }));
    inRoot(<WhatsNew ready updating={false} entries={SOME} />);
    settle();
    const slides = screen.getByRole('dialog').querySelectorAll('.hx-news__card');
    expect(slides.length).toBe(SOME.length);
    expect(slides[0].textContent).toContain(title(newest));
    expect(slides[0].textContent).toContain('Новое');
    expect(slides[1].textContent).toContain(title(older));
    expect(slides[1].textContent).not.toContain('Новое');

    expect(screen.getByText(`1 из ${SOME.length}`)).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Назад' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole('button', { name: 'Дальше' }));
    expect(screen.getByText(`2 из ${SOME.length}`)).toBeTruthy();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'ArrowLeft' });
    expect(screen.getByText(`1 из ${SOME.length}`)).toBeTruthy();
  });

  it('keeps away from a browser that was told of everything', () => {
    window.localStorage.setItem(SEEN_KEY, SEEN_ALL);
    inRoot(<WhatsNew ready updating={false} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('takes the release id kept before entries were files', () => {
    window.localStorage.setItem(SEEN_KEY, `${newest.date}b`);
    inRoot(<WhatsNew ready updating={false} entries={SOME} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('waits while «Вышла новая версия» is up', () => {
    inRoot(<WhatsNew ready updating entries={SOME} />);
    settle();
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('goes away on Escape', () => {
    inRoot(<WhatsNew ready updating={false} entries={SOME} />);
    settle();
    fireEvent.keyDown(screen.getByRole('dialog'), { key: 'Escape' });
    away();
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
