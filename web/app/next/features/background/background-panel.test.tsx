// @vitest-environment jsdom
// A subagent's row in the right column is quiet: name, state with the time, model and depth; nothing of its steps.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import BackgroundPanel from '@/next/features/background/background-panel';
import { forgetFolds } from '@/next/features/background/rail-folds';
import { onOpenSubagent } from '@/next/features/subagents/open';
import type { BackgroundTask, ConvId } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));

/** The wide window: the panels are popovers, not the phone's sheets. */
const wide = () => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
};

beforeEach(() => {
  wide();
  localStorage.clear();
  forgetFolds();
});
afterEach(cleanup);

const NOW = 10_000_000;
const NONE: ReadonlySet<string> = new Set();

const agent = (patch: Partial<BackgroundTask>): BackgroundTask => ({
  id: 'a1',
  conversationId: 'tab-x' as ConvId,
  kind: 'subagent',
  title: 'Harden peer envelope',
  startedAt: NOW - 133_000,
  endedAt: null,
  status: 'running',
  devServer: false,
  stoppable: true,
  outputPath: null,
  model: 'sonnet-5-5',
  effort: 'high',
  last: 'Bash · cd /home/coder/workspace/harnessly',
  ...patch,
});

const draw = (tasks: BackgroundTask[]) =>
  render(
    <BackgroundPanel
      tasks={tasks}
      recap={null}
      shots={[]}
      shotSrc={() => ''}
      shown
      now={NOW}
      stopping={NONE}
      onStop={() => undefined}
    />,
  );

describe('BackgroundPanel subagent rows', () => {
  it('shows the model and depth, not the current tool', () => {
    const { container } = draw([agent({})]);
    expect(screen.getByText('Harden peer envelope')).toBeTruthy();
    expect(container.querySelector('.hx-bg__on')?.textContent).toContain('Sonnet');
    expect(container.querySelector('.hx-bg__last')).toBeNull();
    expect(container.textContent).not.toContain('Bash');
  });

  it('says how long a finished one took, without its last words', () => {
    const { container } = draw([agent({ status: 'done', endedAt: NOW, last: 'Конверт ужесточён' })]);
    expect(container.querySelector('.hx-bg__state')?.textContent).toMatch(/2 min 13 s/);
    expect(container.textContent).not.toContain('Конверт');
  });

  it('keeps a command its own line', () => {
    const { container } = draw([agent({ kind: 'command', title: 'npm run dev', last: 'ready in 412 ms' })]);
    expect(container.querySelector('.hx-bg__last')?.textContent).toBe('ready in 412 ms');
  });
});

describe('BackgroundPanel teammate rows', () => {
  const mate = (patch: Partial<BackgroundTask>) =>
    agent({ teammate: true, stoppable: false, role: 'G1: access tables + data layer', ...patch });

  it('shows the name, a teammate mark and what it was started for', () => {
    const { container } = draw([mate({ id: 'aimpl-g1', title: 'impl-g1' })]);
    expect(container.querySelector('.hx-bg__name')?.textContent).toBe('impl-g1');
    expect(container.querySelector('.hx-bg__mate')?.textContent).toBe('teammate');
    expect(container.querySelector('.hx-bg__last')?.textContent).toBe('G1: access tables + data layer');
    expect(container.querySelector('.hx-bg__row')?.getAttribute('data-status')).toBe('run');
    expect(container.querySelector('.hx-bg__stop')).toBeNull();
  });

  it('an idle teammate waits and does not count as running', () => {
    const run = mate({ id: 'a', title: 'impl-g1' });
    const idle = mate({ id: 'b', title: 'rev-t4', status: 'idle', endedAt: NOW });
    const { container } = draw([run, idle]);
    const rows = [...container.querySelectorAll('.hx-bg__row')];
    expect(rows.map((r) => r.getAttribute('data-status'))).toEqual(['run', 'wait']);
    expect(rows[1].querySelector('.hx-bg__state')?.textContent).toMatch(/^waiting/);
    expect(container.querySelector('.hx-side__text')?.textContent).toBe('1 of 2 running');
    cleanup();
    expect(draw([idle]).container.querySelector('.hx-side__text')?.textContent).toBe('1 waiting');
  });

  it('opens a subagent or a teammate in the pane by its id', () => {
    const asked: string[] = [];
    const stop = onOpenSubagent((id) => asked.push(id));
    const { container } = draw([mate({ id: 'aimpl-g1', title: 'impl-g1' }), agent({ id: 'a2', title: 'x' })]);
    container.querySelectorAll<HTMLButtonElement>('button.hx-bg__main').forEach((b) => fireEvent.click(b));
    stop();
    expect(asked.sort()).toEqual(['a2', 'aimpl-g1']);
  });
});

describe('BackgroundPanel groups', () => {
  const text = (container: HTMLElement) =>
    [...container.querySelectorAll('.hx-side__head')].map(
      (h) => h.querySelector('.hx-side__text')?.textContent,
    );

  it('counts those that run out of all of them, and says how the rest ended once none runs', () => {
    const run = (id: string) => agent({ id, title: id });
    const done = (id: string) => agent({ id, title: id, status: 'done', endedAt: NOW });
    expect(text(draw([run('a'), run('b'), run('c'), done('d')]).container)).toEqual(['3 of 4 running']);
    cleanup();
    expect(text(draw([run('a'), run('b')]).container)).toEqual(['2 running']);
    cleanup();
    expect(text(draw([done('a'), done('b'), done('c')]).container)).toEqual(['3 done']);
    cleanup();
    const cmd = (id: string, status: BackgroundTask['status']) =>
      agent({ id, title: id, kind: 'command', status });
    expect(text(draw([cmd('x', 'running'), cmd('y', 'done')]).container)).toEqual(['1 of 2 running']);
  });

  it('names each group by an icon, and starts the commands folded', () => {
    const { container } = draw([agent({}), agent({ id: 'c1', kind: 'command', title: 'npm run dev' })]);
    const groups = [...container.querySelectorAll('.hx-side__sec')];
    expect(groups.map((g) => g.querySelector('.hx-side__ttl .hx-icon')?.getAttribute('data-i'))).toEqual([
      'workflow',
      'terminal',
    ]);
    expect(groups.map((g) => g.getAttribute('aria-expanded'))).toEqual(['true', 'false']);
  });

  it('remembers the fold the person made in this browser', () => {
    const tasks = [agent({}), agent({ id: 'c1', kind: 'command', title: 'npm run dev' })];
    const first = draw(tasks);
    fireEvent.click(first.container.querySelectorAll('.hx-side__head')[1]);
    expect(first.container.querySelectorAll('.hx-side__sec')[1].getAttribute('aria-expanded')).toBe('true');
    cleanup();
    forgetFolds();
    const again = draw(tasks);
    expect(again.container.querySelectorAll('.hx-side__sec')[1].getAttribute('aria-expanded')).toBe('true');
  });
});
