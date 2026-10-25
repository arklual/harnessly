// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { IAttachRow, TAttachLoad } from '@/next/features/composer/attach-sources';

const source = vi.hoisted(() => ({ load: null as null | TAttachLoad }));
vi.mock('@/next/features/composer/attach-sources', async (actual) => ({
  ...(await actual<typeof import('@/next/features/composer/attach-sources')>()),
  attachSource: () => (q: string, s: AbortSignal) => (source.load as TAttachLoad)(q, s),
}));

const { default: PlusMenu } = await import('@/next/features/composer/plus-menu');
const { PICK_DEBOUNCE_MS } = await import('@/next/features/composer/attach-picker');

const phone = (on: boolean) => {
  window.matchMedia = ((query: string) => ({
    matches: on,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
};

const row = (id: string, title: string): IAttachRow => ({
  key: `design:${id}`,
  ref: { kind: 'design', id, title, target: '' },
  title,
  hint: null,
  thumb: null,
});

beforeEach(() => {
  phone(true);
  vi.useFakeTimers();
  Element.prototype.scrollIntoView = () => undefined;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const show = (onAttach = vi.fn(), attached: ReadonlySet<string> = new Set()) => {
  render(
    <PlusMenu
      contexts={[]}
      onFiles={vi.fn()}
      onContext={vi.fn()}
      onPrompts={vi.fn()}
      attach={{ scope: { machine: 'm1' as never, cwd: '/w' }, attached, onAttach }}
    />,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Add' }));
  return onAttach;
};

const settle = async () => {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(PICK_DEBOUNCE_MS + 1);
  });
};

describe('«+» places', () => {
  it('offers no places without a chat', () => {
    render(<PlusMenu contexts={[]} onFiles={vi.fn()} onContext={vi.fn()} onPrompts={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(screen.queryByRole('menuitem', { name: /Design/ })).toBeNull();
  });

  it('lists the five places in a chat', () => {
    show();
    for (const name of ['Project file', 'Design', 'Document', 'Board', 'MR / task']) {
      expect(screen.getByRole('menuitem', { name })).toBeTruthy();
    }
  });

  it('opens a picker with a loader, then its rows; Enter picks the active one after ↓', async () => {
    source.load = async () => [row('d1', 'Checkout'), row('d2', 'Landing')];
    const onAttach = show();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Design' }));
    expect(screen.getByRole('status').textContent).toContain('Loading…');
    await settle();
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual(['Checkout', 'Landing']);
    const search = screen.getByRole('searchbox', { name: 'Find a design' });
    fireEvent.keyDown(search, { key: 'ArrowDown' });
    expect(screen.getAllByRole('option')[1].getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(search, { key: 'Enter' });
    expect(onAttach).toHaveBeenCalledWith(expect.objectContaining({ key: 'design:d2' }));
  });

  it('marks what the field already holds', async () => {
    source.load = async () => [row('d1', 'Checkout')];
    show(vi.fn(), new Set(['design:d1']));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Design' }));
    await settle();
    expect(screen.getByRole('option').getAttribute('data-picked')).toBe('true');
  });

  it('says when nothing is there, and Backspace on an empty search goes back to the menu', async () => {
    source.load = async () => [];
    show();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Board' }));
    await settle();
    expect(screen.getByText('No such boards')).toBeTruthy();
    fireEvent.keyDown(screen.getByRole('searchbox'), { key: 'Backspace' });
    expect(screen.getByRole('menuitem', { name: 'Board' })).toBeTruthy();
  });

  it('offers a retry when the list fails, and the retry asks again', async () => {
    let calls = 0;
    source.load = async () => {
      calls += 1;
      if (calls === 1) throw new Error('offline');
      return [row('d1', 'Checkout')];
    };
    show();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Design' }));
    await settle();
    expect(screen.getByText('The list did not load')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await settle();
    expect(screen.getByRole('option').textContent).toBe('Checkout');
  });
});
