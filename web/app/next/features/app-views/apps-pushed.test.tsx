// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { applyMachineEvent } from '@/next/core/realtime/apply';
import type { MachineEvent } from '@/next/protocol/v2';

const runtimeJson = vi.fn();
vi.mock('@/next/core/net/runtime-json', async (real) => ({
  ...(await real<typeof import('@/next/core/net/runtime-json')>()),
  runtimeJson: (...a: unknown[]) => runtimeJson(...a),
}));

const { useInstalledApps } = await import('./api');

const status = (appsAt: number): MachineEvent => ({
  type: 'machine_status',
  reach: 'online',
  runtime: null,
  agents: [],
  appsAt,
});

const widget = (updatedAt: number) => ({
  id: 'local.widget-render-progress',
  updatedAt,
  views: [{ id: 'card', targets: ['side'], widget: { size: 's' } }],
});

describe('the apps of a machine, pushed (HRN-1663)', () => {
  afterEach(() => runtimeJson.mockReset());

  it('a widget an agent made shows, and one it changed is a new install, with no reload', async () => {
    runtimeJson
      .mockResolvedValueOnce({ apps: [], viewPort: 1, slots: true })
      .mockResolvedValueOnce({ apps: [widget(1)], viewPort: 1, slots: true })
      .mockResolvedValueOnce({ apps: [widget(2)], viewPort: 1, slots: true })
      .mockResolvedValueOnce({ apps: [], viewPort: 1, slots: true });
    const m = 'm-pushed' as never;
    const { result } = renderHook(() => useInstalledApps(m));
    await act(async () => {});
    expect(result.current.list?.apps).toEqual([]);

    // widget_create: the machine says so on its channel.
    await act(async () => applyMachineEvent(m, status(1)));
    expect(result.current.list?.apps.map((a) => a.id)).toEqual(['local.widget-render-progress']);
    expect(result.current.list?.apps[0]?.updatedAt).toBe(1);

    // widget_update of html/css/js: the install's stamp moves, the card's frame starts anew on it.
    await act(async () => applyMachineEvent(m, status(2)));
    expect(result.current.list?.apps[0]?.updatedAt).toBe(2);

    // widget_remove.
    await act(async () => applyMachineEvent(m, status(3)));
    expect(result.current.list?.apps).toEqual([]);
  });
});
