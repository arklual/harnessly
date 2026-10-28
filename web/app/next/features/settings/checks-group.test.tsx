// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import ChecksGroup from '@/next/features/settings/checks-group';
import { readChecks, type IMachineCheck } from '@/next/features/settings/machine-checks';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

const check = (over: Partial<IMachineCheck>): IMachineCheck => ({
  id: 'tmux',
  ok: true,
  title: 'tmux',
  detail: '',
  fixCommand: '',
  fixable: false,
  ...over,
});

describe('ChecksGroup', () => {
  it('draws nothing while everything is in order', () => {
    const { container } = render(
      <ChecksGroup
        checks={[check({}), check({ id: 'git' })]}
        fixing={null}
        onCopy={vi.fn()}
        onFix={vi.fn()}
      />,
    );
    expect(container.textContent).toBe('');
  });

  it('shows only what is wrong, its command, and copies it', () => {
    const onCopy = vi.fn();
    render(
      <ChecksGroup
        checks={[
          check({ id: 'git' }),
          check({ id: 'ffmpeg', ok: false, fixCommand: 'sudo apt-get install -y ffmpeg' }),
        ]}
        fixing={null}
        onCopy={onCopy}
        onFix={vi.fn()}
      />,
    );
    expect(screen.queryByText('No git')).toBeNull();
    expect(screen.getByText('No ffmpeg')).toBeTruthy();
    expect(screen.getByText('sudo apt-get install -y ffmpeg')).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Fix/ })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Copy/ }));
    expect(onCopy).toHaveBeenCalledWith('sudo apt-get install -y ffmpeg');
  });

  it('offers Fix only where the runtime can, and waits for it', () => {
    const onFix = vi.fn();
    const failing = check({ id: 'local_bin_path', ok: false, fixable: true, fixCommand: 'echo x' });
    const { rerender } = render(
      <ChecksGroup checks={[failing]} fixing={null} onCopy={vi.fn()} onFix={onFix} />,
    );
    fireEvent.click(screen.getByRole('button', { name: /Fix/ }));
    expect(onFix).toHaveBeenCalledWith('local_bin_path');
    rerender(<ChecksGroup checks={[failing]} fixing="local_bin_path" onCopy={vi.fn()} onFix={onFix} />);
    expect((screen.getByRole('button', { name: /Fix/ }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('names a check it has no words for by the runtime title', () => {
    render(
      <ChecksGroup
        checks={[check({ id: 'future_check', ok: false, title: 'Something new' })]}
        fixing={null}
        onCopy={vi.fn()}
        onFix={vi.fn()}
      />,
    );
    expect(screen.getByText('Something new')).toBeTruthy();
  });
});

describe('readChecks', () => {
  it('reads the runtime answer and drops what does not hold together', () => {
    expect(
      readChecks({ checks: [{ id: 'git', ok: false, fix_command: 'x', fixable: true }, { id: 1 }] }),
    ).toEqual([{ id: 'git', ok: false, title: 'git', detail: '', fixCommand: 'x', fixable: true }]);
    expect(readChecks({})).toBeNull();
  });
});
