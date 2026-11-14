// @vitest-environment jsdom
// a chat Sergey opened changed under him a moment later. The chat's view from another device came after the
// chat was open and took the page to the screen it names. A view that comes late, or after the person did anything,
// leaves the page as it is.
import { cleanup, fireEvent, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TRoute } from '@/next/routes/routes';

const goTo = vi.fn();
let applied: (() => void) | null = null;
let kept: TRoute | null = null;
vi.mock('@/next/routes/routes', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/next/routes/routes')>()),
  goTo: (...args: unknown[]) => goTo(...args),
}));
vi.mock('@/next/stores/chat-view', () => ({
  onViewApplied: (listener: () => void) => {
    applied = listener;
    return () => {
      applied = null;
    };
  },
}));
vi.mock('@/next/stores/chat-screen', () => ({
  screenOf: () => null,
  keepScreen: () => undefined,
  forgetScreen: () => undefined,
  keptScreen: () => kept,
}));

import { LATE_VIEW_MS, useChatScreen } from '@/next/bindings/use-chat-screen';
import { ROUTE } from '@/next/routes/routes';

const CHAT: TRoute = { kind: ROUTE.chat, id: 'tab-abc' };
const SCREEN = { kind: ROUTE.designly } as TRoute;

beforeEach(() => {
  vi.useFakeTimers();
  goTo.mockClear();
  kept = null;
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

const viewArrives = () => {
  kept = SCREEN;
  applied?.();
};

describe('a chat is not moved by a view that comes late (HRN-1446)', () => {
  it('a view right after the chat opened still puts its screen back', () => {
    renderHook(() => useChatScreen(CHAT));
    vi.advanceTimersByTime(200);
    viewArrives();
    expect(goTo).toHaveBeenCalledWith(SCREEN, { replace: true });
  });

  it('a view that comes once the person is in the chat leaves the page', () => {
    renderHook(() => useChatScreen(CHAT));
    vi.advanceTimersByTime(LATE_VIEW_MS + 500);
    viewArrives();
    expect(goTo).not.toHaveBeenCalled();
  });

  it('a view that comes after the person clicked or typed leaves the page', () => {
    renderHook(() => useChatScreen(CHAT));
    vi.advanceTimersByTime(100);
    fireEvent.keyDown(window, { key: 'a' });
    vi.advanceTimersByTime(100);
    viewArrives();
    expect(goTo).not.toHaveBeenCalled();
  });
});
