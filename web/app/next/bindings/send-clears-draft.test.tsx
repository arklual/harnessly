// @vitest-environment jsdom
// After a send nothing of the message stays in the field: not its text, not its files — on every way to send, after
// the page writes IndexedDB, and after the machine shows the drafts back.
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { UploadOutcome } from '@/next/commands/uploads';

vi.mock('@/next/bindings/use-limits', () => ({ useAgentLimits: () => null }));
vi.mock('@/next/bindings/use-context-window', () => ({ useContextWindow: () => null }));
const uploads: ((o: UploadOutcome) => void)[] = [];
vi.mock('@/next/commands/uploads', async (load) => ({
  ...(await load<typeof import('@/next/commands/uploads')>()),
  uploadFile: () => new Promise<UploadOutcome>((resolve) => uploads.push(resolve)),
}));

import ChatDock from '@/next/bindings/chat-dock';
import HomeComposer from '@/next/bindings/home-composer';
import { CLOUD_DELAY_MS, startDraftSync } from '@/next/core/drafts-cloud';
import { memoryKV, startPersistence, type Persistence } from '@/next/core/persist';
import { hub } from '@/next/core/realtime/hub';
import { FakeRuntime } from '@/next/core/testing/fake-runtime';
import { bootFake, settle, useFakeTime } from '@/next/core/testing/setup';
import { t } from '@/next/i18n';
import type { AttachmentRef, CommandBodies, ConvId, Draft, MachineId } from '@/next/protocol/v2';
import { conversationsStore } from '@/next/stores/conversations';
import { draftBaseStore } from '@/next/stores/draft-sync';
import { draftsStore, NEW_CHAT, setDraft } from '@/next/stores/drafts';

const CONV = 'tab-k3Jx9QmZ' as ConvId;
const SHOTS: AttachmentRef[] = [
  { id: 'up_a1', name: 'CleanShot 1.png', kind: 'image', mime: 'image/png' },
  { id: 'up_b2', name: 'CleanShot 2.png', kind: 'image', mime: 'image/png' },
];

let rt: FakeRuntime;
let machine: MachineId;
let kv: ReturnType<typeof memoryKV>;
let persistence: Persistence;
let stopSync: () => void;
let book: Map<string, Draft>;

const field = () => screen.getByRole('textbox', { name: t('next.composer.label') }) as HTMLTextAreaElement;
const chips = () => screen.queryAllByRole('button', { name: /CleanShot|notes\.txt/ });
const sent = () => rt.commands.filter((c) => c.op === 'send' || c.op === 'conversation.create');

beforeEach(async () => {
  useFakeTime();
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  );
  rt = new FakeRuntime();
  rt.welcome = { ...rt.welcome, features: [...rt.welcome.features, 'drafts'] };
  book = new Map();
  rt.handlers.set('draft.set', (cmd) => {
    const b = cmd.body as CommandBodies['draft.set'];
    if (b.rev !== (book.get(b.key)?.rev ?? 0) + 1) return { r: 'refused', code: 'conflict' };
    const d: Draft = { ...b, deviceId: 'dev-here', at: Date.now() };
    delete (d as Partial<CommandBodies['draft.set']>).opId;
    book.set(b.key, d);
    return {
      r: 'accepted',
      ch: 'convs',
      seq: rt.emit('convs', { type: 'draft_upsert', draft: d }),
      rev: b.rev,
    };
  });
  ({ m: machine } = await bootFake({ rt }));
  kv = memoryKV();
  persistence = startPersistence(kv);
  await persistence.ready;
  stopSync = startDraftSync();
  uploads.length = 0;
});
afterEach(() => {
  cleanup();
  stopSync();
  persistence.stop();
  hub.stop();
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

/** Everything that writes later has written: IndexedDB, the machine, its event back. */
async function afterAWhile() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(CLOUD_DELAY_MS + 1000);
  });
  await persistence.flushNow();
}

async function expectEmptyEverywhere(key: ConvId | typeof NEW_CHAT) {
  await afterAWhile();
  expect(field().value).toBe('');
  expect(chips()).toHaveLength(0);
  expect(draftsStore.has(key)).toBe(false);
  const kept = kv.dump().drafts.get(key) as { text: string; attachments: AttachmentRef[] } | undefined;
  expect(kept?.attachments ?? []).toEqual([]);
  expect(kept?.text ?? '').toBe('');
  const onMachine = book.get(key === NEW_CHAT ? 'new' : key);
  expect(onMachine?.attachments ?? []).toEqual([]);
  expect(onMachine?.text ?? '').toBe('');
}

/** A field with words and two screenshots that the machine and IndexedDB already have. */
async function composed(key: ConvId | typeof NEW_CHAT) {
  act(() => setDraft(key, { text: 'смотри скрины', attachments: SHOTS }));
  await afterAWhile();
  expect(book.get(key === NEW_CHAT ? 'new' : key)?.attachments).toHaveLength(2);
  expect(chips()).toHaveLength(2);
}

const working = () =>
  act(() =>
    conversationsStore.patch(CONV, (c) =>
      c ? { ...c, state: { k: 'working', since: Date.now(), turnId: 'x:1#0', compacting: false } } : c,
    ),
  );

describe('a send leaves an empty field', () => {
  it('Enter to an idle agent', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    await composed(CONV);
    fireEvent.keyDown(field(), { key: 'Enter' });
    expect(sent()).toHaveLength(0);
    await settle();
    expect(sent().at(-1)?.body).toMatchObject({ text: 'смотри скрины', attachmentIds: ['up_a1', 'up_b2'] });
    await expectEmptyEverywhere(CONV);
  });

  it('Enter into the queue of a working agent', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    await composed(CONV);
    working();
    fireEvent.keyDown(field(), { key: 'Enter' });
    await settle();
    expect(sent().at(-1)?.body).toMatchObject({ mode: 'queue' });
    await expectEmptyEverywhere(CONV);
  });

  it('Ctrl+Enter, straight into the working turn', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    await composed(CONV);
    working();
    fireEvent.keyDown(field(), { key: 'Enter', ctrlKey: true });
    await settle();
    expect(sent().at(-1)?.body).toMatchObject({ mode: 'steer' });
    await expectEmptyEverywhere(CONV);
  });

  it('from the start page, once the chat is made', async () => {
    const onCreated = vi.fn();
    render(<HomeComposer machine={machine} conversations={rt.conversations} onCreated={onCreated} />);
    await composed(NEW_CHAT);
    fireEvent.keyDown(field(), { key: 'Enter' });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(50);
    });
    expect(onCreated).toHaveBeenCalled();
    expect(sent().at(-1)?.op).toBe('conversation.create');
    await expectEmptyEverywhere(NEW_CHAT);
  });

  it('a file still uploading holds the send; once it is up the send takes it and the field empties', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    fireEvent.change(field(), { target: { value: 'и файл' } });
    await act(async () => {
      fireEvent.paste(field(), {
        clipboardData: {
          files: [new File(['x'], 'notes.txt', { type: 'text/plain' })],
          items: [],
          types: ['Files'],
        },
      });
      await settle();
    });
    expect(uploads).toHaveLength(1);
    fireEvent.keyDown(field(), { key: 'Enter' });
    await settle();
    expect(sent()).toHaveLength(0);
    expect(field().value).toBe('и файл');
    const ref: AttachmentRef = { id: 'up_n1', name: 'notes.txt', kind: 'file', mime: 'text/plain' };
    await act(async () => {
      uploads[0]({ ok: true, ref });
      await settle();
    });
    fireEvent.keyDown(field(), { key: 'Enter' });
    await settle();
    expect(sent().at(-1)?.body).toMatchObject({ text: 'и файл', attachmentIds: ['up_n1'] });
    await expectEmptyEverywhere(CONV);
  });
});

describe('a send elsewhere', () => {
  it('takes the files away from a field here that still held them (another tab, a reload from IndexedDB)', async () => {
    render(<ChatDock conv={CONV} machine={machine} />);
    await composed(CONV);
    // The other tab sent: the machine has the field empty, one revision up.
    const rev = (draftBaseStore.get(CONV)?.rev ?? 0) + 1;
    const clear: Draft = { key: CONV, text: '', rev, deviceId: 'dev-other-tab', at: Date.now() };
    book.set(CONV, clear);
    await act(async () => {
      rt.emit('convs', { type: 'draft_upsert', draft: clear });
      await settle();
    });
    await expectEmptyEverywhere(CONV);
  });
});
