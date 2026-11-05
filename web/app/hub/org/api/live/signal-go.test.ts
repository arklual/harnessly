import { describe, expect, it } from 'vitest';

import { KPI_COPY } from '../kpi-copy';
import { signalGo } from './signal-go';

describe('signalGo', () => {
  it('gives no button when the hub sent no action', () => {
    expect(signalGo(null)).toBeNull();
  });
});

describe('KPI_COPY', () => {
  it('has a plain title and a one-sentence hint for every KPI, with no jargon left', () => {
    for (const c of Object.values(KPI_COPY)) {
      expect(c.title.length).toBeGreaterThan(3);
      expect(c.hint.length).toBeGreaterThan(20);
      expect(`${c.title} ${c.hint}`).not.toMatch(/\$|\bMR\/|yield|cache/i);
    }
  });
});
