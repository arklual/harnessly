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
  chatTitle: 'Кешбэк на АЗС',
  placement: PLACEMENT.left,
  float: null,
  onPlacement: vi.fn(),
  onFloat: vi.fn(),
  onBack: vi.fn(),
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

  it('asks for the placement the person picks and goes back to the chat', () => {
    const onPlacement = vi.fn<IAppMainProps['onPlacement']>();
    const onBack = vi.fn();
    render(<AppMain {...props({ onPlacement, onBack })} />);
    fireEvent.click(screen.getByRole('radio', { name: /window|окне/i }));
    expect(onPlacement).toHaveBeenCalledTimes(1);
    expect(onPlacement.mock.calls[0]?.[0] as TPlacement).toBe(PLACEMENT.float);
    fireEvent.click(screen.getByRole('button', { name: /Back to the chat|К чату/ }));
    expect(onBack).toHaveBeenCalled();
  });

  it('is the app alone when there is no chat: no column, no switch, no way back', () => {
    render(<AppMain {...props({ chat: null, island: null, chatTitle: null, onBack: null })} />);
    expect(regions().col).toBeNull();
    expect(screen.queryByRole('radiogroup')).toBeNull();
    expect(screen.queryByRole('button', { name: /Back to the chat|К чату/ })).toBeNull();
  });
});
