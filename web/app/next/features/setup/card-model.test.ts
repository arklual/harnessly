import { describe, expect, it } from 'vitest';
import { readSetupView, type ISetupView } from '@/next/features/setup/api';
import { ACTION, INSIDE_MAX, MARK, cardOf } from '@/next/features/setup/card-model';

const view = (items: unknown[], extra: Record<string, unknown> = {}): ISetupView =>
  readSetupView({ allowed: true, hash: 'h1', done: false, items, ...extra });
const item = (id: string, kind: string, status: string, more: Record<string, unknown> = {}) => ({
  itemId: id,
  kind,
  name: id,
  status,
  ...more,
});

describe('the setup card', () => {
  it('puts the stages in the order of the run and skips an empty one', () => {
    const c = cardOf(view([item('r1', 'repo', 'ok'), item('s1', 'script', 'running')]), null);
    expect(c.stages.map((s) => s.mark)).toEqual([MARK.done, MARK.run]);
    expect(c.current).toBe(1);
    expect(c.stages[1]?.linkDone).toBe(true);
    expect(c.run?.text).toContain('s1');
    expect(c.line.spin).toBe(true);
    expect(c.line.action).toBeNull();
  });

  it('asks for the secret a step waits for', () => {
    const c = cardOf(view([item('a1', 'access', 'pending', { secret: 'gitlab-token' })]), null);
    expect(c.line.mark).toBe(MARK.you);
    expect(c.line.title).toContain('gitlab-token');
    expect(c.line.action?.kind).toBe(ACTION.safe);
  });

  it('offers a retry for a failed step, the failed one shown first', () => {
    const repos = Array.from({ length: INSIDE_MAX + 3 }, (_, i) => item(`r${i}`, 'repo', 'ok'));
    repos.push(item('broken', 'repo', 'error', { message: 'нет доступа' }));
    const c = cardOf(
      view(repos, { run: { runId: 'apply-1', status: 'error', startedAt: 1, finishedAt: 2 } }),
      null,
    );
    expect(c.line.action?.kind).toBe(ACTION.retry);
    expect(c.line.rest).toBe('нет доступа');
    expect(c.steps[0]?.name).toBe('broken');
    expect(c.more).toBe(repos.length - INSIDE_MAX);
    expect(c.title).not.toBe(cardOf(view([item('x', 'repo', 'pending')]), null).title);
  });

  it('shows a picked stage instead of the current one', () => {
    const c = cardOf(view([item('a1', 'access', 'ok'), item('r1', 'repo', 'running')]), 0);
    expect(c.shown).toBe(0);
    expect(c.steps.map((s) => s.name)).toEqual(['a1']);
  });

  it('says the machine is set up when everything is done', () => {
    const c = cardOf(view([item('r1', 'repo', 'ok')], { done: true }), null);
    expect(c.done).toBe(true);
    expect(c.line.mark).toBe(MARK.done);
  });

  it('refuses an answer it does not understand', () => {
    expect(() =>
      readSetupView({ items: [{ itemId: 'x', kind: 'nope', name: 'x', status: 'ok' }] }),
    ).toThrow();
  });
});
