import { describe, expect, it } from 'vitest';
import {
  applyLook,
  colorHex,
  parseParentMessage,
  parseStyleItems,
  readLook,
  restoreLook,
  type IKept,
} from './bridge-core';

/** The inline style of an element as the bridge touches it: values with their priorities. */
const inlineStyle = (start: Record<string, [string, string]> = {}) => {
  const map = new Map(Object.entries(start));
  return {
    map,
    getPropertyValue: (name: string) => map.get(name)?.[0] ?? '',
    getPropertyPriority: (name: string) => map.get(name)?.[1] ?? '',
    setProperty: (name: string, value: string, priority = '') => void map.set(name, [value, priority]),
    removeProperty: (name: string) => void map.delete(name),
  };
};

describe('parseStyleItems', () => {
  it('takes numbered looks with a #rrggbb fill, a radius and an opacity', () => {
    expect(parseStyleItems([{ id: 1, background: '#AbCdEf', radius: 8, opacity: 0.5 }])).toEqual([
      { id: 1, background: '#AbCdEf', radius: 8, opacity: 0.5 },
    ]);
    expect(parseStyleItems([])).toEqual([]);
    expect(parseStyleItems([{ id: 2, background: null, radius: null, opacity: null }])).toHaveLength(1);
  });

  it('drops the whole set for one item that is not right', () => {
    const good = { id: 1, background: null, radius: 1, opacity: 1 };
    for (const bad of [
      { ...good, id: 0 },
      { ...good, id: 1.5 },
      { ...good, background: 'url(x)' },
      { ...good, background: 'red' },
      { ...good, radius: -1 },
      { ...good, radius: 5000 },
      { ...good, opacity: 2 },
      { ...good, opacity: 'x' },
      { id: 1, background: null, radius: null },
      null,
    ]) {
      expect(parseStyleItems([good, bad])).toBeNull();
    }
    expect(parseStyleItems('x')).toBeNull();
    expect(parseStyleItems(Array.from({ length: 65 }, (_, i) => ({ ...good, id: i + 1 })))).toBeNull();
  });

  it('is what the bridge reads of the parent', () => {
    const items = [{ id: 1, background: null, radius: 4, opacity: null }];
    expect(parseParentMessage({ type: 'tk:styles', items })).toEqual({ type: 'tk:styles', items });
    expect(parseParentMessage({ type: 'tk:styles', items: [{ id: 'x' }] })).toBeNull();
  });
});

describe('applyLook', () => {
  const el = (start?: Record<string, [string, string]>) => ({ style: inlineStyle(start) });

  it('writes the look with !important and clears the image under a fill', () => {
    const block = el();
    const kept: IKept = {};
    applyLook(block, { id: 1, background: '#ffffff', radius: 8, opacity: 0.5 }, kept);
    expect(block.style.map.get('background-color')).toEqual(['#ffffff', 'important']);
    expect(block.style.map.get('background-image')).toEqual(['none', 'important']);
    expect(block.style.map.get('border-radius')).toEqual(['8px', 'important']);
    expect(block.style.map.get('opacity')).toEqual(['0.5', 'important']);
  });

  it('gives back what the block had inline, and takes away what it did not have', () => {
    const block = el({ opacity: ['0.9', ''], 'border-radius': ['3px', 'important'] });
    const kept: IKept = {};
    applyLook(block, { id: 1, background: '#000000', radius: 20, opacity: 0.2 }, kept);
    applyLook(block, { id: 1, background: '#111111', radius: 22, opacity: 0.3 }, kept);
    restoreLook(block, kept);
    expect([...block.style.map]).toEqual([
      ['opacity', ['0.9', '']],
      ['border-radius', ['3px', 'important']],
    ]);
    expect(kept).toEqual({});
  });

  it('lets go of one property when it turns null and keeps the others', () => {
    const block = el();
    const kept: IKept = {};
    applyLook(block, { id: 1, background: null, radius: 8, opacity: 0.5 }, kept);
    applyLook(block, { id: 1, background: null, radius: null, opacity: 0.5 }, kept);
    expect([...block.style.map.keys()]).toEqual(['opacity']);
  });
});

describe('readLook', () => {
  const canvas = (resolve: (value: string) => string) => {
    let fill = '#000000';
    return {
      createElement: () => ({
        getContext: () => ({
          get fillStyle() {
            return fill;
          },
          set fillStyle(value: string) {
            fill = resolve(value);
          },
        }),
      }),
    };
  };

  it('reads the fill as #rrggbb, nothing for a clear one', () => {
    const doc = canvas((value) =>
      value === 'clear' ? 'rgba(0, 0, 0, 0)' : value === 'half' ? 'rgba(1, 2, 255, 0.5)' : value,
    );
    expect(colorHex('#ABCDEF', doc)).toBe('#abcdef');
    expect(colorHex('clear', doc)).toBeNull();
    expect(colorHex('half', doc)).toBe('#0102ff');
    expect(
      colorHex(
        'color(display-p3 1 0 0)',
        canvas(() => 'color(display-p3 1 0 0)'),
      ),
    ).toBeNull();
  });

  it('reads the radius in pixels, the opacity and the fill', () => {
    const win = {
      getComputedStyle: () => ({ borderTopLeftRadius: '12.5px', opacity: '0.4', backgroundColor: '#112233' }),
    };
    expect(
      readLook(
        {},
        win,
        canvas((value) => value),
      ),
    ).toEqual({ background: '#112233', radius: 12.5, opacity: 0.4 });
    const percent = {
      getComputedStyle: () => ({ borderTopLeftRadius: '50%', opacity: '1', backgroundColor: 'x' }),
    };
    expect(
      readLook(
        {},
        percent,
        canvas(() => 'rgba(0, 0, 0, 0)'),
      ).radius,
    ).toBeNull();
  });
});
