// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { MachineId } from '@/next/protocol/v2';
import type { IDeckState } from '@/next/features/designly/deck/types';
import type { ISiteStage } from '@/next/features/designly/site-view';

const patchDeck = vi.fn();
const putDeckNotes = vi.fn();
const goTo = vi.fn();
let deck: IDeckState | undefined;
let phone = false;

import { closeDeckDraft, stopDeckPick } from '@/next/features/designly/deck/comment-ui';
vi.mock('@/next/stores/deck', () => ({
  useDeck: () => deck,
  putDeckSlide: vi.fn(),
  putDeckSettingsLocal: vi.fn(),
}));
vi.mock('@/next/features/designly/deck/api', () => ({
  patchDeck: (...args: unknown[]) => patchDeck(...args),
  putDeckNotes: (...args: unknown[]) => putDeckNotes(...args),
  putDeckSettings: vi.fn(),
  startDeckExport: vi.fn(),
  fetchDeckExport: vi.fn(),
  deckExportHref: () => '#',
}));
vi.mock('@/next/features/designly/api', () => ({
  thumbPath: (_m: string, _id: string, board: string, v: string, fresh?: boolean) =>
    `thumb/${board}?v=${v}${fresh ? '&fresh=1' : ''}`,
}));
vi.mock('@/next/routes/routes', () => ({
  goTo: (...args: unknown[]) => goTo(...args),
  ROUTE: { designly: 'designly' },
}));
vi.mock('@/next/bindings/use-voice', () => ({
  useVoice: () => ({ recording: null, recognized: 0, onMic: undefined, onFinishRecording: () => undefined }),
}));

const { default: DeckView } = await import('@/next/features/designly/deck/deck-view');

const boards = [1, 2, 3].map((n) => ({ path: `s${n}.dc.html`, title: `Slide ${n}` }));
const state = (): IDeckState => ({
  rev: 1,
  settings: {
    v: 1,
    minutes: 10,
    slides: null,
    tone: 'talk',
    script: true,
    style: { source: 'acme.pptx', system: null, layouts: 9 },
    theme: 'co',
    font: 'inter',
  },
  chrome: true,
  ffmpeg: false,
  slides: boards.map((b, i) => ({
    id: `P${i + 1}`,
    path: b.path,
    title: b.title,
    w: 1920,
    h: 1080,
    notes: `Notes of ${b.title}`,
    voice: i === 1,
    seconds: 20,
    version: `v${i}`,
    blank: false,
  })),
  comments: [
    {
      id: 'C1',
      page: 'P1',
      x: 10,
      y: 10,
      label: '',
      text: 'one',
      by: { name: 'A', email: 'a@example.org' },
      at: '',
      resolved: false,
      resolvedBy: null,
      resolvedAt: null,
    },
  ],
  guestResolved: [],
  recordings: [],
  published: { video: false, pdf: false, pdfAuto: false, pdfBuilding: false, pdfAt: null, pdfError: null },
});

interface IViewExtra {
  pane?: boolean;
  onGo?: (path: string) => void;
  boards?: { path: string; title: string }[];
  path?: string | null;
  /** The agent of the deck's chat is working. */
  busy?: boolean;
}

const view = (extra: IViewExtra = {}) =>
  render(
    <DeckView
      machine={'m1' as MachineId}
      id="acme"
      title="Acme deck"
      boards={extra.boards ?? boards}
      path={extra.path === undefined ? 's2.dc.html' : extra.path}
      onGo={extra.onGo ?? (() => undefined)}
      src={extra.path === null ? null : `https://slot.example/${extra.path ?? 's2.dc.html'}`}
      frameSrc={(p) => `https://slot.example/${p}`}
      slotError={null}
      onReload={() => undefined}
      pane={extra.pane ? { onBack: () => undefined } : undefined}
      stage={extra.busy === undefined ? undefined : stageOf(extra.busy)}
      maxed={false}
      onToggleMax={() => undefined}
      onOpenDrawer={() => undefined}
      fallback={null}
    />,
  );

/** The site's stage as far as the deck reads it: the chat's agent at work, nothing else. */
const stageOf = (busy: boolean) => ({ busy, run: busy ? 'run' : 'idle' }) as unknown as ISiteStage;

/** A message from the board on screen. */
const fromBoard = (data: Record<string, unknown>) => {
  const frame = document.querySelector<HTMLIFrameElement>('.hx-deck__frame');
  if (!frame?.contentWindow) throw new Error('no frame');
  act(() => {
    window.dispatchEvent(new MessageEvent('message', { data, source: frame.contentWindow }));
  });
};

const root = () => document.querySelector<HTMLElement>('.hx-deck')!;
const key = (k: string) => act(() => void fireEvent.keyDown(window, { key: k }));

beforeEach(() => {
  deck = state();
  phone = false;
  patchDeck.mockReset().mockResolvedValue({ ok: true, rev: 2 });
  putDeckNotes.mockReset();
  goTo.mockReset();
  window.matchMedia = ((query: string) => ({
    matches: phone && query.includes('560'),
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  globalThis.ResizeObserver = class {
    observe() {}
    disconnect() {}
    unobserve() {}
  } as unknown as typeof ResizeObserver;
  Element.prototype.scrollIntoView = () => undefined;
});
afterEach(() => {
  cleanup();
  // The comment being started is the page's, kept per deck: one test's pick must not reach the next.
  stopDeckPick('acme');
  closeDeckDraft('acme');
});

describe('deck shell', () => {
  it('shows the editor bar with the slide in the crumb and the panel docked', () => {
    view();
    expect(root().dataset.screen).toBe('edit');
    expect(root().dataset.panel).toBe('dock');
    expect(document.querySelector('.hx-deck__crumb')?.textContent).toBe('Slide 2');
    const keys = [...document.querySelectorAll('.hx-deck__bar [data-k]')].map((el) =>
      el.getAttribute('data-k'),
    );
    expect(keys).toEqual(['back', 'panel', 'comment', 'style', 'show', 'rec', 'share', 'more']);
    // Without access to share, the share button is there but cannot be pressed.
    expect(document.querySelector<HTMLButtonElement>('[data-k="share"]')?.disabled).toBe(true);
  });

  it('goes to the show with P and back with Esc; the bar of the link has its own buttons', () => {
    view();
    key('p');
    expect(root().dataset.screen).toBe('show');
    key('Escape');
    expect(root().dataset.screen).toBe('edit');
    fireEvent.click(document.querySelector('[data-k="more"]')!);
    fireEvent.click([...document.querySelectorAll('.hx-deck-menu .hx-deck-item')].at(-1)!);
    expect(root().dataset.screen).toBe('link');
    const keys = [...document.querySelectorAll('.hx-deck__bar [data-k]')].map((el) =>
      el.getAttribute('data-k'),
    );
    expect(keys).toEqual(['back', 'comment', 'full', 'more']);
    expect(document.querySelector('.hx-deck__lmode')).not.toBeNull();
    key('f');
    expect(root().dataset.full).toBe('1');
    key('Escape');
    expect(root().dataset.full).toBe('0');
  });

  it('turns the slides with the arrows', () => {
    const onGo = vi.fn();
    view({ onGo });
    key('ArrowRight');
    expect(onGo).toHaveBeenLastCalledWith('s3.dc.html');
    key('ArrowLeft');
    expect(onGo).toHaveBeenLastCalledWith('s1.dc.html');
  });

  it('shows the toolbar over a selected text and hides it while picking a place to comment', () => {
    view();
    fromBoard({
      type: 'tk:deck-select',
      tpl: 7,
      label: 'Title',
      kind: 'title',
      size: 88,
      rect: [100, 200, 900, 120],
    });
    expect(document.querySelector('.hx-deck-tb')).not.toBeNull();
    expect(document.querySelector('.hx-deck-tb [data-k="size"]')?.textContent).toContain('88');
    key('c');
    expect(root().dataset.pick).toBe('1');
    expect(document.querySelector('.hx-deck-tb')).toBeNull();
  });

  it('ends the edit in place on a press outside the block’s tools, not on its toolbar', () => {
    view();
    const frame = document.querySelector<HTMLIFrameElement>('.hx-deck__frame')!;
    const posted: unknown[] = [];
    Object.defineProperty(frame, 'contentWindow', {
      configurable: true,
      value: { postMessage: (m: unknown) => posted.push(m) },
    });
    fromBoard({
      type: 'tk:deck-select',
      tpl: 7,
      label: 'Title',
      kind: 'title',
      size: 88,
      rect: [100, 200, 900, 120],
    });
    const toolbar = document.querySelector<HTMLElement>('.hx-deck-tb')!;
    act(() => void fireEvent.pointerDown(toolbar));
    expect(document.querySelector('.hx-deck-tb')).not.toBeNull();
    act(() => void fireEvent.pointerDown(document.querySelector('.hx-deck__strip')!));
    expect(document.querySelector('.hx-deck-tb')).toBeNull();
    expect(posted).toContainEqual({ type: 'tk:deck-done' });
  });

  it('writes an edited text and a move into the board', () => {
    view();
    fromBoard({ type: 'tk:deck-text', tpl: 7, was: 'Old', html: 'New <b>words</b>' });
    expect(patchDeck).toHaveBeenLastCalledWith('m1', 'acme', 'P2', {
      op: 'text',
      tpl: 7,
      was: 'Old',
      html: 'New <b>words</b>',
    });
    fromBoard({ type: 'tk:deck-drag', tpl: 9, x: 40, y: 0, guides: [{ v: 1, at: 960 }], end: false });
    expect(root().dataset.drag).toBe('1');
    fromBoard({ type: 'tk:deck-drag', tpl: 9, x: 42, y: 0, guides: [], end: true, was: 'div:9a', px: true });
    expect(root().dataset.drag).toBe('0');
    expect(patchDeck).toHaveBeenLastCalledWith('m1', 'acme', 'P2', {
      op: 'move',
      tpl: 9,
      was: 'div:9a',
      x: 42,
      y: 0,
      unit: 'px',
    });
  });

  it('frames a hovered block with its grip, and offers to put a moved one back', () => {
    view();
    fromBoard({
      type: 'tk:deck-hover',
      tpl: 3,
      name: 'List',
      rect: [100, 400, 800, 300],
      moved: true,
      movable: true,
      tx: 20,
      ty: 0,
      was: 'ul:3b',
    });
    expect(document.querySelector('.hx-deck-grip')).not.toBeNull();
    fireEvent.pointerDown(document.querySelector('.hx-deck-bx__reset')!);
    expect(patchDeck).toHaveBeenLastCalledWith('m1', 'acme', 'P2', { op: 'reset', tpl: 3, was: 'ul:3b' });
    fromBoard({ type: 'tk:deck-hover', tpl: null });
    expect(document.querySelector('.hx-deck-bx')).toBeNull();
  });

  it('on a phone: no panel, the dock with the open comments, a selected text opens the edit sheet', () => {
    phone = true;
    view();
    expect(root().dataset.panel).toBe('off');
    expect(document.querySelector('.hx-deck-mdock .hx-deck-badge')?.textContent).toBe('1');
    expect(document.querySelector('.hx-deck-subs__btn')).not.toBeNull();
    fromBoard({
      type: 'tk:deck-select',
      tpl: 7,
      label: 'Title',
      kind: 'title',
      size: 88,
      rect: [0, 0, 10, 10],
    });
    expect(document.querySelector('.hx-deck-esheet')).not.toBeNull();
    expect(root().dataset.es).toBe('1');
    expect(document.querySelector('.hx-deck-mdock')).toBeNull();
  });

  it('in the chat sidebar: embedded, no panel', () => {
    view({ pane: true });
    expect(root().dataset.embed).toBe('1');
    expect(root().dataset.panel).toBe('off');
    expect(document.querySelector('.hx-deck-panel-slot')).toBeNull();
  });
});

describe('a deck being made', () => {
  const show = () => document.querySelector<HTMLButtonElement>('.hx-deck__bar [data-k="show"]')!;

  it('holds a deck with no slides on the app surface while the agent works: skeletons, no pager, no crumb', () => {
    deck = { ...state(), slides: [] };
    view({ boards: [], path: null, busy: true });
    expect(document.querySelector('.hx-deck-hold')?.getAttribute('data-hold')).toBe('making');
    expect(document.querySelector('.hx-deck__frame')).toBeNull();
    expect(document.querySelectorAll('.hx-deck-th[data-pending="1"]')).toHaveLength(3);
    expect(document.querySelector('.hx-deck-pager')).toBeNull();
    expect(document.querySelector('.hx-deck-subs')).toBeNull();
    expect(document.querySelector('.hx-deck__crumb')).toBeNull();
    expect(document.querySelector('.hx-deck__addr')?.textContent).toBe('Acme deck');
    expect(show().getAttribute('aria-disabled')).toBe('true');
    key('p');
    expect(root().dataset.screen).toBe('edit');
  });

  it('says there are no slides once the agent is idle, without skeletons', () => {
    deck = { ...state(), slides: [] };
    view({ boards: [], path: null, busy: false });
    expect(document.querySelector('.hx-deck-hold')?.getAttribute('data-hold')).toBe('none');
    expect(document.querySelectorAll('.hx-deck-th[data-pending="1"]')).toHaveLength(0);
  });

  it('shows the pages the agent made but has not drawn as being made, and keys a drawn one’s picture on its file', () => {
    const made = state();
    deck = { ...made, slides: made.slides.map((s, i) => ({ ...s, blank: i > 0, version: `ver${i}` })) };
    view({ path: 's2.dc.html', busy: true });
    expect(document.querySelector('.hx-deck-hold')?.getAttribute('data-hold')).toBe('making');
    const minis = [...document.querySelectorAll('.hx-deck-th .hx-deck-mini')];
    expect(minis.map((m) => m.getAttribute('data-making'))).toEqual([null, '1', '1']);
    expect(minis[0].querySelector('img')?.getAttribute('src')).toBe('thumb/s1.dc.html?v=ver0.0&fresh=1');
    expect(show().getAttribute('aria-disabled')).toBe('true');
    expect(document.querySelector('.hx-deck-subs')).toBeNull();
  });

  it('shows a blank slide as an empty slide with a hint when the agent has stopped', () => {
    const made = state();
    deck = { ...made, slides: made.slides.map((s, i) => ({ ...s, blank: i === 1 })) };
    view({ path: 's2.dc.html', busy: false });
    expect(document.querySelector('.hx-deck-hold')?.getAttribute('data-hold')).toBe('blank');
    expect(document.querySelectorAll('.hx-deck-mini [data-making], .hx-deck-mini__sk')).toHaveLength(0);
    expect(show().getAttribute('aria-disabled')).toBeNull();
  });

  it('draws a slide’s picture anew when its file changes', () => {
    view({ busy: false });
    const first = document.querySelector('.hx-deck-th img')?.getAttribute('src');
    deck = { ...state(), slides: state().slides.map((s, i) => (i === 0 ? { ...s, version: 'v-new' } : s)) };
    cleanup();
    view({ busy: false });
    const next = document.querySelector('.hx-deck-th img')?.getAttribute('src');
    expect(first).toBe('thumb/s1.dc.html?v=v0.0&fresh=1');
    expect(next).toBe('thumb/s1.dc.html?v=v-new.0&fresh=1');
  });
});
