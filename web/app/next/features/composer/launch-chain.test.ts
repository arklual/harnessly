import { describe, expect, it } from 'vitest';
import {
  chainOver,
  chainPlan,
  chainSteps,
  pendingLine,
  STEP,
  STEP_STATE,
  WHEN,
  type IChainInput,
} from '@/next/features/composer/launch-chain';
import type { PendingModel } from '@/next/protocol/v2';

const pending = (over: Partial<PendingModel> = {}): PendingModel => ({
  id: 'sonnet',
  effort: 'high',
  since: 1,
  applying: false,
  ...over,
});

describe('pendingLine', () => {
  const opus = { id: 'opus', effort: 'low' as const };

  it('waits for the end of the turn while the agent works or asks', () => {
    for (const agent of ['working', 'waiting-question', 'waiting-permission'] as const)
      expect(pendingLine(pending(), agent, opus)).toEqual({
        when: WHEN.turn,
        model: 'sonnet',
        effort: 'high',
        ultracode: null,
      });
  });

  it('waits for the launch while the agent is not started', () => {
    for (const agent of ['stopped', 'gone', 'launch-failed'] as const)
      expect(pendingLine(pending(), agent, opus)?.when).toBe(WHEN.launch);
  });

  it('names no model when only the effort changes', () => {
    expect(pendingLine(pending({ id: 'opus' }), 'working', opus)).toEqual({
      when: WHEN.turn,
      model: null,
      effort: 'high',
      ultracode: null,
    });
  });

  it('says ultracode turned on or off, and no depth when only the mode changes', () => {
    expect(pendingLine(pending({ id: 'opus', effort: 'low', ultracode: true }), 'working', opus)).toEqual({
      when: WHEN.turn,
      model: null,
      effort: null,
      ultracode: true,
    });
    const on = { ...opus, ultracode: true };
    expect(pendingLine(pending({ id: 'sonnet', ultracode: false }), 'working', on)).toEqual({
      when: WHEN.turn,
      model: 'sonnet',
      effort: 'high',
      ultracode: false,
    });
    expect(pendingLine(pending({ id: 'sonnet', ultracode: true }), 'working', on)?.ultracode).toBeNull();
  });

  it('is silent when nothing waits, and while the steps or a moment of nothing take its place', () => {
    expect(pendingLine(undefined, 'working', opus)).toBeNull();
    expect(pendingLine(pending(), 'starting', opus)).toBeNull();
    expect(pendingLine(pending(), 'idle', opus)).toBeNull();
    expect(pendingLine(pending(), 'unknown', opus)).toBeNull();
  });
});

describe('the steps of a start', () => {
  const input = (over: Partial<IChainInput>): IChainInput => ({
    agent: 'starting',
    pending: pending(),
    waiting: 1,
    ...over,
  });
  const states = (plan: ReturnType<typeof chainPlan>, i: IChainInput) =>
    plan ? chainSteps(plan, i).map((s) => `${s.id}:${s.state}`) : null;

  it('goes launch, model, message and keeps every step while each is done', () => {
    const start = input({});
    const plan = chainPlan(start);
    expect(plan).toEqual({ launch: true, model: true, send: true });
    expect(states(plan, start)).toEqual(['launch:active', 'model:todo', 'send:todo']);

    const up = input({ agent: 'idle', pending: pending({ applying: true }) });
    expect(states(plan, up)).toEqual(['launch:done', 'model:active', 'send:todo']);

    const applied = input({ agent: 'idle', pending: undefined });
    expect(states(plan, applied)).toEqual(['launch:done', 'model:done', 'send:active']);
    expect(chainOver(applied)).toBe(false);

    const sent = input({ agent: 'working', pending: undefined, waiting: 0 });
    expect(states(plan, sent)).toEqual(['launch:done', 'model:done', 'send:done']);
    expect(chainOver(sent)).toBe(true);
  });

  it('has only the steps there is something for', () => {
    expect(chainPlan(input({ pending: undefined, waiting: 0 }))).toEqual({
      launch: true,
      model: false,
      send: false,
    });
    expect(chainPlan(input({ pending: undefined }))).toEqual({ launch: true, model: false, send: true });
  });

  it('begins at the typing of a change into an agent that was up, without a launch step', () => {
    const typing = input({ agent: 'idle', pending: pending({ applying: true }) });
    const plan = chainPlan(typing);
    expect(plan).toEqual({ launch: false, model: true, send: true });
    expect(chainSteps(plan!, typing).map((s) => s.id)).toEqual([STEP.model, STEP.send]);
  });

  it('does not begin for a change that only waits', () => {
    expect(chainPlan(input({ agent: 'working' }))).toBeNull();
    expect(chainPlan(input({ agent: 'idle' }))).toBeNull();
    expect(STEP_STATE.done).toBe('done');
  });
});
