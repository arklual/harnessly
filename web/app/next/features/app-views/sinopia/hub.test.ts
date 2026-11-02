import { describe, expect, it, vi } from 'vitest';
import { HlHub, type IHlFrame } from '@/next/features/app-views/sinopia/hub';
import { boards, SLOT } from '@/next/features/app-views/sinopia/fixtures';

const frame = (): IHlFrame & { posted: { data: Record<string, unknown>; origin: string }[] } => {
  const posted: { data: Record<string, unknown>; origin: string }[] = [];
  return {
    posted,
    postMessage: (data, origin) => void posted.push({ data: data as Record<string, unknown>, origin }),
  };
};

const hello = (url: string) => ({ type: 'tk:hl-hello', url });
const clickMsg = {
  type: 'tk:hl-click',
  field: 'amount',
  hl: { field: 'amount' },
  tpl: 2,
  fp: 'f',
  selector: '#a',
  tag: 'span',
  text: '5%',
  rect: { x: 0, y: 0, width: 10, height: 10 },
  url: 'https://evil.example/',
};
const url = `${SLOT}/a/proj/p/index.dc.html`;

describe('HlHub', () => {
  it('answers a hello with whether the apps treat bound elements as theirs, addressed to the slot only', () => {
    const hub = new HlHub(SLOT);
    hub.setOn(true);
    const f = frame();
    expect(hub.onMessage({ source: f, origin: SLOT, data: hello(url) }, boards)?.k).toBe('hello');
    expect(f.posted).toEqual([{ data: { type: 'tk:hl', on: true }, origin: SLOT }]);
  });

  it('hears a click only from a board that said hello, and takes the board from the hello', () => {
    const hub = new HlHub(SLOT);
    const f = frame();
    expect(hub.onMessage({ source: f, origin: SLOT, data: clickMsg }, boards)).toBeNull();
    hub.onMessage({ source: f, origin: SLOT, data: hello(url) }, boards);
    const got = hub.onMessage({ source: f, origin: SLOT, data: clickMsg }, boards);
    expect(got?.k).toBe('click');
    // The click's own url (another origin) is not what decides the board.
    expect(got?.k === 'click' && got.board?.id).toBe(boards[0].id);
  });

  it('hears nobody from another origin, and nobody while there is no slot', () => {
    const hub = new HlHub(SLOT);
    const f = frame();
    expect(hub.onMessage({ source: f, origin: 'https://evil.example', data: hello(url) }, boards)).toBeNull();
    expect(hub.size).toBe(0);
    const none = new HlHub(null);
    expect(none.onMessage({ source: f, origin: SLOT, data: hello(url) }, boards)).toBeNull();
  });

  it('does not take a hello that names an address of another origin', () => {
    const hub = new HlHub(SLOT);
    const f = frame();
    expect(
      hub.onMessage({ source: f, origin: SLOT, data: hello('https://evil.example/x') }, boards),
    ).toBeNull();
    expect(hub.size).toBe(0);
  });

  it('does not hear a message whose source is not a window', () => {
    const hub = new HlHub(SLOT);
    expect(hub.onMessage({ source: null, origin: SLOT, data: hello(url) }, boards)).toBeNull();
    expect(hub.onMessage({ source: 'x', origin: SLOT, data: hello(url) }, boards)).toBeNull();
  });

  it('sends each board the marks of its own page', () => {
    const hub = new HlHub(SLOT);
    const a = frame();
    const b = frame();
    hub.onMessage({ source: a, origin: SLOT, data: hello(url) }, boards);
    hub.onMessage({ source: b, origin: SLOT, data: hello(`${SLOT}/a/proj/p/cashback/card.dc.html`) }, boards);
    a.posted.length = 0;
    b.posted.length = 0;
    hub.setMarks(
      [
        { node: `${boards[0].id}:5`, level: 'error', note: 'мелкий шрифт', source: 'app:x' },
        { node: `${boards[1].id}:1`, level: 'warning', note: 'много слов', source: 'app:x' },
      ],
      boards,
    );
    expect(a.posted[0].data).toEqual({
      type: 'tk:hl-marks',
      items: [{ tpl: 5, level: 'error', note: 'мелкий шрифт' }],
    });
    expect(b.posted[0].data).toEqual({
      type: 'tk:hl-marks',
      items: [{ tpl: 1, level: 'warning', note: 'много слов' }],
    });
    expect(a.posted[0].origin).toBe(SLOT);
  });

  it('forgets a window that closed', () => {
    const hub = new HlHub(SLOT);
    const f = frame();
    hub.onMessage({ source: f, origin: SLOT, data: hello(url) }, boards);
    f.closed = true;
    const spy = vi.spyOn(f, 'postMessage');
    hub.setOn(true);
    expect(spy).not.toHaveBeenCalled();
    expect(hub.size).toBe(0);
  });
});
