// What an app spends of the person's model: the SDK in a frame, the real bridge; ai.complete and agents.run
// reach the host's handlers with their bounds, need their rights, and in the agent's turn ask the person with the
// prompt.
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
  ai: { complete: (p: string, o?: object) => Promise<unknown> };
  agents: { run: (p: string, o?: object) => Promise<unknown> };
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

describe('the model and the agent through the SDK', () => {
  it('asks the model and starts a chat with the given words', async () => {
    const ai = vi.fn(async () => ({ text: 'Париж', usd: 0.001, spentToday: 0.001, budget: 0.5 }));
    const agentsRun = vi.fn(async () => ({ id: 'tab-new' }));
    const { hl } = await view(['ai', 'agents.run'], { ai, agentsRun });
    await expect(hl.ai.complete('Столица?', { model: 'smart', maxTokens: 50 })).resolves.toMatchObject({
      text: 'Париж',
    });
    expect(ai).toHaveBeenCalledWith({ prompt: 'Столица?', system: '', maxTokens: 50, model: 'smart' });
    await expect(hl.agents.run('Проверь тесты', { title: 'Тесты' })).resolves.toEqual({ id: 'tab-new' });
    expect(agentsRun).toHaveBeenCalledWith({ prompt: 'Проверь тесты', title: 'Тесты' });
  });

  it('refuses without the rights', async () => {
    const ai = vi.fn();
    const agentsRun = vi.fn();
    const { hl } = await view([], { ai, agentsRun });
    await expect(hl.ai.complete('x')).rejects.toMatchObject({ code: 'permission_denied' });
    await expect(hl.agents.run('x')).rejects.toMatchObject({ code: 'permission_denied' });
    expect(ai).not.toHaveBeenCalled();
    expect(agentsRun).not.toHaveBeenCalled();
  });

  it('in the agent turn asks the person with the prompt', async () => {
    const agentsRun = vi.fn(async () => ({ id: 'tab-new' }));
    const elevate = vi.fn(async () => true);
    const { hl, bridge } = await view(['agents.run'], { agentsRun, elevate });
    bridge.agentCall('t1');
    await hl.agents.run('удали всё');
    await settle();
    expect(elevate).toHaveBeenCalledWith('harnessly/agents.run', '"удали всё"');
    expect(agentsRun).toHaveBeenCalledWith({ prompt: 'удали всё', title: '', confirmed: true });
  });
});
