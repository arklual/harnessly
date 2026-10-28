import { beforeAll, describe, expect, it } from 'vitest';
import type { IMoveTarget, IMoveTargets } from '@/next/features/chat-move/api';
import { HERE, pickHere } from '@/next/features/chat-move/here';
import { pinLocale } from '@/next/i18n';

beforeAll(() => pinLocale('en'));

const claude = {
  provider: 'claude' as const,
  profile: '',
  short: 'Claude',
  models: [
    { id: 'opus', label: 'Opus', default: true },
    { id: 'sonnet', label: 'Sonnet', default: false },
  ],
};
const codex = {
  provider: 'codex' as const,
  profile: '',
  short: 'Codex',
  models: [{ id: 'gpt', label: 'GPT', default: true }],
};

const target = (over: Partial<IMoveTarget> = {}): IMoveTarget => ({
  machineId: 'runner-2',
  name: 'Моя ВМ',
  online: true,
  ok: true,
  checks: [],
  filesOk: true,
  agents: [codex, claude],
  ...over,
});

const data = (t: IMoveTarget, source: Partial<IMoveTargets['source']> = {}): IMoveTargets => ({
  source: { ok: true, project: true, dirty: false, busy: false, ...source },
  targets: [{ ...target(), machineId: 'third', name: 'Третья' }, t],
});

const chat = { provider: 'claude', model: 'sonnet' };

describe('«Продолжить здесь» in one press', () => {
  it('moves to this machine with the chat’s own agent and model', () => {
    const pick = pickHere(data(target()), 'runner-2', chat);
    expect(pick.k).toBe(HERE.move);
    if (pick.k !== HERE.move) return;
    expect(pick.target.machineId).toBe('runner-2');
    expect(pick.agent.provider).toBe('claude');
    expect(pick.model.id).toBe('sonnet');
  });

  it('takes the default model when this machine lacks the chat’s one', () => {
    const pick = pickHere(data(target()), 'runner-2', { provider: 'claude', model: 'haiku' });
    expect(pick.k === HERE.move && pick.model.id).toBe('opus');
  });

  it('asks the person when the files cannot come along', () => {
    expect(pickHere(data(target({ filesOk: false })), 'runner-2', chat).k).toBe(HERE.ask);
    expect(pickHere(data(target(), { dirty: true }), 'runner-2', chat).k).toBe(HERE.ask);
  });

  it('says why when the chat or this machine cannot', () => {
    expect(pickHere(data(target(), { busy: true }), 'runner-2', chat)).toEqual({
      k: HERE.refused,
      reason: expect.any(String),
    });
    expect(pickHere(data(target(), { ok: false }), 'runner-2', chat).k).toBe(HERE.refused);
    expect(pickHere(data(target({ ok: false, code: 'unsupported' })), 'runner-2', chat).k).toBe(HERE.refused);
    expect(pickHere(data(target()), 'elsewhere', chat)).toEqual({
      k: HERE.refused,
      reason: 'This machine is not among your connected ones — the chat cannot move here.',
    });
  });
});
