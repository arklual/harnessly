// The workspace beyond one file, git and the chat's events: the SDK in a frame, the real bridge, the shim.
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { eventOfEntry } from '@/next/features/app-views/chat-events';
import { parseFileChange } from '@/next/features/app-views/file-events';
import { accepts } from '@/next/features/app-views/file-pick';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';
import type { Entry } from '@/next/protocol/v2';

const context: IViewContext = {
  app: { id: 'acme.changes', version: '1' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'dark', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

type TAsync = (...a: unknown[]) => Promise<unknown>;
interface IHl {
  files: {
    list: TAsync;
    search: TAsync;
    watch: (fn: (p: string[]) => void) => Promise<() => Promise<void>>;
    pick: TAsync;
  };
  git: { status: TAsync; diff: TAsync; log: TAsync };
  events: { on: (kind: string, fn: (e: unknown) => void) => Promise<unknown> };
  has: (name: string) => boolean;
  refused: (v: unknown) => boolean;
}

const view = async (permissions: string[], handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  r.bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.changes', version: '1', permissions, targets: ['pane'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  const hl = await harnessly.connect({ protocol: 2 });
  return { r, hl };
};

describe('the workspace, git and events through the SDK', () => {
  it('lists, searches and watches files, and hears only the changes of its watch', async () => {
    const filesMore = vi.fn(async (op: string) => {
      if (op === 'list') return { entries: [{ path: 'src', dir: true }] };
      if (op === 'search') return { hits: [{ path: 'src/a.go', line: 2, text: 'TODO' }], cut: false };
      return { ok: true };
    });
    const { r, hl } = await view(['files.read:**', 'files.watch'], { filesMore });
    expect(await hl.files.list('')).toEqual([{ path: 'src', dir: true }]);
    expect(await hl.files.search('todo', { glob: 'src/**' })).toMatchObject({ hits: [{ line: 2 }] });
    expect(filesMore).toHaveBeenCalledWith('search', {
      query: 'todo',
      glob: 'src/**',
      case: false,
      limit: 0,
    });
    const heard: string[][] = [];
    const stop = await hl.files.watch((p) => heard.push(p));
    expect(filesMore).toHaveBeenCalledWith('watch', {});
    r.bridge.filesChanged(['src/a.go']);
    await settle();
    expect(heard).toEqual([['src/a.go']]);
    await stop();
    expect(filesMore).toHaveBeenCalledWith('unwatch', {});
  });

  it('refuses a watch without files.watch, as an answer at protocol 2', async () => {
    const { hl } = await view(['files.read:**'], { filesMore: async () => ({}) });
    expect(hl.has('files.watch')).toBe(false);
    const got = await hl.files.watch(() => undefined);
    expect(hl.refused(got)).toBe(true);
  });

  it('reads git through the host and refuses it without git.read', async () => {
    const git = vi.fn(async (op: string) =>
      op === 'status' ? { branch: 'main', files: [{ path: 'a.txt', unstaged: 'M' }] } : { commits: [] },
    );
    const { hl } = await view(['git.read'], { git });
    expect(await hl.git.status()).toMatchObject({ branch: 'main' });
    await hl.git.diff({ path: 'a.txt' });
    expect(git).toHaveBeenCalledWith('diff', { path: 'a.txt', staged: false, ref: '' });
    const none = await view([], { git });
    expect(none.hl.refused(await none.hl.git.status())).toBe(true);
  });

  it('tells a subscribed kind of event, and nothing of a kind the app does not hold', async () => {
    const { r, hl } = await view(['events:turn'], {});
    const turns: unknown[] = [];
    await hl.events.on('turn', (e) => turns.push(e));
    expect(hl.refused(await hl.events.on('tool', () => undefined))).toBe(true);
    r.bridge.event('turn', { state: 'ready', at: 5 });
    r.bridge.event('tool', { tool: 'Bash', at: 6 });
    await settle();
    expect(turns).toEqual([{ kind: 'turn', state: 'ready', at: 5 }]);
  });

  it('gives the file the person picked, and null when they close the dialog', async () => {
    const pick = vi.fn(async () => ({ path: 'notes.md', text: '# hi' }));
    const { hl } = await view([], { pick });
    expect(await hl.files.pick({ accept: ['.md'] })).toEqual({ path: 'notes.md', text: '# hi' });
    expect(pick).toHaveBeenCalledWith(['.md']);
    pick.mockResolvedValueOnce(null as never);
    expect(await hl.files.pick()).toBeNull();
  });
});

describe('the pieces of the page', () => {
  it('says what an entry of the feed is, without its content', () => {
    const base = { id: 'e1', turnId: null, at: 9, rev: 1 };
    expect(
      eventOfEntry({
        ...base,
        t: 'tool',
        toolUseId: 'u',
        tool: 'Bash',
        summary: 'rm -rf',
        ok: null,
        output: null,
      } as Entry),
    ).toEqual({
      kind: 'tool',
      params: { tool: 'Bash', at: 9 },
    });
    expect(
      eventOfEntry({ ...base, t: 'user', text: 'secret', attachments: [], clientId: null } as Entry),
    ).toEqual({
      kind: 'message',
      params: { role: 'person', at: 9 },
    });
    expect(eventOfEntry({ ...base, t: 'think', text: 'x' } as Entry)).toBeNull();
  });

  it('reads a change of the file stream and the endings a pick accepts', () => {
    expect(parseFileChange('{"session":"s","paths":["a",1]}')).toEqual({ session: 's', paths: ['a'] });
    expect(parseFileChange('nope')).toBeNull();
    expect(accepts([], 'x.bin')).toBe(true);
    expect(accepts(['.MD'], 'docs/a.md')).toBe(true);
    expect(accepts(['.md'], 'a.txt')).toBe(false);
  });
});
