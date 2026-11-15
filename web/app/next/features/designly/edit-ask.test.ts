// @vitest-environment jsdom
import { beforeEach, describe, expect, it } from 'vitest';
import { actedOn, ASK_FRESH_MS, asksDesignEdit, designFor, goesFull, markActed } from './edit-ask';

describe('asksDesignEdit', () => {
  it.each([
    'правим макет в синопии',
    'давай отредактируем дизайн главной',
    'Поправь доску с онбордингом',
    'создай в Sinopia дизайн экрана оплаты',
    'нарисуй макет лендинга',
    'сделай макет страницы настроек',
    'в синопии поменяй цвета кнопок',
    'добавь на доску блок с ценами',
    'Edit the Sinopia board',
    'design a landing page in Sinopia',
    "let's redesign the settings screen",
  ])('asks to edit: %s', (text) => {
    expect(asksDesignEdit(text)).toBe(true);
  });

  it.each([
    'покажи макет',
    'открой доску посмотреть',
    'глянь дизайн, что думаешь?',
    'что ты поменял в макете?',
    'не правь макет, только посмотри',
    'сделай скриншот макета',
    'поправь опечатку в README',
    'show me the design',
    'the design looks good',
  ])('does not ask: %s', (text) => {
    expect(asksDesignEdit(text)).toBe(false);
  });
});

describe('goesFull', () => {
  const now = 1_000_000_000;
  const ask = { id: 'e1', text: 'правим макет в синопии', at: now - 1000 };
  const never = () => false;

  it('goes full for a fresh asking message and a design at work', () => {
    expect(goesFull(ask, 'd1', never, now)).toBe(true);
  });
  it('waits for a design', () => {
    expect(goesFull(ask, null, never, now)).toBe(false);
  });
  it('acts once a message', () => {
    expect(goesFull(ask, 'd1', (id) => id === 'e1', now)).toBe(false);
  });
  it('leaves an old message alone', () => {
    expect(goesFull({ ...ask, at: now - ASK_FRESH_MS - 1 }, 'd1', never, now)).toBe(false);
  });
  it('leaves a look in the pane', () => {
    expect(goesFull({ ...ask, text: 'покажи макет' }, 'd1', never, now)).toBe(false);
  });
});

describe('acted messages', () => {
  beforeEach(() => localStorage.clear());
  it('are kept per chat', () => {
    expect(actedOn('c1', 'e1')).toBe(false);
    markActed('c1', 'e1');
    expect(actedOn('c1', 'e1')).toBe(true);
    expect(actedOn('c2', 'e1')).toBe(false);
  });
});

describe('designFor', () => {
  const ask = { id: 'e1', text: 'правим макет дальше', at: 100 };
  it('takes the design taken up after the message', () => {
    expect(
      designFor(ask, [
        { design: 'old', at: 10 },
        { design: 'new', at: 120 },
      ]),
    ).toBe('new');
  });
  it('goes on with the design worked on before', () => {
    expect(designFor(ask, [{ design: 'old', at: 10 }])).toBe('old');
  });
  it('waits for a new design the message asks for', () => {
    expect(designFor({ ...ask, text: 'создай новый макет' }, [{ design: 'old', at: 10 }])).toBeNull();
  });
  it('has none without work', () => {
    expect(designFor(ask, [])).toBeNull();
  });
});
