import { describe, expect, it } from 'vitest';
import { findBlock, mdBlocks, plainText, replaceBlock } from '@/lib/doc-md-blocks';

const DOC = [
  '# План **выката**',
  '',
  'Первый абзац со [ссылкой](https://example.com) и `кодом`,',
  'переносится на вторую строку.',
  '',
  '- пункт один',
  '- пункт *два*',
  '  с продолжением',
  '',
  '| Сервис | Порт |',
  '| --- | --- |',
  '| шлюз | 8022 |',
  '',
  '```ts',
  'const x = 1;',
  '```',
  '',
  '---',
  '',
  '> Цитата **важная**',
].join('\n');

describe('mdBlocks', () => {
  it('splits headings, paragraphs, each list item, a table, code, a rule and a quote, with their source offsets', () => {
    const blocks = mdBlocks(DOC);
    expect(blocks.map((block) => DOC.slice(block.start, block.end))).toEqual([
      '# План **выката**',
      'Первый абзац со [ссылкой](https://example.com) и `кодом`,\nпереносится на вторую строку.',
      '- пункт один',
      '- пункт *два*\n  с продолжением',
      '| Сервис | Порт |\n| --- | --- |\n| шлюз | 8022 |',
      '```ts\nconst x = 1;\n```',
      '---',
      '> Цитата **важная**',
    ]);
    expect(blocks.map((block) => block.text)).toEqual([
      'план выката',
      'первый абзац со ссылкой и кодом, переносится на вторую строку.',
      'пункт один',
      'пункт два с продолжением',
      'сервис порт шлюз 8022',
      'const x = 1;',
      '',
      'цитата важная',
    ]);
  });

  it('keeps a setext heading together and an unclosed fence to the end', () => {
    const source = 'Заголовок\n---\n\n```\nкод без конца';
    expect(mdBlocks(source).map((block) => source.slice(block.start, block.end))).toEqual([
      'Заголовок\n---',
      '```\nкод без конца',
    ]);
  });
});

describe('findBlock', () => {
  const blocks = mdBlocks(DOC);
  const sourceOf = (tag: string, text: string, ordinal = 0, total = 1) => {
    const block = findBlock(blocks, { tag, text, ordinal, total });
    return block && DOC.slice(block.start, block.end);
  };

  it('takes the block of the same kind and place when it reads the same', () => {
    expect(sourceOf('p', 'Первый абзац со ссылкой и кодом,\n  переносится на вторую строку.')).toContain(
      'Первый абзац',
    );
    expect(sourceOf('li', 'пункт два с продолжением', 1, 2)).toBe('- пункт *два*\n  с продолжением');
    expect(sourceOf('blockquote', 'Цитата важная')).toBe('> Цитата **важная**');
    expect(sourceOf('h1', 'План выката')).toBe('# План **выката**');
  });

  it('maps a table by its place exactly, whatever its cells read like on the page', () => {
    expect(sourceOf('table', 'Сервис Порт шлюз 8022')).toBe(
      '| Сервис | Порт |\n| --- | --- |\n| шлюз | 8022 |',
    );
    expect(sourceOf('table', 'Service Port gateway 8022 (rendered differently)')).toBe(
      '| Сервис | Порт |\n| --- | --- |\n| шлюз | 8022 |',
    );
  });

  it('finds the one block with exactly that text when the places differ, and nothing when it is not sure', () => {
    expect(sourceOf('li', 'пункт один', 5, 9)).toBe('- пункт один');
    expect(sourceOf('p', 'Первый абзац со ссылкой')).toBeNull();
    expect(sourceOf('table', 'другая таблица', 1, 2)).toBeNull();
    expect(sourceOf('div', 'План выката')).toBeNull();
    expect(sourceOf('p', '   ')).toBeNull();
  });

  it('tells two tables apart by their place', () => {
    const two = '| a | b |\n| - | - |\n| 1 | 2 |\n\nтекст\n\n| a | b |\n| - | - |\n| 1 | 2 |';
    const second = findBlock(mdBlocks(two), { tag: 'table', text: 'a b 1 2', ordinal: 1, total: 2 });
    expect(second && second.start).toBe(two.lastIndexOf('| a | b |'));
  });

  it('reads a setext heading as a heading of its level', () => {
    const source = 'Заголовок\n===\n\nПодзаголовок\n---';
    expect(mdBlocks(source).map((block) => `${block.kind}:${block.text}`)).toEqual([
      'h1:заголовок',
      'h2:подзаголовок',
    ]);
  });
});

describe('replaceBlock', () => {
  it('changes only the block', () => {
    const [, paragraph] = mdBlocks(DOC);
    const next = replaceBlock(DOC, paragraph, 'Новый абзац.');
    expect(next.startsWith('# План **выката**\n\nНовый абзац.\n\n- пункт один')).toBe(true);
    expect(next.endsWith('> Цитата **важная**')).toBe(true);
  });
});

describe('plainText', () => {
  it('drops marks but keeps snake_case words and escapes', () => {
    expect(plainText('**жирный** _курсив_ snake_case_name \\*звёздочка')).toBe(
      'жирный курсив snake_case_name *звёздочка',
    );
  });
});
