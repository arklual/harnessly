// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import GateCard from '@/next/features/questions/gate-card';
import type { Permission } from '@/next/features/questions/types';

afterEach(cleanup);

const permission = (state: Permission['state']): Permission => ({
  id: 'p:toolu_read',
  kind: 'tool',
  askedAt: 0,
  tool: 'Read',
  command: 'Read /etc/hostname',
  cwd: null,
  planMd: null,
  options: [
    { id: '1', label: 'Yes', scope: 'once' },
    { id: '3', label: 'No', scope: 'deny' },
  ],
  state,
});

describe('GateCard', () => {
  it('titles the permission of a file read as a read, not as a command', () => {
    render(
      <GateCard
        permission={permission({ k: 'open' })}
        device="d1"
        decided={null}
        onDecide={() => Promise.resolve()}
      />,
    );
    expect(screen.getByText('Allow reading the file?')).toBeTruthy();
  });

  it('says a decision the machine could not match goes to the terminal, and opens it', () => {
    const onOpenTerminal = vi.fn();
    render(
      <GateCard
        permission={permission({ k: 'open', refused: 'menu-unmatched' })}
        device="d1"
        decided={null}
        onDecide={() => Promise.resolve()}
        onOpenTerminal={onOpenTerminal}
      />,
    );
    expect(screen.getByRole('status').textContent).toContain('decide in the terminal');
    fireEvent.click(screen.getByText('Open the terminal'));
    expect(onOpenTerminal).toHaveBeenCalledOnce();
    // No button that would do nothing: the terminal is the way.
    expect(screen.queryByText('Yes')).toBeNull();
    expect(screen.queryByText('No')).toBeNull();
  });

  it('draws a shell command with one prompt, not two', () => {
    const shell: Permission = { ...permission({ k: 'open' }), tool: 'Bash', command: '$ rm -f food/*' };
    render(<GateCard permission={shell} device="d1" decided={null} onDecide={() => Promise.resolve()} />);
    expect(document.querySelector('.hx-gate__cmd')?.textContent).toBe('$rm -f food/*');
  });

  it("shows Claude's own countdown to denying the request", () => {
    render(
      <GateCard
        permission={permission({ k: 'open', autoDenyAt: Date.now() + 77_000 })}
        device="d1"
        decided={null}
        onDecide={() => Promise.resolve()}
      />,
    );
    expect(screen.getByRole('timer').textContent).toMatch(/1:1[67]/);
  });

  it('says nothing of the terminal on an open gate', () => {
    render(
      <GateCard
        permission={permission({ k: 'open' })}
        device="d1"
        decided={null}
        onDecide={() => Promise.resolve()}
      />,
    );
    expect(screen.queryByRole('status')).toBeNull();
  });
});
