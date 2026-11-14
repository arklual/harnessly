// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import Greeting from '@/next/features/shell/greeting';

afterEach(cleanup);

describe('the start page with a view that takes the stage (HRN-1430)', () => {
  it('stays as it was without one', () => {
    const { container } = render(<Greeting name="Пётр" />);
    const stage = container.querySelector('.hx-stage');
    expect(stage?.getAttribute('data-home')).toBeNull();
    expect(container.querySelector('.hx-hello__sub')).not.toBeNull();
  });

  it('draws the view on the stage and its greeting in place of the question', () => {
    const { container, getByText } = render(
      <Greeting
        name="Пётр"
        stage={<div data-testid="day">день</div>}
        greeting={{ title: 'Добрый день, Пётр', sub: '' }}
      />,
    );
    expect(container.querySelector('.hx-stage')?.getAttribute('data-home')).toBe('stage');
    expect(getByText('день')).toBeTruthy();
    expect(container.querySelector('.hx-hello__title')?.textContent).toBe('Добрый день, Пётр');
    expect(container.querySelector('.hx-hello__sub')).toBeNull();
  });
});
