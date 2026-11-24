// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, expect, it, vi } from 'vitest';
import { useChatBackground } from '@/next/bindings/chat-background';
import { pinLocale } from '@/next/i18n';
import { IMAGE_VIEW_EXIT_MS } from '@/next/ui/image-view';

const room = vi.hoisted(() => ({ room: false, pane: false }));
vi.mock('@/next/features/background/rail-room', () => ({ useRailRoom: () => room }));
vi.mock('@/next/bindings/code-asks', () => ({ useCodeAsks: () => ({ asks: [] }) }));
vi.mock('@/next/bindings/side-widgets', () => ({ default: () => null }));
vi.mock('@/next/bindings/chat-docs', () => ({ default: () => null }));
vi.mock('@/next/stores/conversation', async (original) => {
  const shots = Array.from({ length: 6 }, (_, i) => ({ id: 's' + i, name: i + '.png', at: 500 }));
  return { ...(await original<typeof import('@/next/stores/conversation')>()), useShots: () => shots };
});

function Chat() {
  const { header, panel } = useChatBackground('tab-test', 'personal', 1000, () => {});
  return (
    <>
      <button className="hx-bg-btn" aria-expanded={header?.open} onClick={header?.onToggle}>
        Widgets
      </button>
      {panel}
    </>
  );
}

beforeAll(() => {
  pinLocale('en');
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it.each([false, true])('keeps screenshot selection open with pane open=%s', (pane) => {
  vi.useFakeTimers();
  room.pane = pane;
  const { container } = render(<Chat />);
  const toggle = screen.getByRole('button', { name: 'Widgets' });
  fireEvent.click(toggle);
  const more = screen.getByRole('button', { name: /2 more/ });
  fireEvent.pointerDown(more);
  expect(toggle.getAttribute('aria-expanded')).toBe('true');
  fireEvent.click(more);
  expect(more.getAttribute('aria-expanded')).toBe('true');
  for (const index of [5, 0]) {
    const shot = container.querySelectorAll('.hx-shots__sq')[index];
    fireEvent.pointerDown(shot);
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
    fireEvent.click(shot);
    const dialog = screen.getByRole('dialog');
    expect(dialog.querySelector('img')?.getAttribute('src')).toContain('shot?id=s' + index);
    const close = dialog.querySelector<HTMLButtonElement>('button[aria-label="Close"]')!;
    fireEvent.pointerDown(close);
    fireEvent.click(close);
    act(() => void vi.advanceTimersByTime(IMAGE_VIEW_EXIT_MS));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(toggle.getAttribute('aria-expanded')).toBe('true');
  }
  if (!pane) {
    fireEvent.pointerDown(document.body);
    expect(toggle.getAttribute('aria-expanded')).toBe('false');
  }
});
