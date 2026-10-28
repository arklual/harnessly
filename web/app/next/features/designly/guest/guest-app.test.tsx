// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import GuestApp from '@/next/features/designly/guest/guest-app';
import type { IGuestConfig } from '@/next/features/designly/guest/guest-config';

class FakeObserver {
  observe() {}
  disconnect() {}
}

const config: IGuestConfig = {
  v: 1,
  token: 'guest-token',
  title: 'Example design',
  rights: 'comment',
  expiresAt: null,
  view: null,
  boards: [{ id: 'main', title: 'Main', path: 'lib/my page.html?Name=a b', w: 800, h: 600 }],
  filesBase: '/s/guest-token/f/',
  commentsUrl: '',
  kind: null,
  deck: null,
  mediaBase: './m/',
  seenUrl: './seen',
};

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', FakeObserver);
  vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
    cb(0);
    return 1;
  });
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }));
  HTMLElement.prototype.scrollTo = vi.fn();
});

afterEach(() => {
  cleanup();
  localStorage.clear();
  vi.unstubAllGlobals();
});

describe('the guest design screen', () => {
  it('opens the current board cleanly in a new tab and keeps commenting available', () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    render(<GuestApp config={config} />);

    expect(screen.getByRole('button', { name: 'Comment' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'More' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Open in the browser' }));

    expect(open).toHaveBeenCalledWith('/s/guest-token/f/lib/my%20page.html?Name=a b', '_blank', 'noopener');
  });

  it('does not offer opening outside where the shared design has no board URL', () => {
    const open = vi.spyOn(window, 'open').mockImplementation(() => null);
    render(<GuestApp config={{ ...config, boards: [] }} />);

    fireEvent.click(screen.getByRole('button', { name: 'More' }));
    expect(screen.queryByRole('menuitem', { name: 'Open in the browser' })).toBeNull();
    expect(open).not.toHaveBeenCalled();
  });

  it('opens the design over the whole screen and leaves it by Esc or F', () => {
    const { container } = render(<GuestApp config={config} />);
    const root = container.querySelector('.hx-guest');

    fireEvent.click(screen.getByRole('button', { name: 'Full screen' }));
    expect(root?.getAttribute('data-full')).toBe('on');
    expect(container.querySelector('.hx-site')?.getAttribute('data-full')).toBe('true');
    expect(screen.getByRole('button', { name: /Exit/ })).toBeTruthy();

    fireEvent.keyDown(window, { key: 'Escape', code: 'Escape' });
    expect(root?.getAttribute('data-full')).toBe('left');
    expect(container.querySelector('.hx-site')?.getAttribute('data-full')).toBe('false');

    fireEvent.keyDown(window, { key: 'f', code: 'KeyF' });
    expect(root?.getAttribute('data-full')).toBe('on');
    fireEvent.keyDown(window, { key: 'а', code: 'KeyF' });
    expect(root?.getAttribute('data-full')).toBe('left');
  });

  it('leaves the keys to a field the guest types in', () => {
    const { container } = render(<GuestApp config={config} />);
    const field = document.createElement('input');
    document.body.appendChild(field);
    fireEvent.keyDown(field, { key: 'f', code: 'KeyF' });
    expect(container.querySelector('.hx-guest')?.getAttribute('data-full')).toBe('off');
    field.remove();
  });
});
