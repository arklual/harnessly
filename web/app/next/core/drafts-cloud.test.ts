import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CLOUD_DELAY_MS, startDraftSync } from '@/next/core/drafts-cloud';
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
    const seq = rt.emit('convs', { type: 'draft_upsert', draft: d });
    return { r: 'accepted', ch: 'convs', seq, rev: b.rev };
  });
  /** Another device of the person writes; `silent`: its event does not reach this page yet. */
  const other = (key: Draft['key'], text: string, silent = false) => {
    const d: Draft = { key, text, rev: (book.get(key)?.rev ?? 0) + 1, deviceId: OTHER, at: Date.now() };
    book.set(key, d);
    if (!silent) rt.emit('convs', { type: 'draft_upsert', draft: d });
    return d;
  };
  return { book, writes, other };
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
    await vi.advanceTimersByTimeAsync(10_000);
    expect(writes).toHaveLength(2);
    rt.emit('convs', { type: 'draft_upsert', draft: late });
    await vi.advanceTimersByTimeAsync(50);
    expect(writes.at(-1)).toEqual(expect.objectContaining({ text: 'один два', rev: 3 }));
    expect(book.get(CONV)).toMatchObject({ text: 'один два', rev: 3 });
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
