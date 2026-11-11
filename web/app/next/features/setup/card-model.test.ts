import { describe, expect, it } from 'vitest';
import { readBaseView, readSetupView, withBase, type ISetupView } from '@/next/features/setup/api';
import { ACTION, INSIDE_MAX, MARK, STEP_ACT, cardOf } from '@/next/features/setup/card-model';
import { t } from '@/next/i18n';

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

  it('makes a waiting key and a failed step pressable, and shows the picked failure on the line', () => {
    const c = cardOf(
      view([
        item('gitlab', 'access', 'pending', { secret: 'gitlab-token' }),
        item('one', 'access', 'error', { message: 'первая' }),
        item('two', 'access', 'error', { message: 'вторая' }),
        item('ok', 'access', 'ok'),
      ]),
      null,
      'two',
    );
    const act = Object.fromEntries(c.steps.map((s) => [s.name, s.act]));
    expect(act).toEqual({ one: STEP_ACT.focus, two: STEP_ACT.focus, gitlab: STEP_ACT.safe, ok: null });
    expect(c.line.title).toContain('two');
    expect(c.line.rest).toBe('вторая');
    expect(c.line.long).toBe(true);
    expect(c.steps.find((s) => s.focused)?.name).toBe('two');
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

  describe('the base package', () => {
    const base = (items: unknown[], extra: Record<string, unknown> = {}) =>
      readBaseView({ service: true, running: false, done: false, proxy: '', command: '', items, ...extra });
    const b = (id: string, status: string, more: Record<string, unknown> = {}) => ({
      id,
      title: id,
      without: `без ${id}`,
      status,
      ...more,
    });

    it('is the first stage, with or without a hub list', () => {
      const v = withBase(null, base([b('ffmpeg', 'installing'), b('git', 'ok')]));
      const c = cardOf(v!, null);
      expect(c.stages[0]?.name).toBe(t('next.setup.stage.base'));
      expect(c.stages[0]?.mark).toBe(MARK.run);
      const both = withBase(view([item('r1', 'repo', 'pending')]), base([b('git', 'ok')]));
      expect(cardOf(both!, null).stages.map((s) => s.name)).toEqual([
        t('next.setup.stage.base'),
        t('next.setup.stage.code'),
      ]);
    });

    it('offers a proxy and a skip when a tool has no network', () => {
      const v = withBase(
        null,
        base([b('voice', 'no_access', { host: 'huggingface.co', message: 'нет доступа к huggingface.co' })]),
      );
      const c = cardOf(v!, null);
      expect(c.line.title).toContain('huggingface.co');
      expect(c.line.action?.kind).toBe(ACTION.proxy);
      expect(c.line.extra.map((x) => x.kind)).toEqual([ACTION.skip]);
      expect(c.line.baseId).toBe('voice');
    });

    it('gives the command to copy on a machine without the service', () => {
      const v = withBase(
        null,
        base([b('ffmpeg', 'missing', { command: 'sudo apt-get install -y ffmpeg' })], {
          service: false,
          command: 'sudo apt-get install -y ffmpeg jq',
        }),
      );
      const c = cardOf(v!, null);
      expect(c.line.action?.kind).toBe(ACTION.copy);
      expect(c.line.command).toBe('sudo apt-get install -y ffmpeg jq');
    });

    it('leaves skipped tools out and is done when the rest is in place', () => {
      const v = withBase(null, base([b('chrome', 'skipped'), b('git', 'ok')], { done: true }));
      expect(v!.items.map((it) => it.name)).toEqual(['git']);
      expect(v!.done).toBe(true);
    });

    it('keeps a missing optional tool off the first screen and shows it once it is in place', () => {
      const missing = withBase(
        null,
        base([b('libreoffice', 'missing', { optional: true }), b('git', 'ok')], { done: true }),
      );
      expect(missing!.items.map((it) => it.name)).toEqual(['git']);
      expect(missing!.done).toBe(true);
      const there = withBase(
        null,
        base([b('libreoffice', 'ok', { optional: true }), b('git', 'ok')], { done: true }),
      );
      expect(there!.items.map((it) => it.name)).toEqual(['libreoffice', 'git']);
    });
  });
});
