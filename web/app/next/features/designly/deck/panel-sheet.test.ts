import { describe, expect, it } from 'vitest';
import { PANEL_DROP, panelDrop, panelFollow } from '@/next/features/designly/deck/panel-sheet';

describe('panelDrop', () => {
  it('a tap toggles', () => expect(panelDrop(3, 80, false)).toBe(PANEL_DROP.tap));
  it('a pull up of more than 40 px makes it tall', () =>
    expect(panelDrop(-41, 900, false)).toBe(PANEL_DROP.tall));
  it('a fast flick up makes it tall', () => expect(panelDrop(-20, 20, false)).toBe(PANEL_DROP.tall));
  it('a pull down of a tall sheet under 220 px brings it back', () =>
    expect(panelDrop(100, 900, true)).toBe(PANEL_DROP.normal));
  it('a pull down of a tall sheet past 220 px closes it', () =>
    expect(panelDrop(230, 900, true)).toBe(PANEL_DROP.close));
  it('a pull down of a normal sheet closes it', () =>
    expect(panelDrop(61, 900, false)).toBe(PANEL_DROP.close));
  it('a fast flick down closes it', () => expect(panelDrop(30, 20, false)).toBe(PANEL_DROP.close));
  it('a short slow move stays', () => expect(panelDrop(30, 900, false)).toBe(PANEL_DROP.stay));
});

describe('panelFollow', () => {
  it('a tall sheet follows only downwards', () => expect(panelFollow(-50, true, 800)).toBe(0));
  it('a normal sheet follows up to the tall height', () =>
    expect(panelFollow(-900, false, 800)).toBe(-(800 * 0.42 - 52)));
});
