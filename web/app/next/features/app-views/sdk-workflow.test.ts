// The status of a document and the person's files: the SDK in a frame, the real bridge, the shim.
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, SLOT } from '@/next/features/app-views/frame-rig';

const context: IViewContext = {
  app: { id: 'acme.proto', version: '1' },
  target: 'sinopia.acme.proto',
  view: 'editor',
  theme: { mode: 'light', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: { name: 'Анна', role: 'edit' },
};

type TAsync = (...a: unknown[]) => Promise<unknown>;
interface IHl {
  context: IViewContext;
  workflow: { get: TAsync; move: TAsync };
  files: { upload: TAsync };
  refused: (v: unknown) => boolean;
}

const view = async (permissions: string[], handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  r.bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.proto', version: '1', permissions, targets: ['sinopia.acme.proto'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  return { dom, hl: await harnessly.connect({ protocol: 2 }) };
};

describe('hl.workflow and hl.files.upload', () => {
  it('tells the view who the person is', async () => {
    const { hl } = await view([], {});
    expect(hl.context.user).toEqual({ name: 'Анна', role: 'edit' });
  });

  it('reads the status and makes a move of the version the view saw', async () => {
    const state = { version: 2, hash: 'h', status: { id: 'draft', title: 'Черновик' }, moves: [] };
    const workflow = vi.fn(async () => state);
    const { hl } = await view([], { workflow });
    expect(await hl.workflow.get()).toEqual(state);
    expect(workflow).toHaveBeenLastCalledWith('get');
    await hl.workflow.move('review', { version: 2, hash: 'h', note: 'готово' });
    expect(workflow).toHaveBeenLastCalledWith('move', {
      to: 'review',
      version: 2,
      hash: 'h',
      note: 'готово',
    });
    expect(hl.refused(await hl.workflow.move('review', { version: 0 }))).toBe(true);
  });

  it('puts the bytes of a file the person gave into the workspace, with the write right only', async () => {
    const upload = vi.fn(async (path: string) => ({ path, size: 3 }));
    const { dom, hl } = await view(['files.write:uploads/**'], { upload });
    const W = dom.window as unknown as { File: typeof File };
    const file = new W.File([new Uint8Array([0, 255, 7])], 'shot.png', { type: 'image/png' });
    expect(await hl.files.upload(file)).toEqual({ path: 'uploads/shot.png', size: 3 });
    expect(upload).toHaveBeenCalledWith('uploads/shot.png', 'AP8H');
    const bare = await view([], { upload });
    expect(bare.hl.refused(await bare.hl.files.upload(file, 'uploads/x.png'))).toBe(true);
  });
});
