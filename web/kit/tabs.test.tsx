// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';

import { Tabs } from './tabs';

function Harness() {
  const [v, setV] = useState('a');
  return (
    <Tabs
      label="Views"
      selected={v}
      onSelect={setV}
      tabs={[
        { value: 'a', label: 'One' },
        { value: 'b', label: 'Two', disabled: true },
        { value: 'c', label: 'Three', count: 4 },
      ]}
    />
  );
}

describe('Tabs', () => {
  it('keeps only the selected tab in the tab order and moves with the keys', () => {
    render(<Harness />);
    const one = screen.getByRole('tab', { name: 'One' });
    expect(one.getAttribute('tabindex')).toBe('0');
    expect(screen.getByRole('tab', { name: /^Three/ }).getAttribute('tabindex')).toBe('-1');
    one.focus();
    fireEvent.keyDown(one, { key: 'ArrowRight' });
    const three = screen.getByRole('tab', { name: /^Three/ });
    expect(three.getAttribute('aria-selected')).toBe('true');
    expect(document.activeElement).toBe(three);
    fireEvent.keyDown(three, { key: 'ArrowRight' });
    expect(screen.getByRole('tab', { name: 'One' }).getAttribute('aria-selected')).toBe('true');
    fireEvent.keyDown(screen.getByRole('tab', { name: 'One' }), { key: 'End' });
    expect(screen.getByRole('tab', { name: /^Three/ }).getAttribute('aria-selected')).toBe('true');
  });
});
