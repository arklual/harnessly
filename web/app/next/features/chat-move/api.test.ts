import { afterEach, describe, expect, it, vi } from 'vitest';
import { fetchMoveStatus, fetchMoveTargets, MoveError, postMove } from '@/next/features/chat-move/api';

const answer = (status: number, body: unknown) =>
  vi.fn().mockResolvedValue({ ok: status < 400, status, json: async () => body });

afterEach(() => vi.unstubAllGlobals());

describe('chat move gateway client', () => {
  it('reads «off, not moved» wherever the gateway does not answer', async () => {
    vi.stubGlobal('fetch', answer(404, { error: 'off', code: 'disabled' }));
    expect(await fetchMoveStatus('vm1')).toEqual({ enabled: false, moved: null });
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('down')));
    expect(await fetchMoveStatus('vm1', 'tab-1')).toEqual({ enabled: false, moved: null });
  });

  it('asks for the machine and the chat, and reads where a chat went', async () => {
    const moved = { toMachine: 'vm2', toName: 'Roma VM', toConv: 'tab-2', at: 5 };
    const fetchMock = answer(200, { enabled: true, moved });
    vi.stubGlobal('fetch', fetchMock);
    expect(await fetchMoveStatus('vm1', 'tab-1')).toEqual({ enabled: true, moved });
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain('/chat-move/status?machine=vm1&conv=tab-1');
  });

  it('carries the gateway words and code of a refusal', async () => {
    vi.stubGlobal('fetch', answer(409, { error: 'The commit is missing', code: 'commit_missing' }));
    const failure = await postMove({
      opId: 'op-1',
      machineId: 'vm1',
      convId: 'tab-1',
      targetMachineId: 'vm2',
      provider: 'claude',
      model: { id: 'opus' },
      withoutFiles: false,
    }).catch((e: unknown) => e);
    expect(failure).toBeInstanceOf(MoveError);
    expect(failure).toMatchObject({ message: 'The commit is missing', code: 'commit_missing', status: 409 });
  });

  it('sends the move as one POST with its operation id and reads the new chat', async () => {
    const fetchMock = answer(200, {
      ok: true,
      targetMachineId: 'vm2',
      targetName: 'Roma VM',
      convId: 'tab-9',
      existing: false,
    });
    vi.stubGlobal('fetch', fetchMock);
    const result = await postMove({
      opId: 'op-1',
      machineId: 'vm1',
      convId: 'tab-1',
      targetMachineId: 'vm2',
      provider: 'claude',
      model: { id: 'opus' },
      withoutFiles: true,
    });
    expect(result).toEqual({ targetMachineId: 'vm2', targetName: 'Roma VM', convId: 'tab-9' });
    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    expect(init.method).toBe('POST');
    expect(JSON.parse(String(init.body))).toMatchObject({ opId: 'op-1', withoutFiles: true });
  });

  it('refuses a targets answer of the wrong shape', async () => {
    vi.stubGlobal('fetch', answer(200, { nope: true }));
    await expect(fetchMoveTargets('vm1', 'tab-1')).rejects.toBeInstanceOf(MoveError);
  });

  it('asks the fleet hub first and goes on asking it while it answers', async () => {
    const fetchMock = answer(200, { enabled: true, moved: null });
    vi.stubGlobal('fetch', fetchMock);
    expect(await fetchMoveStatus('hubvm', 'tab-1')).toEqual({ enabled: true, moved: null });
    expect(String(fetchMock.mock.calls[0]?.[0])).toMatch(/\/api\/v2\/chat-move\/status\?machine=hubvm/);
    const targets = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ source: { ok: true }, targets: [{ machineId: 'vm2', orgName: 'Acme' }] }),
    });
    vi.stubGlobal('fetch', targets);
    const got = await fetchMoveTargets('hubvm', 'tab-1');
    expect(String(targets.mock.calls[0]?.[0])).toMatch(/\/api\/v2\/chat-move\/targets\?machine=hubvm/);
    expect(got.targets[0]?.orgName).toBe('Acme');
  });

  it('falls back to the gateway when the hub has no such path (flag off, standalone)', async () => {
    const fetchMock = vi.fn(async (url: unknown) =>
      String(url).includes('/api/v2/chat-move')
        ? { ok: false, status: 404, json: async () => ({ error: 'off', code: 'not_enabled' }) }
        : { ok: true, status: 200, json: async () => ({ enabled: true, moved: null }) },
    );
    vi.stubGlobal('fetch', fetchMock);
    expect(await fetchMoveStatus('gwvm')).toEqual({ enabled: true, moved: null });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    await fetchMoveStatus('gwvm');
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(String(fetchMock.mock.calls[2]?.[0])).not.toContain('/api/v2/');
  });
});
