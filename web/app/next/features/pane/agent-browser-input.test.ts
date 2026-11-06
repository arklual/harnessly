import { expect, it } from 'vitest';
import { nextClick, pointerButton } from './agent-browser-input';
it('counts nearby clicks within a half second, resetting for movement, time or button', () => {
  const event = { clientX: 20, clientY: 30, button: 0 };
  const one = nextClick(null, event, 100);
  expect(nextClick(one, event, 200).count).toBe(2);
  expect(nextClick(one, event, 700).count).toBe(1);
  expect(nextClick(one, { ...event, clientX: 40 }, 200).count).toBe(1);
  expect(nextClick(one, { ...event, button: 2 }, 200).count).toBe(1);
});
it('preserves the held button during a pointer move', () => {
  expect(pointerButton(-1, 1)).toBe('left');
  expect(pointerButton(-1, 2)).toBe('right');
  expect(pointerButton(-1, 4)).toBe('middle');
  expect(pointerButton(-1, 0)).toBe('none');
});
