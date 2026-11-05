// The agent's actions on a page of the live browser, the same reading of the page as the proxy's bridge
// (web/app/lib/hl-preview/bridge-core.ts, «The coding agent acting on the page»): roles, names, states, refs, and the
// refusal to type into a field for a password or card details. It runs in an isolated world of the page: the page's
// scripts see none of it and cannot change what it reads. Pointer and key events come from the browser itself
// (act.go, Input.*); here are the reads, the scrolls and the setting of fields.
(() => {
  const ROLES = /^(button|link|checkbox|radio|tab|menuitem|option|switch|combobox|textbox)$/;
  const ITEMS =
    'a[href],button,input:not([type="hidden"]),textarea,select,summary,[role]:not([role=""]),' +
    '[contenteditable]:not([contenteditable="false"]),h1,h2,h3,video,audio';
  const SECRET_NAME = /card|cvv|cvc|ccnum|cc-?num|cardnumber|(^|[^a-z])pan([^a-z]|$)/i;
  const SECRET_REFUSAL = 'This field takes a password or card details: the person types it themselves.';
  const MAX_ITEMS = 400;
  const MAX_CHARS = 24000;
  const MAX_READ = 8000;
  const EVENTS = /^(ended|playing|paused)$/;
  const refs = { byEl: new WeakMap(), byRef: new Map(), next: 1 };

  const clip = (value, max) => {
    const flat = String(value ?? '').replace(/\s+/g, ' ').trim();
    return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
  };
  const quote = (value) => `"${value.replace(/["\\]/g, '\\$&')}"`;
  const attr = (el, name) => {
    const value = el.getAttribute(name);
    return value === null || value === undefined ? null : String(value);
  };
  const tagOf = (el) => String(el.tagName || '').toLowerCase();
  const textOf = (node) => String(node.textContent ?? '');
  const inputType = (el) => String(el.type || 'text').toLowerCase();
  const isInput = (el) => el instanceof HTMLInputElement;
  const isTextarea = (el) => el instanceof HTMLTextAreaElement;
  const isSelect = (el) => el instanceof HTMLSelectElement;
  const isEditable = (el) => {
    const value = attr(el, 'contenteditable');
    return value !== null && value.toLowerCase() !== 'false';
  };
  const visible = (el) => {
    const box = el.getBoundingClientRect();
    if (!(box.width > 0 && box.height > 0)) return false;
    const style = getComputedStyle(el);
    return style.visibility !== 'hidden' && style.display !== 'none';
  };
  const isSecret = (el) => {
    const tag = tagOf(el);
    if (tag !== 'input' && tag !== 'textarea' && tag !== 'select') return false;
    if (isInput(el) && inputType(el) === 'password') return true;
    const autocomplete = (attr(el, 'autocomplete') ?? '').toLowerCase();
    if (/(^|\s)(cc-|current-password|new-password|one-time-code)/.test(autocomplete)) return true;
    return [attr(el, 'name'), attr(el, 'id'), autocomplete].some((hint) => hint !== null && SECRET_NAME.test(hint));
  };
  const roleOf = (el) => {
    const tag = tagOf(el);
    const explicit = (attr(el, 'role') ?? '').trim().toLowerCase().split(/\s+/)[0];
    if (explicit && ROLES.test(explicit)) return explicit;
    if (tag === 'a') return 'link';
    if (tag === 'video' || tag === 'audio') return tag;
    if (tag === 'button' || tag === 'summary') return 'button';
    if (tag === 'select') return 'combobox';
    if (tag === 'textarea') return 'textbox';
    if (tag === 'input') {
      const type = inputType(el);
      if (type === 'button' || type === 'submit' || type === 'reset' || type === 'image') return 'button';
      if (type === 'checkbox' || type === 'radio') return type;
      return 'textbox';
    }
    if (isEditable(el)) return 'textbox';
    return '';
  };
  const byIdText = (ids) => {
    const parts = [];
    for (const id of ids.split(/\s+/).filter(Boolean).slice(0, 8)) {
      const found = document.getElementById(id);
      if (found) parts.push(textOf(found));
    }
    return clip(parts.join(' '), 80);
  };
  const nameOf = (el) => {
    const tag = tagOf(el);
    const label = attr(el, 'aria-label');
    if (label && label.trim()) return clip(label, 80);
    const labelledBy = attr(el, 'aria-labelledby');
    if (labelledBy) {
      const named = byIdText(labelledBy);
      if (named) return named;
    }
    if (tag === 'input' || tag === 'textarea' || tag === 'select') {
      let text = '';
      for (const item of Array.from(el.labels ?? [])) text += ` ${textOf(item)}`;
      if (!text.trim()) {
        const wrap = el.closest('label');
        if (wrap) text = textOf(wrap);
      }
      if (text.trim()) return clip(text, 80);
    }
    const placeholder = attr(el, 'placeholder');
    if (placeholder && placeholder.trim()) return clip(placeholder, 80);
    const alt = attr(el, 'alt');
    if (alt && alt.trim()) return clip(alt, 80);
    if (tag === 'input' && ['button', 'submit', 'reset'].includes(inputType(el))) {
      const value = attr(el, 'value');
      if (value && value.trim()) return clip(value, 80);
    }
    const title = attr(el, 'title');
    if (title && title.trim()) return clip(title, 80);
    if (tag === 'input' || tag === 'select' || tag === 'textarea') return '';
    return clip(textOf(el), 80);
  };
  const hrefOf = (el) => {
    const raw = attr(el, 'href');
    if (raw === null || raw.trim() === '' || /^\s*javascript:/i.test(raw)) return null;
    try {
      const url = new URL(raw, location.href);
      return clip(url.origin === location.origin ? `${url.pathname}${url.search}${url.hash}` : url.href, 100);
    } catch {
      return clip(raw, 100);
    }
  };
  const statesOf = (el, role) => {
    const words = [];
    const tag = tagOf(el);
    const flag = (name) => attr(el, name)?.toLowerCase();
    if (isMedia(el)) return mediaWords(el);
    if (el.disabled === true || flag('aria-disabled') === 'true') words.push('disabled');
    if (role === 'checkbox' || role === 'radio' || role === 'switch') {
      const checked = isInput(el) ? el.checked === true : flag('aria-checked') === 'true';
      if (checked) words.push('checked');
    }
    if (flag('aria-expanded') === 'true') words.push('expanded');
    if (flag('aria-expanded') === 'false') words.push('collapsed');
    if (flag('aria-selected') === 'true') words.push('selected');
    if (flag('aria-pressed') === 'true') words.push('pressed');
    if (tag === 'a') {
      const href = hrefOf(el);
      if (href) words.push(`href=${href}`);
    }
    const secret = isSecret(el);
    if (secret) words.push(isInput(el) && inputType(el) === 'password' ? 'password' : 'sensitive');
    if (tag === 'input' && role === 'textbox') {
      const type = inputType(el);
      if (type !== 'text' && type !== 'password') words.push(`type=${type}`);
      const value = String(el.value ?? '');
      if (!secret && value) words.push(`value=${quote(clip(value, 60))}`);
    } else if (tag === 'textarea') {
      const value = String(el.value ?? '');
      if (!secret && value) words.push(`value=${quote(clip(value, 60))}`);
    } else if (isSelect(el)) {
      const options = Array.from(el.options ?? []);
      const picked = options.filter((option) => option.selected).map((option) => clip(textOf(option), 40));
      if (!secret && picked.length) words.push(`value=${quote(picked.join(', '))}`);
      const shown = options.slice(0, 20).map((option) => quote(clip(textOf(option), 40)));
      words.push(`options=[${shown.join(', ')}${options.length > 20 ? `, … ${options.length - 20} more` : ''}]`);
    } else if (isEditable(el)) {
      const value = clip(textOf(el), 60);
      if (value) words.push(`value=${quote(value)}`);
    }
    return words;
  };
  const refOf = (el) => {
    const known = refs.byEl.get(el);
    if (known) return known;
    if (refs.next > 999000) {
      refs.byRef.clear();
      refs.next = 1;
    }
    const ref = `e${refs.next}`;
    refs.next += 1;
    refs.byEl.set(el, ref);
    refs.byRef.set(ref, new WeakRef(el));
    return ref;
  };
  const snapshot = () => {
    const lines = [`url: ${location.href}`, `title: ${clip(document.title, 200)}`];
    let size = lines.join('\n').length;
    let listed = 0;
    let skipped = 0;
    for (const el of Array.from(document.querySelectorAll(ITEMS))) {
      const tag = tagOf(el);
      const heading = /^h[1-3]$/.test(tag);
      const role = heading ? 'heading' : roleOf(el);
      if (!role || !visible(el)) continue;
      let line;
      if (heading) {
        const text = clip(textOf(el), 80);
        if (!text) continue;
        line = `- heading ${quote(text)} level=${tag.slice(1)}`;
      } else {
        if (listed >= MAX_ITEMS || size > MAX_CHARS) {
          skipped += 1;
          continue;
        }
        const name = nameOf(el);
        const parts = [`- ${role}`];
        if (name) parts.push(quote(name));
        parts.push(`[${refOf(el)}]`, ...statesOf(el, role));
        line = parts.join(' ');
        listed += 1;
      }
      if (size + line.length > MAX_CHARS + 400) {
        skipped += 1;
        continue;
      }
      size += line.length + 1;
      lines.push(line);
    }
    if (skipped > 0) lines.push(`… (${skipped} more)`);
    if (listed === 0) lines.push('(nothing to act on is shown on this page yet)');
    return lines.join('\n');
  };
  const describe = (el) => {
    const role = roleOf(el) || tagOf(el) || 'element';
    const name = nameOf(el);
    return name ? `${role} ${quote(name)}` : role;
  };
  const fail = (text) => ({ ok: false, text });
  const resolve = (ref) => {
    if (!ref) return { fail: fail('This action needs a ref from the last snapshot.') };
    const entry = refs.byRef.get(ref);
    if (!entry) return { fail: fail(`${ref} is not a ref of this page: take a snapshot first.`) };
    const el = entry.deref();
    if (!el || !el.isConnected) return { fail: fail(`${ref} is gone from the page: take a new snapshot`) };
    return { el };
  };
  const box = (el) => {
    const b = el.getBoundingClientRect();
    return { x: b.left, y: b.top, width: b.width, height: b.height };
  };
  const bringIntoView = (el) => {
    try {
      el.scrollIntoView({ block: 'center', inline: 'nearest', behavior: 'instant' });
    } catch {
      // Acted on where it is.
    }
  };
  const scrolled = () => {
    const root = document.scrollingElement || document.documentElement;
    return `scrolled to y=${Math.round(Number(scrollY) || 0)} of ${Math.round(Number(root?.scrollHeight) || 0)}`;
  };
  const pageText = () => {
    const body = document.body;
    if (!body) return '';
    const text = body.innerText;
    return String(typeof text === 'string' ? text : (body.textContent ?? ''));
  };
  const pause = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  // A video or audio of the page: whether it plays, where it is and how long it is.
  const isMedia = (el) => el instanceof HTMLMediaElement;
  const seconds = (n) => (Number.isFinite(n) ? `${Math.round(n * 10) / 10}s` : '?');
  const mediaWords = (el) => {
    const words = [el.ended ? 'ended' : el.paused ? 'paused' : 'playing', `at=${seconds(el.currentTime)}`, `of=${seconds(el.duration)}`];
    if (el.muted) words.push('muted');
    if (el.loop) words.push('loop');
    return words;
  };
  const area = (el) => {
    const b = el.getBoundingClientRect();
    return b.width * b.height;
  };
  const mediaShown = () => Array.from(document.querySelectorAll('video,audio')).filter((el) => visible(el) || !el.paused);
  // The media an event is about: the ref given, else the one playing, else the biggest one shown.
  const mediaOf = (ref) => {
    if (ref) {
      const found = resolve(ref);
      if ('fail' in found) return found;
      if (!isMedia(found.el)) return { fail: fail(`${ref} is not a video or audio: give the ref of one from the snapshot, or no ref.`) };
      return found;
    }
    const shown = mediaShown();
    const el = shown.find((item) => !item.paused && !item.ended) ?? shown.sort((a, b) => area(b) - area(a))[0];
    return el ? { el } : { fail: fail('No video or audio is on the page.') };
  };
  const readPage = (act) => {
    const lines = [`url: ${location.href}`, `title: ${clip(document.title, 200)}`];
    for (const el of mediaShown()) lines.push(`- ${tagOf(el)} [${refOf(el)}] ${mediaWords(el).join(' ')}`);
    let text;
    if (act.ref) {
      const found = resolve(act.ref);
      if ('fail' in found) return found.fail;
      const own = found.el.innerText;
      text = String(typeof own === 'string' ? own : textOf(found.el));
    } else {
      text = pageText();
    }
    text = text.replace(/[ \t]+\n/g, '\n').replace(/\n{3,}/g, '\n\n').trim();
    lines.push('text:', text.length > MAX_READ ? `${text.slice(0, MAX_READ)}… (${text.length - MAX_READ} more characters)` : text || '(no text)');
    return { ok: true, text: lines.join('\n') };
  };
  const waitEvent = async (act) => {
    const event = String(act.event);
    if (!EVENTS.test(event)) return fail('wait knows the events ended, playing and paused.');
    const found = mediaOf(act.ref);
    if ('fail' in found) return found.fail;
    const el = found.el;
    const what = `${tagOf(el)} [${refOf(el)}]`;
    const limit = Math.min(act.ms || 25000, 25000);
    let waited = 0;
    let last = el.currentTime;
    for (;;) {
      const now = el.currentTime;
      // A clip that loops, or that the page starts over, never ends: its jump back to the start counts as its end.
      const over = el.ended === true || (last > 0.5 && now + 0.5 < last) || (Number.isFinite(el.duration) && el.duration > 0 && now >= el.duration - 0.05);
      const met = event === 'ended' ? over : event === 'playing' ? !el.paused && !el.ended : el.paused;
      if (met) {
        const verb = event === 'ended' ? (el.ended ? 'has ended' : 'played to its end') : event === 'playing' ? 'is playing' : 'is paused';
        return { ok: true, text: `the ${what} ${verb}: ${mediaWords(el).join(' ')}`, rect: box(el) };
      }
      if (waited >= limit) return fail(`The ${what} is not ${event} after ${limit} ms: ${mediaWords(el).join(' ')}. Call again to keep waiting.`);
      last = now;
      const step = Math.min(100, limit - waited);
      await pause(step);
      waited += step;
    }
  };
  const wait = async (act) => {
    if (act.event !== undefined) return waitEvent(act);
    if (act.text === undefined && act.ms === undefined) return fail('wait needs text, an event or ms.');
    if (act.text === undefined) {
      await pause(act.ms);
      return { ok: true, text: `waited ${act.ms} ms` };
    }
    const wanted = String(act.text).replace(/\s+/g, ' ').trim();
    const limit = Math.min(act.ms ?? 5000, 10000);
    let waited = 0;
    for (;;) {
      if (pageText().replace(/\s+/g, ' ').includes(wanted))
        return { ok: true, text: `the text ${quote(clip(wanted, 80))} is on the page` };
      if (waited >= limit) return fail(`The text ${quote(clip(wanted, 80))} did not show up within ${limit} ms.`);
      const step = Math.min(100, limit - waited);
      await pause(step);
      waited += step;
    }
  };
  const setField = (el, text) => {
    // The native setter, not `el.value =`: a framework that tracks the value on the element (React) then sees a change.
    const proto = isInput(el) ? HTMLInputElement.prototype : HTMLTextAreaElement.prototype;
    const set = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
    if (set) set.call(el, text);
    else el.value = text;
    el.dispatchEvent(new InputEvent('input', { bubbles: true, composed: true, inputType: 'insertText', data: text }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
  };
  const type = (el, text) => {
    if (isSecret(el)) return fail(SECRET_REFUSAL);
    const what = describe(el);
    const input = isInput(el);
    const area = isTextarea(el);
    if (el.disabled === true) return fail(`The ${what} is disabled.`);
    if ((input || area) && el.readOnly === true) return fail(`The ${what} is read-only.`);
    if (input && !/^(text|search|email|url|tel|number|date|time|datetime-local|month|week|color|range)$/.test(inputType(el)))
      return fail(`The ${what} does not take text: use click for a checkbox, radio or button.`);
    el.focus();
    if (input || area) setField(el, text);
    else if (isEditable(el)) {
      let done = false;
      try {
        const range = document.createRange();
        range.selectNodeContents(el);
        const selection = getSelection();
        selection.removeAllRanges();
        selection.addRange(range);
        done = document.execCommand(text === '' ? 'delete' : 'insertText', false, text);
      } catch {
        done = false;
      }
      if (!done) {
        el.textContent = text;
        el.dispatchEvent(new InputEvent('input', { bubbles: true, composed: true, inputType: 'insertText', data: text }));
      }
    } else return fail(`The ${what} is not a text field.`);
    return { ok: true, text: `typed into ${what}`, what };
  };
  const select = (el, value) => {
    if (isSecret(el)) return fail(SECRET_REFUSAL);
    const what = describe(el);
    if (!isSelect(el)) return fail(`The ${what} is not a select: use click or type.`);
    if (value === undefined) return fail("select needs the option's value or text.");
    const options = Array.from(el.options);
    const text = (option) => textOf(option).trim();
    const found =
      options.find((option) => option.value === value) ??
      options.find((option) => text(option) === value.trim()) ??
      options.find((option) => text(option).toLowerCase() === value.trim().toLowerCase());
    if (!found) {
      const shown = options.slice(0, 10).map((option) => quote(text(option)));
      return fail(`No option "${value}" in the ${what}; it has ${shown.join(', ')}${options.length > 10 ? ', …' : ''}.`);
    }
    if (found.disabled) return fail(`The option "${text(found)}" is disabled.`);
    el.focus();
    el.selectedIndex = options.indexOf(found);
    el.dispatchEvent(new Event('input', { bubbles: true, composed: true }));
    el.dispatchEvent(new Event('change', { bubbles: true }));
    return { ok: true, text: `selected "${text(found)}" in ${what}` };
  };
  const activeBox = () => {
    const el = focused();
    return el && el !== document.body ? box(el) : null;
  };
  // What has the focus, down through shadow roots.
  const focused = () => {
    let el = document.activeElement;
    while (el && el.shadowRoot && el.shadowRoot.activeElement) el = el.shadowRoot.activeElement;
    return el;
  };
  // Whether what has the focus takes a password or card details: keys go nowhere then. A frame's inside cannot be
  // read from here, so a focused frame counts as one.
  // Elements that hold a document of their own: what is inside cannot be read from here.
  const FRAMES = ['iframe', 'frame', 'object', 'embed', 'fencedframe', 'portal'];
  const isFrame = (el) => !!el && FRAMES.includes(tagOf(el));
  const activeFrame = () => isFrame(focused());
  const activeSecret = () => {
    const el = focused();
    if (!el) return false;
    return activeFrame() || isSecret(el);
  };
  // Whether the element holds the focus (itself, or something inside it for an editable block).
  const holds = (el) => {
    const now = focused();
    return !!now && !isFrame(now) && (now === el || el.contains(now));
  };
  // Whether a point of the element is the element's own (not under a layer the page put over it): the browser's
  // click lands on what is at the point, so a covered element is not clicked.
  const covered = (el, x, y) => {
    const hit = document.elementFromPoint(x, y);
    if (!hit) return true;
    if (hit === el || el.contains(hit)) return false;
    // A label's control, a control's label: the click reaches the same thing.
    if (hit instanceof HTMLLabelElement && hit.control === el) return false;
    if (el instanceof HTMLLabelElement && el.control === hit) return false;
    return !hit.contains(el) || isFrame(hit);
  };

  // The ops the browser side asks for. prepare: an element for a pointer action (click, hover) — checked, brought
  // into view, its box given; the browser then moves the mouse there. type sets the field; the Enter of submit is the
  // browser's.
  globalThis.__hxAct = async (act) => {
    try {
      // The site the person allowed, checked on the very document the action runs on.
      if (act.host && location.hostname.toLowerCase() !== String(act.host).toLowerCase()) {
        return fail(`The page is on another site now (${location.hostname || 'no site'}), not ${act.host}: the person has to allow it; take a snapshot.`);
      }
      switch (act.op) {
        case 'snapshot':
          return { ok: true, text: snapshot() };
        case 'read':
          return readPage(act);
        case 'tools':
          return { ok: true, text: 'This page offers no tools of its own (WebMCP) in the live browser.' };
        case 'call':
          return fail('Site tools (WebMCP) are not called in the live browser: act on the page with click and type.');
        case 'wait':
          return await wait(act);
        case 'active':
          return { ok: true, text: '', rect: activeBox(), secret: activeSecret(), frame: activeFrame() };
        case 'scroll':
          if (!act.ref) {
            if (act.dy === undefined) return fail('scroll needs a ref or dy.');
            scrollBy({ left: 0, top: act.dy, behavior: 'instant' });
            return { ok: true, text: scrolled() };
          }
          break;
        default:
          break;
      }
      const found = resolve(act.ref);
      if ('fail' in found) return found.fail;
      const el = found.el;
      const what = describe(el);
      if (act.op === 'hit') {
        // Right before the browser's click, without moving anything: what is at the point is still this element.
        if (!visible(el) || covered(el, act.x, act.y)) return fail(`The ${what} [${act.ref}] moved or got covered before the click: it was not clicked; take a snapshot.`);
        return { ok: true, text: what, rect: box(el) };
      }
      if (act.op !== 'scroll' && !visible(el)) return fail(`The ${what} [${act.ref}] is not shown on the page now.`);
      bringIntoView(el);
      const rect = box(el);
      switch (act.op) {
        case 'scroll':
          return { ok: true, text: scrolled(), rect };
        case 'locate':
          return { ok: true, text: `found ${what}`, rect };
        case 'prepare':
          if (act.for === 'click' && el.disabled === true) return fail(`The ${what} is disabled.`);
          if (covered(el, rect.x + rect.width / 2, rect.y + rect.height / 2)) return fail(`The ${what} [${act.ref}] is covered by something else on the page: it cannot be clicked now.`);
          return { ok: true, text: what, rect };
        case 'holds':
          // Right before the Enter of a submit: the field typed into still has the focus.
          if (!holds(el)) return fail(`The ${what} [${act.ref}] lost the focus before Enter: nothing was submitted; take a snapshot.`);
          if (isSecret(el)) return fail(SECRET_REFUSAL);
          return { ok: true, text: what, rect };
        case 'select': {
          const result = select(el, act.value);
          return result.ok ? { ...result, rect } : result;
        }
        case 'type': {
          const result = type(el, act.text ?? '');
          return result.ok ? { ...result, rect: box(el) } : result;
        }
        default:
          return fail('Unknown action.');
      }
    } catch (error) {
      return fail(`The page got in the way of the action: ${String(error?.message ?? error).slice(0, 200)}`);
    }
  };
})();
