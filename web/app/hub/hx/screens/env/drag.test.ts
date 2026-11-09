import { describe, expect, it } from 'vitest';
import { moved, scriptOrder } from './drag';

// GitHub issue 40: moves start from the saved order, not the names, so repeated moves go on from the last one.
describe('scriptOrder', () => {
  const row = (entryId: string, name: string, position: number, kind = 'script') => ({
    entryId,
    kind,
    name,
    position,
  });

  it('orders scripts by saved position, then name, and leaves other kinds out', () => {
    const items = [
      row('e-a', 'Альфа', 3),
      row('e-b', 'Бета', 2),
      row('e-r', 'Репо', 0, 'repo'),
      row('e-v', 'Вега', 1),
      row('e-z', 'Зета', 0),
    ];
    expect(scriptOrder(items)).toEqual(['e-z', 'e-v', 'e-b', 'e-a']);
  });

  it('moves the same script up twice from the saved order', () => {
    let items = [row('e-v', 'Вега', 1), row('e-b', 'Бета', 2), row('e-a', 'Альфа', 3)];
    for (let n = 0; n < 2; n++) {
      const ids = scriptOrder(items);
      const next = moved(ids, 'e-a', ids.indexOf('e-a') - 1);
      // What the hub saves: positions 1, 2, … in the order sent.
      items = items.map((r) => ({ ...r, position: next.indexOf(r.entryId) + 1 }));
    }
    expect(scriptOrder(items)).toEqual(['e-a', 'e-v', 'e-b']);
  });
});
