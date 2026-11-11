// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
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
vi.mock('@/next/stores/connection', () => ({ useLinkRestored: () => 0, onReconnectNow: () => undefined }));
// The kit link is read once per project; these tests count the page's own requests.
vi.mock('@/next/features/designly/kit/use-kit-link', () => ({
  useKitLink: () => null,
  boardNode: () => null,
}));
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
// A project without a kit: the kit's band asks nothing of the runtime the slot calls are counted on.
vi.mock('@/next/features/designly/versions/versions-api', async (original) => ({
  ...(await original<typeof import('@/next/features/designly/versions/versions-api')>()),
  fetchProjectKit: () => Promise.resolve({ pin: null, update: null }),
}));
vi.mock('@/next/features/designly/site-view', () => ({
  default: (props: {
    src: string | null;
    tab: string;
    onNavigate: (page: string) => void;
    onReload: () => void;
    mode?: { editing: boolean; action: { onClick: () => void } | null };
  }) => (
    <div>
      <output data-testid="mode">{props.mode ? String(props.mode.editing) : ''}</output>
      <button onClick={() => props.mode?.action?.onClick()}>switch</button>
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
// The frame that spends the ticket (never seen); the page's own frame waits for it.
const enterSrc = () => document.querySelector('.hx-kit-enter')?.getAttribute('src') ?? null;
const enterFrame = async () => {
  await act(async () => {
    const frame = document.querySelector('.hx-kit-enter');
    if (frame) fireEvent.load(frame);
  });
};
// The slot gateway's 401 page in a frame tells the page above (hlViewError, deploy preview-gateway frameTell).
const refusedBySlot = async (origin: string) => {
  await act(async () => {
    window.dispatchEvent(
      new MessageEvent('message', { origin, data: { hlViewError: { kind: 'refused', status: 401 } } }),
    );
  });
};
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
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=1`);
    expect(src()).toBe('');
    await enterFrame();
    expect(enterSrc()).toBeNull();
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    await click('b');
    expect(open).toHaveBeenCalledTimes(1);
    expect(src()).toBe(`${ORIGIN}/a/d1/p/b.html`);
    await click('a');
    expect(open).toHaveBeenCalledTimes(1);
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
  });

  it('shows the page when the entering frame never loads', async () => {
    mount();
    await act(async () => {});
    expect(src()).toBe('');
    // A 204 fires no load in a browser: the page must not stay a skeleton for it.
    await act(async () => {
      vi.advanceTimersByTime(7000);
    });
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    // The entering frame is not taken away by the wait: its navigation carries the slot's cookie.
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=1`);
  });

  it('keeps the entering frame past the wait and reloads the page once it enters late', async () => {
    mount();
    await act(async () => {});
    await act(async () => {
      vi.advanceTimersByTime(3000);
    });
    // A slow ticket redirect: the frame is still entering, the page waits.
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=1`);
    expect(src()).toBe('');
    await act(async () => {
      vi.advanceTimersByTime(4000);
    });
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=1`);
    // The entry lands after the page was shown without the cookie: the page's frame is dropped for one render…
    await enterFrame();
    expect(enterSrc()).toBeNull();
    expect(src()).toBe('');
    // …and loads its address again, now with the cookie; the slot is not opened anew.
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    expect(open).toHaveBeenCalledTimes(1);
  });
  it('hides the slot refusal met before the entry and reloads after it, without a new opening', async () => {
    mount();
    await act(async () => {});
    await act(async () => {
      vi.advanceTimersByTime(7000);
    });
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    // The page's frame met «Откройте превью»: the gateway's page tells the page above.
    await refusedBySlot(ORIGIN);
    expect(src()).toBe('');
    await enterFrame();
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    expect(open).toHaveBeenCalledTimes(1);
  });

  it('opens the slot anew once when the page is refused after the entry', async () => {
    mount();
    await act(async () => {});
    await enterFrame();
    expect(src()).toBe(`${ORIGIN}/a/d1/p/a.html`);
    await refusedBySlot(ORIGIN);
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(open).toHaveBeenCalledTimes(2);
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=2`);
    await enterFrame();
    await refusedBySlot(ORIGIN);
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(open).toHaveBeenCalledTimes(2);
  });

  it('ignores a refusal from another origin', async () => {
    mount();
    await act(async () => {});
    await enterFrame();
    await refusedBySlot('https://other.example');
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
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
    expect(open.mock.calls[2][1]).toBe('/favicon.ico');
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=3`);
  });

  it('reopens the slot when another window stamped it', async () => {
    mount();
    await act(async () => {});
    localStorage.setItem(`hl-preview-slot:1`, 'other-window');
    await click('b');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=2`);
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
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=2`);
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
    await enterFrame();
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
    expect(open.mock.calls[1][1]).toBe('/favicon.ico');
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=2`);
  });

  // a chat leaves and comes back from the sidebar: the design mounts again on the pages it was left on. The
  // ticket of the new opening is spent by the entering frame alone; a frame of the page never carries it, so a page
  // frame created again (the stage remounting, a layout restored) cannot meet a spent ticket.
  it('enters a fresh slot when the chat comes back, and shows its page on the plain address', async () => {
    const view = mount();
    await act(async () => {});
    await enterFrame();
    await click('b');
    expect(src()).toBe(`${ORIGIN}/a/d1/p/b.html`);
    view.unmount();
    mount();
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=2`);
    expect(src()).toBe('');
    await enterFrame();
    expect(src()).not.toContain('/enter');
    expect(enterSrc()).toBeNull();
  });

  it('never gives the ticket address to the page frame, before or after entering', async () => {
    mount();
    const seen = new Set<string>();
    for (let i = 0; i < 3; i += 1) {
      await act(async () => {});
      seen.add(src() ?? '');
    }
    await enterFrame();
    await click('b');
    seen.add(src() ?? '');
    expect([...seen].some((address) => address.includes('/enter'))).toBe(false);
  });

  it('reload asks for a fresh slot', async () => {
    mount();
    await act(async () => {});
    await click('reload');
    await act(async () => {});
    expect(open).toHaveBeenCalledTimes(2);
    expect(enterSrc()).toBe(`${ORIGIN}/enter?t=2`);
  });
});

// the switch of the bar turns the design between looking and editing, both ways, with or without its chat.
describe('DesignlySite mode', () => {
  const Routed = ({ chat = null }: { chat?: string | null }) => {
    const [edit, setEdit] = useState(false);
    return (
      <DesignlySite
        machine={M}
        id="d1"
        chat={chat}
        edit={edit}
        onMode={setEdit}
        maxed={false}
        onToggleMax={() => undefined}
        onOpenDrawer={() => undefined}
        onMissing={() => undefined}
      />
    );
  };
  const mode = () => screen.getByTestId('mode').textContent;

  it('goes to editing and back from a link', async () => {
    render(<Routed />);
    await act(async () => {});
    expect(mode()).toBe('false');
    await click('switch');
    expect(mode()).toBe('true');
    await click('switch');
    expect(mode()).toBe('false');
  });

  it('goes to looking and back from its chat', async () => {
    render(<Routed chat="c1" />);
    await act(async () => {});
    expect(mode()).toBe('true');
    await click('switch');
    expect(mode()).toBe('false');
    await click('switch');
    expect(mode()).toBe('true');
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
    // The slot is being entered, or (the entering frame waited out) the page is already shown.
    expect(enterSrc() === `${ORIGIN}/enter?t=1` || src() === `${ORIGIN}/a/d1/p/a.html`).toBe(true);
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

  // After about fifteen seconds the project is still on its way to the store: a card says so, with the way back, and
  // the page keeps looking on its own and opens the project when it arrives.
  it('says the project is on its way after fifteen seconds and opens it when it arrives', async () => {
    const away = () => document.querySelector('.hx-acc-card[role="status"]');
    kept = null;
    shown = { ...library, designs: [] };
    gateway = granted;
    const missing = vi.fn();
    mount(missing);
    await act(async () => {});
    await pass(10_000);
    expect(away()).toBeNull();
    await pass(10_000);
    expect(missing).not.toHaveBeenCalled();
    expect(away()?.textContent).toContain('K');
    shown = library;
    await pass(16_000);
    expect(away()).toBeNull();
    expect(enterSrc() === `${ORIGIN}/enter?t=1` || src() === `${ORIGIN}/a/d1/p/a.html`).toBe(true);
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
