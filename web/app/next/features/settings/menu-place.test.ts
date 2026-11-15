import { describe, expect, it } from 'vitest';
import { opensUp } from './rows';

const button = (top: number, height = 40) => ({ top, bottom: top + height }) as DOMRect;

describe('the menu of a settings row', () => {
  it('opens under its button when it fits above the field', () => {
    expect(opensUp(button(200), 120, 900, 80)).toBe(false);
  });

  it('opens over its button when under it the field of Settings would cover it (HRN-1487)', () => {
    expect(opensUp(button(720), 120, 900, 80)).toBe(true);
  });

  it('stays under when there is even less room above', () => {
    expect(opensUp(button(60), 400, 500, 80)).toBe(false);
  });
});
