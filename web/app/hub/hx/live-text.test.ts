import { describe, expect, it } from 'vitest';

import { tweenText } from './live-text';

describe('tweenText', () => {
  it('moves a number and keeps the unit', () => {
    expect(tweenText('0 ₽', '1 200 ₽', 0.5)).toBe('600 ₽');
    expect(tweenText('0 ₽', '1 200 ₽', 1)).toBe('1 200 ₽');
  });
  it('keeps decimals of the target', () => {
    expect(tweenText('1,0%', '3,5%', 0.4)).toBe('2,0%');
  });
  it('leaves a string of another shape as it is', () => {
    expect(tweenText('—', '12', 0.4)).toBe('12');
    expect(tweenText('1 из 2', '3', 0.4)).toBe('3');
  });
});
