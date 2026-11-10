// The safe through the SDK (HRN-861): secrets.put reaches the host with the name and the value; the app's own secret
// is quiet in the agent's turn, the person's shared one is asked about by its name, never by its value.
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';

const context: IViewContext = {
  app: { id: 'acme.helper', version: '1' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'dark', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

interface IHl {
  secrets: { put: (n: string, v: string, o?: object) => Promise<unknown> };
}

const view = async (permissions: string[], handlers: IBridgeHandlers) => {
  const r = new FrameRig();
  const bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.helper', version: '1', permissions, targets: ['pane'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
  });
  r.bridge = bridge;
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  return { hl: await harnessly.connect(), bridge };
};

describe('the safe through the SDK', () => {
  it('puts the own secret and the shared one', async () => {
    const secretsPut = vi.fn(async () => ({ created: true }));
    const { hl } = await view(['secrets.write', 'secrets.write:GITHUB_TOKEN'], { secretsPut });
    await expect(hl.secrets.put('token', 'v-1234')).resolves.toEqual({ created: true });
    expect(secretsPut).toHaveBeenCalledWith({ name: 'token', value: 'v-1234', shared: false });
    await hl.secrets.put('GITHUB_TOKEN', 'v-5678', { shared: true });
    expect(secretsPut).toHaveBeenLastCalledWith({ name: 'GITHUB_TOKEN', value: 'v-5678', shared: true });
  });

  it('refuses without the right', async () => {
    const secretsPut = vi.fn();
    const { hl } = await view([], { secretsPut });
    await expect(hl.secrets.put('token', 'v-1234')).rejects.toMatchObject({ code: 'permission_denied' });
    expect(secretsPut).not.toHaveBeenCalled();
  });

  it('in the agent turn asks about the shared secret by its name only', async () => {
    const secretsPut = vi.fn(async () => ({ created: false }));
    const elevate = vi.fn(async () => true);
    const { hl, bridge } = await view(['secrets.write', 'secrets.write:GITHUB_TOKEN'], {
      secretsPut,
      elevate,
    });
    bridge.agentCall('t1');
    await hl.secrets.put('token', 'own-1234');
    await settle();
    expect(elevate).not.toHaveBeenCalled();
    await hl.secrets.put('GITHUB_TOKEN', 'shared-1234', { shared: true });
    await settle();
    expect(elevate).toHaveBeenCalledWith('harnessly/secrets.put', '"GITHUB_TOKEN"');
    expect(secretsPut).toHaveBeenLastCalledWith({
      name: 'GITHUB_TOKEN',
      value: 'shared-1234',
      shared: true,
      confirmed: true,
    });
  });
});
