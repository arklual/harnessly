// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as core from './bridge-core';

/** jsdom lays nothing out: every element gets a box, and `display: none` / `visibility: hidden` hide it as the page would. */
const BOX = { left: 10, top: 20, width: 100, height: 30, right: 110, bottom: 50, x: 10, y: 20 };

const act = (fields: Record<string, unknown>) => {
  const parsed = core.parseParentMessage({ type: 'tk:act', id: 'a1', ...fields });
  if (!parsed || parsed.type !== 'tk:act') throw new Error(`not an act: ${JSON.stringify(fields)}`);
  return parsed;
};

describe('tk:act parsing', () => {
  it('takes a well-formed action', () => {
    expect(core.parseParentMessage({ type: 'tk:act', id: 'x', op: 'click', ref: 'e12' })).toEqual({
      type: 'tk:act',
      id: 'x',
      op: 'click',
      ref: 'e12',
    });
    expect(
      core.parseParentMessage({ type: 'tk:act', id: 'x', op: 'type', ref: 'e1', text: '', submit: true }),
    ).toEqual({
      type: 'tk:act',
      id: 'x',
      op: 'type',
      ref: 'e1',
      text: '',
      submit: true,
    });
  });

  it('reads the empty or zero fields the runtime sends for an unused one as absent', () => {
    const sent = {
      id: 'x',
      op: 'snapshot',
      ref: '',
      text: '',
      key: '',
      value: '',
      dy: 0,
      ms: 0,
      submit: false,
    };
    expect(core.parseParentMessage({ type: 'tk:act', ...sent })).toEqual({
      type: 'tk:act',
      id: 'x',
      op: 'snapshot',
    });
  });

  it('drops what is not right', () => {
    const bad: Record<string, unknown>[] = [
      { id: '', op: 'click' },
      { id: 'x'.repeat(65), op: 'click' },
      { id: 7, op: 'click' },
      { id: 'x', op: 'navigate' },
      { id: 'x', op: 'click', ref: 'e' },
      { id: 'x', op: 'click', ref: 'e1234567' },
      { id: 'x', op: 'click', ref: 'x1' },
      { id: 'x', op: 'click', ref: 5 },
      { id: 'x', op: 'type', text: 'a'.repeat(4001) },
      { id: 'x', op: 'press', key: 'k'.repeat(33) },
      { id: 'x', op: 'select', value: 'v'.repeat(501) },
      { id: 'x', op: 'scroll', dy: 20_001 },
      { id: 'x', op: 'scroll', dy: Number.NaN },
      { id: 'x', op: 'scroll', dy: '5' },
      { id: 'x', op: 'wait', ms: -1 },
      { id: 'x', op: 'wait', ms: 10_001 },
      { id: 'x', op: 'type', submit: 'yes' },
    ];
    for (const fields of bad)
      expect(core.parseParentMessage({ type: 'tk:act', ...fields }), JSON.stringify(fields)).toBeNull();
    expect(core.parseParentMessage({ type: 'tk:act', op: 'click' })).toBeNull();
  });
});

describe('the agent acting on the page', () => {
  let bound: Record<string, unknown>;
  let refs: core.IActRefs;
  const run = (fields: Record<string, unknown>) =>
    core.runAct(window, document, bound, core, refs, act(fields));
  const refOf = (snapshot: string, name: string): string => {
    const line = snapshot.split('\n').find((item) => item.includes(name));
    const ref = line?.match(/\[(e\d+)\]/)?.[1];
    if (!ref) throw new Error(`no ref for ${name} in:\n${snapshot}`);
    return ref;
  };
  const snapshot = async () => (await run({ op: 'snapshot' })).text;

  beforeEach(() => {
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (this: Element) {
      // Laid out as a page would: nothing under a `display: none` has a box.
      return (this.closest('[style*="display:none"]') ? { ...BOX, width: 0, height: 0 } : BOX) as DOMRect;
    });
    window.scrollBy = vi.fn();
    document.body.innerHTML = '';
    bound = { ...core.captureNatives(window), origin: window.location.origin, parent: window };
    refs = core.makeActRefs();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('lists links, buttons and fields with refs, and keeps a ref for an element across snapshots', async () => {
    document.body.innerHTML = `
      <h1>Checkout</h1>
      <a href="/cart?x=1">Back to cart</a>
      <button disabled>Pay</button>
      <label for="mail">E-mail</label><input id="mail" type="email" value="a@b.c">
      <input type="checkbox" aria-label="Gift" checked>
      <select aria-label="Size"><option value="s">Small</option><option value="m" selected>Medium</option></select>
      <div role="button" aria-expanded="true">Menu</div>`;
    const first = await snapshot();
    expect(first).toContain('url: ');
    expect(first).toContain('- heading "Checkout" level=1');
    expect(first).toMatch(/- link "Back to cart" \[e\d+\] href=\/cart\?x=1/);
    expect(first).toMatch(/- button "Pay" \[e\d+\] disabled/);
    expect(first).toMatch(/- textbox "E-mail" \[e\d+\] type=email value="a@b\.c"/);
    expect(first).toMatch(/- checkbox "Gift" \[e\d+\] checked/);
    expect(first).toMatch(/- combobox "Size" \[e\d+\] value="Medium" options=\["Small", "Medium"\]/);
    expect(first).toMatch(/- button "Menu" \[e\d+\] expanded/);
    const link = refOf(first, 'Back to cart');
    // Another element appears before it: the link keeps its ref, the new one gets a new one.
    document.body.insertAdjacentHTML('afterbegin', '<button>New</button>');
    const second = await snapshot();
    expect(refOf(second, 'Back to cart')).toBe(link);
    expect(refOf(second, '"New"')).not.toBe(link);
  });

  it('skips what is not shown', async () => {
    document.body.innerHTML = `
      <button id="a" style="display:none">Gone</button>
      <div style="display:none"><button>Inside gone</button></div>
      <button style="visibility:hidden">Invisible</button>
      <input type="hidden" name="t" value="secret">
      <button>Here</button>`;
    const text = await snapshot();
    expect(text).toContain('"Here"');
    expect(text).not.toMatch(/Gone|Inside gone|Invisible|secret/);
  });

  it('never shows the value of a password or a card field', async () => {
    document.body.innerHTML = `
      <input type="password" aria-label="Password" value="hunter2">
      <input name="cardnumber" aria-label="Number" value="4111111111111111">
      <input autocomplete="cc-exp" aria-label="Expires" value="12/30">
      <input aria-label="Name" value="Anna">`;
    const text = await snapshot();
    expect(text).not.toMatch(/hunter2|4111|12\/30/);
    expect(text).toMatch(/"Password" \[e\d+\] password/);
    expect(text).toContain('value="Anna"');
  });

  it('clicks with the events of a pointer, which the page sees', async () => {
    document.body.innerHTML = '<button>Buy</button>';
    const seen: string[] = [];
    const button = document.querySelector('button') as HTMLElement;
    for (const type of ['pointerdown', 'mousedown', 'mouseup', 'click'])
      button.addEventListener(type, (event) =>
        seen.push(`${type}@${(event as MouseEvent).clientX},${(event as MouseEvent).clientY}`),
      );
    const ref = refOf(await snapshot(), 'Buy');
    const result = await run({ op: 'click', ref });
    expect(result).toMatchObject({ ok: true, text: 'clicked button "Buy"' });
    expect(result.rect).toEqual({ x: 10, y: 20, width: 100, height: 30 });
    expect(seen).toContain('click@60,35');
    expect(seen.indexOf('mousedown@60,35')).toBeLessThan(seen.indexOf('mouseup@60,35'));
  });

  it('refuses to click a disabled button, and a ref whose element left the page', async () => {
    document.body.innerHTML = '<button disabled>Pay</button><a href="#x">Go</a>';
    const text = await snapshot();
    expect(await run({ op: 'click', ref: refOf(text, 'Pay') })).toMatchObject({
      ok: false,
      text: expect.stringContaining('disabled'),
    });
    const go = refOf(text, 'Go');
    document.querySelector('a')?.remove();
    expect(await run({ op: 'click', ref: go })).toEqual({
      ok: false,
      text: `${go} is gone from the page: take a new snapshot`,
    });
    expect((await run({ op: 'click', ref: 'e999' })).ok).toBe(false);
  });

  it('sets a field through the native setter and tells it to the page, so a tracked value sees the change', async () => {
    document.body.innerHTML = '<input aria-label="Search"><textarea aria-label="Note"></textarea>';
    const input = document.querySelector('input') as HTMLInputElement;
    // What React does: a tracker on the element that a plain `el.value =` keeps current and the native setter does not.
    let tracked = '';
    const proto = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value') as PropertyDescriptor;
    Object.defineProperty(input, 'value', {
      configurable: true,
      get: () => proto.get?.call(input),
      set: (value: string) => {
        tracked = value;
        proto.set?.call(input, value);
      },
    });
    const events: string[] = [];
    for (const type of ['input', 'change']) input.addEventListener(type, () => events.push(type));
    const text = await snapshot();
    const result = await run({ op: 'type', ref: refOf(text, 'Search'), text: 'шапка' });
    expect(result).toMatchObject({ ok: true, text: 'typed into textbox "Search"' });
    expect(input.value).toBe('шапка');
    expect(tracked).toBe('');
    expect(events).toEqual(['input', 'change']);
    const area = document.querySelector('textarea') as HTMLTextAreaElement;
    await run({ op: 'type', ref: refOf(text, 'Note'), text: 'line 1\nline 2' });
    expect(area.value).toBe('line 1\nline 2');
  });

  it('refuses to type into a password or a card field', async () => {
    document.body.innerHTML = `
      <input type="password" aria-label="Password">
      <input name="card_number" aria-label="Number">
      <input autocomplete="one-time-code" aria-label="Code">
      <input autocomplete="cc-csc" aria-label="Security">`;
    const text = await snapshot();
    for (const name of ['Password', 'Number', 'Code', 'Security']) {
      expect(await run({ op: 'type', ref: refOf(text, name), text: 'x' })).toEqual({
        ok: false,
        text: core.ACT_SECRET_REFUSAL,
      });
    }
    for (const field of Array.from(document.querySelectorAll('input'))) expect(field.value).toBe('');
  });

  it('does not type into a checkbox', async () => {
    document.body.innerHTML = '<input type="checkbox" aria-label="Gift">';
    const result = await run({ op: 'type', ref: refOf(await snapshot(), 'Gift'), text: 'x' });
    expect(result.ok).toBe(false);
  });

  it('chooses an option by its text or its value, and lists the options when there is none', async () => {
    document.body.innerHTML =
      '<select aria-label="Size"><option value="s">Small</option><option value="m">Medium</option></select>';
    const select = document.querySelector('select') as HTMLSelectElement;
    const changed = vi.fn();
    select.addEventListener('change', changed);
    const ref = refOf(await snapshot(), 'Size');
    expect(await run({ op: 'select', ref, value: 'Medium' })).toMatchObject({ ok: true });
    expect(select.value).toBe('m');
    expect(await run({ op: 'select', ref, value: 's' })).toMatchObject({ ok: true });
    expect(select.value).toBe('s');
    expect(changed).toHaveBeenCalledTimes(2);
    const missing = await run({ op: 'select', ref, value: 'Huge' });
    expect(missing).toMatchObject({ ok: false });
    expect(missing.text).toContain('"Small", "Medium"');
  });

  it('submits the form when Enter is pressed in a field, or with a typed text and submit', async () => {
    document.body.innerHTML = '<form><input aria-label="Query"><button>Go</button></form>';
    const form = document.querySelector('form') as HTMLFormElement;
    const submitted = vi.fn((event: Event) => event.preventDefault());
    form.addEventListener('submit', submitted);
    const keys: string[] = [];
    const input = document.querySelector('input') as HTMLInputElement;
    for (const type of ['keydown', 'keypress', 'keyup']) input.addEventListener(type, () => keys.push(type));
    const ref = refOf(await snapshot(), 'Query');
    await run({ op: 'type', ref, text: 'abc', submit: true });
    expect(submitted).toHaveBeenCalledTimes(1);
    expect(keys).toEqual(['keydown', 'keypress', 'keyup']);
    expect(input.value).toBe('abc');
    input.focus();
    expect(await run({ op: 'press', key: 'Enter' })).toMatchObject({ ok: true, text: 'pressed Enter' });
    expect(submitted).toHaveBeenCalledTimes(2);
  });

  it('does not submit when the page cancels the Enter key', async () => {
    document.body.innerHTML = '<form><input aria-label="Query"></form>';
    const form = document.querySelector('form') as HTMLFormElement;
    const submitted = vi.fn((event: Event) => event.preventDefault());
    form.addEventListener('submit', submitted);
    const input = document.querySelector('input') as HTMLInputElement;
    input.addEventListener('keydown', (event) => event.preventDefault());
    input.focus();
    await run({ op: 'press', key: 'Enter' });
    expect(submitted).not.toHaveBeenCalled();
  });

  it('moves focus on Tab', async () => {
    document.body.innerHTML = '<input id="a" aria-label="A"><input id="b" aria-label="B">';
    (document.getElementById('a') as HTMLElement).focus();
    await run({ op: 'press', key: 'Tab' });
    expect(document.activeElement?.id).toBe('b');
  });

  it('hovers and locates', async () => {
    document.body.innerHTML = '<a href="/x">Item</a>';
    const seen: string[] = [];
    const link = document.querySelector('a') as HTMLElement;
    for (const type of ['mouseover', 'mouseenter', 'mousemove'])
      link.addEventListener(type, () => seen.push(type));
    const ref = refOf(await snapshot(), 'Item');
    expect(await run({ op: 'hover', ref })).toMatchObject({
      ok: true,
      rect: { x: 10, y: 20, width: 100, height: 30 },
    });
    expect(seen).toEqual(['mouseover', 'mouseenter', 'mousemove']);
    expect(await run({ op: 'locate', ref })).toMatchObject({ ok: true, rect: { width: 100 } });
  });

  it('scrolls by a distance, and reports where it is', async () => {
    const result = await run({ op: 'scroll', dy: 300 });
    expect(window.scrollBy).toHaveBeenCalledWith({ left: 0, top: 300, behavior: 'instant' });
    expect(result.text).toMatch(/^scrolled to y=\d+ of \d+$/);
    expect((await run({ op: 'scroll' })).ok).toBe(false);
  });

  it('waits for a text to show, and says when it did not', async () => {
    document.body.innerHTML = '<p>Loading</p>';
    window.setTimeout(() => {
      document.body.innerHTML = '<p>Order   placed</p>';
    }, 150);
    expect(await run({ op: 'wait', text: 'Order placed', ms: 3000 })).toMatchObject({ ok: true });
    const late = await run({ op: 'wait', text: 'Never there', ms: 250 });
    expect(late.ok).toBe(false);
    expect(late.text).toContain('did not show up within 250 ms');
    expect(await run({ op: 'wait', ms: 20 })).toEqual({ ok: true, text: 'waited 20 ms' });
  });

  it('lists a video with its state, reads the page, and waits for the clip to end', async () => {
    document.body.innerHTML = '<h1>Ролик</h1><p>Про мероприятие</p><video aria-label="Клип"></video>';
    const video = document.querySelector('video') as HTMLVideoElement;
    const state = { paused: true, ended: false, currentTime: 0, duration: 12 };
    for (const key of Object.keys(state) as (keyof typeof state)[])
      Object.defineProperty(video, key, { get: () => state[key], configurable: true });
    const text = await snapshot();
    expect(text).toMatch(/- video "Клип" \[e\d+\] paused at=0s of=12s/);
    const ref = refOf(text, '"Клип"');
    const read = await run({ op: 'read' });
    expect(read.ok).toBe(true);
    expect(read.text).toContain(`- video [${ref}] paused at=0s of=12s\ntext:\n`);
    expect(read.text).toContain('Про мероприятие');
    const one = await run({ op: 'read', ref: refOf(text, '"Клип"') });
    expect(one.ok).toBe(true);
    const notYet = await run({ op: 'wait', event: 'ended', ref, ms: 250 });
    expect(notYet.ok).toBe(false);
    expect(notYet.text).toContain(`The video [${ref}] is not ended after 250 ms`);
    expect((await run({ op: 'wait', event: 'ended', ref: 'e999' })).ok).toBe(false);
    state.paused = false;
    expect(await run({ op: 'wait', event: 'playing', ms: 1000 })).toMatchObject({
      ok: true,
      text: `the video [${ref}] is playing: playing at=0s of=12s`,
    });
    window.setTimeout(() => {
      state.currentTime = 11.9;
      state.ended = true;
      state.paused = true;
    }, 150);
    const over = await run({ op: 'wait', event: 'ended', ms: 3000 });
    expect(over).toMatchObject({ ok: true, text: `the video [${ref}] has ended: ended at=11.9s of=12s` });
    // A clip that starts over (a loop, or the page's own replay) counts as ended.
    state.ended = false;
    state.paused = false;
    state.currentTime = 11;
    window.setTimeout(() => {
      state.currentTime = 0.2;
    }, 150);
    expect((await run({ op: 'wait', event: 'ended', ms: 3000 })).text).toContain('played to its end');
    expect(core.parseParentMessage({ type: 'tk:act', id: 'x', op: 'wait', event: 'stopped' })).toBeNull();
    expect(
      core.parseParentMessage({ type: 'tk:act', id: 'x', op: 'wait', event: 'ended', ms: 20000 }),
    ).toMatchObject({
      event: 'ended',
      ms: 20000,
    });
    expect(core.parseParentMessage({ type: 'tk:act', id: 'x', op: 'wait', ms: 20000 })).toBeNull();
  });

  it('reads an element whose name shadows a built-in of its form', async () => {
    document.body.innerHTML =
      '<form><input name="tagName" aria-label="Odd"><input name="querySelectorAll" aria-label="Odd 2"></form>';
    const text = await snapshot();
    expect(text).toContain('"Odd"');
    expect(await run({ op: 'type', ref: refOf(text, '"Odd"'), text: 'ok' })).toMatchObject({ ok: true });
  });
});
