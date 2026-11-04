// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { FILL_SCREEN } from '@/next/features/pane/browser-device';
import BrowserView, { type IBrowserViewProps } from '@/next/features/pane/browser-view';

beforeAll(() => {
  pinLocale('en');
  // The device menu asks whether the window is a phone's.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const props = (over: Partial<IBrowserViewProps> = {}): IBrowserViewProps => ({
  address: '',
  title: null,
  canBack: false,
  canForward: false,
  src: null,
  error: null,
  ports: [],
  portsError: null,
  onRetryPorts: () => {},
  onWantPorts: () => {},
  pins: [{ text: 'admin.example.com', name: 'Test admin' }],
  visits: ['example.org/docs'],
  onPin: () => {},
  onUnpin: () => {},
  reloadKey: 0,
  onFrameLoad: () => {},
  signIn: 'none',
  onSignIn: () => {},
  onSignInRefresh: () => {},
  theme: 'dark',
  onGo: () => {},
  onHistory: () => {},
  onReload: () => {},
  onNewTab: () => {},
  picking: null,
  onPick: () => {},
  picked: null,
  onPickSend: () => {},
  onPickCancel: () => {},
  screen: FILL_SCREEN,
  onScreen: () => {},
  outsideUrl: null,
  agent: null,
  devtools: null,
  vault: null,
  ...over,
});

const field = () => screen.getByRole('combobox');

describe('BrowserView', () => {
  it('asks for an address, a port or a search', () => {
    render(<BrowserView {...props()} />);
    expect(field().getAttribute('placeholder')).toBe('Address, port or search');
  });

  it('goes to a port, a site and a search by what is typed', () => {
    const onGo = vi.fn();
    render(<BrowserView {...props({ onGo })} />);
    fireEvent.focus(field());
    fireEvent.change(field(), { target: { value: '4400' } });
    fireEvent.keyDown(field(), { key: 'Enter' });
    expect(onGo.mock.calls[0][0].target).toEqual({ kind: 'port', port: 4400 });
    fireEvent.focus(field());
    fireEvent.change(field(), { target: { value: 'hello world' } });
    fireEvent.keyDown(field(), { key: 'Enter' });
    expect(onGo.mock.calls[1][0]).toMatchObject({
      target: { origin: 'https://html.duckduckgo.com' },
      text: '? hello world',
    });
  });

  it('suggests pins and visits under the field and goes to the one chosen with the arrows', () => {
    const onGo = vi.fn();
    render(<BrowserView {...props({ onGo })} />);
    fireEvent.focus(field());
    expect(screen.getAllByRole('option').map((o) => o.textContent)).toEqual([
      'Test adminadmin.example.com',
      'example.org/docs',
    ]);
    fireEvent.keyDown(field(), { key: 'ArrowDown' });
    fireEvent.keyDown(field(), { key: 'ArrowDown' });
    fireEvent.keyDown(field(), { key: 'Enter' });
    expect(onGo).toHaveBeenCalledWith(expect.objectContaining({ text: 'example.org/docs' }));
  });

  it('offers the search for the words as the last row', () => {
    render(<BrowserView {...props()} />);
    fireEvent.focus(field());
    fireEvent.change(field(), { target: { value: 'cats' } });
    const rows = screen.getAllByRole('option');
    expect(rows[rows.length - 1].textContent).toBe('Search for “cats”');
  });

  it('lets the list leave on Escape instead of cutting it', () => {
    render(<BrowserView {...props()} />);
    fireEvent.focus(field());
    fireEvent.keyDown(field(), { key: 'Escape' });
    expect(screen.getByRole('listbox').getAttribute('data-state')).toBe('closing');
  });

  it('lists the pinned pages on a new tab and opens one by its name', () => {
    const onGo = vi.fn();
    render(<BrowserView {...props({ onGo })} />);
    fireEvent.click(screen.getByText('Test admin'));
    expect(onGo).toHaveBeenCalledWith(
      expect.objectContaining({ text: 'admin.example.com', title: 'Test admin' }),
    );
  });

  it('unpins from the new tab and pins or unpins the page that is open', () => {
    const onUnpin = vi.fn();
    const onPin = vi.fn();
    const { rerender } = render(<BrowserView {...props({ onUnpin })} />);
    fireEvent.click(screen.getByLabelText('Unpin'));
    expect(onUnpin).toHaveBeenCalledWith('admin.example.com');
    rerender(<BrowserView {...props({ address: 'a.example.org', title: 'A', pins: [], onPin })} />);
    fireEvent.click(screen.getByLabelText('Pin this page'));
    expect(onPin).toHaveBeenCalledWith({ text: 'a.example.org', name: 'A' });
    rerender(
      <BrowserView {...props({ address: 'a.example.org', pins: [{ text: 'a.example.org' }], onUnpin })} />,
    );
    fireEvent.click(screen.getByLabelText('Unpin'));
    expect(onUnpin).toHaveBeenCalledWith('a.example.org');
  });

  it('has no pin button for a search, or before the person is known', () => {
    const { rerender } = render(<BrowserView {...props({ address: '? cats' })} />);
    expect(screen.queryByLabelText('Pin this page')).toBeNull();
    rerender(<BrowserView {...props({ address: 'a.example.org', pins: null })} />);
    expect(screen.queryByLabelText('Pin this page')).toBeNull();
  });

  it('tells the page the theme through the frame', () => {
    const { container } = render(
      <BrowserView {...props({ address: 'a.org', src: '/enter', theme: 'dark' })} />,
    );
    expect(container.querySelector('iframe')?.getAttribute('data-theme')).toBe('dark');
  });

  it('offers «Sign in» when the stand asks for it, and «Refresh» while the person signs in elsewhere', () => {
    const onSignIn = vi.fn();
    const onSignInRefresh = vi.fn();
    const { rerender } = render(<BrowserView {...props({ address: 'a.org', signIn: 'asked', onSignIn })} />);
    fireEvent.click(screen.getByText('Sign in'));
    expect(onSignIn).toHaveBeenCalled();
    rerender(<BrowserView {...props({ address: 'a.org', signIn: 'waiting', onSignInRefresh })} />);
    fireEvent.click(screen.getByText('Refresh'));
    expect(onSignInRefresh).toHaveBeenCalled();
    rerender(<BrowserView {...props({ address: 'a.org', signIn: 'none' })} />);
    expect(screen.queryByRole('group')).toBeNull();
  });
});

describe('BrowserView: the tools of a page', () => {
  it('turns picking on only once the page has a bridge, and opens the page outside', () => {
    const onPick = vi.fn();
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    const { rerender } = render(
      <BrowserView {...props({ address: 'stand.example.com', src: 'https://slot/x', onPick })} />,
    );
    const pick = screen.getByRole('button', { name: 'Pick an element and comment' });
    fireEvent.click(pick);
    expect(onPick).not.toHaveBeenCalled();
    rerender(
      <BrowserView
        {...props({
          address: 'stand.example.com',
          src: 'https://slot/x',
          onPick,
          picking: false,
          outsideUrl: 'https://stand.example.com/x',
        })}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Pick an element and comment' }));
    expect(onPick).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Open in the browser' }));
    expect(open).toHaveBeenCalledWith('https://stand.example.com/x', '_blank', 'noopener,noreferrer');
    open.mockRestore();
  });

  it('asks for a comment on the picked element and sends it', () => {
    const onPickSend = vi.fn();
    render(
      <BrowserView
        {...props({
          address: 'stand.example.com',
          src: 'https://slot/x',
          picking: false,
          onPickSend,
          picked: {
            selector: 'button.buy',
            tag: 'button',
            text: 'Buy',
            rect: { x: 0, y: 0, width: 10, height: 10 },
            html: '',
            component: null,
            url: 'https://slot/x',
          },
        })}
      />,
    );
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'bigger' } });
    fireEvent.click(screen.getByRole('button', { name: 'To the chat' }));
    expect(onPickSend).toHaveBeenCalledWith('bigger');
  });

  it('shows the page on a phone', () => {
    globalThis.ResizeObserver ??= class {
      observe() {}
      disconnect() {}
      unobserve() {}
    } as unknown as typeof ResizeObserver;
    const { container } = render(
      <BrowserView
        {...props({
          address: 'stand.example.com',
          src: 'https://slot/x',
          screen: { device: 'phone', turned: false },
        })}
      />,
    );
    expect(container.querySelector('.hx-tbrowser__stage')?.getAttribute('data-device')).toBe('phone');
  });
});
