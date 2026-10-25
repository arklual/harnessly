// @vitest-environment jsdom
// A dynamic workflow: one card in the feed with its phases' progress that opens into its agents by phase, a row of its
// own in the right column, and «Ждёт workflow» under an answer that waits for it.
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { pinLocale } from '@/next/i18n';
import type { ToolEntry } from '@/next/features/chat/types';
import { waitingLabel } from '@/next/features/chat/waiting';
import BackgroundPanel from '@/next/features/background/background-panel';
import { forgetFolds } from '@/next/features/background/rail-folds';
import { onOpenSubagent } from '@/next/features/subagents/open';
import WorkflowCard from '@/next/features/workflows/workflow-card';
import { currentPhase, progressOf, runsByCall, WorkflowRuns } from '@/next/features/workflows/run';
import { sampleWorkflow } from '@/next/dev/workflow-sample';
import type { BackgroundTask, ConvId } from '@/next/protocol/v2';

beforeAll(() => pinLocale('en'));
beforeEach(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
  })) as unknown as typeof window.matchMedia;
  localStorage.clear();
  forgetFolds();
});
afterEach(cleanup);

const NOW = 1_800_000_000_000;
const CONV = 'tab-x' as ConvId;
const CALL = 'toolu_flow';
const run = sampleWorkflow(CONV, NOW, CALL);

const call = (patch: Partial<ToolEntry> = {}): ToolEntry => ({
  id: 'e1',
  turnId: null,
  at: NOW,
  rev: 1,
  t: 'tool',
  toolUseId: CALL,
  tool: 'Workflow',
  summary: 'Workflow checkout-review',
  ok: true,
  output: null,
  ...patch,
});

describe('a workflow run', () => {
  it('says how far its started phases are and which one is at work', () => {
    expect(progressOf(run.workflow)).toBe('Review 3/3 · Fix 1/2');
    expect(currentPhase(run.workflow)?.title).toBe('Fix');
    expect(runsByCall([run]).get(CALL)).toBe(run);
  });
});

describe('the card in the feed', () => {
  it('is «Workflow · name» with the progress, and opens into the agents by phase', () => {
    render(
      <WorkflowRuns value={runsByCall([run])}>
        <WorkflowCard entry={call()} />
      </WorkflowRuns>,
    );
    expect(screen.getByText('· checkout-review')).toBeTruthy();
    expect(screen.getByText('Review 3/3 · Fix 1/2')).toBeTruthy();
    expect(screen.queryByText('review:payments')).toBeNull();
    fireEvent.click(screen.getByRole('button', { expanded: false }));
    expect(screen.getByText('review:payments')).toBeTruthy();
    expect(screen.getByText('Verify')).toBeTruthy();
    expect(screen.getByText('Edit · web/app/pay/form.ts')).toBeTruthy();
    const opened: string[] = [];
    const stop = onOpenSubagent((id) => opened.push(id));
    fireEvent.click(screen.getAllByRole('button', { name: /Open/ })[4]);
    stop();
    expect(opened).toEqual(['a1c0ffee00000005e']);
  });

  it('keeps the name and how the call went once the run is no longer listed', () => {
    const { container } = render(<WorkflowCard entry={call({ ok: false })} />);
    expect(screen.getByText('· checkout-review')).toBeTruthy();
    expect(container.querySelector('.hx-flow')?.getAttribute('data-status')).toBe('failed');
    expect(screen.queryByRole('button')).toBeNull();
  });
});

describe('the right column', () => {
  it('lists a workflow in a group of its own, with its progress; the row opens it', () => {
    const subagent: BackgroundTask = {
      ...run,
      id: 'a9',
      kind: 'subagent',
      title: 'Explore',
      workflow: undefined,
    };
    render(
      <BackgroundPanel
        tasks={[subagent, run]}
        recap={null}
        shots={[]}
        shotSrc={() => ''}
        shown
        now={NOW}
        stopping={new Set()}
        onStop={() => undefined}
      />,
    );
    expect(screen.getByText('Workflows')).toBeTruthy();
    expect(screen.getByText('checkout-review')).toBeTruthy();
    const opened: string[] = [];
    const stop = onOpenSubagent((id) => opened.push(id));
    fireEvent.click(screen.getByText('Review 3/3 · Fix 1/2'));
    stop();
    expect(opened).toEqual([run.id]);
  });
});

describe('the line under an answer', () => {
  it('waits for a workflow in its own words', () => {
    const subagent: BackgroundTask = { ...run, id: 'a9', kind: 'subagent', workflow: undefined };
    expect(waitingLabel([run])).toBe('Waiting for a workflow');
    expect(waitingLabel([run, { ...run, id: 'w2' }])).toBe('Waiting for 2 workflows');
    expect(waitingLabel([subagent, subagent, run])).toBe('Waiting for 2 background agents and 1 workflow');
    expect(waitingLabel([subagent])).toBe('Waiting for 1 background agent');
  });
});
