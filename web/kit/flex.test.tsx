// @vitest-environment jsdom
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { Row, Stack } from './flex';

describe('Flex', () => {
  it('lays out a column with a step of the spacing scale', () => {
    const { container } = render(<Stack gap={6} align="start" />);
    const el = container.firstElementChild as HTMLElement;
    expect(el.style.flexDirection).toBe('column');
    expect(el.style.gap).toBe('var(--k-space-6)');
    expect(el.style.alignItems).toBe('flex-start');
  });

  it('wraps a row and keeps a caller style', () => {
    const { container } = render(<Row justify="between" style={{ marginTop: 4 }} />);
    const el = container.firstElementChild as HTMLElement;
    expect(el.style.flexWrap).toBe('wrap');
    expect(el.style.justifyContent).toBe('space-between');
    expect(el.style.marginTop).toBe('4px');
  });
});
