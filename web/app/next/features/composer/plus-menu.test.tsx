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
