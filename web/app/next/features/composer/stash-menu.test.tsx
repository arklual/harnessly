// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import PlusMenu from '@/next/features/composer/plus-menu';
import type { IStash } from '@/next/features/composer/stash-list';
import { isStashKey } from '@/next/features/composer/keys';

beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: true,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const stashOf = (over: Partial<IStash> = {}): IStash => ({
  rows: [],
  canStash: true,
  onStash: vi.fn(),
  onRestore: vi.fn(),
  onDrop: vi.fn(),
  ...over,
});

const open = (stash: IStash) => {
  render(<PlusMenu contexts={[]} onFiles={vi.fn()} onContext={vi.fn()} onPrompts={vi.fn()} stash={stash} />);
  fireEvent.click(screen.getByRole('button', { name: 'Add' }));
};

describe('stashed drafts in «+»', () => {
  it('puts the draft aside from the menu', () => {
    const stash = stashOf();
    open(stash);
    expect(screen.queryByRole('menuitem', { name: /^Stashed/ })).toBeNull();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Stash the draft' }));
    expect(stash.onStash).toHaveBeenCalledTimes(1);
  });

  it('cannot put aside an empty field', () => {
    const stash = stashOf({ canStash: false });
    open(stash);
    expect(screen.getByRole('menuitem', { name: 'Stash the draft' })).toHaveProperty('disabled', true);
  });

  it('lists the stashed drafts, brings one back and forgets another', () => {
    const stash = stashOf({
      rows: [
        { id: 'a', preview: 'First words', files: 0, at: Date.now() },
        { id: 'b', preview: '', files: 2, at: Date.now() },
      ],
    });
    open(stash);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Stashed (2)' }));
    expect(screen.getByText('First words')).toBeTruthy();
    expect(screen.getByText('Files: 2')).toBeTruthy();
    fireEvent.click(screen.getAllByRole('button', { name: 'Delete the stashed draft' })[1]);
    expect(stash.onDrop).toHaveBeenCalledWith('b');
    fireEvent.click(screen.getByText('First words'));
    expect(stash.onRestore).toHaveBeenCalledWith('a');
  });
});

describe('the stash shortcut', () => {
  const key = (over: Partial<Parameters<typeof isStashKey>[0]>) => ({
    key: 's',
    shiftKey: true,
    ctrlKey: false,
    metaKey: true,
    altKey: false,
    isComposing: false,
    ...over,
  });

  it('is ⌘⇧S or Ctrl+Shift+S', () => {
    expect(isStashKey(key({}))).toBe(true);
    expect(isStashKey(key({ metaKey: false, ctrlKey: true, key: 'S' }))).toBe(true);
    expect(isStashKey(key({ shiftKey: false }))).toBe(false);
    expect(isStashKey(key({ metaKey: false }))).toBe(false);
    expect(isStashKey(key({ altKey: true }))).toBe(false);
  });
});
