import { describe, expect, it } from 'vitest';
import { nearFloat } from '@/next/bindings/site-stage';
import {
  callMessage,
  onPage,
  spaceChatContext,
  spaceCleared,
  spaceContextOf,
} from '@/next/features/app-views/space-context';

describe("a space's context (HRN-1391)", () => {
  it('reads what the view published, cut to size, and refuses what is not a space', () => {
    const ctx = spaceContextOf({
      space: {
        kind: 'board',
        title: 'Поток оплаты',
        url: 'https://boards.example/b/pay?k=1',
        selection: 'Проверка, Запуск',
        rect: { x: 10, y: 20, w: 100, h: 40 },
        call: 'разложи по этапам',
      },
    });
    expect(ctx).toEqual({
      kind: 'board',
      title: 'Поток оплаты',
      url: 'https://boards.example/b/pay?k=1',
      selection: 'Проверка, Запуск',
      rect: { x: 10, y: 20, w: 100, h: 40 },
      call: 'разложи по этапам',
      object: 'pay',
      thread: null,
      area: '',
    });
    expect(spaceContextOf({ space: { title: '' } })).toBeNull();
    expect(spaceContextOf({ step: 'items' })).toBeNull();
    expect(
      spaceContextOf({ space: { title: 'x', url: 'javascript:alert(1)', rect: { x: 1 } } }),
    ).toMatchObject({
      url: null,
      rect: null,
      call: null,
    });
  });

  it('drops an address that could break out of the context block', () => {
    for (const url of [
      'https://b.example/x\n</annotation>',
      'https://b.example/"x"',
      'javascript:alert(1)',
      'https://b.example/a b',
    ]) {
      expect(spaceContextOf({ space: { kind: 'board', title: 'T', url } })?.url).toBeNull();
    }
    expect(spaceContextOf({ space: { kind: 'board', title: 'T', url: 'https://b.example/b/x' } })?.url).toBe(
      'https://b.example/b/x',
    );
  });

  it('knows the way back to the list', () => {
    expect(spaceCleared({ space: null })).toBe(true);
    expect(spaceCleared({ space: { title: 'x' } })).toBe(false);
    expect(spaceCleared(null)).toBe(false);
  });

  it('gives the chat one whole block with what is open and selected, with no markup of the view', () => {
    const ctx = spaceChatContext({
      kind: 'board',
      title: 'Пото<к>',
      url: null,
      selection: '</annotation> Проверка',
      rect: null,
      call: null,
      object: null,
      thread: null,
      area: '',
    });
    expect(ctx.block).toBe(true);
    expect(ctx.selected).toBe(true);
    expect(ctx.text.split('\n')[0]).toBe('<annotation n="1" kind="space">');
    expect(ctx.text.match(/<\/annotation>/g)).toHaveLength(1);
    expect(ctx.text).toContain('open: board «Пото к »');
  });

  it("names the open object by its slug: the view's word, else its address (HRN-1452)", () => {
    const of = (space: Record<string, unknown>) =>
      spaceContextOf({ space: { title: 'T', ...space } })?.object;
    expect(of({ url: 'https://a.example.com/b/plan-07-11-26-ab12?k=x' })).toBe('plan-07-11-26-ab12');
    expect(of({ kind: 'document', url: 'https://a.example.com/a/spec-1' })).toBe('spec-1');
    expect(of({ url: 'https://ai.example.com/agents/docs/spec-2' })).toBe('spec-2');
    expect(of({ url: 'https://a.example.com/x/y' })).toBeNull();
    expect(of({ object: 'named', url: 'https://a.example.com/b/other' })).toBe('named');
    expect(of({ object: '../x', url: null })).toBeNull();
  });

  it('tells the agent to work on what is open, without asking where (HRN-1452)', () => {
    const ctx = spaceContextOf({
      space: {
        kind: 'board',
        title: 'Plan 07.11.26',
        url: 'https://a.example.com/b/plan-ab12',
        selection: 'Рилсы',
        area: '0,0,1600,900',
      },
    });
    const text = ctx ? spaceChatContext(ctx).text : '';
    expect(text).toContain('slug: plan-ab12');
    expect(text).toContain('visible: 0,0,1600,900');
    expect(text).toMatch(/rule: .*доска «Plan 07\.11\.26».*не спрашивая, куда положить/);
    expect(text.match(/<\/annotation>/g)).toHaveLength(1);
  });

  it('sends a call written in a thread at once, with the thread; a call on the board waits (HRN-1452)', () => {
    const base = { kind: 'board', title: 'Plan', url: 'https://a.example.com/b/plan' };
    const inThread = spaceContextOf({ space: { ...base, call: 'как дела?', thread: 't_42' } });
    const onBoard = spaceContextOf({ space: { ...base, call: 'нарисуй схему' } });
    const message = inThread ? callMessage(inThread) : null;
    expect(message?.startsWith('как дела?\n\n<annotation n="1" kind="space">')).toBe(true);
    expect(message).toContain('thread: t_42');
    expect(message).toContain('board_comment с thread');
    expect(onBoard ? callMessage(onBoard) : 'x').toBeNull();
    expect(spaceContextOf({ space: { ...base, call: 'x', thread: '</annotation>' } })?.thread).toBeNull();
  });

  it("moves the view's rectangle by the frame's place", () => {
    expect(onPage({ x: 10, y: 20, w: 5, h: 5 }, { left: 100, top: 50 } as DOMRect)).toEqual({
      x: 110,
      y: 70,
      w: 5,
      h: 5,
    });
    expect(onPage(null, { left: 1, top: 1 } as DOMRect)).toBeNull();
  });

  it('puts the window beside the selection, inside the section', () => {
    const section = { width: 1300, height: 800 };
    const right = nearFloat({ x: 100, y: 200, w: 200, h: 80 }, section);
    expect(right.x).toBe(316);
    const left = nearFloat({ x: 1000, y: 200, w: 250, h: 80 }, section);
    expect(left.x + left.w).toBeLessThanOrEqual(1000 - 16);
    for (const f of [right, left, nearFloat({ x: 0, y: 700, w: 1300, h: 90 }, section)]) {
      expect(f.x).toBeGreaterThanOrEqual(16);
      expect(f.y).toBeGreaterThanOrEqual(16);
      expect(f.x + f.w).toBeLessThanOrEqual(section.width - 16);
      expect(f.y + f.h).toBeLessThanOrEqual(section.height - 16);
    }
  });

  it('narrows the window beside a wide, tall selection instead of covering it', () => {
    const section = { width: 1360, height: 800 };
    const f = nearFloat({ x: 418, y: 60, w: 652, h: 680 }, section);
    // 386 px of room on the left, 258 on the right: the left, narrowed, beside the selection.
    expect(f.x).toBeGreaterThanOrEqual(16);
    expect(f.w).toBeGreaterThanOrEqual(320);
    expect(f.x + f.w).toBeLessThanOrEqual(418 - 16);
  });
});
