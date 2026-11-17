import { describe, expect, it } from 'vitest';
import { chatByName, quickPlan, type IQuickContext } from '@/next/bindings/voice-quick';

const ctx: IQuickContext = {
  chats: [
    { id: 'c1', title: 'Простая доска с примерными карточками' },
    { id: 'c2', title: 'Голосовой режим: роутер' },
    { id: 'c3', title: 'Тарифы и оплата' },
    { id: 'c4', title: 'Доска для тарифов' },
  ],
  readingPaused: false,
  overBoard: false,
};

const first = (text: string, c: IQuickContext = ctx) => quickPlan(text, c)?.actions[0] ?? null;

describe('quickPlan', () => {
  it('switches the theme', () => {
    expect(first('Включи тёмную тему')).toEqual({ do: 'theme', value: 'dark' });
    expect(first('светлая тема, пожалуйста')).toEqual({ do: 'theme', value: 'light' });
    expect(first('давай ночной режим')).toEqual({ do: 'theme', value: 'dark' });
    expect(first('тема как в системе')).toEqual({ do: 'theme', value: 'system' });
  });

  it('leaves a board its own theme unless the interface is named', () => {
    const board = { ...ctx, overBoard: true };
    expect(first('сделай тёмную тему', board)).toBeNull();
    expect(first('включи тёмную тему интерфейса', board)).toEqual({ do: 'theme', value: 'dark' });
  });

  it('does not take a task about a theme', () => {
    expect(first('сделай тёмную тему для лендинга')).toBeNull();
    expect(first('напиши статью про тёмную тему в интерфейсах')).toBeNull();
  });

  it('folds and opens the sidebar and the pane', () => {
    expect(first('скрой боковую панель')).toEqual({ do: 'sidebar', open: false });
    expect(first('покажи сайдбар')).toEqual({ do: 'sidebar', open: true });
    expect(first('закрой правую панель')).toEqual({ do: 'pane', open: false });
    expect(first('открой терминал')).toEqual({ do: 'pane', open: true, tab: 'terminal' });
  });

  it('opens settings and their sections', () => {
    expect(first('открой настройки')).toEqual({ do: 'open', page: 'settings' });
    expect(first('настройки голоса')).toEqual({ do: 'open', page: 'settings', section: 'voice' });
    expect(first('открой сейф в настройках')).toEqual({ do: 'open', page: 'settings', section: 'safe' });
    expect(first('сделай настройки удобнее')).toBeNull();
  });

  it('opens pages', () => {
    expect(first('открой задачи')).toEqual({ do: 'open', page: 'tasks' });
    expect(first('на главную')).toEqual({ do: 'open', page: 'home' });
    expect(first('перейди в дизайн')).toEqual({ do: 'open', page: 'designly' });
    expect(first('задачи')).toBeNull();
  });

  it('opens a chat by a name only it has', () => {
    expect(first('открой чат про голосовой режим')).toEqual({ do: 'open_chat', chat: 'c2' });
    expect(first('перейди в чат тарифы и оплата')).toEqual({ do: 'open_chat', chat: 'c3' });
    // Two chats about a board: the router asks which.
    expect(first('открой чат доска')).toBeNull();
    expect(first('открой чат про космос')).toBeNull();
  });

  it('goes on with a paused reading only', () => {
    expect(first('продолжи')).toBeNull();
    expect(first('продолжи читать', { ...ctx, readingPaused: true })).toEqual({
      do: 'read_aloud',
      source: 'resume',
    });
  });

  it('ends the talk', () => {
    expect(first('заверши разговор')).toEqual({ do: 'stop' });
    expect(first('отбой')).toEqual({ do: 'stop' });
  });

  it('leaves long and compound phrases to the router', () => {
    expect(first('открой настройки и переключи модель на опус в этом чате')).toBeNull();
    expect(first('создай новый чат и сделай там синопию')).toBeNull();
    expect(first('переключи на опус')).toBeNull();
  });
});

describe('chatByName', () => {
  it('matches word forms', () => {
    expect(chatByName('доской с карточками', ctx.chats)?.id).toBe('c1');
    expect(chatByName('доска', ctx.chats)).toBeNull();
  });
});
