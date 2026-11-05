// @vitest-environment jsdom
// «Файлы» on a phone: a finger held on a row picks it (and the click that follows does not open it); a quick tap opens;
// a finger that scrolls does not pick.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import FilesView from '@/next/features/pane/files-view';
import type { DirListing } from '@/next/protocol/v2';

beforeAll(() => {
  pinLocale('en');
  // jsdom does not lay out: the list of «Go to file» scrolls its row into view.
  Element.prototype.scrollIntoView = () => undefined;
});
beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const LISTING = {
  path: '',
  truncated: false,
  entries: [
    { name: 'src', path: 'src', kind: 'directory' },
    { name: 'a.txt', path: 'a.txt', kind: 'file' },
  ],
} as unknown as DirListing;

const view = (onOpen = vi.fn()) => {
  render(
    <FilesView
      root="/work/p"
      path=""
      listing={LISTING}
      file={null}
      editor={null}
      error={null}
      opening="folder"
      onRetry={() => undefined}
      onOpen={onOpen}
      onDownload={() => undefined}
    />,
  );
  return onOpen;
};

const row = (name: string) => screen.getByText(name).closest('button') as HTMLButtonElement;
const touch = (el: Element, type: 'pointerDown' | 'pointerMove' | 'pointerUp', x = 10, y = 10) =>
  fireEvent[type](el, { pointerType: 'touch', clientX: x, clientY: y });

describe('picking by a held finger', () => {
  it('picks the held row and does not open it', () => {
    const onOpen = view();
    touch(row('a.txt'), 'pointerDown');
    act(() => vi.advanceTimersByTime(500));
    touch(row('a.txt'), 'pointerUp');
    fireEvent.click(row('a.txt'));
    expect(onOpen).not.toHaveBeenCalled();
    expect(row('a.txt').getAttribute('aria-checked')).toBe('true');
    expect(row('src').getAttribute('aria-checked')).toBe('false');
  });

  it('opens on a quick tap, and a scrolling finger picks nothing', () => {
    const onOpen = view();
    touch(row('src'), 'pointerDown');
    touch(row('src'), 'pointerMove', 10, 40);
    act(() => vi.advanceTimersByTime(500));
    expect(row('src').getAttribute('aria-checked')).toBeNull();
    touch(row('a.txt'), 'pointerDown');
    act(() => vi.advanceTimersByTime(100));
    touch(row('a.txt'), 'pointerUp');
    fireEvent.click(row('a.txt'));
    expect(onOpen).toHaveBeenCalledWith('a.txt', 'file');
  });
});

const viewAt = (path: string, listing: DirListing, more: Partial<Parameters<typeof FilesView>[0]> = {}) => {
  const onOpen = vi.fn();
  const onRetry = vi.fn();
  render(
    <FilesView
      root="/work/p/"
      path={path}
      listing={listing}
      file={null}
      editor={null}
      error={null}
      opening="folder"
      onRetry={onRetry}
      onOpen={onOpen}
      onDownload={() => undefined}
      {...more}
    />,
  );
  return { onOpen, onRetry };
};

describe('«Машина»: the same list over the workspace (HRN-679)', () => {
  const deep = {
    path: '/home/me/work/x',
    root: '/home/me',
    truncated: false,
    entries: [{ name: 'y.txt', path: '/home/me/work/x/y.txt', kind: 'file' }],
  } as unknown as DirListing;

  it('leads the crumbs from «Machine» through the place the folder lies in', () => {
    const { onOpen } = viewAt('/home/me/work/x', deep);
    const crumbs = [...document.querySelectorAll('.hx-disk__crumb')].map((c) => c.textContent);
    expect(crumbs).toEqual(['Machine', '/home/me', 'work', 'x']);
    fireEvent.click(screen.getByText('/home/me'));
    expect(onOpen).toHaveBeenLastCalledWith('/home/me', 'directory');
    fireEvent.click(screen.getByText('work'));
    expect(onOpen).toHaveBeenLastCalledWith('/home/me/work', 'directory');
  });

  it('turns the chat’s folder into the machine’s and back', () => {
    const { onOpen } = viewAt('src', LISTING);
    fireEvent.click(screen.getByRole('radio', { name: 'Machine' }));
    expect(onOpen).toHaveBeenLastCalledWith('/work/p/src', 'directory');
    cleanup();
    const back = viewAt('/work/p/src', deep).onOpen;
    fireEvent.click(screen.getByRole('radio', { name: 'Project' }));
    expect(back).toHaveBeenLastCalledWith('src', 'directory');
    cleanup();
    const away = viewAt('/home/me/work/x', deep).onOpen;
    fireEvent.click(screen.getByRole('radio', { name: 'Project' }));
    expect(away).toHaveBeenLastCalledWith('', 'directory');
  });

  it('makes a new folder where it may, and says why not', async () => {
    vi.useRealTimers();
    const makeFolder = vi.fn().mockResolvedValueOnce('Name taken').mockResolvedValueOnce(null);
    const { onRetry } = viewAt('', LISTING, { take: { fromInput: vi.fn(), fromDrop: vi.fn(), makeFolder } });
    fireEvent.click(screen.getByRole('button', { name: 'New folder' }));
    const name = screen.getByRole('textbox', { name: 'Folder name' });
    fireEvent.change(name, { target: { value: 'docs' } });
    fireEvent.submit(name.closest('form') as HTMLFormElement);
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Name taken');
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await vi.waitFor(() => expect(onRetry).toHaveBeenCalled());
    expect(makeFolder).toHaveBeenLastCalledWith('docs');
    expect(screen.queryByRole('textbox', { name: 'Folder name' })).toBeNull();
  });

  it('offers no upload or new folder outside the places', () => {
    viewAt('', LISTING);
    expect(screen.queryByRole('button', { name: 'New folder' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Upload files' })).toBeNull();
  });
});

describe('«Go to file» (⌘P)', () => {
  it('opens on the shortcut, finds as typed, opens the picked file', async () => {
    vi.useRealTimers();
    const find = vi.fn(async (query: string) => (query === '' ? [] : ['src/a.ts', 'b.ts']));
    const { onOpen } = viewAt('', LISTING, { find });
    fireEvent.keyDown(window, { key: 'p', code: 'KeyP', ctrlKey: true });
    const field = await screen.findByRole('combobox', { name: 'Go to file' });
    fireEvent.change(field, { target: { value: 'a' } });
    await screen.findByText('b.ts');
    fireEvent.keyDown(field, { key: 'ArrowDown' });
    fireEvent.keyDown(field, { key: 'Enter' });
    expect(onOpen).toHaveBeenLastCalledWith('b.ts', 'file');
    expect(screen.queryByRole('combobox')).toBeNull();
  });

  it('leaves on Esc', async () => {
    vi.useRealTimers();
    viewAt('', LISTING, { find: async () => [] });
    fireEvent.click(screen.getByRole('button', { name: 'Go to file' }));
    const field = await screen.findByRole('combobox', { name: 'Go to file' });
    fireEvent.keyDown(field, { key: 'Escape' });
    expect(screen.queryByRole('combobox')).toBeNull();
  });
});

describe('«Find in files» (⇧⌘F)', () => {
  it('finds lines as typed, groups them by file and opens the picked one at its place', async () => {
    vi.useRealTimers();
    const findText = vi.fn(async () => ({
      hits: [
        { path: 'src/a.ts', line: 3, col: 7, text: 'const needle = 1;' },
        { path: 'src/a.ts', line: 9, col: 1, text: 'needle()' },
        { path: 'b.md', line: 1, col: 4, text: 'a needle' },
      ],
      truncated: false,
    }));
    const onOpenAt = vi.fn();
    viewAt('', LISTING, { findText, onOpenAt });
    fireEvent.keyDown(window, { key: 'F', code: 'KeyF', ctrlKey: true, shiftKey: true });
    const field = await screen.findByRole('textbox', { name: 'Find in files' });
    fireEvent.change(field, { target: { value: 'needle' } });
    expect(await screen.findByText('Matches: 3, files: 2')).toBeTruthy();
    expect(findText).toHaveBeenCalledWith('needle', expect.any(AbortSignal));
    expect(document.querySelectorAll('.hx-fif__file')).toHaveLength(2);
    fireEvent.keyDown(field, { key: 'ArrowDown' });
    fireEvent.keyDown(field, { key: 'Enter' });
    expect(onOpenAt).toHaveBeenCalledWith({ path: 'src/a.ts', line: 9, col: 1, text: 'needle()' }, 6);
    expect(screen.queryByRole('dialog', { name: 'Find in files' })).toBeNull();
    // It comes back with what was found.
    fireEvent.click(screen.getByRole('button', { name: 'Find in files' }));
    expect(await screen.findByText('Matches: 3, files: 2')).toBeTruthy();
  });
});

describe('git’s marks', () => {
  it('colours a changed or new entry and says what it is', () => {
    const marked = {
      path: '',
      truncated: false,
      entries: [
        { name: 'src', path: 'src', kind: 'directory', git: 'changed' },
        { name: 'new.ts', path: 'new.ts', kind: 'file', git: 'new' },
        { name: 'same.ts', path: 'same.ts', kind: 'file' },
      ],
    } as unknown as DirListing;
    viewAt('', marked);
    expect(screen.getByRole('img', { name: 'Has changes inside, not committed' })).toBeTruthy();
    expect(screen.getByRole('img', { name: 'New — not in git yet' })).toBeTruthy();
    expect(screen.getAllByRole('img')).toHaveLength(2);
  });
});

describe('new file, rename, trash', () => {
  const menuOf: Parameters<typeof FilesView>[0]['menuOf'] = (path, _kind, _trigger, acts) => (
    <span>
      <button type="button" onClick={acts?.onRename}>{`rename ${path}`}</button>
      <button type="button" onClick={acts?.onTrash}>{`trash ${path}`}</button>
    </span>
  );
  const ops = () => ({
    newFile: vi.fn().mockResolvedValue('notes.md'),
    rename: vi.fn().mockRejectedValueOnce(new Error('Name taken')).mockResolvedValue('b.txt'),
    trash: vi.fn().mockResolvedValue('a.txt'),
  });

  it('makes an empty file and opens it', async () => {
    vi.useRealTimers();
    const entryOps = ops();
    const { onOpen } = viewAt('', LISTING, { entryOps, menuOf });
    fireEvent.click(screen.getByRole('button', { name: 'New file' }));
    const name = screen.getByRole('textbox', { name: 'File name' });
    fireEvent.change(name, { target: { value: 'notes.md' } });
    fireEvent.submit(name.closest('form') as HTMLFormElement);
    await vi.waitFor(() => expect(onOpen).toHaveBeenLastCalledWith('notes.md', 'file'));
    expect(entryOps.newFile).toHaveBeenCalledWith('notes.md');
  });

  it('renames in place and says why not', async () => {
    vi.useRealTimers();
    const entryOps = ops();
    const { onRetry } = viewAt('', LISTING, { entryOps, menuOf });
    fireEvent.click(screen.getByText('rename a.txt'));
    const name = screen.getByRole('textbox', { name: 'New name' }) as HTMLInputElement;
    expect(name.value).toBe('a.txt');
    fireEvent.change(name, { target: { value: 'b.txt' } });
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }));
    expect(await screen.findByRole('alert')).toHaveProperty('textContent', 'Name taken');
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }));
    await vi.waitFor(() => expect(onRetry).toHaveBeenCalled());
    expect(entryOps.rename).toHaveBeenLastCalledWith('a.txt', 'b.txt');
    expect(screen.queryByRole('textbox', { name: 'New name' })).toBeNull();
  });

  it('asks before the trash, and Cancel leaves the file', async () => {
    vi.useRealTimers();
    const entryOps = ops();
    const { onRetry } = viewAt('', LISTING, { entryOps, menuOf });
    fireEvent.click(screen.getByText('trash a.txt'));
    expect(screen.getByRole('alertdialog', { name: 'Move “a.txt” to the trash?' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(entryOps.trash).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('trash a.txt'));
    fireEvent.click(screen.getByRole('button', { name: 'Move to trash' }));
    await vi.waitFor(() => expect(onRetry).toHaveBeenCalled());
    expect(entryOps.trash).toHaveBeenCalledWith('a.txt');
  });

  it('offers none of it without the ops', () => {
    viewAt('', LISTING, { menuOf });
    expect(screen.queryByRole('button', { name: 'New file' })).toBeNull();
  });
});

describe('a tab of its own', () => {
  it('opens a row beside on ⌘-click and the middle button, in place on a plain click', () => {
    const onOpenBeside = vi.fn();
    const { onOpen } = viewAt('', LISTING, { onOpenBeside });
    fireEvent.click(row('a.txt'), { metaKey: true });
    expect(onOpenBeside).toHaveBeenLastCalledWith('a.txt', 'file');
    fireEvent(row('src'), new MouseEvent('auxclick', { bubbles: true, button: 1 }));
    expect(onOpenBeside).toHaveBeenLastCalledWith('src', 'directory');
    expect(onOpen).not.toHaveBeenCalled();
    fireEvent.click(row('a.txt'));
    expect(onOpen).toHaveBeenLastCalledWith('a.txt', 'file');
  });
});
