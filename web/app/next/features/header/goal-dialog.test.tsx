// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createRef } from 'react';
import { PortalContext } from '@/next/ui/portal';
import GoalDialog, { oneLine, type IHeaderGoal } from '@/next/features/header/goal-dialog';

afterEach(cleanup);

// The dialog mounts into the interface's own root, as in the app.
const host = createRef<HTMLDivElement>();
const inRoot = (ui: React.ReactNode) =>
  render(
    <PortalContext.Provider value={host}>
      <div className="hx-root" ref={host} />
      {ui}
    </PortalContext.Provider>,
  );

const goalOf = (onSave: IHeaderGoal['onSave'], text: string | null): IHeaderGoal => ({
  current: text === null ? null : { text, status: 'active', since: Date.now() - 3_600_000 },
  onSave,
});

describe('oneLine', () => {
  it('folds every run of white space into one space', () => {
    expect(oneLine('  ship \n the\t contract  ')).toBe('ship the contract');
  });
});

describe('the goal dialog', () => {
  it('shows the whole goal and where it stands, and saves nothing until it changes', async () => {
    inRoot(<GoalDialog open goal={goalOf(vi.fn(), 'finish the contract')} onClose={vi.fn()} />);
    expect(((await screen.findByRole('textbox')) as HTMLTextAreaElement).value).toBe('finish the contract');
    expect(await screen.findByText('Active')).toBeTruthy();
    expect(((await screen.findByRole('button', { name: 'Save' })) as HTMLButtonElement).disabled).toBe(true);
  });

  it('types the edited goal as one line and closes when the runtime took it', async () => {
    const onSave = vi.fn().mockResolvedValue(undefined);
    const onClose = vi.fn();
    inRoot(<GoalDialog open goal={goalOf(onSave, 'old')} onClose={onClose} />);
    fireEvent.change(await screen.findByRole('textbox'), { target: { value: 'new\ngoal' } });
    fireEvent.click(await screen.findByRole('button', { name: 'Save' }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    expect(onSave).toHaveBeenCalledWith('new goal');
  });

  it('clears the goal with an empty text, and says why when the runtime refuses', async () => {
    const onSave = vi.fn().mockRejectedValue(new Error('Not supported'));
    const onClose = vi.fn();
    inRoot(<GoalDialog open goal={goalOf(onSave, 'old')} onClose={onClose} />);
    fireEvent.click(await screen.findByRole('button', { name: 'Clear goal' }));
    await waitFor(() => expect(screen.getByRole('alert').textContent).toBe('Not supported'));
    expect(onSave).toHaveBeenCalledWith('');
    expect(onClose).not.toHaveBeenCalled();
  });

  it('has no clear button for a session without a goal', async () => {
    inRoot(<GoalDialog open goal={goalOf(vi.fn(), null)} onClose={vi.fn()} />);
    await screen.findByRole('textbox');
    expect(screen.queryByRole('button', { name: 'Clear goal' })).toBeNull();
  });
});
