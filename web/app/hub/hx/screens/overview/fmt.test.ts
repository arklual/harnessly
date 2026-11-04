import { describe, expect, it } from 'vitest';

import { genitive, overviewLead, perPersonLong, thousandBare, tokensLong, whoSub } from './fmt';

describe('tokensLong', () => {
  it('writes billions, millions and thousands the board way', () => {
    expect(tokensLong(1_460_000_000)).toBe('1,46 млрд');
    expect(tokensLong(410_000_000)).toBe('410 млн');
    expect(tokensLong(12_000)).toBe('12 тыс');
    expect(tokensLong(950)).toBe('950');
  });
});

describe('perPersonLong', () => {
  it('a fraction takes «задачи», a whole number is declined', () => {
    expect(perPersonLong(8.1)).toBe('8,1 задачи на чел.');
    expect(perPersonLong(5)).toBe('5 задач на чел.');
    expect(perPersonLong(2)).toBe('2 задачи на чел.');
  });
});

describe('thousandBare', () => {
  it('drops «тыс» so that it can stand once after the second number', () => {
    expect(thousandBare(12_700)).toBe('12,7');
  });
});

describe('overviewLead', () => {
  it('names the scope and the period', () => {
    expect(overviewLead('admin', { tenantName: 'Продукт', teamName: null }, 'week')).toBe(
      'Тенант Продукт · неделя',
    );
    expect(overviewLead('lead', { tenantName: null, teamName: 'Фронт' }, 'week')).toBe(
      'Команда Фронт · неделя',
    );
    expect(overviewLead('lead', { tenantName: null, teamName: null }, 'week')).toBe('Твоя команда · неделя');
    expect(overviewLead('member', { tenantName: null, teamName: null }, 'week')).toBe(
      'Только твоя работа · неделя',
    );
    expect(overviewLead('admin', { tenantName: null, teamName: null }, 'week')).toBe(
      'Вся организация · неделя',
    );
  });
});

describe('whoSub', () => {
  const now = Date.parse('2026-11-03T12:00:00Z');
  it('says invited, lead with time, long absence', () => {
    expect(whoSub({ isLead: false, lastActiveAt: null, invited: true }, now)).toBe(
      'приглашена, ещё не зашла',
    );
    expect(whoSub({ isLead: true, lastActiveAt: null, invited: false }, now)).toBe('лид');
    expect(whoSub({ isLead: true, lastActiveAt: '2026-11-03T09:00:00Z', invited: false }, now)).toBe(
      'лид · 3 ч назад',
    );
    expect(whoSub({ isLead: false, lastActiveAt: '2026-10-25T12:00:00Z', invited: false }, now)).toBe(
      '9 дней не заходит',
    );
  });
});

describe('genitive', () => {
  it.each([
    ['Аня', 'Ани'],
    ['Маша', 'Маши'],
    ['Олег', 'Олега'],
    ['Павел', 'Павла'],
    ['Андрей', 'Андрея'],
    ['Игорь', 'Игоря'],
    ['Мария', 'Марии'],
  ])('%s -> %s', (name, want) => {
    expect(genitive(name)).toBe(want);
  });
  it('gives null for what the rules do not cover', () => {
    expect(genitive('Маша Ким')).toBeNull();
    expect(genitive('Anna')).toBeNull();
    expect(genitive('')).toBeNull();
  });
});

describe('machineLine', () => {
  it('shows the load only when it is known', async () => {
    const { machineLine } = await import('./fmt');
    expect(machineLine({ name: 'a-main', online: true, loadPct: 41.4 })).toBe(
      'a-main на связи · загрузка 41%',
    );
    expect(machineLine({ name: 'a-main', online: true, loadPct: null })).toBe('a-main на связи');
    expect(machineLine({ name: 'a-main', online: false, loadPct: 10 })).toBe('a-main не в сети');
  });
});
