// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const LIST_MS = 800;
const runtimeJson = vi.fn();
vi.mock('@/next/core/net/runtime-json', async (real) => ({
  ...(await real<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (...a: unknown[]) => runtimeJson(...a),
}));

const { useWidgetApps } = await import('@/next/bindings/side-widgets');
const { readCachedSide } = await import('@/next/features/widgets/cache');

const apps = (n: number) =>
  Array.from({ length: n }, (_, i) => ({
    id: `local.widget-w${i}`,
    updatedAt: 1,
    views: [{ id: 'side', targets: ['side'], widget: { size: 's' } }],
  }));

// The machine answers the list after LIST_MS (it starts the view server): what the person waits for when a chat opens.
const slowList = (n: number) =>
  runtimeJson.mockImplementation(
    () =>
      new Promise((resolve) =>
        setTimeout(() => resolve({ apps: apps(n), viewPort: 7, slots: true }), LIST_MS),
      ),
  );

describe('the apps with a widget, kept in the browser (HRN-1664)', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    window.localStorage.clear();
  });
  afterEach(() => {
    vi.useRealTimers();
    runtimeJson.mockReset();
  });

  /** Virtual milliseconds from the chat opening to the stack having its apps (the cards drawn, not placeholders). */
  const firstPaintMs = async (machine: string, n: number) => {
    slowList(n);
    const { result } = renderHook(() => useWidgetApps(machine as never));
    let ms = 0;
    while (result.current === null && ms < 5_000) {
      await act(async () => {
        await vi.advanceTimersByTimeAsync(10);
      });
      ms += 10;
    }
    return ms;
  };

  it('draws the stack at once from the last list, instead of waiting for the machine', async () => {
    const cold = await firstPaintMs('m-cold', 6);
    expect(cold).toBeGreaterThanOrEqual(LIST_MS);
    // The list that came is kept; the next chat opened has it at the first render.
    expect(readCachedSide('m-cold' as never)?.apps).toHaveLength(6);
    const warm = await firstPaintMs('m-cold', 6);
    expect(warm).toBe(0);
  });

  it('keeps only the apps that have a side view, and forgets nothing it cannot read', async () => {
    runtimeJson.mockResolvedValue({
      apps: [...apps(1), { id: 'acme.docs', views: [{ id: 'doc', targets: ['pane'] }] }],
      viewPort: 7,
      slots: true,
    });
    renderHook(() => useWidgetApps('m-side' as never));
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(readCachedSide('m-side' as never)?.apps.map((a) => a.id)).toEqual(['local.widget-w0']);
    window.localStorage.setItem('hx-wg-apps:m-bad', '{not json');
    expect(readCachedSide('m-bad' as never)).toBeNull();
  });
});
