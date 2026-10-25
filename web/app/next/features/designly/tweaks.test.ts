import { describe, expect, it } from 'vitest';
import type { ITweak } from '@/next/features/designly/page-bridge';
import { rangeShare, tweakGroups, tweakKind, tweakValueText } from '@/next/features/designly/tweaks';

const tweak = (over: Partial<ITweak>): ITweak => ({
  name: 'x',
  label: 'X',
  section: 'Screen',
  editor: 'string',
  value: null,
  options: [],
  min: null,
  max: null,
  step: null,
  ...over,
});
const opts = (...texts: string[]) => texts.map((text) => ({ value: text.toLowerCase(), text }));

describe('tweakKind', () => {
  it('few short options are segments, many or long ones a list', () => {
    expect(tweakKind(tweak({ editor: 'enum', options: opts('Dark', 'Light') }))).toBe('seg');
    expect(tweakKind(tweak({ editor: 'enum', options: opts('A', 'B', 'C', 'D', 'E') }))).toBe('select');
    expect(
      tweakKind(tweak({ editor: 'enum', options: opts('Compact layout', 'Full layout with banner') })),
    ).toBe('select');
  });

  it('options that are all colours are swatches', () => {
    const colors = [
      { value: '#7c5cff', text: 'Violet' },
      { value: '#3b82f6', text: 'Blue' },
    ];
    expect(tweakKind(tweak({ options: colors }))).toBe('colors');
  });

  it('a switch, a slider with bounds, a number without, a text', () => {
    expect(tweakKind(tweak({ editor: 'boolean', value: true }))).toBe('toggle');
    expect(tweakKind(tweak({ editor: 'number', value: 4, min: 2, max: 6 }))).toBe('range');
    expect(tweakKind(tweak({ editor: 'number', value: 4 }))).toBe('number');
    expect(tweakKind(tweak({ editor: 'number', value: 4, min: 5, max: 5 }))).toBe('number');
    expect(tweakKind(tweak({ editor: 'string', value: 'Buy' }))).toBe('text');
  });
});

describe('tweakValueText', () => {
  it('the option the value stands at, else the value itself', () => {
    const c = tweak({ value: 'light', options: opts('Dark', 'Light') });
    expect(tweakValueText(c)).toBe('Light');
    expect(tweakValueText(tweak({ value: 12 }))).toBe('12');
    expect(tweakValueText(tweak({ value: null }))).toBe('');
  });
});

describe('tweakGroups', () => {
  it('keeps the board order and puts a section once', () => {
    const groups = tweakGroups([
      tweak({ name: 'a', section: 'Screen' }),
      tweak({ name: 'b', section: 'Props' }),
      tweak({ name: 'c', section: 'Screen' }),
    ]);
    expect(groups.map((g) => [g.section, g.items.map((i) => i.name)])).toEqual([
      ['Screen', ['a', 'c']],
      ['Props', ['b']],
    ]);
  });
});

describe('rangeShare', () => {
  it('how far along the bounds, kept inside them', () => {
    expect(rangeShare(4, 2, 6)).toBe('50.0%');
    expect(rangeShare(9, 2, 6)).toBe('100.0%');
    expect(rangeShare(0, 2, 6)).toBe('0.0%');
  });
});
