// @vitest-environment jsdom
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useState } from 'react';
import type { MachineId } from '@/next/protocol/v2';
import type { IProjectAccess } from '@/next/features/designly/access/api';

const open = vi.fn();
// The real openPreview (it stamps the slot); only the runtime call under it is faked.
vi.mock('@/next/core/net/runtime-json', () => ({
  postJson: (body: unknown) => body,
  runtimeJson: (_m: string, _path: string, body: { slot: number; path: string }) =>
    open(body.slot, body.path),
}));
vi.mock('@/next/bindings/pane-stage', () => ({
  usePaneChatStage: () => ({ stage: null, chat: null }),
  useSiteOnlyStage: () => ({ stage: null, chat: null }),
}));
vi.mock('@/lib/app-path', () => ({
  appFetch: () =>
    Promise.resolve(
      new Response(JSON.stringify({ slot: 1, origin: 'https://panel.example', enterUrl: 'x' })),
    ),
  scopedAppPath: (p: string) => p,
  currentMachineId: () => undefined,
}));
vi.mock('@/next/stores/connection', () => ({ useLinkRestored: () => 0 }));
vi.mock('@/next/stores/tabs', () => ({
  useTabState: <T,>(_key: string, initial: T) => useState<T>(initial),
  useTabTitle: () => undefined,
}));
vi.mock('@/next/features/designly/api', async (original) => ({
  ...(await original<typeof import('@/next/features/designly/api')>()),
  keptLibrary: () => kept,
  fetchLibrary: () => Promise.resolve(shown),
}));
vi.mock('@/next/features/designly/access/api', async (original) => ({
  ...(await original<typeof import('@/next/features/designly/access/api')>()),
  fetchAccess: () => Promise.resolve(gateway),
}));
vi.mock('@/next/features/designly/site-view', () => ({
  default: (props: {
    src: string | null;
    tab: string;
    onNavigate: (page: string) => void;
    onReload: () => void;
  }) => (
    <div>
      <output data-testid="src">{props.src ?? ''}</output>
      <button onClick={() => props.onNavigate('b.html')}>b</button>
      <button onClick={() => props.onNavigate('a.html')}>a</button>
      <button onClick={props.onReload}>reload</button>
    </div>
  ),
}));

const library = {
  port: 4100,
  root: '/srv/designs',
  designs: [
    {
      id: 'd1',
      title: 'D',
      boards: [
        { path: 'a.html', title: 'A' },
        { path: 'b.html', title: 'B' },
      ],
    },
  ],
};

// What the library holds now, and what the gateway says of the project.
let shown: typeof library = library;
// What the home's last read left: a link opened from outside starts with none.
let kept: typeof library | null = library;
let gateway: IProjectAccess | null = null;

import DesignlySite from '@/next/bindings/designly-site';
import { openPreview } from '@/next/core/net/preview';
// The older panels' opener is the other writer of the slot stamp: the test crosses the interfaces on purpose.
// eslint-disable-next-line no-restricted-imports
import { openPreview as openFromPanel } from '@/lib/hl-preview-client';

const M = 'm1' as MachineId;
const ORIGIN = 'https://slot1.example';
let n = 0;

const mount = (onMissing: () => void = () => undefined) =>
  render(
    <DesignlySite
      machine={M}
      id="d1"
      maxed={false}
      onToggleMax={() => undefined}
      onOpenDrawer={() => undefined}
      onMissing={onMissing}
    />,
  );
const src = () => screen.getByTestId('src').textContent;
const click = async (name: string) => {
  await act(async () => {
    screen.getByText(name).click();
  });
};

beforeEach(() => {
  vi.useFakeTimers();
  // The wide window (the share dialog asks which frame it is in): jsdom has no matchMedia.
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  n = 0;
  shown = library;
  kept = library;
  gateway = null;
  open.mockReset();
  localStorage.clear();
  open.mockImplementation(() =>
    Promise.resolve({ slot: 1, origin: ORIGIN, enterUrl: `${ORIGIN}/enter?t=${++n}` }),
  );
});
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe('DesignlySite page switch', () => {
  it('opens the slot once for two pages', async () => {
    mount();
    await act(async () => {});
    expect(src()).toBe(`${ORIGIN}/enter?t=1`);
    await click('b');
    expect(open).toHaveBeenCalledTimes(1);
    expect(src()).toBe(`${ORIGIN}/a/d1/p/b.html`);
    await click('a');
    expect(open).toHaveBeenCalledTimes(1);
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
  });

  it('reopens the slot when another opening took it', async () => {
    mount();
    await act(async () => {});
    await act(async () => {
      await openPreview(M, { kind: 'port', port: 5200 }, 1, '/');
    });
    expect(open).toHaveBeenCalledTimes(2);
    await click('b');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(3);
    expect(open.mock.calls[2][1]).toBe('/a/d1/p/b.html');
    expect(src()).toBe(`${ORIGIN}/enter?t=3`);
  });

  it('reopens the slot when another window stamped it', async () => {
    mount();
    await act(async () => {});
    localStorage.setItem(`hl-preview-slot:1`, 'other-window');
    await click('b');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(src()).toBe(`${ORIGIN}/enter?t=2`);
  });

  it('reopens the slot when an older panel opened it', async () => {
    mount();
    await act(async () => {});
    await act(async () => {
      await openFromPanel({ kind: 'port', port: 5200, path: '/' }, 1, [], M);
    });
    await click('b');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(src()).toBe(`${ORIGIN}/enter?t=2`);
  });

  it('reopens the slot when an older panel opened it without a machine', async () => {
    mount();
    await act(async () => {});
    await act(async () => {
      await openFromPanel({ kind: 'port', port: 5200, path: '/' }, 1);
    });
    await click('b');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
  });

  it('does not touch the slot when the page does not change', async () => {
    mount();
    await act(async () => {});
    localStorage.setItem(`hl-preview-slot:1`, 'other-window');
    await click('a');
    expect(open).toHaveBeenCalledTimes(1);
  });

  it('keeps the slot when only another slot number was opened', async () => {
    mount();
    await act(async () => {});
    await act(async () => {
      await openPreview(M, { kind: 'port', port: 5200 }, 3, '/');
    });
    await click('b');
    expect(open).toHaveBeenCalledTimes(2);
    expect(src()).toBe(`${ORIGIN}/a/d1/p/b.html`);
  });

  it('reopens the slot on a switch once its cookie is about to expire', async () => {
    mount();
    await act(async () => {});
    vi.setSystemTime(Date.now() + 11 * 60 * 60 * 1000 + 1);
    await click('b');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(open.mock.calls[1][1]).toBe('/a/d1/p/b.html');
    expect(src()).toBe(`${ORIGIN}/enter?t=2`);
  });

  it('reload asks for a fresh slot', async () => {
    mount();
    await act(async () => {});
    await click('reload');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(src()).toBe(`${ORIGIN}/enter?t=2`);
  });
});

const granted: IProjectAccess = {
  role: 'view',
  owner: null,
  ownerName: 'K',
  team: 'none',
  canManage: false,
  editorsCanShare: false,
  request: null,
};

/** Lets time pass in small steps, each one rendered: a timer set by an effect only exists after its render. */
const pass = async (ms: number) => {
  for (let left = ms; left > 0; left -= 250) await act(async () => vi.advanceTimersByTimeAsync(250));
};

describe('DesignlySite project not in the library', () => {
  // The grant reaches the runtime a moment after the gateway's answer: the library is read again until it lists it.
  it('reads the library again while the gateway says the person has a role', async () => {
    shown = { ...library, designs: [] };
    gateway = granted;
    const missing = vi.fn();
    mount(missing);
    await act(async () => {});
    await pass(2000);
    expect(missing).not.toHaveBeenCalled();
    shown = library;
    await pass(4000);
    expect(missing).not.toHaveBeenCalled();
    expect(src()).toBe(`${ORIGIN}/enter?t=1`);
  });

  // the wait shows the design's frame with its loader, never an empty screen.
  it('stands on the frame while it waits', async () => {
    kept = null;
    shown = { ...library, designs: [] };
    gateway = granted;
    mount();
    await act(async () => {});
    await pass(2000);
    expect(screen.getByTestId('src').textContent).toBe('');
  });

  // after about fifteen seconds the project is somewhere the library does not reach: a card says so, with
  // a new look and a way back, instead of leaving or staying empty.
  it('says the project is away after about fifteen seconds and looks again on demand', async () => {
    kept = null;
    shown = { ...library, designs: [] };
    gateway = granted;
    const missing = vi.fn();
    mount(missing);
    await act(async () => {});
    await pass(10_000);
    expect(screen.queryByRole('alert')).toBeNull();
    await pass(10_000);
    expect(missing).not.toHaveBeenCalled();
    expect(screen.getByRole('alert').textContent).toContain('K');
    shown = library;
    await act(async () => {
      screen.getAllByRole('button')[0].click();
    });
    await pass(1000);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(src()).toBe(`${ORIGIN}/enter?t=1`);
  });

  it('leaves at once where the gateway knows no role of its own', async () => {
    shown = { ...library, designs: [] };
    gateway = null;
    const missing = vi.fn();
    mount(missing);
    await act(async () => {});
    expect(missing).toHaveBeenCalledTimes(1);
  });

  it('shows the card, not a way back, for a person without a role', async () => {
    shown = { ...library, designs: [] };
    gateway = { ...granted, role: null };
    const missing = vi.fn();
    mount(missing);
    await act(async () => {});
    await pass(20_000);
    expect(missing).not.toHaveBeenCalled();
  });
});
