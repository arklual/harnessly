// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { pinLocale } from '@/next/i18n';
import { MenuRow, SegRow, SwitchRow } from '@/next/features/settings/rows';

beforeAll(() => pinLocale('en'));
afterEach(cleanup);

describe('SegRow', () => {
  const choices = [
    { value: 'low', label: 'Low' },
    { value: 'high', label: 'High' },
  ];

  it('picks another value, and does not answer a click on the chosen one', () => {
    const onPick = vi.fn();
    render(<SegRow label="Effort" value="low" choices={choices} onPick={onPick} />);
    fireEvent.click(screen.getByRole('radio', { name: 'Low' }));
    expect(onPick).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('radio', { name: 'High' }));
    expect(onPick).toHaveBeenCalledWith('high');
  });

  it('shows a row that cannot be changed dimmed and does not take clicks', () => {
    const onPick = vi.fn();
    render(<SegRow label="Effort" value="low" choices={choices} off onPick={onPick} />);
    fireEvent.click(screen.getByRole('radio', { name: 'High' }));
    expect(onPick).not.toHaveBeenCalled();
  });
});

describe('MenuRow', () => {
  const choices = [
    { value: null, label: 'Default', sub: null },
    { value: 'opus', label: 'Opus', sub: 'The most capable' },
  ];

  it('opens under its button, picks a value and closes', async () => {
    const onPick = vi.fn();
    render(<MenuRow label="Model" value={null} choices={choices} onPick={onPick} />);
    expect(screen.queryByRole('listbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: /Default/ }));
    expect(screen.getByRole('listbox')).toBeTruthy();
    fireEvent.click(screen.getByRole('option', { name: /Opus/ }));
    expect(onPick).toHaveBeenCalledWith('opus');
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 250));
    });
    expect(screen.queryByRole('listbox')).toBeNull();
  });

  it('closes on Escape and on a press outside, picking nothing', async () => {
    const onPick = vi.fn();
    render(<MenuRow label="Model" value="opus" choices={choices} onPick={onPick} />);
    const open = () => fireEvent.click(screen.getByRole('button', { name: /Opus/ }));
    open();
    fireEvent.keyDown(document, { key: 'Escape' });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 250));
    });
    expect(screen.queryByRole('listbox')).toBeNull();
    open();
    fireEvent.pointerDown(document.body);
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 250));
    });
    expect(screen.queryByRole('listbox')).toBeNull();
    expect(onPick).not.toHaveBeenCalled();
  });

  it('says «not chosen» when the value is not among the choices', () => {
    render(<MenuRow label="Model" value="gone" choices={choices} onPick={() => undefined} />);
    expect(screen.getByRole('button', { name: /Not chosen/i })).toBeTruthy();
  });
});

describe('SwitchRow', () => {
  it('reports the new state and marks a row that waits for the machine', () => {
    const onChange = vi.fn();
    const { container } = render(<SwitchRow label="Thinking" on={false} busy onChange={onChange} />);
    fireEvent.click(screen.getByRole('switch', { name: 'Thinking' }));
    expect(onChange).toHaveBeenCalledWith(true);
    expect(container.querySelector('[data-busy]')).not.toBeNull();
  });
});
