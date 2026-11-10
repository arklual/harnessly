// Commands, processes and the pane's browser (HRN-851): the SDK in a frame, the real bridge, the shim; and the agent
// that does not get more through an app than the person allowed.
import { describe, expect, it, vi } from 'vitest';
import { AppBridge, type IBridgeHandlers, type IViewContext } from '@/next/features/app-views/bridge';
import { classicSdk, FrameRig, NONCE, settle, SLOT } from '@/next/features/app-views/frame-rig';

const context: IViewContext = {
  app: { id: 'acme.runner', version: '1' },
  target: 'pane',
  view: 'main',
  theme: { mode: 'dark', tokens: {} },
  locale: 'ru',
  chat: { id: 'c1' },
  user: null,
};

type TAsync = (...a: unknown[]) => Promise<unknown>;
interface IHl {
  terminal: { run: TAsync };
  process: { start: TAsync; stop: TAsync; list: TAsync; output: TAsync };
  preview: { open: TAsync };
  tools: { register: (tool: object, run: (a: unknown) => unknown) => Promise<unknown> };
  has: (name: string) => boolean;
  refused: (v: unknown) => boolean;
}

const view = async (permissions: string[], handlers: IBridgeHandlers, now?: () => number) => {
  const r = new FrameRig();
  const bridge = new AppBridge({
    frame: () => r.proxy,
    origin: SLOT,
    app: { id: 'acme.runner', version: '1', permissions, targets: ['pane'] },
    context,
    handlers,
    hostVersion: '0.17.0',
    nonce: NONCE,
    ...(now ? { now } : {}),
  });
  r.bridge = bridge;
  const dom = r.load('<html><head></head><body></body></html>', NONCE, [classicSdk()]);
  const harnessly = (dom.window as unknown as { harnessly: { connect: (o?: object) => Promise<IHl> } })
    .harnessly;
  const hl = await harnessly.connect({ protocol: 2 });
  return { r, hl, bridge };
};

describe('commands and processes through the SDK', () => {
  it('runs a command, starts, reads, lists and stops a process, and opens a port in the pane', async () => {
    const run = vi.fn(async (op: string, p: Record<string, unknown>) => {
      if (op === 'run') return { code: 0, output: 'ok\n', ms: 12 };
      if (op === 'start') return { id: 'p1', command: p.command, startedAt: 1, running: true };
      if (op === 'output')
        return { id: 'p1', command: 'dev', startedAt: 1, running: true, text: 'ready', next: 5 };
      if (op === 'list') return { processes: [{ id: 'p1', command: 'dev', startedAt: 1, running: true }] };
      if (op === 'stop') return { id: 'p1', command: 'dev', startedAt: 1, running: false, code: 0 };
      return { ok: true };
    });
    const previewOpen = vi.fn(async () => undefined);
    const { hl } = await view(['terminal.run:npm test*', 'process'], { run, previewOpen });
    expect(hl.has('terminal.run')).toBe(true);
    expect(await hl.terminal.run('npm test', { timeout: 30 })).toMatchObject({ code: 0, output: 'ok\n' });
    expect(run).toHaveBeenCalledWith('run', { command: 'npm test', cwd: '', timeout: 30 });
    expect(await hl.process.start('npm run dev')).toMatchObject({ id: 'p1', running: true });
    expect(await hl.process.output('p1', { since: 0 })).toMatchObject({ text: 'ready', next: 5 });
    expect(await hl.process.list()).toHaveLength(1);
    expect(await hl.process.stop('p1')).toMatchObject({ running: false });
    await hl.preview.open(5173, { path: '/app' });
    // The runtime checks the right and journals it before the pane opens.
    expect(run).toHaveBeenCalledWith('preview', { port: 5173 });
    expect(previewOpen).toHaveBeenCalledWith(5173, '/app');
    expect(hl.refused(await hl.preview.open(80))).toBe(true);
  });

  it('refuses without the rights, as an answer', async () => {
    const run = vi.fn(async () => ({}));
    const { hl } = await view([], { run });
    expect(hl.has('terminal.run')).toBe(false);
    expect(hl.refused(await hl.terminal.run('ls'))).toBe(true);
    expect(hl.refused(await hl.process.start('npm run dev'))).toBe(true);
    expect(run).not.toHaveBeenCalled();
  });

  it('asks the person in the host dialog when the agent drives the view, not when the person does', async () => {
    let t = 1000;
    const run = vi.fn(async () => ({ code: 0, output: '', ms: 1 }));
    const elevate = vi.fn(async () => false);
    const answered = vi.fn();
    const { hl, bridge } = await view(
      ['terminal.run', 'process', 'tools.page'],
      { run, elevate, pageTools: () => undefined, pageToolResult: answered },
      () => t,
    );
    await hl.tools.register({ name: 'noop' }, () => ({ ok: true }));
    await hl.terminal.run('npm test');
    expect(elevate).not.toHaveBeenCalled();

    bridge.agentCall('call-1');
    const refused = await hl.terminal.run('rm -rf .');
    expect(elevate).toHaveBeenCalledWith('harnessly/terminal.run', '"rm -rf ."');
    expect(hl.refused(refused)).toBe(true);
    expect(run).toHaveBeenCalledTimes(1);
    // Reading a process's output asks nothing.
    await hl.process.list();
    expect(elevate).toHaveBeenCalledTimes(1);

    elevate.mockResolvedValueOnce(true);
    await hl.process.start('npm run dev');
    expect(run).toHaveBeenCalledWith('start', { confirmed: true, command: 'npm run dev', cwd: '' });

    // The view answers the agent's call: for a moment after it the calls are still taken for the agent's.
    bridge.notify('harnessly/notifications/tool-call', { id: 'call-1', name: 'noop', arguments: {} });
    await settle();
    expect(answered).toHaveBeenCalledWith('call-1', { result: { ok: true } });
    await hl.terminal.run('make deploy');
    expect(elevate).toHaveBeenCalledTimes(3);
    t += 40_000;
    await hl.terminal.run('npm test');
    expect(elevate).toHaveBeenCalledTimes(3);
  });

  it('shows a command whole, line breaks as they are, and refuses one too long to show', async () => {
    const run = vi.fn(async () => ({ code: 0 }));
    const elevate = vi.fn(async () => true);
    const { hl, bridge } = await view(['terminal.run'], { run, elevate });
    bridge.agentCall('call-1');
    await hl.terminal.run('npm test\nrm -rf ~');
    expect(elevate).toHaveBeenCalledWith('harnessly/terminal.run', '"npm test\\nrm -rf ~"');
    expect(hl.refused(await hl.terminal.run(`npm test ${'x'.repeat(2000)}`))).toBe(true);
    expect(run).toHaveBeenCalledTimes(1);
  });

  it('refuses a loud call in the agent turn when the host has no dialog', async () => {
    const run = vi.fn(async () => ({ code: 0 }));
    const { hl, bridge } = await view(['terminal.run'], { run });
    bridge.agentCall('call-1');
    expect(hl.refused(await hl.terminal.run('ls'))).toBe(true);
    expect(run).not.toHaveBeenCalled();
  });
});
