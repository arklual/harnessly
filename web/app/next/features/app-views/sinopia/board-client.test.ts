// @vitest-environment jsdom
// The board's side of the bindings (internal/design/texts/client.js): the part that answers `tk:hl`, draws the marks of
// `tk:hl-marks` and turns a click on a `data-hl-field` element into `tk:hl-click`. The part is cut out of the script and
// run on a page of its own, with stubs for what the rest of the script gives it.
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

const source = readFileSync(resolve(process.cwd(), '../internal/design/texts/client.js'), 'utf8');
const start = source.indexOf('  var HL_COLORS');
const end = source.indexOf("  // A presentation's slide (the deck)");
if (start < 0 || end < start) throw new Error('the bindings part is not where the test looks for it');
const part = source.slice(start, end);

const HOST = 'https://app.example.org';
const sent: Record<string, any>[] = []; // eslint-disable-line @typescript-eslint/no-explicit-any
let parent: Window;
let slot = false;

/** Each boot adds listeners to the window; they are taken off after the test so that the boards of earlier tests are not heard. */
const added: [string, EventListenerOrEventListenerObject, boolean | AddEventListenerOptions | undefined][] =
  [];
const realAdd = window.addEventListener.bind(window);
window.addEventListener = ((
  type: string,
  fn: EventListenerOrEventListenerObject,
  options?: boolean | AddEventListenerOptions,
) => {
  added.push([type, fn, options]);
  realAdd(type, fn, options);
}) as typeof window.addEventListener;

const boot = () => {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  parent = frame.contentWindow as Window;
  const run = new Function(
    'parentWin',
    'slotBridge',
    'hostOrigin',
    'picking',
    'pickTarget',
    'pickSelector',
    'tellHostDom',
    'deckFingerprint',
    `${part}\nreturn { hlOf: hlOf };`,
  );
  return run(
    parent,
    slot,
    HOST,
    false,
    (event: Event) => event.target,
    (el: Element) => el.tagName.toLowerCase() + (el.id ? `#${el.id}` : ''),
    (message: Record<string, unknown>) => void sent.push(message),
    (el: Element) => `fp-${el.getAttribute('data-dc-tpl')}`,
  ) as { hlOf: (el: Element) => Record<string, string> | null };
};

const fromHost = (data: unknown, origin = HOST) =>
  window.dispatchEvent(new MessageEvent('message', { data, source: parent, origin }));

beforeEach(() => {
  sent.length = 0;
  slot = false;
  document.body.innerHTML =
    '<main><span id="amount" data-dc-tpl="4" data-hl-field="amount" data-hl-attr="text">5%</span><p id="free" data-dc-tpl="5">free</p></main>';
});
afterEach(() => {
  for (const [type, fn, options] of added.splice(0)) window.removeEventListener(type, fn, options);
  document.documentElement.removeAttribute('data-tk-hl');
});

describe('the board’s bindings', () => {
  it('says hello with its address', () => {
    boot();
    expect(sent[0]).toEqual({ type: 'tk:hl-hello', url: location.href });
  });

  it('reads the data-hl-* of an element: names without the prefix, a few and short', () => {
    const { hlOf } = boot();
    expect(hlOf(document.getElementById('amount') as Element)).toEqual({ field: 'amount', attr: 'text' });
    expect(hlOf(document.getElementById('free') as Element)).toBeNull();
    const many = document.createElement('div');
    for (let i = 0; i < 20; i += 1) many.setAttribute(`data-hl-a${i}`, 'x'.repeat(300));
    const got = hlOf(many) ?? {};
    expect(Object.keys(got)).toHaveLength(12);
    expect(got.a0).toHaveLength(200);
  });

  it('leaves a click alone until the host turns the bindings on', () => {
    boot();
    sent.length = 0;
    document.getElementById('amount')?.click();
    expect(sent.find((m) => m.type === 'tk:hl-click')).toBeUndefined();
  });

  it('answers a click on a bound element with the field, its number and its fingerprint, and keeps the click from the board', () => {
    boot();
    fromHost({ type: 'tk:hl', on: true });
    let reached = false;
    document.body.addEventListener('click', () => (reached = true));
    sent.length = 0;
    document.getElementById('amount')?.click();
    const click = sent.find((m) => m.type === 'tk:hl-click');
    expect(click).toMatchObject({
      field: 'amount',
      hl: { field: 'amount', attr: 'text' },
      tpl: 4,
      fp: 'fp-4',
      tag: 'span',
      text: '5%',
    });
    expect(reached).toBe(false);
  });

  it('does not take a click on an element that is not bound', () => {
    boot();
    fromHost({ type: 'tk:hl', on: true });
    let reached = false;
    document.body.addEventListener('click', () => (reached = true));
    sent.length = 0;
    document.getElementById('free')?.click();
    expect(sent.find((m) => m.type === 'tk:hl-click')).toBeUndefined();
    expect(reached).toBe(true);
  });

  it('hears the host only from its window and origin', () => {
    boot();
    fromHost({ type: 'tk:hl', on: true }, 'https://evil.example');
    sent.length = 0;
    document.getElementById('amount')?.click();
    expect(sent.find((m) => m.type === 'tk:hl-click')).toBeUndefined();
  });

  it('outlines the marked elements with the note, with or without bindings, and takes the outline away with the marks', () => {
    boot();
    // jsdom lays nothing out: the element is given a box.
    (document.getElementById('free') as Element).getBoundingClientRect = () =>
      ({
        left: 10,
        top: 20,
        width: 200,
        height: 40,
        right: 210,
        bottom: 60,
        x: 10,
        y: 20,
        toJSON: () => ({}),
      }) as DOMRect;
    fromHost({ type: 'tk:hl', on: false });
    fromHost({ type: 'tk:hl-marks', items: [{ tpl: 5, level: 'error', note: 'Слишком много слов' }] });
    const badge = document.querySelector('[data-tk-library-mark] [title]');
    expect(badge?.getAttribute('title')).toBe('Слишком много слов');
    // The board's overlay does not take clicks: the page under it stays as it was.
    expect((document.querySelector('[data-tk-library-mark]') as HTMLElement).style.pointerEvents).toBe(
      'none',
    );
    // A list that is not one is not drawn.
    fromHost({ type: 'tk:hl-marks', items: 'broken' });
    expect(document.querySelector('[data-tk-library-mark] [title]')).not.toBeNull();
    fromHost({ type: 'tk:hl-marks', items: [] });
    expect(document.querySelector('[data-tk-library-mark]')).toBeNull();
  });
});
