// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import PlusMenu from '@/next/features/composer/plus-menu';

const phone = (on: boolean) => {
  window.matchMedia = ((query: string) => ({
    matches: on,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
};

beforeEach(() => phone(true));
afterEach(cleanup);

const pickers = () => [...document.querySelectorAll<HTMLInputElement>('input[type=file]')];

describe('«+» pickers', () => {
  it('keeps one hidden file input per kind in the page, before the menu is opened', () => {
    render(<PlusMenu contexts={[]} onFiles={vi.fn()} onContext={vi.fn()} onPrompts={vi.fn()} />);
    const kinds = pickers().map((input) => input.dataset.kind);
    expect(kinds).toEqual(['photo', 'camera', 'file']);
    const [photo, camera, file] = pickers();
    expect(photo?.accept).toBe('image/*');
    expect(photo?.multiple).toBe(true);
    expect(camera?.getAttribute('capture')).toBe('environment');
    expect(camera?.multiple).toBe(false);
    expect(file?.accept).toBe('');
  });

  it('opens the picker inside the tap, and the input stays when the sheet closes', () => {
    render(<PlusMenu contexts={[]} onFiles={vi.fn()} onContext={vi.fn()} onPrompts={vi.fn()} />);
    const [photo] = pickers();
    const open = vi.spyOn(photo as HTMLInputElement, 'click');
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Photos' }));
    expect(open).toHaveBeenCalledTimes(1);
    expect(photo?.isConnected).toBe(true);
  });

  it.each([true, false])('hands the chosen files over, on a phone: %s', (on) => {
    phone(on);
    const onFiles = vi.fn();
    render(<PlusMenu contexts={[]} onFiles={onFiles} onContext={vi.fn()} onPrompts={vi.fn()} />);
    const [photo] = pickers();
    const shot = new File(['x'], 'image.jpg', { type: 'image/jpeg' });
    fireEvent.change(photo as HTMLInputElement, { target: { files: [shot] } });
    expect(onFiles).toHaveBeenCalledWith([shot]);
  });

  it('reports nothing for a choice that was cancelled', () => {
    const onFiles = vi.fn();
    render(<PlusMenu contexts={[]} onFiles={onFiles} onContext={vi.fn()} onPrompts={vi.fn()} />);
    fireEvent.change(pickers()[0] as HTMLInputElement, { target: { files: [] } });
    expect(onFiles).not.toHaveBeenCalled();
  });
});

describe('«+» → «Приложение»', () => {
  const installed = {
    apps: [
      { id: 'a.one', name: 'Кешбэки', description: 'Банк', targets: ['chat'], views: [] },
      { id: 'b.two', name: 'Ревью', description: '', targets: ['chat'], views: [] },
      { id: 'c.pane', name: 'Только панель', description: '', targets: ['pane'], views: [] },
    ],
    viewPort: 1,
  };

  beforeEach(() => {
    vi.resetModules();
  });

  it('is not offered without the apps of the chat', () => {
    render(<PlusMenu contexts={[]} onFiles={vi.fn()} onContext={vi.fn()} onPrompts={vi.fn()} />);
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    expect(screen.queryByRole('menuitem', { name: /Приложение|App/ })).toBeNull();
  });

  it('lists the apps that work in a chat, the ones on checked; a pick turns one on or off', async () => {
    vi.doMock('@/next/features/app-views/api', () => ({
      useInstalledApps: () => ({ list: installed, error: null }),
    }));
    const { default: Menu } = await import('@/next/features/composer/plus-menu');
    const onPick = vi.fn();
    render(
      <Menu
        contexts={[]}
        onFiles={vi.fn()}
        onContext={vi.fn()}
        onPrompts={vi.fn()}
        apps={{ machine: 'm1' as never, on: ['a.one'], onPick, onAll: vi.fn() }}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    fireEvent.click(screen.getByRole('menuitem', { name: /App/ }));
    // Not for a chat (c.pane) stays out; the one on (a.one) is there, checked.
    expect(screen.queryByText('Только панель')).toBeNull();
    expect(screen.getByRole('option', { name: /Кешбэки/ }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('option', { name: /Ревью/ }).getAttribute('aria-selected')).toBe('false');
    fireEvent.click(screen.getByRole('option', { name: /Ревью/ }));
    expect(onPick).toHaveBeenCalledWith('b.two', true);
  });
});
