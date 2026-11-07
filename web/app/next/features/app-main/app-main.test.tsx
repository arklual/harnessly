// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import AppMain, { type IAppMainProps } from '@/next/features/app-main/app-main';
import { PLACEMENT, type TPlacement } from '@/next/features/app-main/placement';

beforeEach(() => {
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const props = (over: Partial<IAppMainProps> = {}): IAppMainProps => ({
  title: 'Кешбэки',
  icon: 'layout-grid',
  chatTitle: 'Кешбэк на АЗС',
  chatSwitch: <button type="button">Кешбэк на АЗС</button>,
  onShare: vi.fn(),
  placement: PLACEMENT.left,
  float: null,
  onPlacement: vi.fn(),
  onFloat: vi.fn(),
  onOpenDrawer: vi.fn(),
  menu: [],
  stage: <div data-testid="stage" />,
  chat: <div data-testid="chat" />,
  island: <div data-testid="island" />,
  inspector: null,
  ...over,
});

const regions = () => ({
  col: document.querySelector('.hx-appmain__col'),
  win: document.querySelector('.hx-appmain__float'),
  island: document.querySelector('.hx-appmain__island'),
});

describe('AppMain', () => {
  it.each([
    [PLACEMENT.left, 'col'],
    [PLACEMENT.float, 'win'],
    [PLACEMENT.island, 'island'],
  ] as const)('puts the chat in one place only: %s', (placement, place) => {
    render(<AppMain {...props({ placement })} />);
    const found = regions();
    for (const [name, el] of Object.entries(found)) expect(el !== null).toBe(name === place);
    expect(screen.getByTestId('stage')).toBeTruthy();
  });

  it('shows the chat inside the column and the field inside the island', () => {
    const { unmount } = render(<AppMain {...props()} />);
    expect(regions().col?.contains(screen.getByTestId('chat'))).toBe(true);
    unmount();
    render(<AppMain {...props({ placement: PLACEMENT.island })} />);
    expect(regions().island?.contains(screen.getByTestId('island'))).toBe(true);
    expect(screen.queryByTestId('chat')).toBeNull();
  });

  it('asks for the placement the person picks and shares the app', () => {
    const onPlacement = vi.fn<IAppMainProps['onPlacement']>();
    const onShare = vi.fn();
    render(<AppMain {...props({ onPlacement, onShare })} />);
    fireEvent.click(screen.getByRole('radio', { name: /window|окне/i }));
    expect(onPlacement).toHaveBeenCalledTimes(1);
    expect(onPlacement.mock.calls[0]?.[0] as TPlacement).toBe(PLACEMENT.float);
    fireEvent.click(screen.getByRole('button', { name: /^(Share|Поделиться)$/ }));
    expect(onShare).toHaveBeenCalled();
  });

  it('shows the chat switch under the name and no way back', () => {
    render(<AppMain {...props()} />);
    const titles = document.querySelector('.hx-appmain__titles');
    expect(titles?.textContent).toContain('Кешбэки');
    expect(titles?.contains(screen.getByRole('button', { name: 'Кешбэк на АЗС' }))).toBe(true);
    expect(screen.queryByRole('button', { name: /Back to the chat|К чату/ })).toBeNull();
  });

  it('is the app alone when there is no chat: no column, no placement switch', () => {
    render(<AppMain {...props({ chat: null, island: null, chatTitle: null })} />);
    expect(regions().col).toBeNull();
    expect(screen.queryByRole('radiogroup')).toBeNull();
  });
});
