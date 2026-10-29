// @vitest-environment jsdom
import { act, cleanup, fireEvent, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import DeckGuest, { type IDeckGuestComments } from '@/next/features/designly/deck/deck-guest';
import { DECK_OPENS } from '@/next/features/designly/deck/types';

const slides = [
  { id: 'A', title: 'One', path: 'one.dc.html', src: 'https://slot.example/one.dc.html', w: 1920, h: 1080 },
];

const show = (comments: IDeckGuestComments) =>
  render(
    <DeckGuest
      title="Deck"
      slides={slides}
      video={null}
      pdf={null}
      start={DECK_OPENS.slides}
      comments={comments}
    />,
  );

/** A pick as the board sends it now: with the point it was made at. */
const picked = {
  type: 'tk:picked',
  selector: 'h1',
  tag: 'h1',
  text: 'Заголовок',
  rect: { x: 100, y: 100, width: 400, height: 80 },
  point: [960, 540],
};

const fromBoard = (data: Record<string, unknown>) => {
  const frame = document.querySelector<HTMLIFrameElement>('.hx-deck-guest__frame');
  if (!frame?.contentWindow) throw new Error('no frame');
  act(() => {
    window.dispatchEvent(new MessageEvent('message', { data, source: frame.contentWindow }));
  });
};

const flush = () => act(async () => undefined);

beforeEach(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

describe('a guest comments a slide', () => {
  it('opens the card on a pick that carries its point', async () => {
    const comments: IDeckGuestComments = {
      ensureName: vi.fn(async () => true),
      pins: [],
      send: vi.fn(async () => true),
    };
    show(comments);
    fromBoard(picked);
    await flush();
    expect(document.querySelector('textarea')).not.toBeNull();
  });

  it('sends the place of the pick', async () => {
    const send = vi.fn(async () => true);
    show({ ensureName: async () => true, pins: [], send });
    fromBoard(picked);
    await flush();
    const area = document.querySelector('textarea')!;
    fireEvent.change(area, { target: { value: 'Поправить' } });
    const button = [...document.querySelectorAll('button')].find(
      (b) => b.textContent?.trim() === 'Отправить' || b.textContent?.trim() === 'Send',
    );
    expect(button).toBeDefined();
    fireEvent.click(button!);
    await flush();
    expect(send).toHaveBeenCalledWith(0, { x: 50, y: 50 }, 'Заголовок', 'Поправить');
  });

  it('opens nothing when the guest gives no name', async () => {
    show({ ensureName: async () => false, pins: [], send: async () => true });
    fromBoard(picked);
    await flush();
    expect(document.querySelector('textarea')).toBeNull();
  });
});
