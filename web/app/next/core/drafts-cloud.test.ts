import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  BEHIND_MS,
  CLOUD_DELAY_MS,
  CLOUD_EVERY_MS,
  flushDrafts,
  startDraftSync,
} from '@/next/core/drafts-cloud';
import { memoryKV, startPersistence } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import type { CommandBodies, ConvId, Draft } from '@/next/protocol/v2';
import { draftBaseStore } from '@/next/stores/draft-sync';
import { clearDraft, draftsStore, draftText, NEW_CHAT, setDraft, setDraftText } from '@/next/stores/drafts';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const OTHER = 'dev-phone';

let stop: (() => void) | null = null;

beforeEach(useFakeTime);
afterEach(() => {
  stop?.();
  stop = null;
  hub.stop();
  vi.useRealTimers();
});

/** The machine's register of drafts, as internal/convo keeps it: a write is taken only as the next revision. */
function withDrafts(rt: FakeRuntime, on = true) {
  const book = new Map<string, Draft>();
  if (on) rt.welcome = { ...rt.welcome, features: [...rt.welcome.features, 'drafts'] };
  const writes: CommandBodies['draft.set'][] = [];
  /** The machine's convs snapshot carries its register as it is now. */
  const show = () => {
    rt.drafts = [...book.values()];
  };
  rt.handlers.set('draft.set', (cmd) => {
    const b = cmd.body as CommandBodies['draft.set'];
    writes.push(b);
    const cur = book.get(b.key);
    if (b.rev !== (cur?.rev ?? 0) + 1) return { r: 'refused', code: 'conflict' };
    const d: Draft = {
      key: b.key,
      text: b.text,
      attachments: b.attachments,
      rev: b.rev,
      deviceId: 'dev-here',
      at: Date.now(),
    };
    book.set(b.key, d);
    show();
    const seq = rt.emit('convs', { type: 'draft_upsert', draft: d });
    return { r: 'accepted', ch: 'convs', seq, rev: b.rev };
  });
  /** Another device of the person writes; `silent`: its event does not reach this page yet. */
  const other = (key: Draft['key'], text: string, silent = false) => {
    const d: Draft = { key, text, rev: (book.get(key)?.rev ?? 0) + 1, deviceId: OTHER, at: Date.now() };
    book.set(key, d);
    show();
    if (!silent) rt.emit('convs', { type: 'draft_upsert', draft: d });
    return d;
  };
  /** The machine loses what it kept of a draft (a restart before its saver ran, a draft past the ones it keeps). */
  const lose = (key: Draft['key'], back?: Draft) => {
    if (back) book.set(key, back);
    else book.delete(key);
    show();
  };
  return { book, writes, other, lose };
}

async function start(on = true) {
  const rt = new FakeRuntime();
  const machine = withDrafts(rt, on);
  const booted = await bootFake({ rt });
  stop = startDraftSync();
  await settle();
  return { ...booted, ...machine };
}

const typing = async (text: string, key: typeof CONV | typeof NEW_CHAT = CONV) => {
  setDraftText(key, text);
  await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 50);
};

describe('drafts on the machine', () => {
  it('writes the field a moment after it changes, each write the next revision', async () => {
    const { writes, book } = await start();
    setDraftText(CONV, 'Пров');
    setDraftText(CONV, 'Проверь');
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS - 100);
    expect(writes).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(200);
    expect(writes.map((w) => [w.text, w.rev])).toEqual([['Проверь', 1]]);
    await typing('Проверь миграцию');
    expect(book.get(CONV)).toMatchObject({ text: 'Проверь миграцию', rev: 2 });
    expect(draftBaseStore.get(CONV)).toEqual({ rev: 2, text: 'Проверь миграцию', attachments: [] });
    // Its own event changed nothing: no write went out for it.
    expect(writes).toHaveLength(2);
  });

  it('a pristine field takes what another device wrote', async () => {
    const { other } = await start();
    await typing('черновик');
    other(CONV, 'черновик, дописанный с телефона');
    await settle();
    expect(draftText(CONV)).toBe('черновик, дописанный с телефона');
    expect(draftBaseStore.get(CONV)?.rev).toBe(2);
  });

  it('a clear on another device clears a pristine field here, and nothing brings the text back', async () => {
    const { other, writes, book } = await start();
    await typing('Проверь миграцию');
    other(CONV, '');
    await settle();
    expect(draftsStore.has(CONV)).toBe(false);
    await vi.advanceTimersByTimeAsync(5000);
    expect(writes).toHaveLength(1);
    expect(book.get(CONV)).toMatchObject({ text: '', rev: 2 });
  });

  it('a field changed here keeps its text against another device, and its next write wins with a fresh revision', async () => {
    const { other, book } = await start();
    await typing('общий текст');
    setDraftText(CONV, 'общий текст, а это я печатаю');
    other(CONV, 'с телефона');
    await settle();
    expect(draftText(CONV)).toBe('общий текст, а это я печатаю');
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 50);
    expect(book.get(CONV)).toMatchObject({ text: 'общий текст, а это я печатаю', rev: 3 });
  });

  it('a stale write is refused, waits for the current draft and goes again on top of it', async () => {
    const { other, writes, book, rt } = await start();
    await typing('один');
    // The phone wrote, its event is late.
    const late = other(CONV, 'с телефона', true);
    await typing('один два');
    expect(writes.at(-1)).toEqual(expect.objectContaining({ text: 'один два', rev: 2 }));
    expect(book.get(CONV)?.text).toBe('с телефона');
    await vi.advanceTimersByTimeAsync(BEHIND_MS - CLOUD_DELAY_MS - 500);
    expect(writes).toHaveLength(2);
    rt.emit('convs', { type: 'draft_upsert', draft: late });
    await vi.advanceTimersByTimeAsync(50);
    expect(writes.at(-1)).toEqual(expect.objectContaining({ text: 'один два', rev: 3 }));
    expect(book.get(CONV)).toMatchObject({ text: 'один два', rev: 3 });
  });

  it("the event of the other device never comes: the page reads the machine's list and writes on top of it", async () => {
    const { other, book } = await start();
    await typing('один');
    other(CONV, 'с телефона', true);
    await typing('один два');
    expect(book.get(CONV)?.text).toBe('с телефона');
    await vi.advanceTimersByTimeAsync(BEHIND_MS + 100);
    expect(draftText(CONV)).toBe('один два');
    expect(book.get(CONV)).toMatchObject({ text: 'один два', rev: 3 });
  });

  it('a machine that lost the draft: after the refusal the field is saved again, and what was typed stays', async () => {
    const { book, lose, writes } = await start();
    await typing('первая мысль');
    await typing('первая мысль, вторая');
    expect(book.get(CONV)).toMatchObject({ rev: 2 });
    lose(CONV);
    await typing('первая мысль, вторая, третья');
    // Refused: the machine has no draft, the page writes revision 3.
    expect(writes.at(-1)).toMatchObject({ rev: 3 });
    expect(book.has(CONV)).toBe(false);
    await vi.advanceTimersByTimeAsync(BEHIND_MS + 100);
    expect(draftText(CONV)).toBe('первая мысль, вторая, третья');
    expect(book.get(CONV)).toMatchObject({ text: 'первая мысль, вторая, третья', rev: 1 });
    // And it keeps saving.
    await typing('первая мысль, вторая, третья, четвёртая');
    expect(book.get(CONV)).toMatchObject({ text: 'первая мысль, вторая, третья, четвёртая', rev: 2 });
  });

  it('a machine that fell back to an older revision: its revision becomes the base, the field wins', async () => {
    const { book, lose } = await start();
    await typing('раз');
    const first = book.get(CONV)!;
    await typing('раз два');
    lose(CONV, first);
    await typing('раз два три');
    await vi.advanceTimersByTimeAsync(BEHIND_MS + 100);
    expect(draftText(CONV)).toBe('раз два три');
    expect(book.get(CONV)).toMatchObject({ text: 'раз два три', rev: 2 });
  });

  it('a reload heals too: the snapshot of a machine behind the kept base rebases the field', async () => {
    const rt = new FakeRuntime();
    const { book } = withDrafts(rt);
    const booted = await bootFake({ rt, start: false });
    // What IndexedDB brought back: a field ahead of a base the machine does not have any more.
    draftBaseStore.set(CONV, { rev: 7, text: 'было', attachments: [] });
    setDraftText(CONV, 'было, и дописано до перезагрузки');
    stop = startDraftSync();
    hub.start([booted.m], booted.m);
    await settle();
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 50);
    expect(book.get(CONV)).toMatchObject({ text: 'было, и дописано до перезагрузки', rev: 1 });
  });

  it('typing without a pause writes once in two seconds; a pause writes at once', async () => {
    const { writes } = await start();
    for (let i = 1; i <= 10; i++) {
      setDraftText(CONV, 'а'.repeat(i));
      await vi.advanceTimersByTimeAsync(400);
    }
    // Four seconds of keys 400 ms apart: two writes, not one per pause of the old 700 ms and not none.
    expect(writes).toHaveLength(Math.floor((10 * 400) / CLOUD_EVERY_MS));
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS);
    expect(writes.at(-1)?.text).toBe('а'.repeat(10));
  });

  it('leaving the field or the page (flushDrafts) writes what waits, at once', async () => {
    const { writes } = await start();
    setDraftText(CONV, 'ухожу');
    flushDrafts();
    await vi.advanceTimersByTimeAsync(50);
    expect(writes.map((w) => w.text)).toEqual(['ухожу']);
    setDraftText(CONV, 'ухожу совсем');
    flushDrafts();
    await vi.advanceTimersByTimeAsync(50);
    expect(writes.map((w) => w.text)).toEqual(['ухожу', 'ухожу совсем']);
  });

  it('a send clears the draft on the machine too', async () => {
    const { book } = await start();
    await typing('отправлю сейчас');
    clearDraft(CONV);
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 50);
    expect(book.get(CONV)).toMatchObject({ text: '', rev: 2 });
  });

  it('a write whose answer was lost does not come back into the field after a send', async () => {
    const { rt, book } = await start();
    // The machine takes the write, its answer is lost and its event comes late (a loaded machine).
    const takes = rt.handlers.get('draft.set')!;
    rt.handlers.set('draft.set', (cmd) => {
      const b = cmd.body as CommandBodies['draft.set'];
      const d: Draft = { key: b.key, text: b.text, rev: b.rev, deviceId: 'dev-here', at: Date.now() };
      book.set(b.key, d);
      setTimeout(() => rt.emit('convs', { type: 'draft_upsert', draft: d }), 3000);
      rt.handlers.set('draft.set', takes);
      return { r: 'accepted', ch: 'convs', seq: 0, rev: b.rev };
    });
    rt.loseAnswers = 1;
    await typing('отправлю сейчас');
    clearDraft(CONV); // sent
    await vi.advanceTimersByTimeAsync(3000);
    await settle();
    expect(draftsStore.has(CONV) ? draftText(CONV) : '').toBe('');
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 50);
    expect(book.get(CONV)).toMatchObject({ text: '', rev: 2 });
  });

  it('the new-chat field goes to the active machine under its own key', async () => {
    const { book } = await start();
    await typing('новый чат про', NEW_CHAT);
    expect(book.get('new')).toMatchObject({ text: 'новый чат про', rev: 1 });
  });

  it('takes the drafts of the snapshot into the empty fields, and writes nothing back', async () => {
    const rt = new FakeRuntime();
    const { writes } = withDrafts(rt);
    rt.drafts = [{ key: 'new', text: 'с ноутбука', rev: 5, deviceId: OTHER, at: 1 }];
    await bootFake({ rt });
    stop = startDraftSync();
    await settle();
    expect(draftText(NEW_CHAT)).toBe('с ноутбука');
    expect(draftBaseStore.get(NEW_CHAT)?.rev).toBe(5);
    expect(writes).toHaveLength(0);
  });

  it('a runtime without drafts gets no writes; the field stays on this device', async () => {
    const { writes } = await start(false);
    await typing('только здесь');
    await vi.advanceTimersByTimeAsync(10_000);
    expect(writes).toHaveLength(0);
    expect(draftText(CONV)).toBe('только здесь');
  });

  it('a file attached here goes to the machine with the text, and a clear elsewhere takes it away', async () => {
    const { book, other } = await start();
    const shot = { id: 'up_a1', name: 'CleanShot.png', kind: 'image', mime: 'image/png' } as const;
    setDraft(CONV, { text: 'смотри', attachments: [shot] });
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 50);
    expect(book.get(CONV)).toMatchObject({ text: 'смотри', attachments: [shot], rev: 1 });
    other(CONV, '');
    await settle();
    expect(draftsStore.has(CONV)).toBe(false);
  });

  it('IndexedDB keeps the revision with the field, an empty one too', async () => {
    const kv = memoryKV();
    const { other } = await start();
    const persistence = startPersistence(kv);
    await persistence.ready;
    await typing('до перезагрузки');
    other(CONV, '');
    await settle();
    await persistence.flushNow();
    expect(kv.dump().drafts.get(CONV)).toEqual({
      text: '',
      attachments: [],
      base: { rev: 2, text: '', attachments: [] },
    });
    persistence.stop();
  });
});

describe('what IndexedDB kept, when it comes back late', () => {
  const shot = { id: 'up_a1', name: 'CleanShot.png', kind: 'image', mime: 'image/png' } as const;

  /** A KV whose reads answer when the test says. */
  function slowKV(kept: Record<string, unknown>) {
    const kv = memoryKV();
    for (const [k, v] of Object.entries(kept)) void kv.set('drafts', k, v);
    let open: () => void = () => undefined;
    const gate = new Promise<void>((resolve) => (open = resolve));
    const all: typeof kv.all = async (area) => {
      const rows = await kv.all(area);
      await gate;
      return rows as never;
    };
    return { kv: { ...kv, all }, open };
  }

  it('does not put back a field sent before the read finished', async () => {
    await start(false);
    const { kv, open } = slowKV({ [CONV]: { text: 'смотри', attachments: [shot] } });
    setDraft(CONV, { text: 'смотри', attachments: [shot] });
    const persistence = startPersistence(kv);
    clearDraft(CONV);
    open();
    await persistence.ready;
    expect(draftsStore.has(CONV)).toBe(false);
    persistence.stop();
  });

  it('does not put back a field the machine has moved past', async () => {
    const { other } = await start();
    const { kv, open } = slowKV({
      [CONV]: { text: 'старое', attachments: [shot], base: { rev: 1, text: 'старое', attachments: [shot] } },
    });
    const persistence = startPersistence(kv);
    other(CONV, 'отправлено с телефона');
    other(CONV, '');
    await settle();
    open();
    await persistence.ready;
    expect(draftsStore.has(CONV)).toBe(false);
    expect(draftBaseStore.get(CONV)?.rev).toBe(2);
    persistence.stop();
  });
});
