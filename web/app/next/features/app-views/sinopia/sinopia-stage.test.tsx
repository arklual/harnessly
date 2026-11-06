// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { activeApps, type ISelectionAction } from '@/next/features/app-views/sinopia/contributions';
import { cashback, lint } from '@/next/features/app-views/sinopia/fixtures';
import SinopiaStage from '@/next/features/app-views/sinopia/sinopia-stage';
import type { ISinopia } from '@/next/features/app-views/sinopia/use-sinopia';

// The frame is the bridge's business (app-frame.test.ts); here it is a mark that says which view is in it.
vi.mock('@/next/features/app-views/app-frame', () => ({
  default: ({
    app,
    view,
    target,
    document: doc,
  }: {
    app: { id: string };
    view: { id: string };
    target: string;
    document: string;
  }) => (
    <div data-testid="frame" data-view={`${app.id}/${view.id}`} data-target={target} data-document={doc} />
  ),
}));
vi.mock('@/next/features/shell/modal', () => ({
  default: ({ open, title, children }: { open: boolean; title: string; children: unknown }) =>
    open ? (
      <div role="dialog" aria-label={title}>
        {children as never}
      </div>
    ) : null,
}));

const base = (over: Partial<ISinopia>): ISinopia => ({
  active: [],
  viewPort: 7000,
  state: null,
  selection: [],
  focus: null,
  notice: null,
  blocked: null,
  clearSelection: vi.fn(),
  closeFocus: vi.fn(),
  dismissBlocked: vi.fn(),
  runAction: vi.fn(async () => undefined),
  pickBlock: vi.fn(),
  gate: vi.fn(),
  onViewCall: vi.fn(),
  ...over,
});

const stageOf = (sinopia: ISinopia, kind: string | null, viewing: boolean) => (
  <SinopiaStage
    machine={'m1' as never}
    kind={kind}
    id="proj1"
    conversation={null}
    sinopia={sinopia}
    viewing={viewing}
  >
    <div data-testid="editor" />
  </SinopiaStage>
);
const stage = (sinopia: ISinopia, kind: string | null = 'slides', viewing = false) =>
  render(stageOf(sinopia, kind, viewing));

afterEach(cleanup);

describe('SinopiaStage', () => {
  it('is only the editor where no app is on in the document', () => {
    stage(base({}));
    expect(screen.getByTestId('editor')).toBeTruthy();
    expect(screen.queryByTestId('frame')).toBeNull();
  });

  it('puts the panel of «Валидатор презентаций» beside a presentation, in a view of its document', () => {
    stage(base({ active: activeApps([lint], [lint.id], 'slides') }));
    const frame = screen.getByTestId('frame');
    expect(frame.dataset.view).toBe('harnessly.deck-lint/panel');
    expect(frame.dataset.target).toBe('sinopia.slides');
    expect(frame.dataset.document).toBe('proj1');
  });

  it('opens the inspector of the app for the field that was clicked, over the panels', () => {
    const active = activeApps([cashback], [cashback.id], 'site');
    const view = cashback.views[0];
    stage(base({ active, focus: { app: cashback, view, field: 'amount' } }), 'site');
    expect(screen.getByTestId('frame').dataset.view).toBe('harnessly.cashback/inspector');
  });

  it('hides the panel on the person’s word, and the editor stays where it was', () => {
    const closeFocus = vi.fn();
    stage(base({ active: activeApps([lint], [lint.id], 'slides'), closeFocus }));
    const editor = screen.getByTestId('editor');
    fireEvent.click(screen.getByLabelText('Hide the panel'));
    // It leaves with an animation: it is in its closing phase first.
    expect(screen.getByLabelText('App panel').getAttribute('data-state')).toBe('closing');
    expect(screen.getByTestId('editor')).toBe(editor);
  });

  it('offers the selection items of the apps while something is picked, and runs the one the person taps', () => {
    const runAction = vi.fn(async (_action: ISelectionAction) => undefined);
    stage(
      base({
        active: activeApps([cashback], [cashback.id], 'site'),
        selection: [{ id: 'p:4', tag: 'span' }],
        runAction,
      }),
      'site',
    );
    fireEvent.click(screen.getByText('Объяснить'));
    expect(runAction).toHaveBeenCalledOnce();
    expect(runAction.mock.calls[0][0].item.id).toBe('explain');
  });

  it('looked at («Макет»), has no app panel, no selection menu and no way to reopen them', () => {
    stage(
      base({
        active: activeApps([cashback], [cashback.id], 'site'),
        selection: [{ id: 'p:4', tag: 'span' }],
      }),
      'site',
      true,
    );
    expect(screen.getByTestId('editor')).toBeTruthy();
    expect(screen.queryByTestId('frame')).toBeNull();
    expect(screen.queryByText('Объяснить')).toBeNull();
    expect(screen.queryByRole('button', { name: 'App panel' })).toBeNull();
  });

  it('switched to looking, lets the panel leave with its animation and keeps the editor', () => {
    const sinopia = base({ active: activeApps([lint], [lint.id], 'slides') });
    const view = stage(sinopia);
    const editor = screen.getByTestId('editor');
    view.rerender(stageOf(sinopia, 'slides', true));
    expect(screen.getByLabelText('App panel').getAttribute('data-state')).toBe('closing');
    expect(screen.getByTestId('editor')).toBe(editor);
  });

  it('looked at, still tells why a share is blocked', () => {
    stage(
      base({
        blocked: [
          {
            app: lint.id,
            appName: lint.name,
            check: 'rules',
            findings: [{ node: 'p:1', level: 'error', message: 'Длинно' }],
            blocks: true,
          },
        ],
      }),
      'slides',
      true,
    );
    expect(screen.getByText('Длинно')).toBeTruthy();
  });

  it('tells why a share is blocked', () => {
    stage(
      base({
        blocked: [
          {
            app: lint.id,
            appName: lint.name,
            check: 'rules',
            findings: [{ node: 'p:1', level: 'error', message: 'Слишком много слов' }],
            blocks: true,
          },
        ],
      }),
    );
    expect(screen.getByText('Слишком много слов')).toBeTruthy();
  });
});
