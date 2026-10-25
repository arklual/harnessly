// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ToolsLauncher, { type ITool } from '@/next/features/pane/tools-launcher';

const TOOLS: ITool[] = [
  { id: 'plan', group: 'chat', icon: 'list-checks', name: 'План', hint: 'Шаги агента' },
  {
    id: 'subs',
    group: 'chat',
    icon: 'workflow',
    name: 'Сабагенты',
    hint: 'Кого запускал агент',
    live: '2 работают',
  },
  { id: 'shell', group: 'vm', icon: 'terminal', name: 'Терминал', hint: 'Своя оболочка', kbd: '⌃`' },
];
const GROUPS = [
  { id: 'chat', label: 'Этот чат' },
  { id: 'vm', label: 'Машина' },
];

const setup = () => {
  const onOpen = vi.fn();
  const onAddress = vi.fn();
  render(<ToolsLauncher tools={TOOLS} groups={GROUPS} onOpen={onOpen} onAddress={onAddress} />);
  return { onOpen, onAddress, input: screen.getByRole('combobox') };
};

beforeEach(() => {
  // jsdom has no layout to scroll.
  Element.prototype.scrollIntoView = vi.fn();
});
afterEach(cleanup);

describe('ToolsLauncher', () => {
  it('shows what a tool holds now instead of its hint, and the shortcut at the end', () => {
    setup();
    expect(screen.getByText('2 работают')).toBeTruthy();
    expect(screen.queryByText('Кого запускал агент')).toBeNull();
    expect(screen.getByText('⌃`')).toBeTruthy();
  });

  it('walks the rows with the arrows from the search and opens the one under the cursor', () => {
    const { onOpen, input } = setup();
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onOpen).toHaveBeenCalledWith('subs');
  });

  it('wraps from the first row to the last', () => {
    const { onOpen, input } = setup();
    fireEvent.keyDown(input, { key: 'ArrowDown' });
    fireEvent.keyDown(input, { key: 'ArrowUp' });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onOpen).toHaveBeenCalledWith('shell');
  });

  it('opens the first found while typing, and an address with Enter', () => {
    const { onOpen, onAddress, input } = setup();
    fireEvent.change(input, { target: { value: 'терм' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onOpen).toHaveBeenCalledWith('shell');
    fireEvent.change(input, { target: { value: 'localhost:3000' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onAddress).toHaveBeenCalled();
  });

  it('clears the search with the first Escape', () => {
    const { input } = setup();
    fireEvent.change(input, { target: { value: 'пл' } });
    fireEvent.keyDown(input, { key: 'Escape' });
    expect((input as HTMLInputElement).value).toBe('');
  });
});
