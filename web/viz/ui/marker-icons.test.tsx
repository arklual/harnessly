// @vitest-environment jsdom
// SPDX-License-Identifier: Apache-2.0
import { render } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import { MARKER_ICONS, renderMarkerIcon } from './marker-icons';

describe('renderMarkerIcon', () => {
  it('draws an svg for every known kind', () => {
    for (const kind of Object.keys(MARKER_ICONS)) {
      const { container, unmount } = render(<>{renderMarkerIcon({ kind })}</>);
      expect(container.querySelector('svg')).not.toBeNull();
      expect(container.querySelector('svg')?.getAttribute('aria-hidden')).toBe('true');
      unmount();
    }
  });

  it('draws nothing for an unknown kind', () => {
    const { container } = render(<>{renderMarkerIcon({ kind: 'nope' })}</>);
    expect(container.innerHTML).toBe('');
  });
});
