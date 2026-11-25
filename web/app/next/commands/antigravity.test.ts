import { describe, expect, it } from 'vitest';
import { unsupportedAntigravityCommand } from './run';
import type { Op } from '@/next/protocol/v2';

describe('Antigravity command capability boundary', () => {
  const unsupported: Op[] = [
    'rewind',
    'rewind.undo',
    'conversation.fork',
    'conversation.merge',
    'model',
    'conversation.apps',
  ];
  it.each(unsupported)('refuses unsupported %s rather than using Claude semantics', (op) => {
    expect(unsupportedAntigravityCommand(op, {})).toBe(true);
  });

  it('refuses steering, native files, profiles and fast/ultracode', () => {
    expect(unsupportedAntigravityCommand('send', { mode: 'steer' })).toBe(true);
    expect(unsupportedAntigravityCommand('promote', { to: 'steer' })).toBe(true);
    expect(unsupportedAntigravityCommand('send', { attachmentIds: ['file'] })).toBe(true);
    expect(unsupportedAntigravityCommand('conversation.create', { first: { attachmentIds: ['file'] } })).toBe(
      true,
    );
    expect(unsupportedAntigravityCommand('conversation.create', { profile: 'gpt' })).toBe(true);
    expect(unsupportedAntigravityCommand('conversation.create', { model: { fast: true } })).toBe(true);
    expect(unsupportedAntigravityCommand('conversation.create', { model: { ultracode: true } })).toBe(true);
  });

  it('lets a chat beside its ACP bridge steer, attach pictures, decide and switch models', () => {
    expect(unsupportedAntigravityCommand('send', { mode: 'steer' }, true)).toBe(false);
    expect(unsupportedAntigravityCommand('promote', { to: 'steer' }, true)).toBe(false);
    expect(unsupportedAntigravityCommand('send', { attachmentIds: ['file'] }, true)).toBe(false);
    expect(
      unsupportedAntigravityCommand('conversation.create', { first: { attachmentIds: ['file'] } }, true),
    ).toBe(false);
    expect(unsupportedAntigravityCommand('model', {}, true)).toBe(false);
    expect(unsupportedAntigravityCommand('decide', {}, true)).toBe(false);
    // What the bridge does not do stays refused.
    for (const op of ['rewind', 'conversation.fork', 'conversation.apps', 'answer'] as Op[])
      expect(unsupportedAntigravityCommand(op, {}, true)).toBe(true);
    expect(unsupportedAntigravityCommand('conversation.create', { model: { fast: true } }, true)).toBe(true);
  });

  it('allows ordinary text and a machine-advertised model at creation', () => {
    expect(unsupportedAntigravityCommand('send', { mode: 'queue', attachmentIds: [] })).toBe(false);
    expect(
      unsupportedAntigravityCommand('conversation.create', {
        model: { id: 'machine-model', fast: false, ultracode: false },
        first: { attachmentIds: [] },
      }),
    ).toBe(false);
  });
});
