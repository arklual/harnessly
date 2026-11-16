// @vitest-environment jsdom
import { cleanup, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { usePillWidth } from '@/next/ui/use-pill-width';

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

const Pill = () => {
  const { outer, inner } = usePillWidth();
  return (
    <div ref={outer} data-testid="pill" style={{ paddingLeft: '14px', paddingRight: '5px' }}>
      <span
        ref={(node) => {
          // The content as it enters: laid out 300px wide, drawn at scale(0.98) — its rect is 294px.
          if (node) {
            node.style.width = '300px';
            node.getBoundingClientRect = () => ({ width: 294 }) as DOMRect;
          }
          inner(node);
        }}
      />
    </div>
  );
};

describe('the pill width (HRN-1530)', () => {
  it('follows the laid-out width of the content, not its scaled rect, so the button at the end is never cut', async () => {
    vi.stubGlobal(
      'ResizeObserver',
      class {
        // A ResizeObserver reports each element once as it starts observing it.
        report: () => void;
        constructor(report: () => void) {
          this.report = report;
        }
        observe() {
          queueMicrotask(this.report);
        }
        disconnect() {}
      },
    );
    const { getByTestId } = render(<Pill />);
    await Promise.resolve();
    expect(getByTestId('pill').style.getPropertyValue('--hx-pill-w')).toBe('319px');
  });
});
