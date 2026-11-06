import { describe, expect, it } from 'vitest';

import { applyText } from './actions';

describe('applyText', () => {
  it('says what happens on the machines', () => {
    expect(applyText({ machines: 0, started: 0, waiting: 0 }, 'now')).toMatch(/нет машин/);
    expect(applyText({ machines: 3, started: 3, waiting: 0 }, 'now')).toBe('Применяем на 3 машинах');
    expect(applyText({ machines: 1, started: 1, waiting: 0 }, 'now')).toBe('Применяем на 1 машине');
    expect(applyText({ machines: 3, started: 2, waiting: 1 }, 'now')).toBe(
      'Применяем на 2 машинах, ещё на 1 машине — при следующем запуске',
    );
    expect(applyText({ machines: 2, started: 0, waiting: 2 }, 'now')).toMatch(/^Машины не в сети/);
    expect(applyText({ machines: 5, started: 0, waiting: 5 }, 'next')).toBe(
      'Применится на 5 машинах при следующем запуске',
    );
  });
});
