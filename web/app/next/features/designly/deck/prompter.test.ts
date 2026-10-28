import { describe, expect, it } from 'vitest';
import { advance, createPrompter, finished, normalise, sameWord, splitLines, view } from './prompter';

const SCRIPT =
  'MCP — это способ, которым агент ходит в ваши сервисы. У каждого сервиса есть описание: что он умеет. ' +
  'Агент читает его и сам выбирает, что вызвать. Сервис отвечает, и агент продолжает работу уже с ответом. ' +
  'Всего сервисов двадцать пять, а запросов в день — десять тысяч.';

const tokens = (text: string) => normalise(text.split(/\s+/)).map((x) => x.t);

describe('normalise', () => {
  it('lowers, folds ё and drops punctuation', () => {
    expect(tokens('Всё, ЕЩЁ — да!')).toEqual(['все', 'еще', 'да']);
  });
  it('folds spoken numbers into digits', () => {
    expect(tokens('двадцать пять')).toEqual(['25']);
    expect(tokens('сто двадцать три сервиса')).toEqual(['123', 'сервиса']);
    expect(tokens('две тысячи пять')).toEqual(['2005']);
    expect(tokens('десять тысяч')).toEqual(['10000']);
    expect(tokens('twenty five')).toEqual(['25']);
  });
  it('does not glue unrelated numbers', () => {
    expect(tokens('два три')).toEqual(['2', '3']);
    expect(tokens('пять двадцать')).toEqual(['5', '20']);
  });
  it('keeps digits as they are and maps both to one token', () => {
    expect(tokens('25')).toEqual(['25']);
    expect(tokens('10 тысяч')).toEqual(['10000']);
  });
  it('remembers the raw word a token ends at', () => {
    const t = normalise(['всего', 'двадцать', 'пять', 'штук']);
    expect(t.map((x) => x.to)).toEqual([0, 2, 3]);
  });
});

describe('sameWord', () => {
  it('allows endings, not other words or numbers', () => {
    expect(sameWord('сервис', 'сервисы')).toBe(true);
    expect(sameWord('агента', 'агент')).toBe(true);
    expect(sameWord('агент', 'ответ')).toBe(false);
    expect(sameWord('25', '26')).toBe(false);
    expect(sameWord('он', 'они')).toBe(false);
  });
});

describe('splitLines', () => {
  it('splits sentences and keeps long ones in pieces', () => {
    const lines = createPrompter(SCRIPT).lines;
    expect(lines[0]?.text).toBe('MCP — это способ, которым агент ходит в ваши сервисы.');
    expect(lines.length).toBeGreaterThanOrEqual(5);
    expect(lines.map((l) => l.text).join(' ')).toBe(createPrompter(SCRIPT).words.join(' '));
  });
  it('breaks a sentence without a stop', () => {
    const words = Array.from({ length: 40 }, (_, i) => `слово${i}`);
    const lines = splitLines(words);
    expect(lines.length).toBeGreaterThan(1);
    expect(lines.every((l) => l.to - l.from <= 21)).toBe(true);
  });
  it('ignores the markers of the script', () => {
    expect(createPrompter('Раз [пауза] два [клик] три').words).toEqual(['Раз', 'два', 'три']);
  });
});

describe('advance', () => {
  it('follows the voice word by word', () => {
    let p = createPrompter(SCRIPT);
    p = advance(p, 'MCP это способ которым');
    expect(p.cursor).toBe(5);
    p = advance(p, 'MCP это способ которым агент ходит в ваши');
    expect(p.cursor).toBe(9);
    expect(view(p).said).toBe('MCP — это способ, которым агент ходит в ваши ');
    expect(view(p).rest).toBe('сервисы.');
  });

  it('is robust to endings and to the recognition dropping punctuation', () => {
    let p = createPrompter(SCRIPT);
    p = advance(p, 'эмси пи это способ которым агента ходит в ваш сервис');
    expect(p.cursor).toBeGreaterThanOrEqual(9);
  });

  it('stays put while the speaker improvises', () => {
    let p = createPrompter(SCRIPT);
    p = advance(p, 'MCP это способ которым агент');
    const before = p.cursor;
    p = advance(p, 'MCP это способ которым агент ну вообще-то я хотел рассказать про погоду за окном');
    expect(p.cursor).toBe(before);
  });

  it('jumps ahead over a skipped sentence', () => {
    let p = createPrompter(SCRIPT);
    p = advance(p, 'MCP это способ которым агент ходит в ваши сервисы');
    // skips «У каждого сервиса…» and starts the third sentence
    p = advance(p, 'агент читает его и сам выбирает что вызвать');
    expect(view(p).was).toBe('Агент читает его и сам выбирает, что вызвать.');
  });

  it('never moves back', () => {
    let p = createPrompter(SCRIPT);
    p = advance(p, 'агент читает его и сам выбирает что вызвать');
    const at = p.cursor;
    p = advance(p, 'MCP это способ которым агент ходит в ваши сервисы');
    expect(p.cursor).toBe(at);
  });

  it('does not look further than the window', () => {
    const filler = Array.from({ length: 60 }, (_, i) => `слово${i}`).join(' ');
    let p = createPrompter(`${filler} финальная фраза про слайд`);
    p = advance(p, 'финальная фраза про слайд');
    expect(p.cursor).toBe(0);
  });

  it('matches a number said in words against digits in the script', () => {
    let p = createPrompter('Итого у нас 25 сервисов и 10000 запросов в день');
    p = advance(p, 'итого у нас двадцать пять сервисов и десять тысяч');
    expect(p.cursor).toBe(7);
  });

  it('matches digits in the speech against words in the script', () => {
    let p = createPrompter('Всего сервисов двадцать пять штук в системе');
    p = advance(p, 'всего сервисов 25 штук');
    expect(p.cursor).toBe(5);
  });

  it('ignores a heard text that is too short to trust', () => {
    const p = createPrompter(SCRIPT);
    expect(advance(p, 'агент')).toBe(p);
  });

  it('reaches the end and says so', () => {
    let p = createPrompter('Раз два три четыре пять шесть');
    expect(finished(p)).toBe(false);
    p = advance(p, 'раз два три четыре пять шесть');
    expect(finished(p)).toBe(true);
  });
});

describe('view', () => {
  it('shows the previous, the current and the next line', () => {
    let p = createPrompter(SCRIPT);
    p = advance(p, 'у каждого сервиса есть описание что он умеет');
    const v = view(p);
    // a line that is fully said moves up and the next one becomes the current
    expect(v.was).toBe('У каждого сервиса есть описание: что он умеет.');
    expect(v.line).toBe(2);
    expect(v.said).toBe('');
    expect(v.next.startsWith('Сервис отвечает')).toBe(true);
  });
  it('handles an empty script', () => {
    expect(view(createPrompter('')).line).toBe(-1);
  });
});
